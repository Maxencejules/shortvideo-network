package config

// PostgreSQL integration test: a real disposable PostgreSQL 18 cluster
// started by internal/pgtest (behind the machine-wide test lock). It has
// no skip path: a missing PostgreSQL binary fails it.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/pgdev"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/pgtest"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// TestExplicitPQConfigReadsNoEnv checks plan 7.7 and section 11: the
// connection built from the configuration (ConnConfig, then store.OpenDB:
// pq.Config field by field, pq.NewConnectorConfig) reaches the configured
// database as the configured role while every PG* variable lib/pq reads
// points somewhere else, and the start guard refuses that environment.
func TestExplicitPQConfigReadsNoEnv(t *testing.T) {
	ctx := context.Background()
	srv, err := pgtest.Start(ctx)
	if err != nil {
		t.Fatalf("start PostgreSQL: %v", err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Errorf("remove cluster: %v", err)
		}
	})
	t.Logf("server version: %s", srv.Cluster.Version())
	password, err := srv.Cluster.CreateLoginRole(ctx, "cfg_reader")
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.NewDatabase(ctx, "cfg_db", "cfg_reader"); err != nil {
		t.Fatal(err)
	}
	pwFile := filepath.Join(t.TempDir(), "db-password")
	if err := os.WriteFile(pwFile, []byte(password+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := serveFile()
	m["database"] = map[string]any{
		"socket_dir": srv.Cluster.SocketDir(), "port": pgdev.Port, "name": "cfg_db", "role": "cfg_reader",
		"password_file": pwFile, "application_name": "cfg-explicit", "connect_timeout_seconds": 10,
	}
	c := mustParse(t, m)

	// Every variable points at another host, port, user, database,
	// password, password file, service, TLS mode and session option.
	envDir := t.TempDir()
	for name, value := range map[string]string{
		"PGHOST": envDir, "PGHOSTADDR": "192.0.2.1", "PGPORT": "1", "PGUSER": "env_user", "PGDATABASE": "env_db",
		"PGPASSWORD": "env-password", "PGPASSFILE": filepath.Join(envDir, "pgpass"), "PGSERVICE": "env_service",
		"PGSERVICEFILE": filepath.Join(envDir, "pg_service.conf"), "PGSSLMODE": "verify-full",
		"PGOPTIONS": "-c statement_timeout=1", "PGAPPNAME": "env-app", "PGCONNECT_TIMEOUT": "1",
		"PGTARGETSESSIONATTRS": "read-only", "PGCLIENTENCODING": "LATIN1", "PGTZ": "Pacific/Chatham",
	} {
		t.Setenv(name, value)
	}
	if err := CheckEnvironment(os.Environ()); !errors.Is(err, ErrEnvironment) {
		t.Fatalf("the start guard accepted the environment: %v", err)
	}

	cc, err := c.ConnConfig()
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.OpenDB(cc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("the configured connection failed under the PG* environment: %v", err)
	}
	var user, database, app, timeout, encoding, tz string
	if err := db.QueryRowContext(ctx, `SELECT current_user, current_database(), current_setting('application_name'),
		current_setting('statement_timeout'), current_setting('client_encoding'), current_setting('TimeZone')`).Scan(
		&user, &database, &app, &timeout, &encoding, &tz); err != nil {
		t.Fatal(err)
	}
	if user != "cfg_reader" || database != "cfg_db" || app != "cfg-explicit" || timeout != "0" || encoding != "UTF8" ||
		tz == "Pacific/Chatham" {
		t.Fatalf("the session took a value from the environment: user %q db %q app %q statement_timeout %q encoding %q tz %q",
			user, database, app, timeout, encoding, tz)
	}

	// A wrong password in the file is refused by the server: the password
	// comes from the file, not from PGPASSWORD or a password file.
	if err := os.WriteFile(pwFile, []byte("not-the-password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bad, err := c.ConnConfig()
	if err != nil {
		t.Fatal(err)
	}
	badDB, err := store.OpenDB(bad)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := badDB.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	if err := badDB.PingContext(ctx); err == nil {
		t.Fatal("a wrong password connected")
	}
}
