package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRehearseCommand(t *testing.T) {
	if _, err := os.Stat("/usr/lib/postgresql/18/bin/initdb"); err != nil {
		t.Skip("PostgreSQL 18 is not installed")
	}
	work := t.TempDir()
	if code := run([]string{"rehearse", "--work", work}); code != 0 {
		t.Fatalf("rehearse exit %d", code)
	}
}

func TestRotateCommandRefusesWithoutApproval(t *testing.T) {
	out := filepath.Join(t.TempDir(), "public")
	if code := run([]string{"rotate-service", "--public-out", out}); code == 0 {
		t.Fatal("rotation ran without approval")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("public file was written")
	}
	if code := run([]string{"rotate-service", "--drill", "--public-out", out}); code != 0 {
		t.Fatalf("drill rotation exit %d", code)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != 32 {
		t.Fatalf("public file is %d bytes", info.Size())
	}
}
