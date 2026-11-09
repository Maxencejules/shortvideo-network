package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAndEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "indexer.json")
	body := []byte(`{
	  "chain_id": "shortvideo-dev-cfg",
	  "rpc_url": "http://127.0.0.1:26657",
	  "listen": "127.0.0.1:9",
	  "database": {
	    "socket_dir": "/tmp/sock",
	    "port": 5432,
	    "name": "indexer",
	    "owner_role": "svn_indexer_owner",
	    "owner_password_file": "/tmp/o",
	    "role": "svn_indexer",
	    "password_file": "/tmp/r",
	    "application_name": "svn-indexer",
	    "connect_timeout_seconds": 5
	  }
	}`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Numbers.StaleAfterSeconds != 8 {
		t.Fatalf("default stale %d", f.Numbers.StaleAfterSeconds)
	}
	bad := append([]byte{}, body...)
	bad = append(bad, []byte(`{"token":1}`)...)
	if err := os.WriteFile(path, []byte(`{"chain_id":"mainnet","rpc_url":"http://127.0.0.1:1","database":{"socket_dir":"/tmp","port":1,"name":"indexer","owner_role":"a","owner_password_file":"/o","role":"b","password_file":"/p"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("a chain id without the development prefix was accepted")
	}
	if err := CheckEnvironment([]string{"PGPASSWORD=x", "PATH=/bin"}); err == nil {
		t.Fatal("PGPASSWORD was accepted")
	}
	_ = bad
}
