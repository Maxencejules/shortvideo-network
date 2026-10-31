package app

import (
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
)

func TestAuthorityStateReportsHexErrors(t *testing.T) {
	doc := genesis.Document{
		Validators: []genesis.Validator{{
			NodeID:             "v1",
			Power:              1,
			ConsensusPubkeyHex: "zz",
		}},
	}
	if _, err := authorityState(doc); err == nil {
		t.Fatal("validator hex error was dropped")
	}
	doc.Validators[0].ConsensusPubkeyHex = "11"
	doc.Submitters = []genesis.Submitter{{AddressHex: "zz", Role: genesis.RoleAuthority}}
	if _, err := authorityState(doc); err == nil {
		t.Fatal("submitter hex error was dropped")
	}
}

func TestDevnetParametersAreSorted(t *testing.T) {
	doc := genesis.Document{
		DevnetTestParameters: []genesis.DevnetTestParameter{
			{DecisionID: "DEC-008", Label: "b", RoleName: "a", NumericValue: 2},
			{DecisionID: "DEC-001", Label: "a", RoleName: "b", NumericValue: 9},
			{DecisionID: "DEC-008", Label: "a", RoleName: "b", NumericValue: 1},
		},
	}
	state, err := authorityState(doc)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, row := range state.GetDevnetTestParameters() {
		ids = append(ids, row.GetDecisionId()+"/"+row.GetLabel())
	}
	got := strings.Join(ids, ",")
	want := "DEC-001/a,DEC-008/a,DEC-008/b"
	if got != want {
		t.Fatalf("sorted %s", got)
	}
}
