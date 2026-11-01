package app_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// councilModules is the module genesis JSON of doc, with the authority
// genesis edited through its proto when edit is not nil.
func councilModules(t *testing.T, doc genesis.Document, edit func(*authorityv1.GenesisState)) map[string]json.RawMessage {
	t.Helper()
	modules, err := app.AppStateFromDocument(codecFromApp(t, doc.ChainID), doc)
	if err != nil {
		t.Fatal(err)
	}
	if edit == nil {
		return modules
	}
	var state authorityv1.GenesisState
	if err := protojson.Unmarshal(modules[authority.ModuleName], &state); err != nil {
		t.Fatal(err)
	}
	edit(&state)
	raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&state)
	if err != nil {
		t.Fatal(err)
	}
	modules[authority.ModuleName] = raw
	return modules
}

// initChain runs InitChain on a new app and returns its error.
func initChain(t *testing.T, doc genesis.Document, modules map[string]json.RawMessage, initialHeight int64) (*app.App, error) {
	t.Helper()
	chainApp := newTestApp(t, doc.ChainID, nil)
	params := consensusParams()
	_, err := chainApp.InitChain(&abci.RequestInitChain{
		ChainId:         doc.ChainID,
		Time:            doc.Time(),
		AppStateBytes:   mustJSON(t, modules),
		InitialHeight:   initialHeight,
		Validators:      validatorUpdates(t, doc),
		ConsensusParams: &params,
	})
	return chainApp, err
}

func bootModules(t *testing.T, doc genesis.Document, modules map[string]json.RawMessage) *running {
	t.Helper()
	chainApp, err := initChain(t, doc, modules, 1)
	if err != nil {
		t.Fatal(err)
	}
	return &running{app: chainApp, params: consensusParams(), when: doc.Time()}
}

// pendingEntry is one genesis pending validator change. unix != 0 selects
// a time activation.
func pendingEntry(id, node string, pub []byte, power int64, height uint64, unix int64) *authorityv1.PendingValidatorPower {
	activation := &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: height}}
	if unix != 0 {
		activation = &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_UnixSeconds{UnixSeconds: unix}}
	}
	return &authorityv1.PendingValidatorPower{
		ProposalId:      id,
		ConsensusPubkey: pub,
		Power:           power,
		NodeId:          node,
		ReasonDigest:    &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: bytes32(7)},
		Activation:      activation,
	}
}

// TestFreshStateExportRoundTrip: a chain with no authority side state
// exports, imports, and exports again byte for byte, with the same app hash
// after the first block. A genesis pending entry is carried by the proto
// and round-trips too.
func TestFreshStateExportRoundTrip(t *testing.T) {
	doc, _, _ := councilDocument()
	cases := []struct {
		name    string
		pending []*authorityv1.PendingValidatorPower
	}{
		{name: "no pending entry"},
		{name: "genesis pending entry", pending: []*authorityv1.PendingValidatorPower{pendingEntry("gen-add-v5", "v5", bytes32(9), 1, 50, 0)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			modules := councilModules(t, doc, func(state *authorityv1.GenesisState) {
				state.Authority.PendingValidatorPower = tc.pending
			})
			first := bootModules(t, doc, modules)
			first.deliver(t, nil)
			exported, err := first.app.Export()
			if err != nil {
				t.Fatal(err)
			}
			var state authorityv1.GenesisState
			if err := protojson.Unmarshal(exported[authority.ModuleName], &state); err != nil {
				t.Fatal(err)
			}
			got := state.GetAuthority().GetPendingValidatorPower()
			if len(got) != len(tc.pending) {
				t.Fatalf("exported %d pending entries, want %d", len(got), len(tc.pending))
			}
			for i := range got {
				if !proto.Equal(got[i], tc.pending[i]) {
					t.Fatalf("pending entry %d changed on export", i)
				}
			}
			second := bootModules(t, doc, exported)
			second.deliver(t, nil)
			again, err := second.app.Export()
			if err != nil {
				t.Fatal(err)
			}
			if len(again) != len(exported) {
				t.Fatalf("re-export has %d modules, want %d", len(again), len(exported))
			}
			for name, raw := range exported {
				if !bytes.Equal(raw, again[name]) {
					t.Fatalf("module %s changed on re-export\n%s\n%s", name, raw, again[name])
				}
			}
			if !bytes.Equal(mustJSON(t, exported), mustJSON(t, again)) {
				t.Fatal("re-export is not byte-equal")
			}
			if !bytes.Equal(first.app.LastCommitID().Hash, second.app.LastCommitID().Hash) {
				t.Fatal("import/export app hash mismatch")
			}
			if !bytes.Equal(first.app.AuthoritySnapshotForTest(), second.app.AuthoritySnapshotForTest()) {
				t.Fatal("the imported authority store differs")
			}
		})
	}
}

// TestExportRefusesSideState pins DR-06-01 and DR-06-02. Export refuses
// while the authority store holds state the genesis proto cannot carry, and
// refuses a genesis the import would reject. Rejected messages leave no
// such state.
func TestExportRefusesSideState(t *testing.T) {
	c := newCouncil(t, nil)
	s1 := c.seat(0)
	auth := c.keys.authority
	if _, err := c.run.app.Export(); err != nil {
		t.Fatalf("fresh export: %v", err)
	}
	if res := c.send(t, s1, &authorityv1.MsgProposeChange{Signer: s1.PubKey().Address()}); res.Code != 7 {
		t.Fatalf("malformed propose code %d", res.Code)
	}
	valid := change{id: "add-v5", base: 1, node: "v5", pub: bytes32(9), power: 1, height: 1000}
	if res := c.send(t, auth, valid.msg(t, auth)); res.Code != 1 {
		t.Fatalf("authority-signed propose code %d", res.Code)
	}
	if _, err := c.run.app.Export(); err != nil {
		t.Fatalf("export after rejected messages only: %v", err)
	}
	if _, panicked := c.run.app.ExportAuthorityModuleForTest(); panicked != nil {
		t.Fatalf("module export panicked on a fresh store: %v", panicked)
	}

	c.ok(t, s1, valid.msg(t, s1))
	_, err := c.run.app.Export()
	if err == nil {
		t.Fatal("export dropped the proposal record")
	}
	if !strings.HasPrefix(err.Error(), "export refused: ") || !errors.Is(err, authority.ErrExportRefused) ||
		!strings.Contains(err.Error(), "proposal records (prefix 0x10)") {
		t.Fatalf("export error %v", err)
	}
	_, panicked := c.run.app.ExportAuthorityModuleForTest()
	perr, ok := panicked.(error)
	if !ok || !errors.Is(perr, authority.ErrExportRefused) || err.Error() != "export refused: "+perr.Error() {
		t.Fatalf("module export panic %v, export error %v", panicked, err)
	}

	t.Run("unequal powers", func(t *testing.T) {
		doc, _, _ := councilDocument()
		for i := range doc.Validators {
			doc.Validators[i].Power = 2
		}
		modules := councilModules(t, doc, func(state *authorityv1.GenesisState) {
			state.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{pendingEntry("v1-down", "v1", bytes32(1), 1, 3, 0)}
		})
		run := bootModules(t, doc, modules)
		for run.height < 3 {
			run.deliver(t, nil)
		}
		if len(run.last.ValidatorUpdates) != 1 {
			t.Fatalf("height 3 updates %+v", run.last.ValidatorUpdates)
		}
		_, err := run.app.Export()
		if err == nil || !strings.HasPrefix(err.Error(), "export refused: exported genesis does not validate") ||
			!strings.Contains(err.Error(), "validator powers are not equal") {
			t.Fatalf("export of unequal powers: %v", err)
		}
	})
}

// TestInitChainRejectsUnprojectableGenesis: InitChain projects genesis
// pending entries on the genesis set and checks them against the start
// point, as Execute would. Each bad genesis returns an error naming its
// code; a good entry boots and is applied at its height.
func TestInitChainRejectsUnprojectableGenesis(t *testing.T) {
	doc, _, _ := councilDocument()
	start := doc.GenesisUnixSeconds
	cases := []struct {
		name    string
		edit    func(*authorityv1.GenesisState)
		initial int64
		code    string
	}{
		{name: "removal that leaves three validators", code: authority.CodeForbidden, edit: func(s *authorityv1.GenesisState) {
			s.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{pendingEntry("rm-v4", "v4", bytes32(4), 0, 5, 0)}
		}},
		{name: "removal with the wrong key", code: authority.CodeConflict, edit: func(s *authorityv1.GenesisState) {
			s.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{pendingEntry("rm-v4", "v4", bytes32(99), 0, 5, 0)}
		}},
		{name: "power change that breaks the share cap", code: authority.CodeForbidden, edit: func(s *authorityv1.GenesisState) {
			s.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{pendingEntry("v1-up", "v1", bytes32(1), 3, 5, 0)}
		}},
		{name: "height and time entries mixed", code: authority.CodeConflict, edit: func(s *authorityv1.GenesisState) {
			s.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{
				pendingEntry("add-v5", "v5", bytes32(9), 1, 5, 0),
				pendingEntry("add-v6", "v6", bytes32(10), 1, 0, start+100),
			}
		}},
		{name: "genesis proposal", code: authority.CodeMalformed, edit: func(s *authorityv1.GenesisState) {
			s.Authority.Proposals = []*authorityv1.Proposal{{ProposalId: "p-1"}}
		}},
		{name: "approved council", code: authority.CodeForbidden, edit: func(s *authorityv1.GenesisState) {
			s.Authority.ApprovedCouncil = []*authorityv1.DevnetTestSeat{{SeatId: "seat-x", PublicKey: bytes32(77)}}
		}},
		{name: "height before the initial height", code: authority.CodeOutOfRange, initial: 10, edit: func(s *authorityv1.GenesisState) {
			s.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{pendingEntry("add-v5", "v5", bytes32(9), 1, 5, 0)}
		}},
		{name: "time not after genesis time", code: authority.CodeOutOfRange, edit: func(s *authorityv1.GenesisState) {
			s.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{pendingEntry("add-v5", "v5", bytes32(9), 1, 0, start)}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			initial := tc.initial
			if initial == 0 {
				initial = 1
			}
			_, err := initChain(t, doc, councilModules(t, doc, tc.edit), initial)
			if err == nil {
				t.Fatal("InitChain accepted the genesis")
			}
			if !strings.Contains(err.Error(), "authority genesis: "+tc.code) {
				t.Fatalf("InitChain error %v does not name %s", err, tc.code)
			}
		})
	}
	t.Run("valid entry is applied at its height", func(t *testing.T) {
		run := bootModules(t, doc, councilModules(t, doc, func(s *authorityv1.GenesisState) {
			s.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{pendingEntry("add-v5", "v5", bytes32(9), 1, 3, 0)}
		}))
		for run.height < 3 {
			run.deliver(t, nil)
			if run.height < 3 && len(run.last.ValidatorUpdates) != 0 {
				t.Fatalf("updates at height %d", run.height)
			}
		}
		updates := run.last.ValidatorUpdates
		if len(updates) != 1 || updates[0].Power != 1 || !bytes.Equal(updates[0].PubKey.GetEd25519(), bytes32(9)) {
			t.Fatalf("height 3 updates %+v", updates)
		}
	})
}
