package milestone_test

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"

	"github.com/Maxencejules/shortvideo-network/chain/address"
	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/devnet"
)

// TestMilestone is the goal 18 upload-to-playback scenario on one machine.
// SVN_SKIP_DEVNET=1 skips it. The skip is not a pass.
func TestMilestone(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip("SVN_SKIP_DEVNET=1")
	}
	m := newMile(t)
	m.boot()
	m.upload()
	m.registerAndPublish()
	m.play()
	m.retry()
	m.corrupt()
	m.faults()
	m.unauthorized()
	m.quorum()
	m.withdraw()
	fmt.Println("phase=done prototype=true public_beta=false single_machine=true")
}

type mile struct {
	t         *testing.T
	root      string
	work      string
	bins      map[string]string
	layout    devnet.Layout
	enc       app.Encoding
	rpc       *rpchttp.HTTP
	vals      []*child
	chainID   string
	svc       loaded
	auth      loaded
	p1        loaded
	p2        loaded
	seats     []loaded
	policy    uint64
	intent    ed25519.PrivateKey
	bc        *http.Client
	bcURL     string
	indexer   string
	sched     string
	schedProc *child
	api       string
	stage     string
	stageCA   string
	views     map[string]viewInfo
	prov      map[string]*provider
	content   string
	digest    string
	pkgBytes  uint64
	segments  []seg
	deliver   string
	deliverCA string
	worker    string
	clipSHA   string
	pg        pgInfo
}

type viewInfo struct {
	URL string `json:"url"`
	CA  string `json:"ca_file"`
}

type provider struct {
	id, dir, ca, url string
	child            *child
}

type seg struct {
	ID, SHA, Path string
	N             uint64
}

type loaded struct {
	name string
	key  cryptotypes.PrivKey
}

type child struct {
	name string
	cmd  *exec.Cmd
	out  *syncBuf
	done chan struct{}
	once sync.Once
}

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func newMile(t *testing.T) *mile {
	t.Helper()
	address.Configure()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	enc, err := app.MakeEncoding()
	if err != nil {
		t.Fatal(err)
	}
	m := &mile{t: t, root: root, work: t.TempDir(), enc: enc, bins: map[string]string{}, policy: 1, views: map[string]viewInfo{}, prov: map[string]*provider{}}
	m.build()
	return m
}

func (m *mile) build() {
	m.t.Helper()
	specs := []struct{ name, dir, pkg string }{
		{"svnd", "chain", "./cmd/svnd"},
		{"svn-provider", "agents/provider", "./cmd/svn-provider"},
		{"svn-broadcaster", "services/broadcaster", "./cmd/svn-broadcaster"},
		{"svn-indexer", "services/indexer", "./cmd/svn-indexer"},
		{"svn-api", "services/api", "./cmd/svn-api"},
		{"svn-scheduler", "services/scheduler", "./cmd/svn-scheduler"},
		{"svn-chainview", "services/milestone", "./cmd/svn-chainview"},
	}
	for _, spec := range specs {
		out := filepath.Join(m.work, spec.name)
		cmd := exec.Command("go", "build", "-o", out, spec.pkg)
		cmd.Dir = filepath.Join(m.root, spec.dir)
		cmd.Env = os.Environ()
		var buf bytes.Buffer
		cmd.Stdout, cmd.Stderr = &buf, &buf
		if err := cmd.Run(); err != nil {
			m.t.Fatalf("build %s: %v\n%s", spec.name, err, buf.String())
		}
		m.bins[spec.name] = out
	}
}

func (m *mile) boot() {
	m.t.Helper()
	home := filepath.Join(m.work, "devnet")
	layout, err := devnet.Init(devnet.Options{Home: home})
	if err != nil {
		m.t.Fatal(err)
	}
	m.layout = layout
	m.chainID = layout.Topology.ChainID
	fmt.Printf("phase=devnet chain_id=%s genesis=%s physical_count=1 independent=false\n", m.chainID, layout.Topology.GenesisSHA256)
	m.vals = make([]*child, 4)
	var rpcs []string
	for i, node := range layout.Topology.Validators {
		m.vals[i] = m.start("v"+strconv.Itoa(i+1), m.bins["svnd"], "start", "--home", node.Home)
		rpcs = append(rpcs, node.RPC)
	}
	height, appHash, err := devnet.WaitEqual(rpcs, 3, 90*time.Second)
	if err != nil {
		m.t.Fatalf("devnet: %v\n%s", err, m.vals[0].out.String())
	}
	fmt.Printf("phase=devnet-height height=%d app_hash=%s validators=4\n", height, appHash)
	client, err := rpchttp.New(rpcs[0], "/websocket")
	if err != nil {
		m.t.Fatal(err)
	}
	m.rpc = client
	m.svc = m.load("service", false)
	m.auth = m.load("authority", false)
	m.p1 = m.load("provider-1", false)
	m.p2 = m.load("provider-2", false)
	for i := 1; i <= 4; i++ {
		m.seats = append(m.seats, m.load(fmt.Sprintf("seat-%d", i), true))
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		m.t.Fatal(err)
	}
	m.intent = priv
	m.bootChainviews()
	m.bootIndexer()
	m.bootScheduler()
	m.bootProviders(pub)
	m.admitProviders(pub)
	m.bootBroadcaster(pub)
	m.bootAPI()
}

func (m *mile) load(name string, ed bool) loaded {
	m.t.Helper()
	raw, err := os.ReadFile(filepath.Join(m.layout.Home, "accounts", name+".hex"))
	if err != nil {
		m.t.Fatal(err)
	}
	buf, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		m.t.Fatal(err)
	}
	var key cryptotypes.PrivKey
	if ed {
		key = &sdked.PrivKey{Key: buf}
	} else {
		key = &sdksecp.PrivKey{Key: buf}
	}
	return loaded{name: name, key: key}
}

func (m *mile) bootChainviews() {
	m.t.Helper()
	m.views["a"] = m.chainview(m.bech(m.p1.key), "p1", "p2", "p3")
	m.views["b"] = m.chainview(m.bech(m.p2.key), "p4")
}

func (m *mile) chainview(account string, providers ...string) viewInfo {
	m.t.Helper()
	args := []string{"--rpc", m.layout.Topology.Validators[0].RPC, "--listen", "127.0.0.1:0", "--account", account}
	for _, id := range providers {
		args = append(args, "--provider", id)
	}
	c := m.start("chainview-"+providers[0], m.bins["svn-chainview"], args...)
	line := m.waitLine(c, `"url"`, 30*time.Second)
	var info viewInfo
	if err := json.Unmarshal([]byte(line), &info); err != nil {
		m.t.Fatalf("chainview: %v %s", err, c.out.String())
	}
	fmt.Printf("phase=chainview account=%s url=%s providers=%s\n", account, info.URL, strings.Join(providers, ","))
	return info
}

func (m *mile) bootScheduler() {
	m.t.Helper()
	port := freePort(m.t)
	m.sched = "http://127.0.0.1:" + strconv.Itoa(port)
	hosts := []any{}
	for _, spec := range []struct{ id, op, role string }{
		{"p1", "op-p1", "replica"}, {"p2", "op-p2", "replica"}, {"p3", "op-p3", "replica"}, {"p4", "op-p4", "spare"},
	} {
		hosts = append(hosts, map[string]any{
			"provider_id": spec.id, "endpoint": "https://127.0.0.1:1", "ca_file": filepath.Join(m.work, "missing.crt"),
			"declared_operator": spec.op, "declared_failure_domain": "sim-" + spec.id, "simulated_labels": true,
			"policy_id": "policy-1", "declared_capacity_bytes": 50000000, "role": spec.role,
		})
	}
	cfg := map[string]any{
		"listen": "127.0.0.1:" + strconv.Itoa(port), "indexer_url": m.indexer, "policy_id": "policy-1",
		"physical_failure_domain": "vm-1", "stage_token": "milestone-source", "package_root": m.work,
		"confirm_seconds": 90, "offer_commit_url": m.offerHook(), "hosts": hosts,
	}
	path := filepath.Join(m.work, "scheduler.json")
	writeJSON(m.t, path, cfg)
	c := m.start("scheduler", m.bins["svn-scheduler"], "serve", path)
	m.schedProc = c
	m.stage = strings.TrimSpace(strings.TrimPrefix(m.waitPrefixed(c, "stage_url=", 20*time.Second), "stage_url="))
	m.stageCA = strings.TrimSpace(strings.TrimPrefix(m.waitPrefixed(c, "stage_ca=", 20*time.Second), "stage_ca="))
	fmt.Printf("phase=scheduler url=%s stage=%s\n", m.sched, m.stage)
}

func (m *mile) offerHook() string {
	m.t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		m.t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(m.commitOffer)}
	go func() { _ = srv.Serve(ln) }()
	m.t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String() + "/offers"
}
