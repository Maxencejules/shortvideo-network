package authority

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// One row per rule of ValidateGenesisState, with the expected code text.
// Fails on the old code, whose ValidateGenesis ran only InitialUpdates and
// accepted every pending, proposal, council and projection row below.
func TestValidateGenesisState(t *testing.T) {
	start := &GenesisPoint{InitialHeight: 1, GenesisUnixSeconds: testTime}
	rows := []struct {
		name string
		edit func(*authorityv1.GenesisState)
		at   *GenesisPoint
		code string // empty: accepted
	}{
		{"ready genesis", func(*authorityv1.GenesisState) {}, start, ""},
		{"one future add", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{heightEntry("g1", "v5", pub(9), 1, 50)}
		}, start, ""},
		{"missing authority state", func(g *authorityv1.GenesisState) { g.Authority = nil }, nil, CodeMalformed},
		{"genesis proposal", func(g *authorityv1.GenesisState) {
			g.Authority.Proposals = []*authorityv1.Proposal{{ProposalId: "p1"}}
		}, nil, CodeMalformed},
		{"approved council", func(g *authorityv1.GenesisState) {
			g.Authority.ApprovedCouncil = []*authorityv1.DevnetTestSeat{{SeatId: "s1", PublicKey: pub(50)}}
		}, nil, CodeForbidden},
		{"quorum loss not UNSET", func(g *authorityv1.GenesisState) {
			g.Authority.QuorumLoss = authorityv1.QuorumLossBehavior_QUORUM_LOSS_BEHAVIOR_UNSPECIFIED
		}, nil, CodeForbidden},
		{"three seats", func(g *authorityv1.GenesisState) { g.Authority.DevnetTestSeats = g.Authority.DevnetTestSeats[:3] }, nil, CodeBounds},
		{"duplicate seat id", func(g *authorityv1.GenesisState) { g.Authority.DevnetTestSeats[1].SeatId = "seat-1" }, nil, CodeDuplicate},
		{"duplicate seat key", func(g *authorityv1.GenesisState) { g.Authority.DevnetTestSeats[1].PublicKey = pub(30) }, nil, CodeDuplicate},
		{"short submitter", func(g *authorityv1.GenesisState) { g.Authority.Submitters[0].Address = bytes20(1)[:19] }, nil, CodeBounds},
		{"submitter without role", func(g *authorityv1.GenesisState) {
			g.Authority.Submitters[1].Role = typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED
		}, nil, CodeMalformed},
		{"duplicate submitter", func(g *authorityv1.GenesisState) { g.Authority.Submitters[1].Address = bytes20(1) }, nil, CodeDuplicate},
		{"three validators", func(g *authorityv1.GenesisState) { g.Authority.Validators = g.Authority.Validators[:3] }, nil, CodeForbidden},
		{"bad node id", func(g *authorityv1.GenesisState) { g.Authority.Validators[0].NodeId = "V1!" }, nil, CodeBounds},
		{"duplicate node id", func(g *authorityv1.GenesisState) { g.Authority.Validators[1].NodeId = "v1" }, nil, CodeDuplicate},
		{"duplicate validator key", func(g *authorityv1.GenesisState) { g.Authority.Validators[1].ConsensusPubkey = pub(1) }, nil, CodeDuplicate},
		{"zero power", func(g *authorityv1.GenesisState) { g.Authority.Validators[0].Power = 0 }, nil, CodeForbidden},
		{"share cap", func(g *authorityv1.GenesisState) { g.Authority.Validators[0].Power = 2 }, nil, CodeForbidden},
		{"total above safe max", func(g *authorityv1.GenesisState) {
			for _, row := range g.Authority.Validators {
				row.Power = int64(1) << 51
			}
		}, nil, CodeForbidden},
		{"129 pending entries", func(g *authorityv1.GenesisState) {
			for i := 0; i <= MaxPendingChanges; i++ {
				g.Authority.PendingValidatorPower = append(g.Authority.PendingValidatorPower,
					heightEntry("g"+strconv.Itoa(i), "v1", pub(1), 1, uint64(50+i)))
			}
		}, nil, CodeBounds},
		{"short pending key", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{heightEntry("g1", "v5", pub(9)[:31], 1, 50)}
		}, nil, CodeBounds},
		{"pending without activation", func(g *authorityv1.GenesisState) {
			entry := heightEntry("g1", "v5", pub(9), 1, 50)
			entry.Activation = nil
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{entry}
		}, nil, CodeMalformed},
		{"pending reason not sha256", func(g *authorityv1.GenesisState) {
			entry := heightEntry("g1", "v5", pub(9), 1, 50)
			entry.ReasonDigest.Algorithm = typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{entry}
		}, nil, CodeMalformed},
		{"pending height 0", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{heightEntry("g1", "v5", pub(9), 1, 0)}
		}, nil, CodeBounds},
		{"pending time past unix max", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{timeEntry("g1", "v5", pub(9), 1, unixMax+1)}
		}, nil, CodeBounds},
		{"duplicate pending id", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{
				heightEntry("g1", "v5", pub(9), 1, 50), heightEntry("g1", "v6", pub(10), 1, 60),
			}
		}, nil, CodeDuplicate},
		{"removal leaves three", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{heightEntry("g1", "v1", pub(1), 0, 50)}
		}, nil, CodeForbidden},
		{"removal with the wrong key", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{
				heightEntry("g0", "v5", pub(9), 1, 40), heightEntry("g1", "v1", pub(8), 0, 50),
			}
		}, nil, CodeConflict},
		// Spec projection binds a removal before applying it, so an unknown
		// node is CONFLICT there too, not NOT_FOUND.
		{"removal of an unknown node", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{
				heightEntry("g0", "v5", pub(9), 1, 40), heightEntry("g1", "v9", pub(8), 0, 50),
			}
		}, nil, CodeConflict},
		{"power change breaks the share cap", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{heightEntry("g1", "v1", pub(1), 2, 50)}
		}, nil, CodeForbidden},
		{"add with another node's key", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{heightEntry("g1", "v5", pub(2), 1, 50)}
		}, nil, CodeDuplicate},
		{"mixed height and time", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{
				heightEntry("g1", "v5", pub(9), 1, 50), timeEntry("g2", "v6", pub(10), 1, testTime+100),
			}
		}, nil, CodeConflict},
		{"height before the first block", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{heightEntry("g1", "v5", pub(9), 1, 4)}
		}, &GenesisPoint{InitialHeight: 5, GenesisUnixSeconds: testTime}, CodeOutOfRange},
		{"time at genesis time", func(g *authorityv1.GenesisState) {
			g.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{timeEntry("g1", "v5", pub(9), 1, testTime)}
		}, start, CodeOutOfRange},
		{"initial height 0", func(*authorityv1.GenesisState) {}, &GenesisPoint{InitialHeight: 0, GenesisUnixSeconds: testTime}, CodeMalformed},
	}
	for _, row := range rows {
		gen, _, _ := readyGenesis()
		row.edit(gen)
		err := ValidateGenesisState(gen, row.at)
		if row.code == "" {
			if err != nil {
				t.Fatalf("%s: %v", row.name, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), row.code) {
			t.Fatalf("%s: got %v, want %s", row.name, err, row.code)
		}
		if row.at == nil {
			// The same state never reaches the store.
			if err := InitStore(newMemKV(), gen); err == nil {
				t.Fatalf("%s: InitStore accepted it", row.name)
			}
		}
	}
	// A past activation is accepted without a start point and refused with one.
	gen, _, _ := readyGenesis()
	gen.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{heightEntry("g1", "v5", pub(9), 1, 4)}
	if err := ValidateGenesisState(gen, nil); err != nil {
		t.Fatal(err)
	}
}

// InitStore writes the config key with the same bytes as the genesis state
// when it has no pending entries, so the app pin holds; pending entries go to
// 0x03 and admission rows to 0x02. A second InitStore is refused. Fails on
// the old code, which kept pending entries in the state key.
func TestInitStoreLayout(t *testing.T) {
	gen, _, _ := readyGenesis()
	kv := storeFrom(t, gen)
	want, err := proto.MarshalOptions{Deterministic: true}.Marshal(gen)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(kv.Get([]byte{configKey}), want) {
		t.Fatal("config key is not the genesis encoding")
	}
	if countPrefix(kv, admitPrefix) != 2 || kv.Get(AdmissionKey(bytes20(1)))[0] != byte(typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY) {
		t.Fatal("admission rows")
	}
	if err := InitStore(kv, gen); err == nil {
		t.Fatal("InitStore wrote over a used store")
	}

	withPending, _, _ := readyGenesis()
	entry := heightEntry("g1", "v5", pub(9), 1, 50)
	withPending.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{entry}
	kv2 := storeFrom(t, withPending)
	if !bytes.Equal(kv2.Get([]byte{configKey}), want) {
		t.Fatal("pending entries leaked into the config key")
	}
	if got := pendingOf(t, kv2); len(got) != 1 || !proto.Equal(got[0], entry) {
		t.Fatalf("pending %v", got)
	}
}

// Each side prefix makes ExportState refuse and name it; a store with only
// the config, admission and pending keys exports and round-trips. Fails on
// the old code, which exported the state key and dropped everything else.
func TestExportStateRefusesEachPrefix(t *testing.T) {
	for _, prefix := range []byte{policyKey, 0x05, proposalPrefix, businessPrefix, executedPrefix, governedPrefix, openPrefix,
		retiredCouncilPrefix, retiredServicePrefix, providerPrefix, legacySideKey} {
		kv, _, _ := readyStore(t)
		kv.Set([]byte{prefix, 'x'}, []byte{1})
		_, err := ExportState(kv)
		if !errors.Is(err, ErrExportRefused) || !strings.Contains(err.Error(), fmt.Sprintf("prefix 0x%02x", prefix)) {
			t.Fatalf("prefix 0x%02x: %v", prefix, err)
		}
	}

	gen, _, _ := readyGenesis()
	gen.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{
		heightEntry("g1", "v5", pub(9), 1, 50), heightEntry("g2", "v6", pub(10), 1, 60),
	}
	kv := storeFrom(t, gen)
	exported, err := ExportState(kv)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(exported, gen) {
		t.Fatal("export differs from the genesis it came from")
	}
	again := storeFrom(t, exported)
	if !bytes.Equal(dumpKV(again), dumpKV(kv)) {
		t.Fatal("export then import is not byte-identical")
	}

	kv.Delete(AdmissionKey(bytes20(2)))
	if _, err := ExportState(kv); err == nil || errors.Is(err, ErrExportRefused) {
		t.Fatalf("missing admission row: %v", err)
	}
	if _, err := ExportState(newMemKV()); !errors.Is(err, errMissing) {
		t.Fatalf("empty store: %v", err)
	}
}

// ExportState requires one admission row per submitter, at the submitter's
// address and with its role. A role byte that differs, a row moved to
// another address (same count), an extra row, or a missing row is an export
// error, not ErrExportRefused, and the control store exports. No message
// writes such a store. The second fix-round review found that reducing the
// role comparison to a presence check passed every suite.
func TestExportStateChecksAdmissionRows(t *testing.T) {
	broadcaster := []byte{byte(typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER)}
	service := []byte{byte(typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE)}
	for _, row := range []struct {
		name string
		want string
		edit func(KV)
	}{
		{"control", "", func(KV) {}},
		{"role byte differs", "does not match submitter", func(kv KV) { kv.Set(AdmissionKey(bytes20(2)), broadcaster) }},
		{"row at another address", "does not match submitter", func(kv KV) {
			kv.Delete(AdmissionKey(bytes20(2)))
			kv.Set(AdmissionKey(bytes20(9)), service)
		}},
		{"extra row", "3 admission rows for 2 submitters", func(kv KV) { kv.Set(AdmissionKey(bytes20(9)), service) }},
		{"missing row", "1 admission rows for 2 submitters", func(kv KV) { kv.Delete(AdmissionKey(bytes20(2))) }},
	} {
		kv, _, _ := readyStore(t)
		row.edit(kv)
		_, err := ExportState(kv)
		switch {
		case row.want == "" && err != nil:
			t.Fatalf("%s: %v", row.name, err)
		case row.want != "" && (err == nil || errors.Is(err, ErrExportRefused) || !strings.Contains(err.Error(), row.want)):
			t.Fatalf("%s: %v", row.name, err)
		}
	}
}
