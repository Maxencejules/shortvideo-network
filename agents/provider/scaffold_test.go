package provider

import "testing"

func TestScaffoldIsNotAnAgent(t *testing.T) {
	if !Scaffold {
		t.Fatal("scaffold flag cleared")
	}
}
