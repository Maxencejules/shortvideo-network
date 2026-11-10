package provider

import "testing"

func TestScaffoldCleared(t *testing.T) {
	if Scaffold {
		t.Fatal("scaffold flag still set")
	}
}
