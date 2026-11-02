package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
