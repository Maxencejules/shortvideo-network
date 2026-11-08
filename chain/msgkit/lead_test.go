package msgkit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
)

// TestActivationLeadConstants pins the two PROPOSED lead numbers of the Q3
// (a) option text and the relations the authority and policy checks rely
// on: the minimum is at least one block (a plan is always strictly in the
// future), the minimum is below the maximum (the window is not empty), and
// a height plus the maximum lead cannot leave the safe integer range for
// any height at or below safe_integer_max minus the lead.
func TestActivationLeadConstants(t *testing.T) {
	if MinActivationLeadBlocks != 100 {
		t.Errorf("MinActivationLeadBlocks = %d, want the PROPOSED 100", MinActivationLeadBlocks)
	}
	if MaxActivationLeadBlocks != 1_000_000 {
		t.Errorf("MaxActivationLeadBlocks = %d, want the PROPOSED 1,000,000", MaxActivationLeadBlocks)
	}
	if MinActivationLeadBlocks < 1 || MinActivationLeadBlocks >= MaxActivationLeadBlocks {
		t.Errorf("lead window [%d, %d] is empty or not strictly in the future", MinActivationLeadBlocks, MaxActivationLeadBlocks)
	}
	if MaxActivationLeadBlocks >= uint64(genesis.SafeIntegerMax) {
		t.Errorf("MaxActivationLeadBlocks %d is not below safe_integer_max", MaxActivationLeadBlocks)
	}
}

// TestActivationLeadConstantsLabelledProposed fails when the doc comment of
// either lead constant in lead.go stops saying PROPOSED or stops naming the
// decision request, so the source never presents the numbers as approved.
func TestActivationLeadConstantsLabelledProposed(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "lead.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			value := spec.(*ast.ValueSpec)
			for _, name := range value.Names {
				found[name.Name] = value.Doc.Text()
			}
		}
	}
	for _, name := range []string{"MinActivationLeadBlocks", "MaxActivationLeadBlocks"} {
		doc, ok := found[name]
		if !ok {
			t.Errorf("%s is not declared in lead.go", name)
			continue
		}
		for _, want := range []string{"PROPOSED", "Q3 (a)", "DR-10-03"} {
			if !strings.Contains(doc, want) {
				t.Errorf("%s doc comment does not say %q: %q", name, want, doc)
			}
		}
	}
}
