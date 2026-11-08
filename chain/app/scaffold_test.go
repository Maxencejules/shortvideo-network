package app

import (
	"testing"

	dbm "github.com/cosmos/cosmos-db"

	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	"github.com/Maxencejules/shortvideo-network/chain/x/scaffold"
)

// TestModuleSlotsStayScaffolding: the svn header slot is still a scaffold
// module (DR-10-28), so Scaffold stays true; x/policy replaced the scaffold
// in its slot in Goal 10.
func TestModuleSlotsStayScaffolding(t *testing.T) {
	if !Scaffold {
		t.Fatal("the svn header slot is marked complete, but it is still a scaffold module (DR-10-28)")
	}
	built, err := New(Config{DB: dbm.NewMemDB(), ChainID: "shortvideo-dev-slots", SkipLoad: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := built.mm.Modules[ModuleSVN].(scaffold.Module); !ok {
		t.Fatalf("svn slot runs %T, want the scaffold module", built.mm.Modules[ModuleSVN])
	}
	if _, ok := built.mm.Modules[ModulePolicy].(policy.AppModule); !ok {
		t.Fatalf("policy slot runs %T, want x/policy", built.mm.Modules[ModulePolicy])
	}
}
