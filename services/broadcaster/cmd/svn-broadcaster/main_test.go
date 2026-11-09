package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/logx"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/signer"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

const unitChainID = "shortvideo-dev-cmdtest"

// result is one in-process run of the command.
type result struct {
	code           int
	stdout, stderr string
}

// runWith runs the command in process with environ and euid.
func runWith(t *testing.T, environ []string, euid uint32, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(context.Background(), args, processEnv{stdout: &out, stderr: &errOut, environ: environ, euid: euid})
	return result{code: code, stdout: out.String(), stderr: errOut.String()}
}

// runCmd runs the command with a clean environment and the test's uid.
func runCmd(t *testing.T, args ...string) result {
	t.Helper()
	return runWith(t, []string{"PATH=/usr/bin:/bin"}, uint32(os.Geteuid()), args...)
}

// want fails the test unless r has exit code code and its standard error
// contains every fragment.
func (r result) want(t *testing.T, what string, code int, fragments ...string) {
	t.Helper()
	if r.code != code {
		t.Fatalf("%s: exit %d, want %d\nstdout: %s\nstderr: %s", what, r.code, code, r.stdout, r.stderr)
	}
	for _, f := range fragments {
		if !strings.Contains(r.stderr, f) {
			t.Fatalf("%s: stderr lacks %q:\n%s", what, f, r.stderr)
		}
	}
}

// configFiles writes serve, signer and migrate configuration files that
// pass the configuration checks; the files they name do not exist.
type configFiles struct {
	dir, serve, signer, migrate, serveOwner, migrateKeys, migrateNoCallers string
}

func writeConfigs(t *testing.T) configFiles {
	t.Helper()
	dir := t.TempDir()
	db := func(role string) map[string]any {
		return map[string]any{"socket_dir": "/tmp/svn-pg-none/run", "port": 5432, "name": "broadcaster", "role": role,
			"password_file": filepath.Join(dir, "absent.password"), "application_name": "cmd-test", "connect_timeout_seconds": 1}
	}
	signerSection := map[string]any{"socket_path": filepath.Join(dir, "signer.sock"), "serve_uid": 2001, "signer_uid": 2002}
	callers := []any{map[string]any{"principal": "app-fixture", "kinds": []string{"REGISTER_CONTENT"}}}
	keys := []any{map[string]any{"name": "service", "class": "SERVICE", "path": filepath.Join(dir, "service.key")},
		map[string]any{"name": "broadcaster", "class": "BROADCASTER", "path": filepath.Join(dir, "broadcaster.key")}}
	serve := map[string]any{"chain_id": unitChainID, "rpc_url": "http://127.0.0.1:1", "database": db(store.RuntimeRole),
		"listen": map[string]any{"address": "127.0.0.1:18443", "tls_cert_file": filepath.Join(dir, "server.pem"),
			"tls_key_file": filepath.Join(dir, "server-key.pem"), "client_ca_file": filepath.Join(dir, "ca.pem")},
		"signer": signerSection, "intent_key_registry": filepath.Join(dir, "intent-keys.json"), "callers": callers}
	c := configFiles{dir: dir}
	write := func(name string, m map[string]any) string {
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	c.serve = write("serve.json", serve)
	c.signer = write("signer.json", map[string]any{"chain_id": unitChainID, "rpc_url": "http://127.0.0.1:1",
		"signer": signerSection, "intent_key_registry": filepath.Join(dir, "intent-keys.json"), "keys": keys})
	c.migrate = write("migrate.json", map[string]any{"chain_id": unitChainID, "rpc_url": "http://127.0.0.1:1",
		"database": db(store.OwnerRole), "callers": callers})
	owner := map[string]any{}
	for k, v := range serve {
		owner[k] = v
	}
	owner["database"] = db(store.OwnerRole)
	c.serveOwner = write("serve-owner.json", owner)
	c.migrateKeys = write("migrate-keys.json", map[string]any{"chain_id": unitChainID, "rpc_url": "http://127.0.0.1:1",
		"database": db(store.OwnerRole), "keys": keys})
	c.migrateNoCallers = write("migrate-nocallers.json", map[string]any{"chain_id": unitChainID, "rpc_url": "http://127.0.0.1:1",
		"database": db(store.OwnerRole)})
	return c
}

// configCommands are the subcommands that take --config, with the other
// arguments each needs to get past its own flag checks.
func configCommands(t *testing.T) map[string][]string {
	t.Helper()
	codec, err := chain.NewCodec(unitChainID)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := codec.Bech32(bytes.Repeat([]byte{7}, 20))
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]string{
		"serve": nil, "signer": nil, "migrate": nil, "status": nil, "reconcile-once": nil,
		"signer-clear": {"--signer", addr, "--sequence", "3", "--foreign-hash", strings.Repeat("ab", 32)},
	}
}

func TestUsage(t *testing.T) {
	runCmd(t).want(t, "no subcommand", exitUsage, "usage: svn-broadcaster")
	help := runCmd(t, "help")
	help.want(t, "help", exitOK)
	for _, name := range commandNames() {
		if !strings.Contains(help.stdout, name) {
			t.Fatalf("help does not list %s: %s", name, help.stdout)
		}
	}
	if len(commandNames()) != 8 {
		t.Fatalf("subcommands %v, want the 8 of plan 4.2", commandNames())
	}
	runCmd(t, "deploy").want(t, "unknown subcommand", exitUsage, "unknown subcommand")
	runCmd(t, strings.Repeat("x", 100)).want(t, "long unknown subcommand", exitUsage, strings.Repeat("x", 32)+`"`)
	for _, name := range commandNames() {
		runCmd(t, name, "-h").want(t, name+" -h", exitUsage)
		runCmd(t, name, "--no-such-flag").want(t, name+" unknown flag", exitUsage, "flag provided but not defined")
		runCmd(t, name, "--config", "/x", "stray").want(t, name+" positional argument", exitUsage)
	}
}

// TestEnvironmentRefusedFirst checks plan 7.7: every subcommand refuses a
// PG* or PQGO_DEBUG variable before it parses its flags, and the log names
// the variable, never its value.
func TestEnvironmentRefusedFirst(t *testing.T) {
	for _, name := range commandNames() {
		for _, kv := range []string{"PGPASSWORD=canary-7f3a", "PGHOST=canary-7f3a", "PQGO_DEBUG=canary-7f3a", "PGSERVICEFILE=canary-7f3a"} {
			r := runWith(t, []string{"PATH=/usr/bin", kv}, uint32(os.Geteuid()), name, "--no-such-flag")
			variable, _, _ := strings.Cut(kv, "=")
			r.want(t, name+" "+variable, exitRefused, variable)
			if strings.Contains(r.stderr+r.stdout, "canary-7f3a") {
				t.Fatalf("%s: the output holds the variable's value: %s", name, r.stderr)
			}
		}
	}
	if r := runWith(t, []string{"PATH=/usr/bin", "PGHOST=x"}, uint32(os.Geteuid()), "help"); r.code != exitOK {
		t.Fatal("help refused the environment")
	}
}

func TestConfigFlagRules(t *testing.T) {
	c := writeConfigs(t)
	broken := filepath.Join(c.dir, "broken.json")
	if err := os.WriteFile(broken, []byte(`{"chain_id":"`+unitChainID+`","unknown_field":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, extra := range configCommands(t) {
		runCmd(t, append([]string{name}, extra...)...).want(t, name+" without --config", exitUsage, "--config is required")
		runCmd(t, append([]string{name, "--config", "serve.json"}, extra...)...).want(t, name+" relative config", exitUsage,
			"absolute path")
		runCmd(t, append([]string{name, "--config", filepath.Join(c.dir, "absent.json")}, extra...)...).want(t,
			name+" missing config", exitRefused, "config: open")
		runCmd(t, append([]string{name, "--config", broken}, extra...)...).want(t, name+" unknown field", exitRefused,
			"unknown field")
	}
}

// TestSectionAndRoleRefusals checks that each subcommand refuses a
// configuration of the wrong process role, the wrong database role or the
// wrong uid before it reads any file the configuration names.
func TestSectionAndRoleRefusals(t *testing.T) {
	c := writeConfigs(t)
	for _, tc := range []struct {
		what     string
		euid     uint32
		args     []string
		fragment string
	}{
		{"serve with the signer's file", 2001, []string{"serve", "--config", c.signer}, "required section missing: database"},
		{"serve under the owner role", 2001, []string{"serve", "--config", c.serveOwner}, "runtime role"},
		{"serve under another uid", 2002, []string{"serve", "--config", c.serve}, "another uid"},
		{"serve as root", 0, []string{"serve", "--config", c.serve}, "another uid"},
		{"reconcile-once under another uid", 2002, []string{"reconcile-once", "--config", c.serve}, "another uid"},
		{"reconcile-once with the signer's file", 2001, []string{"reconcile-once", "--config", c.signer}, "database"},
		{"signer with serve's file", 2002, []string{"signer", "--config", c.serve}, "required section missing: keys"},
		{"signer under the serve uid", 2001, []string{"signer", "--config", c.signer}, "another uid"},
		{"signer as root", 0, []string{"signer", "--config", c.signer}, "another uid"},
		{"migrate under the runtime role", 0, []string{"migrate", "--config", c.serve}, "owner role"},
		{"migrate naming key files", 0, []string{"migrate", "--config", c.migrateKeys}, "key files"},
		{"migrate --callers without callers", 0, []string{"migrate", "--config", c.migrateNoCallers, "--callers"}, "at least one caller"},
		{"status without a database", 0, []string{"status", "--config", c.signer}, "database"},
		{"signer-clear under the owner role", 0, append([]string{"signer-clear", "--config", c.migrate},
			configCommands(t)["signer-clear"]...), "runtime role"},
	} {
		runWith(t, []string{"PATH=/usr/bin"}, tc.euid, tc.args...).want(t, tc.what, exitRefused, tc.fragment)
	}
	// Past the role checks, a password file that is missing refuses the
	// start before any connection.
	runWith(t, []string{"PATH=/usr/bin"}, 2001, "serve", "--config", c.serve).want(t, "serve without its password file",
		exitRefused, "password file")
	runWith(t, []string{"PATH=/usr/bin"}, 0, "migrate", "--config", c.migrate).want(t, "migrate without its password file",
		exitRefused, "password file")
}

func TestSignerClearArguments(t *testing.T) {
	c := writeConfigs(t)
	good := configCommands(t)["signer-clear"]
	with := func(flag, value string) []string {
		out := []string{"signer-clear", "--config", c.serve}
		for i := 0; i < len(good); i += 2 {
			if good[i] == flag {
				if value != "" {
					out = append(out, flag, value)
				}
				continue
			}
			out = append(out, good[i], good[i+1])
		}
		return out
	}
	for _, tc := range []struct{ flag, value, fragment string }{
		{"--signer", "", "--signer"},
		{"--signer", "not-an-address", "bech32"},
		{"--signer", "cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu", "bech32"},
		{"--signer", strings.Repeat("s", maxAddressLen+1), "bech32"},
		{"--sequence", "", "--sequence is required"},
		{"--foreign-hash", "", "--foreign-hash"},
		{"--foreign-hash", strings.Repeat("AB", 32), "--foreign-hash"},
		{"--foreign-hash", strings.Repeat("a", 63), "--foreign-hash"},
		{"--foreign-hash", strings.Repeat("g", 64), "--foreign-hash"},
	} {
		runCmd(t, with(tc.flag, tc.value)...).want(t, "signer-clear "+tc.flag+"="+tc.value, exitUsage, tc.fragment)
	}
	runCmd(t, with("--sequence", "-1")...).want(t, "negative sequence", exitUsage)
	runCmd(t, with("--sequence", "9223372036854775808")...).want(t, "sequence above int64", exitUsage, "--sequence")
	addr, err := parseAccount(good[1])
	if err != nil || !bytes.Equal(addr, bytes.Repeat([]byte{7}, 20)) {
		t.Fatalf("parse %s: %x %v", good[1], addr, err)
	}
}

func TestExitCodeMapping(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code int
	}{
		{nil, exitOK},
		{errors.New("x"), exitFailure},
		{usageError(errors.New("x")), exitUsage},
		{refusedError(errors.New("x")), exitRefused},
		{fmt.Errorf("wrapped: %w", lockHeldError(errors.New("x"))), exitLockHeld},
		{errors.Join(errors.New("a"), notFoundError(errors.New("x"))), exitNotFound},
	} {
		if got := exitCode(tc.err); got != tc.code {
			t.Errorf("exitCode(%v) = %d, want %d", tc.err, got, tc.code)
		}
	}
}

func TestStartError(t *testing.T) {
	netErr := fmt.Errorf("signer keys: %w", &net.OpError{Op: "dial", Net: "unix", Err: errors.New("no such file")})
	if exitCode(startError(netErr)) != exitFailure {
		t.Fatal("a network error is not a run-time failure")
	}
	urlErr := fmt.Errorf("chain: status: %w", &url.Error{Op: "Post", URL: "http://127.0.0.1:1", Err: errors.New("refused")})
	if exitCode(startError(urlErr)) != exitFailure {
		t.Fatal("an HTTP transport error is not a run-time failure")
	}
	if exitCode(startError(&policy.Refusal{Code: policy.CodeRole, Detail: "no role"})) != exitRefused {
		t.Fatal("a role refusal is not a refused start")
	}
	if exitCode(startError(errors.New("signer: key directory mode"))) != exitRefused {
		t.Fatal("a file rule refusal is not a refused start")
	}
}

func TestCheckKeyClasses(t *testing.T) {
	svc := signer.KeyInfo{Class: policy.ClassService}
	bc := signer.KeyInfo{Class: policy.ClassBroadcaster}
	if err := checkKeyClasses([]signer.KeyInfo{svc, bc}); err != nil {
		t.Fatal(err)
	}
	for name, keys := range map[string][]signer.KeyInfo{
		"none": nil, "service only": {svc}, "two service": {svc, svc}, "unknown class": {svc, {Class: "AUDITOR"}},
		"three": {svc, bc, bc},
	} {
		if err := checkKeyClasses(keys); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestFileOwner(t *testing.T) {
	for _, tc := range []struct {
		flag    int64
		euid    uint32
		want    uint32
		errCode int
	}{
		{-1, 1000, 1000, exitOK},
		{1000, 1000, 1000, exitOK},
		{2000, 0, 2000, exitOK},
		{2000, 1000, 0, exitRefused},
		{0, 0, 0, exitUsage},
		{-2, 0, 0, exitUsage},
		{1 << 31, 0, 0, exitUsage},
	} {
		got, err := fileOwner(tc.flag, tc.euid)
		if exitCode(err) != tc.errCode || (err == nil && got != tc.want) {
			t.Errorf("fileOwner(%d, %d) = %d, %v", tc.flag, tc.euid, got, err)
		}
	}
}

// TestServeUntilDone checks how serve stops: a signal stops both halves
// cleanly; a half that fails stops the other and is the error; a half
// that returns on its own is an error too.
func TestServeUntilDone(t *testing.T) {
	waitThen := func(err error) func(context.Context) error {
		return func(ctx context.Context) error {
			<-ctx.Done()
			if err != nil {
				return err
			}
			return nil
		}
	}
	workerLike := func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	log := discardLogger()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := serveUntilDone(ctx, log, waitThen(nil), workerLike); err != nil {
		t.Fatalf("signal stop: %v", err)
	}

	boom := errors.New("listen: address in use")
	err := serveUntilDone(context.Background(), log, func(context.Context) error { return boom }, workerLike)
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "api") || strings.Contains(err.Error(), "worker") {
		t.Fatalf("API failure: %v", err)
	}
	err = serveUntilDone(context.Background(), log, waitThen(nil), func(context.Context) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "worker stopped on its own") {
		t.Fatalf("worker returned on its own: %v", err)
	}
	shutdown := errors.New("shutdown timed out")
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if err := serveUntilDone(ctx, log, waitThen(shutdown), workerLike); !errors.Is(err, shutdown) {
		t.Fatalf("a failed shutdown after a signal: %v", err)
	}
}

// discardLogger is a logger that writes nothing.
func discardLogger() *slog.Logger { return logx.Discard() }
