//go:build svn_roottest

package signer

// Root-only uid boundary tests (plan 4.1, 13.6 and section 15
// uid-boundary.txt; DR-11-35). They run only with -tags svn_roottest and
// skip with a reason when the test process is not root.
//
// What they do to the system: they create no user or group. They pick
// three numeric uids that /etc/passwd and /etc/group do not list, that no
// running process uses, and that own nothing directly under the system
// temp directory. They create one directory under the system temp
// directory, chown files in it to two of those uids, copy the test binary
// into it, and run that copy as child processes under the uids with an
// empty supplementary group list (SysProcAttr.Credential, the mechanism
// internal/pgdev uses for postgres). Every child is killed and the
// directory is removed at the end; the test then checks that no file
// directly under the temp directory is owned by the three uids.

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"

	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/keys"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
)

// helperEnv selects a child role of the copied test binary; configEnv
// carries its rootChild configuration as JSON (paths, uids and public
// addresses only).
const (
	helperEnv = "SVN_SIGNER_ROOTTEST_HELPER"
	configEnv = "SVN_SIGNER_ROOTTEST_CONFIG"
)

// rootChild is a child's configuration.
type rootChild struct {
	Socket       string   `json:"socket"`
	ServeUID     uint32   `json:"serve_uid"`
	ServicePath  string   `json:"service_path"`
	RelayPath    string   `json:"relay_path"`
	RegistryPath string   `json:"registry_path"`
	ServiceAddr  string   `json:"service_addr"`
	RelayAddr    string   `json:"relay_addr"`
	KeyDir       string   `json:"key_dir"`
	Expect       []string `json:"expect,omitempty"`
}

func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "":
		os.Exit(m.Run())
	case "signer":
		os.Exit(runSignerChild())
	case "client":
		os.Exit(runClientChild())
	case "readkey":
		os.Exit(runReadKeyChild())
	default:
		fmt.Fprintln(os.Stderr, "unknown helper role")
		os.Exit(2)
	}
}

func childConfig() (rootChild, error) {
	var c rootChild
	err := json.Unmarshal([]byte(os.Getenv(configEnv)), &c)
	return c, err
}

// result prints the child's one result line.
func result(v map[string]string) {
	out, _ := json.Marshal(v)
	fmt.Println("result " + string(out))
}

// runSignerChild runs the signer under DistinctUIDs on a fake node that
// gives the two keys their roles, prints "ready", and serves until
// SIGTERM. A start failure is printed as the result.
func runSignerChild() int {
	c, err := childConfig()
	if err != nil {
		result(map[string]string{"start": "config: " + err.Error()})
		return 1
	}
	service, _ := hex.DecodeString(c.ServiceAddr)
	relay, _ := hex.DecodeString(c.RelayAddr)
	node := &fakeNode{chainID: testChainID, height: 100, submitters: []*typesv1.Submitter{
		{Address: service, Role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE},
		{Address: relay, Role: typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER},
	}}
	cfg := Config{
		ChainID: testChainID, RPCURL: "http://127.0.0.1:26657", RPCTimeout: 2 * time.Second, PollInterval: 100 * time.Millisecond,
		FreshnessPolls: 4, MaxTimeoutOffset: 1000, SocketPath: c.Socket, Boundary: policy.DistinctUIDs, ServeUID: c.ServeUID,
		RegistryPath: c.RegistryPath,
		Keys:         []KeyConfig{{Name: "service", Class: policy.ClassService, Path: c.ServicePath}, {Name: "broadcaster", Class: policy.ClassBroadcaster, Path: c.RelayPath}},
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer stop()
	srv, err := start(ctx, cfg, node, uint32(os.Geteuid()))
	if err != nil {
		result(map[string]string{"start": err.Error()})
		return 1
	}
	fmt.Println("ready")
	if err := srv.Serve(ctx); err != nil {
		return 1
	}
	return 0
}

// runClientChild asks the signer for its keys and one signature, and
// prints what happened.
func runClientChild() int {
	c, err := childConfig()
	if err != nil {
		return 1
	}
	codec, err := chain.NewCodec(testChainID)
	if err != nil {
		return 1
	}
	client, err := NewClient(c.Socket, codec, 5*time.Second)
	if err != nil {
		return 1
	}
	out := map[string]string{"uid": strconv.Itoa(os.Geteuid())}
	infos, err := client.Keys(context.Background())
	if err != nil {
		out["keys"] = "refused:" + string(policy.CodeOf(err))
		result(out)
		return 0
	}
	out["keys"] = strconv.Itoa(len(infos))
	offer := &leasesv1.MsgOfferLease{Signer: infos[0].Address, BusinessId: "bcast-root-1", OfferId: "offer-root", ContentId: "content-1",
		ContentVersion: 1, ProviderId: "provider-1", ManifestDigest: digest("m"), PackageBytes: 1, TermsDigest: digest("t"),
		StartUnixSeconds: 1_900_000_000, DeadlineUnixSeconds: 1_900_000_600, ExpiryUnixSeconds: 1_900_003_600, TermVersion: 1}
	signed, err := client.RequestSignature(context.Background(), SignRequest{KeyName: "service", AccountNumber: 1, Sequence: 0, Gas: 2_000_000, TimeoutHeight: 140, Msg: offer})
	if err != nil {
		out["sign"] = "error:" + string(policy.CodeOf(err))
	} else {
		out["sign"] = hex.EncodeToString(signed.Hash[:])
	}
	result(out)
	return 0
}

// errnoName names the errno of a filesystem error.
func errnoName(err error) string {
	var errno syscall.Errno
	switch {
	case err == nil:
		return "ok"
	case errors.As(err, &errno) && errno == syscall.EACCES:
		return "EACCES"
	case errors.As(err, &errno) && errno == syscall.EPERM:
		return "EPERM"
	}
	return "other"
}

// runReadKeyChild tries, as its own uid, to read and list the signer's
// key files and to load them with the key loader.
func runReadKeyChild() int {
	c, err := childConfig()
	if err != nil {
		return 1
	}
	_, readErr := os.ReadFile(c.ServicePath)
	_, listErr := os.ReadDir(c.KeyDir)
	_, statErr := os.Stat(c.ServicePath)
	_, loadErr := keys.Load(c.ServicePath, uint32(os.Geteuid()))
	result(map[string]string{
		"uid": strconv.Itoa(os.Geteuid()), "read": errnoName(readErr), "list": errnoName(listErr), "stat": errnoName(statErr),
		"load": map[bool]string{true: "refused", false: "loaded"}[loadErr != nil],
	})
	return 0
}

// rootEnv is the parent's setup.
type rootEnv struct {
	base                string
	binary              string
	signerUID, serveUID uint32
	otherUID            uint32
	service, relay      cryptotypes.PrivKey
	child               rootChild
}

// uidUnused reports whether uid has no passwd or group entry, runs no
// process, and owns no entry directly under dir.
func uidUnused(uid uint32, dir string) (bool, error) {
	id := strconv.FormatUint(uint64(uid), 10)
	if _, err := user.LookupId(id); !errors.As(err, new(user.UnknownUserIdError)) {
		return false, nil
	}
	if _, err := user.LookupGroupId(id); !errors.As(err, new(user.UnknownGroupIdError)) {
		return false, nil
	}
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	for _, p := range procs {
		if _, err := strconv.Atoi(p.Name()); err != nil {
			continue
		}
		info, err := os.Stat(filepath.Join("/proc", p.Name()))
		if err != nil {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Uid == uid {
			return false, nil
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		info, err := os.Lstat(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Uid == uid {
			return false, nil
		}
	}
	return true, nil
}

// pickUIDs returns n distinct unused uids from a high range.
func pickUIDs(t *testing.T, n int) []uint32 {
	t.Helper()
	var out []uint32
	for tries := 0; len(out) < n && tries < 100; tries++ {
		uid := uint32(2_000_000_000 + rand.IntN(100_000_000))
		ok, err := uidUnused(uid, os.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		dup := false
		for _, u := range out {
			dup = dup || u == uid
		}
		if ok && !dup {
			out = append(out, uid)
		}
	}
	if len(out) < n {
		t.Fatal("no unused uids found")
	}
	return out
}

func chownTree(t *testing.T, uid uint32, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.Lchown(p, int(uid), int(uid)); err != nil {
			t.Fatal(err)
		}
	}
}

// newRootEnv builds the directory tree and the binary copy.
func newRootEnv(t *testing.T) *rootEnv {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("skipped: the uid boundary tests need euid 0 to run children under distinct uids (plan 4.1); a same-uid run does not verify the boundary")
	}
	uids := pickUIDs(t, 3)
	e := &rootEnv{signerUID: uids[0], serveUID: uids[1], otherUID: uids[2], service: sdksecp.GenPrivKey(), relay: sdksecp.GenPrivKey()}
	t.Logf("boundary_uids=signer:%d,serve:%d (other peer uid %d)", e.signerUID, e.serveUID, e.otherUID)
	base, err := os.MkdirTemp("", "svn-roottest-")
	if err != nil {
		t.Fatal(err)
	}
	e.base = base
	t.Cleanup(func() {
		if err := os.RemoveAll(base); err != nil {
			t.Errorf("remove %s: %v", base, err)
		}
		for _, uid := range uids {
			if ok, err := uidUnused(uid, os.TempDir()); err != nil || !ok {
				t.Errorf("uid %d still owns an entry under %s or runs a process after the test", uid, os.TempDir())
			}
		}
	})
	if err := os.Chmod(base, 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	e.binary = filepath.Join(base, "signer.test")
	if err := copyFile(self, e.binary, 0o755); err != nil {
		t.Fatal(err)
	}
	keyDir := filepath.Join(base, "keys")
	sockDir := filepath.Join(base, "sock")
	regDir := filepath.Join(base, "registry")
	for dir, mode := range map[string]os.FileMode{keyDir: 0o700, sockDir: 0o711, regDir: 0o755} {
		if err := os.Mkdir(dir, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
	}
	servicePath := writeKey(t, keyDir, "service.hex", e.service)
	relayPath := writeKey(t, keyDir, "broadcaster.hex", e.relay)
	chownTree(t, e.signerUID, keyDir, servicePath, relayPath, sockDir)
	regPath := filepath.Join(regDir, "intent-keys.json")
	if err := os.WriteFile(regPath, []byte(`{"entries":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(regPath, 0o644); err != nil {
		t.Fatal(err)
	}
	e.child = rootChild{Socket: filepath.Join(sockDir, "signer.sock"), ServeUID: e.serveUID, ServicePath: servicePath,
		RelayPath: relayPath, RegistryPath: regPath, KeyDir: keyDir,
		ServiceAddr: hex.EncodeToString(e.service.PubKey().Address()), RelayAddr: hex.EncodeToString(e.relay.PubKey().Address())}
	return e
}

func copyFile(from, to string, mode os.FileMode) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Chmod(mode); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// command runs the binary copy in role under uid.
func (e *rootEnv) command(ctx context.Context, t *testing.T, role string, uid uint32, child rootChild) *exec.Cmd {
	t.Helper()
	cfg, err := json.Marshal(child)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, e.binary)
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=/nonexistent", helperEnv + "=" + role, configEnv + "=" + string(cfg)}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uid, Gid: uid, Groups: []uint32{}},
		Pdeathsig:  syscall.SIGKILL,
	}
	return cmd
}

// startSigner starts the signer child as the signer uid and waits for
// "ready"; it returns the start failure text instead when the child
// refuses to start.
func (e *rootEnv) startSigner(t *testing.T, child rootChild) (refusal string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	cmd := e.command(ctx, t, "signer", e.signerUID, child)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	lines := bufio.NewScanner(stdout)
	if !lines.Scan() {
		_ = cmd.Wait()
		cancel()
		t.Fatalf("signer child printed nothing: %s", stderr.String())
	}
	first := lines.Text()
	if first != "ready" {
		_ = cmd.Wait()
		cancel()
		return first
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		cancel()
	})
	return ""
}

// runChild runs a short-lived child and returns its result line.
func (e *rootEnv) runChild(t *testing.T, role string, uid uint32, child rootChild) map[string]string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := e.command(ctx, t, role, uid, child).Output()
	if err != nil {
		t.Fatalf("%s child as uid %d: %v (%s)", role, uid, err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(line, "result "); ok {
			var r map[string]string
			if err := json.Unmarshal([]byte(rest), &r); err != nil {
				t.Fatal(err)
			}
			return r
		}
	}
	t.Fatalf("%s child printed no result: %s", role, out)
	return nil
}

// TestSignerRefusesOtherUIDPeer runs the signer as its own non-root uid
// with the serve uid configured, then connects from the serve uid (served:
// keys listed and an offer signed), from a third uid and from root (both
// refused with PEER_REFUSED).
func TestSignerRefusesOtherUIDPeer(t *testing.T) {
	e := newRootEnv(t)
	if refusal := e.startSigner(t, e.child); refusal != "" {
		t.Fatalf("signer refused to start: %s", refusal)
	}
	serve := e.runChild(t, "client", e.serveUID, e.child)
	if serve["keys"] != "2" || len(serve["sign"]) != 64 {
		t.Fatalf("serve uid %d: %v", e.serveUID, serve)
	}
	other := e.runChild(t, "client", e.otherUID, e.child)
	if other["keys"] != "refused:"+string(CodePeerRefused) {
		t.Fatalf("other uid %d: %v", e.otherUID, other)
	}
	codec, err := chain.NewCodec(testChainID)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(e.child.Socket, codec, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Keys(context.Background())
	wantRefusal(t, err, CodePeerRefused)
	t.Logf("peer uid %d served (signature %s); uid %d and uid 0 refused", e.serveUID, serve["sign"], e.otherUID)
}

// TestServeUIDCannotReadKeyFiles checks that the serve uid can neither
// read nor list nor stat the signer's key files, and that the signer, as
// its own uid, refuses key files owned by the serve uid, a registry it
// could write, a registry file or directory owned by the serve uid, and a
// configuration naming its own uid as the serve uid.
func TestServeUIDCannotReadKeyFiles(t *testing.T) {
	e := newRootEnv(t)
	got := e.runChild(t, "readkey", e.serveUID, e.child)
	if got["read"] != "EACCES" || got["list"] != "EACCES" || got["stat"] != "EACCES" || got["load"] != "refused" {
		t.Fatalf("serve uid %d reaching the key files: %v", e.serveUID, got)
	}
	t.Logf("serve uid %d: read=%s list=%s stat=%s load=%s", e.serveUID, got["read"], got["list"], got["stat"], got["load"])

	t.Run("signer loads its own keys", func(t *testing.T) {
		if refusal := e.startSigner(t, e.child); refusal != "" {
			t.Fatalf("signer refused to start: %s", refusal)
		}
		if r := e.runChild(t, "client", e.serveUID, e.child); r["keys"] != "2" {
			t.Fatalf("keys through the socket: %v", r)
		}
	})
	t.Run("key file owned by the serve uid", func(t *testing.T) {
		chownTree(t, e.serveUID, e.child.ServicePath)
		defer chownTree(t, e.signerUID, e.child.ServicePath)
		child := e.child
		child.Socket = filepath.Join(filepath.Dir(e.child.Socket), "s2.sock")
		refusal := e.startSigner(t, child)
		if !strings.Contains(refusal, "owner uid") {
			t.Fatalf("signer start with a key file of the serve uid: %q", refusal)
		}
	})
	t.Run("registry writable by the signer uid", func(t *testing.T) {
		chownTree(t, e.signerUID, e.child.RegistryPath)
		defer chownTree(t, 0, e.child.RegistryPath)
		child := e.child
		child.Socket = filepath.Join(filepath.Dir(e.child.Socket), "s3.sock")
		refusal := e.startSigner(t, child)
		if !strings.Contains(refusal, policy.ErrRegistryWritable.Error()) {
			t.Fatalf("signer start with a registry it owns: %q", refusal)
		}
	})
	t.Run("registry owned by the serve uid", func(t *testing.T) {
		chownTree(t, e.serveUID, e.child.RegistryPath)
		defer chownTree(t, 0, e.child.RegistryPath)
		child := e.child
		child.Socket = filepath.Join(filepath.Dir(e.child.Socket), "s5.sock")
		refusal := e.startSigner(t, child)
		if !strings.Contains(refusal, policy.ErrRegistryServeWritable.Error()) {
			t.Fatalf("signer start with a registry the serve uid owns: %q", refusal)
		}
	})
	t.Run("registry directory owned by the serve uid", func(t *testing.T) {
		dir := filepath.Dir(e.child.RegistryPath)
		chownTree(t, e.serveUID, dir)
		defer chownTree(t, 0, dir)
		child := e.child
		child.Socket = filepath.Join(filepath.Dir(e.child.Socket), "s6.sock")
		refusal := e.startSigner(t, child)
		if !strings.Contains(refusal, policy.ErrRegistryServeWritable.Error()) {
			t.Fatalf("signer start with a registry directory the serve uid owns: %q", refusal)
		}
	})
	t.Run("serve uid equal to the signer uid", func(t *testing.T) {
		child := e.child
		child.ServeUID = e.signerUID
		child.Socket = filepath.Join(filepath.Dir(e.child.Socket), "s4.sock")
		refusal := e.startSigner(t, child)
		if !strings.Contains(refusal, "distinct uids") {
			t.Fatalf("signer start with its own uid as serve uid: %q", refusal)
		}
	})
	if _, err := os.Stat(e.child.KeyDir); errors.Is(err, fs.ErrNotExist) {
		t.Fatal("key directory vanished")
	}
}
