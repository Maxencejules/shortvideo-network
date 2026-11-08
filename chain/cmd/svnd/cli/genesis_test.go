package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cmttypes "github.com/cometbft/cometbft/types"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// editAuthority returns raw (a CometBFT genesis) with its authority module
// genesis decoded, edited, and encoded again. Only the app_state.authority
// value changes.
func editAuthority(t *testing.T, raw []byte, edit func(*authorityv1.GenesisState)) []byte {
	t.Helper()
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var modules map[string]json.RawMessage
	if err := json.Unmarshal(doc["app_state"], &modules); err != nil {
		t.Fatal(err)
	}
	var state authorityv1.GenesisState
	if err := protojson.Unmarshal(modules[authority.ModuleName], &state); err != nil {
		t.Fatal(err)
	}
	edit(&state)
	encoded, err := protojson.Marshal(&state)
	if err != nil {
		t.Fatal(err)
	}
	modules[authority.ModuleName] = encoded
	if doc["app_state"], err = json.Marshal(modules); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestValidateGenesisAppliesAuthorityRules: `svnd genesis validate` refuses
// every authority genesis that InitChain refuses, with the same rule. The
// genesis is a fresh `devnet init` document; each row edits only its
// authority module state. Fails on the round-3 review tree, where the
// command ran only DocumentFromModules and genesis.Validate, which ignore
// pending entries, proposals, the approved council, and quorum loss, and
// returned nil for every row below.
func TestValidateGenesisAppliesAuthorityRules(t *testing.T) {
	home := t.TempDir()
	if _, err := devnet.Init(devnet.Options{Home: home}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "genesis.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGenesisFile(raw); err != nil {
		t.Fatalf("fresh devnet genesis: %v", err)
	}
	if unchanged := editAuthority(t, raw, func(*authorityv1.GenesisState) {}); validateGenesisFile(unchanged) != nil {
		t.Fatal("re-encoding the authority genesis changed its validity")
	}
	reason := &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: bytes.Repeat([]byte{7}, 32)}
	atHeight := func(h uint64) *authorityv1.ActivationPoint {
		return &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: h}}
	}
	first := func(state *authorityv1.GenesisState) *authorityv1.ValidatorPower {
		return state.GetAuthority().GetValidators()[0]
	}
	genDoc, err := cmttypes.GenesisDocFromJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	genesisUnix := genDoc.GenesisTime.Unix()
	rows := []struct {
		name string
		code string
		edit func(*authorityv1.GenesisState)
	}{
		{"approved council", authority.CodeForbidden, func(state *authorityv1.GenesisState) {
			state.Authority.ApprovedCouncil = []*authorityv1.DevnetTestSeat{{SeatId: "seat-x", PublicKey: bytes.Repeat([]byte{1}, 32)}}
		}},
		{"quorum loss set", authority.CodeForbidden, func(state *authorityv1.GenesisState) {
			state.Authority.QuorumLoss = authorityv1.QuorumLossBehavior_QUORUM_LOSS_BEHAVIOR_UNSPECIFIED
		}},
		{"genesis proposal", authority.CodeMalformed, func(state *authorityv1.GenesisState) {
			state.Authority.Proposals = []*authorityv1.Proposal{{ProposalId: "p-1"}}
		}},
		{"pending removal that leaves three validators", authority.CodeForbidden, func(state *authorityv1.GenesisState) {
			v := first(state)
			state.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{{
				ProposalId: "rm-1", ConsensusPubkey: v.GetConsensusPubkey(), Power: 0, NodeId: v.GetNodeId(),
				ReasonDigest: reason, Activation: atHeight(5),
			}}
		}},
		{"pending removal bound to the wrong key", authority.CodeConflict, func(state *authorityv1.GenesisState) {
			v := first(state)
			state.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{{
				ProposalId: "rm-1", ConsensusPubkey: bytes.Repeat([]byte{1}, 32), Power: 0, NodeId: v.GetNodeId(),
				ReasonDigest: reason, Activation: atHeight(5),
			}}
		}},
		{"pending change at genesis time", authority.CodeOutOfRange, func(state *authorityv1.GenesisState) {
			state.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{{
				ProposalId: "add-1", ConsensusPubkey: bytes.Repeat([]byte{2}, 32), Power: 1, NodeId: "v9",
				ReasonDigest: reason,
				Activation:   &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_UnixSeconds{UnixSeconds: genesisUnix}},
			}}
		}},
	}
	for _, row := range rows {
		err := validateGenesisFile(editAuthority(t, raw, row.edit))
		if err == nil || !strings.Contains(err.Error(), row.code) {
			t.Errorf("%s: %v, want an error naming %s", row.name, err, row.code)
		}
	}
	// A well-formed pending add in the future is accepted.
	future := editAuthority(t, raw, func(state *authorityv1.GenesisState) {
		state.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{{
			ProposalId: "add-1", ConsensusPubkey: bytes.Repeat([]byte{2}, 32), Power: 1, NodeId: "v9",
			ReasonDigest: reason, Activation: atHeight(50),
		}}
	})
	if err := validateGenesisFile(future); err != nil {
		t.Fatalf("future pending add: %v", err)
	}
}

// editConsensusParams returns raw (a CometBFT genesis) with its
// consensus_params object passed to edit, or with consensus_params removed
// when edit is nil. Nothing else changes.
func editConsensusParams(t *testing.T, raw []byte, edit func(map[string]json.RawMessage)) []byte {
	t.Helper()
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if edit == nil {
		delete(doc, "consensus_params")
	} else {
		var params map[string]json.RawMessage
		if err := json.Unmarshal(doc["consensus_params"], &params); err != nil {
			t.Fatal(err)
		}
		edit(params)
		encoded, err := json.Marshal(params)
		if err != nil {
			t.Fatal(err)
		}
		doc["consensus_params"] = encoded
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// appVersionRows are genesis documents whose consensus Version.App is not
// app.GenesisAppVersion: 0, 2, no version params (CometBFT decodes Version
// as its zero value, App 0), and no consensus params (CometBFT fills in its
// defaults, App 0).
func appVersionRows(t *testing.T, raw []byte) []struct {
	name string
	doc  []byte
} {
	t.Helper()
	setApp := func(value string) func(map[string]json.RawMessage) {
		return func(params map[string]json.RawMessage) {
			params["version"] = json.RawMessage(`{"app":"` + value + `"}`)
		}
	}
	return []struct {
		name string
		doc  []byte
	}{
		{"Version.App 0", editConsensusParams(t, raw, setApp("0"))},
		{"Version.App 2", editConsensusParams(t, raw, setApp("2"))},
		{"no version params", editConsensusParams(t, raw, func(params map[string]json.RawMessage) { delete(params, "version") })},
		{"no consensus params", editConsensusParams(t, raw, nil)},
	}
}

// genesisAppVersionRefusal is the error InitChain returns for a genesis
// whose consensus Version.App is not app.GenesisAppVersion.
const genesisAppVersionRefusal = "genesis consensus params Version.App must be 1"

// TestValidateGenesisChecksTheConsensusAppVersion: `svnd genesis validate`
// refuses a genesis whose consensus Version.App is not
// app.GenesisAppVersion, with the error InitChain returns for it, and
// accepts the fresh devnet genesis (Version.App 1) and the same document
// with Version.App written as 1 again. Fails on main dbf0897, where the
// command checked no consensus param and returned nil for every row below
// (PR 13 review finding discussion_r4505012913).
func TestValidateGenesisChecksTheConsensusAppVersion(t *testing.T) {
	home := t.TempDir()
	if _, err := devnet.Init(devnet.Options{Home: home}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "genesis.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := validateGenesisFile(raw); err != nil {
		t.Fatalf("fresh devnet genesis: %v", err)
	}
	same := editConsensusParams(t, raw, func(params map[string]json.RawMessage) {
		params["version"] = json.RawMessage(`{"app":"1"}`)
	})
	if err := validateGenesisFile(same); err != nil {
		t.Fatalf("Version.App 1 written again: %v", err)
	}
	for _, row := range appVersionRows(t, raw) {
		if _, err := cmttypes.GenesisDocFromJSON(row.doc); err != nil {
			t.Fatalf("%s: CometBFT refuses the document itself (%v); the row does not test the app version", row.name, err)
		}
		if err := validateGenesisFile(row.doc); err == nil || !strings.Contains(err.Error(), genesisAppVersionRefusal) {
			t.Errorf("%s: %v, want %q", row.name, err, genesisAppVersionRefusal)
		}
	}
}

// TestStartRefusesWhatValidateRefusesForTheAppVersion: for each genesis
// row of appVersionRows, `start` of the default build on a new devnet home
// holding that genesis is refused by the CometBFT handshake's InitChain
// with the same error `genesis validate` returns, before the node starts
// (no port is bound). It shows that the validate rule is InitChain's rule
// on the document the node actually reads. A start that does not return
// within startRefusalWait has started the node: the test fails then, and
// that node runs until the test binary exits.
func TestStartRefusesWhatValidateRefusesForTheAppVersion(t *testing.T) {
	for i, name := range []string{"Version.App 0", "Version.App 2", "no version params", "no consensus params"} {
		t.Run(name, func(t *testing.T) {
			layout, err := devnet.Init(devnet.Options{Home: t.TempDir(), ChainID: "shortvideo-dev-cli-appversion"})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(layout.Topology.Validators[0].Home, "config", "genesis.json")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			row := appVersionRows(t, raw)[i]
			if row.name != name {
				t.Fatalf("row %d is %q, want %q", i, row.name, name)
			}
			if err := validateGenesisFile(row.doc); err == nil || !strings.Contains(err.Error(), genesisAppVersionRefusal) {
				t.Fatalf("validate: %v, want %q", err, genesisAppVersionRefusal)
			}
			if err := os.WriteFile(path, row.doc, 0o600); err != nil {
				t.Fatal(err)
			}
			root := Root(Options{})
			root.SetArgs([]string{"start", "--home", layout.Topology.Validators[0].Home})
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			done := make(chan error, 1)
			go func() { done <- root.Execute() }()
			select {
			case err = <-done:
			case <-time.After(startRefusalWait):
				t.Fatalf("start did not return within %s: it started the node on a genesis with %s", startRefusalWait, name)
			}
			if err == nil || !strings.Contains(err.Error(), genesisAppVersionRefusal) {
				t.Fatalf("start: %v, want InitChain's %q", err, genesisAppVersionRefusal)
			}
		})
	}
}
