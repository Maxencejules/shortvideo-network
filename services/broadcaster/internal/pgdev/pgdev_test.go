package pgdev_test

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/pgdev"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/pgtest"
)

// helperStartAndHang is the helper of TestPostgresDiesWithParent.
const helperStartAndHang = "start-and-hang"

func TestMain(m *testing.M) {
	switch pgtest.Helper() {
	case "":
		os.Exit(m.Run())
	case helperStartAndHang:
		os.Exit(startAndHang())
	default:
		fmt.Fprintln(os.Stderr, "unknown helper", pgtest.Helper())
		os.Exit(2)
	}
}

// startAndHang starts a cluster, prints "READY <postmaster pid> <dir>" and
// blocks until it is killed.
func startAndHang() int {
	srv, err := pgtest.Start(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper start:", err)
		return 1
	}
	pid, _, _ := srv.Cluster.Postmaster()
	fmt.Printf("READY %d %s\n", pid, srv.Cluster.Dir())
	select {}
}

// connect opens a pool to the cluster as user with password.
func connect(t *testing.T, c *pgdev.Cluster, database, user, password string) *sql.DB {
	t.Helper()
	connector, err := pq.NewConnectorConfig(pq.Config{
		Host:               c.SocketDir(),
		Port:               pgdev.Port,
		Database:           database,
		User:               user,
		Password:           password,
		SSLMode:            pq.SSLModeDisable,
		ClientEncoding:     "UTF8",
		Datestyle:          "ISO, MDY",
		MinProtocolVersion: pq.ProtocolVersion30,
		MaxProtocolVersion: pq.ProtocolVersion30,
		ConnectTimeout:     10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	t.Cleanup(func() { db.Close() })
	return db
}

// procStatus returns the Uid and Groups lines of /proc/<pid>/status.
func procStatus(t *testing.T, pid int) (uids, groups string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "Uid:"); ok {
			uids = strings.Join(strings.Fields(v), " ")
		}
		if v, ok := strings.CutPrefix(line, "Groups:"); ok {
			groups = strings.TrimSpace(v)
		}
	}
	return uids, groups
}

// TestClusterLifecycle: the server answers on its Unix socket only, runs with
// the DR-11-37 log settings and fsync on, runs as "postgres" with no
// supplementary groups when the test is root, leaves no initdb password file,
// keeps data across a fast stop and restart, and Remove deletes the
// directory and ends the server.
func TestClusterLifecycle(t *testing.T) {
	ctx := context.Background()
	srv, err := pgtest.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	removed := false
	t.Cleanup(func() {
		if !removed {
			if err := srv.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	c := srv.Cluster
	t.Logf("server version: %s", c.Version())
	if !strings.Contains(c.Version(), "(PostgreSQL) 18.") {
		t.Errorf("version %q is not PostgreSQL 18", c.Version())
	}
	if err := pgdev.ValidateDir(c.Dir()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(c.Dir(), "pwfile")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("initdb password file still present: %v", err)
	}
	fi, err := os.Stat(c.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o711 {
		t.Errorf("cluster dir mode %o, want 711", fi.Mode().Perm())
	}

	db, err := c.SuperuserDB("postgres")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	want := map[string]string{
		"listen_addresses":                  "",
		"fsync":                             "on",
		"log_min_messages":                  "fatal",
		"log_min_error_statement":           "panic",
		"log_statement":                     "none",
		"log_parameter_max_length_on_error": "0",
		"log_error_verbosity":               "terse",
		"unix_socket_directories":           c.SocketDir(),
		"password_encryption":               "scram-sha-256",
	}
	for name, v := range want {
		var got string
		if err := db.QueryRowContext(ctx, "SELECT current_setting($1)", name).Scan(&got); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != v {
			t.Errorf("%s = %q, want %q", name, got, v)
		}
	}
	var hbaTrust int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM pg_hba_file_rules WHERE auth_method NOT IN ('scram-sha-256')").Scan(&hbaTrust); err != nil {
		t.Fatal(err)
	}
	if hbaTrust != 0 {
		t.Errorf("%d pg_hba rules use a method other than scram-sha-256", hbaTrust)
	}

	pid, _, ok := c.Postmaster()
	if !ok {
		t.Fatal("no postmaster")
	}
	uids, groups := procStatus(t, pid)
	if os.Geteuid() == 0 {
		u, err := user.Lookup(pgdev.ServerUser)
		if err != nil {
			t.Fatal(err)
		}
		if uids != strings.Repeat(u.Uid+" ", 3)+u.Uid {
			t.Errorf("postmaster Uid line %q, want all %s", uids, u.Uid)
		}
		if groups != "" {
			t.Errorf("postmaster supplementary groups %q, want none", groups)
		}
	}
	pgrp, err := syscall.Getpgid(pid)
	if err != nil {
		t.Fatal(err)
	}
	if pgrp != pid {
		t.Errorf("postmaster process group %d, want its own %d", pgrp, pid)
	}

	if _, err := db.ExecContext(ctx, "CREATE TABLE kept (v integer)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO kept VALUES (7)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if err := c.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(ctx); !errors.Is(err, pgdev.ErrRunning) {
		t.Fatalf("second Start: want ErrRunning, got %v", err)
	}
	db2, err := c.SuperuserDB("postgres")
	if err != nil {
		t.Fatal(err)
	}
	var v int
	if err := db2.QueryRowContext(ctx, "SELECT v FROM kept").Scan(&v); err != nil || v != 7 {
		t.Fatalf("after restart: v=%d err=%v", v, err)
	}
	db2.Close()

	pid2, exited, _ := c.Postmaster()
	dir := c.Dir()
	removed = true
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	default:
		t.Fatal("server still running after Remove")
	}
	if err := pgtest.WaitGroupGone(pid2, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cluster dir still present: %v", err)
	}
}

// TestRolesAndDatabases: a created role logs in with its password over
// scram-sha-256; a wrong password and a role without CONNECT are refused,
// because CreateDatabase revokes PUBLIC's privileges.
func TestRolesAndDatabases(t *testing.T) {
	ctx := context.Background()
	srv, err := pgtest.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Error(err)
		}
	})
	c := srv.Cluster
	ownerPass, err := c.CreateLoginRole(ctx, "role_owner")
	if err != nil {
		t.Fatal(err)
	}
	otherPass, err := c.CreateLoginRole(ctx, "role_other")
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerPass) != 48 || ownerPass == otherPass {
		t.Fatalf("passwords not 24 random bytes in hex")
	}
	if _, err := c.CreateLoginRole(ctx, "role_owner"); err == nil {
		t.Fatal("duplicate role created")
	}
	if err := srv.NewDatabase(ctx, "db_one", "role_owner"); err != nil {
		t.Fatal(err)
	}
	var who string
	if err := connect(t, c, "db_one", "role_owner", ownerPass).QueryRowContext(ctx, "SELECT current_user").Scan(&who); err != nil || who != "role_owner" {
		t.Fatalf("owner login: %q %v", who, err)
	}
	if err := connect(t, c, "db_one", "role_owner", otherPass).PingContext(ctx); err == nil {
		t.Fatal("wrong password accepted")
	}
	err = connect(t, c, "db_one", "role_other", otherPass).PingContext(ctx)
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || pqErr.Code != "42501" {
		t.Fatalf("role without CONNECT: want 42501, got %v", err)
	}
	if err := c.GrantConnect(ctx, "db_one", "role_other"); err != nil {
		t.Fatal(err)
	}
	if err := connect(t, c, "db_one", "role_other", otherPass).PingContext(ctx); err != nil {
		t.Fatalf("after GrantConnect: %v", err)
	}
}

// TestPostgresDiesWithParent: a helper process starts a cluster and is
// killed with SIGKILL. Pdeathsig makes the kernel kill the postmaster, its
// backends follow, and the machine-wide lock is free again (F18).
func TestPostgresDiesWithParent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := pgtest.HelperCommand(ctx, helperStartAndHang)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 1)
	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			if strings.HasPrefix(s.Text(), "READY ") {
				lines <- s.Text()
				return
			}
		}
		close(lines)
	}()
	var line string
	select {
	case l, ok := <-lines:
		if !ok {
			t.Fatal("helper exited before READY")
		}
		line = l
	case <-ctx.Done():
		t.Fatal("helper did not report READY")
	}
	fields := strings.Fields(line)
	if len(fields) != 3 {
		t.Fatalf("bad READY line %q", line)
	}
	pid, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatal(err)
	}
	dir := fields[2]
	if err := pgdev.ValidateDir(dir); err != nil {
		t.Fatalf("helper dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if live, err := pgtest.ProcessLive(pid); err != nil || !live {
		t.Fatalf("postmaster %d not live before the kill: %v %v", pid, live, err)
	}

	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("helper exited cleanly after SIGKILL")
	}
	if err := pgtest.WaitGroupGone(pid, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	if live, err := pgtest.ProcessLive(pid); err != nil || live {
		t.Fatalf("postmaster %d still live: %v %v", pid, live, err)
	}

	srv, err := pgtest.Start(ctx)
	if err != nil {
		t.Fatalf("lock not released after the helper died: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
}
