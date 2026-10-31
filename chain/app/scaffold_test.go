package app

import "testing"

func TestModuleSlotsStayScaffolding(t *testing.T) {
	if !Scaffold {
		t.Fatal("module slots are marked complete before goals 06-10")
	}
}
