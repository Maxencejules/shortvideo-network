package msgkittest

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestNewContextMountsRealStores(t *testing.T) {
	ctx, keys := NewContext(t, "providers", "content")
	if len(keys) != 2 || keys["providers"] == nil || keys["content"] == nil {
		t.Fatalf("keys = %v", keys)
	}
	if ctx.ChainID() != ChainID || ctx.BlockHeight() != BlockHeight || ctx.BlockTime().Unix() != BlockUnixSeconds {
		t.Fatalf("header chain=%q height=%d time=%d", ctx.ChainID(), ctx.BlockHeight(), ctx.BlockTime().Unix())
	}
	if ctx.GasMeter().Limit() != GasLimit || len(ctx.TxBytes()) != TxBytesLen {
		t.Fatalf("gas limit %d tx bytes %d", ctx.GasMeter().Limit(), len(ctx.TxBytes()))
	}
	if ctx.ExecMode() != sdk.ExecModeFinalize || ctx.IsCheckTx() {
		t.Fatalf("exec mode %v check %v", ctx.ExecMode(), ctx.IsCheckTx())
	}
	// Writes through the gas-metered context land in the IAVL store and
	// stay isolated per store key.
	ctx.KVStore(keys["providers"]).Set([]byte{0x01, 'a'}, []byte("v"))
	if got := ctx.KVStore(keys["providers"]).Get([]byte{0x01, 'a'}); !bytes.Equal(got, []byte("v")) {
		t.Fatalf("read back %q", got)
	}
	if ctx.KVStore(keys["content"]).Has([]byte{0x01, 'a'}) {
		t.Fatal("write leaked into another store")
	}
	if ctx.GasMeter().GasConsumed() == 0 {
		t.Fatal("store access was not gas metered")
	}
	cms, ok := ctx.MultiStore().(storetypes.CommitMultiStore)
	if !ok {
		t.Fatalf("multistore is %T, want a CommitMultiStore", ctx.MultiStore())
	}
	commit := cms.Commit()
	if commit.Version != 1 || len(commit.Hash) != 32 {
		t.Fatalf("commit %+v", commit)
	}
}

// fatalRecorder turns Fatalf into a recoverable panic so a test can check
// that a helper refuses bad input.
type fatalRecorder struct {
	testing.TB
}

type fatalSignal struct{ message string }

func (r fatalRecorder) Helper() {}

func (r fatalRecorder) Fatal(args ...any) { panic(fatalSignal{fmt.Sprint(args...)}) }

func (r fatalRecorder) Fatalf(format string, args ...any) {
	panic(fatalSignal{fmt.Sprintf(format, args...)})
}

func expectFatal(t *testing.T, want string, fn func(tb testing.TB)) {
	t.Helper()
	defer func() {
		got := recover()
		signal, ok := got.(fatalSignal)
		if !ok {
			t.Fatalf("expected a fatal containing %q, got %v", want, got)
		}
		if !strings.Contains(signal.message, want) {
			t.Fatalf("fatal %q does not contain %q", signal.message, want)
		}
	}()
	fn(fatalRecorder{t})
}

func TestNewContextRejectsBadNames(t *testing.T) {
	expectFatal(t, "mounted twice", func(tb testing.TB) { NewContext(tb, "a", "a") })
	expectFatal(t, "empty store name", func(tb testing.TB) { NewContext(tb, "") })
}

func writeGo(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestScanDeterministicFindsEveryForbiddenSource(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "bad.go", `package bad

import (
	crand "crypto/rand"
	"math/rand"
	mrand2 "math/rand/v2"
	"net/http"
	"net/url"
	"os"
	clock "time"
)

var _ = crand.Reader
var _ = rand.Int
var _ = mrand2.Int
var _ = http.Get
var _ = url.Parse
var _ = os.Getenv

func when() clock.Time { return clock.Now() }

func since(t clock.Time) clock.Duration { return clock.Since(t) }

func ratio(a, b int) float64 { return float64(a) / float64(b) }

var c complex128
`)
	writeGo(t, dir, "dot.go", `package bad

import . "time"

var _ = Second
`)
	writeGo(t, dir, "floats.go", `package bad

import (
	"math"
	"math/big"
	conv "strconv"
	"time"
)

func scale(n int) int { return int(0.75 * 4) * n }

var exponent = 1e3

var imaginary = 2i

var root = math.Sqrt

var huge = math.Inf

var bits = math.Float64bits

var limit = math.MaxUint64

var bigFloat = big.NewFloat

var parsed, _ = conv.ParseFloat("1", 64)

var ticks = time.Tick

var timer = time.NewTimer

var later = time.After

var callLater = time.AfterFunc

var ticker = time.NewTicker

var nap = time.Sleep
`)
	writeGo(t, dir, "dotmath.go", `package bad

import . "math"

var _ = MaxInt64
`)
	writeGo(t, dir, "host.go", `package bad

import (
	"hash/maphash"
	"io/fs"
	"os/signal"
	"os/user"
	"path/filepath"
	"plugin"
	"runtime"
	"runtime/debug"
	"syscall/js"
	"time"
)

var seed = maphash.MakeSeed

var _ fs.FS

var _ = signal.Notify

var who, _ = user.Current()

var files, _ = filepath.Glob("/etc/*")

var walk = filepath.WalkDir

var walkOld = filepath.Walk

var abs = filepath.Abs

var links = filepath.EvalSymlinks

var joined = filepath.Join("a", "b")

var _ = plugin.Open

var cpus = runtime.NumCPU()

var _ = debug.Stack

var _ = js.Global

var zone = time.Local

var loaded, _ = time.LoadLocation("UTC")
`)
	writeGo(t, dir, "dotfilepath.go", `package bad

import . "path/filepath"

var _ = Base
`)
	// Test files are not scanned.
	writeGo(t, dir, "bad_test.go", `package bad

import "os"

var _ = os.Getenv
`)
	findings, scanned, err := scanDeterministic(dir)
	if err != nil {
		t.Fatal(err)
	}
	if scanned != 6 {
		t.Fatalf("scanned %d files, want 6", scanned)
	}
	joined := strings.Join(findings, "\n")
	for _, want := range []string{
		`imports "crypto/rand"`,
		`imports "math/rand"`,
		`imports "math/rand/v2"`,
		`imports "net/http"`,
		`imports "net/url"`,
		`imports "os"`,
		"uses time.Now",
		"uses time.Since",
		"uses float64",
		"uses complex128",
		`dot-imports "time"`,
		`dot-imports "math"`,
		"uses the literal 0.75",
		"uses the literal 1e3",
		"uses the literal 2i",
		"uses floating-point math.Sqrt",
		"uses floating-point math.Inf",
		"uses floating-point math.Float64bits",
		"uses math/big.NewFloat",
		"uses strconv.ParseFloat",
		"uses time.Tick",
		"uses time.NewTimer",
		"uses time.After",
		"uses time.AfterFunc",
		"uses time.NewTicker",
		"uses time.Sleep",
		`imports "hash/maphash"`,
		`imports "io/fs"`,
		`imports "os/signal"`,
		`imports "os/user"`,
		`imports "plugin"`,
		`imports "runtime"`,
		`imports "runtime/debug"`,
		`imports "syscall/js"`,
		"uses filepath.Glob",
		"uses filepath.WalkDir",
		"uses filepath.Walk",
		"uses filepath.Abs",
		"uses filepath.EvalSymlinks",
		"uses time.Local",
		"uses time.LoadLocation",
		`dot-imports "path/filepath"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing finding %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "bad_test.go") {
		t.Fatalf("test file scanned:\n%s", joined)
	}
	// Integer constants of package math are not floating-point, and
	// path arithmetic of path/filepath reads no file.
	if strings.Contains(joined, "math.MaxUint64") || strings.Contains(joined, "filepath.Join") {
		t.Fatalf("math.MaxUint64 or filepath.Join flagged:\n%s", joined)
	}
	// "net/url" is under the net tree; "osx"-style prefixes are not os.
	for _, path := range []string{"os", "os/exec", "net", "net/url", "runtime", "runtime/debug", "syscall", "syscall/js"} {
		if !forbiddenImport(path) {
			t.Errorf("%s is not forbidden", path)
		}
	}
	for _, path := range []string{"osx", "netip", "runtimes", "path/filepath", "io", "hash/crc32", "time"} {
		if forbiddenImport(path) {
			t.Errorf("%s is forbidden", path)
		}
	}
}

func TestScanDeterministicAcceptsCleanSource(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "clean.go", `package clean

import (
	"math"
	"sort"
	"strconv"
	"time"
)

// Block time arrives as a value; time.Unix is not a clock read.
func at(seconds int64) time.Time { return time.Unix(seconds, 0).UTC() }

func order(xs []string) { sort.Strings(xs) }

func last(n uint64) bool { return n == math.MaxUint64 || int64(n) > math.MaxInt64-1 }

func text(n uint64) string { return strconv.FormatUint(n, 10) }

const hex = 0x1e3
`)
	findings, scanned, err := scanDeterministic(dir)
	if err != nil {
		t.Fatal(err)
	}
	if scanned != 1 || len(findings) != 0 {
		t.Fatalf("scanned %d, findings %v", scanned, findings)
	}
}

func TestAssertDeterministicSourcesFailsOnEmptyDir(t *testing.T) {
	expectFatal(t, "no non-test Go files", func(tb testing.TB) { AssertDeterministicSources(tb, t.TempDir()) })
	expectFatal(t, "determinism scan", func(tb testing.TB) {
		AssertDeterministicSources(tb, filepath.Join(t.TempDir(), "missing"))
	})
}

func TestScannerFlagsTestSupportPackage(t *testing.T) {
	// msgkittest reads files and the working directory on purpose; it is
	// test support and is not chain code. The scanner must say so.
	findings, scanned, err := scanDeterministic(".")
	if err != nil {
		t.Fatal(err)
	}
	if scanned == 0 || len(findings) == 0 {
		t.Fatalf("expected findings for the os import in test support, got %d files %v", scanned, findings)
	}
}

func TestForbiddenNamePatternsLoadBounds(t *testing.T) {
	patterns, exemptions := ForbiddenNamePatterns(t)
	for _, want := range []string{"ip", "addr", "email", "username", "media", "video_bytes", "fee", "token"} {
		found := false
		for _, p := range patterns {
			found = found || p == want
		}
		if !found {
			t.Fatalf("pattern %q missing from %v", want, patterns)
		}
	}
	if len(exemptions) == 0 {
		t.Fatal("no exemptions loaded")
	}
}

// Expected hits were produced by the goal-02 pattern_matches function in
// docs/evidence/goal-02/check_protocol.py over the same bounds.json.
func TestNameHitsMatchesGoal02Rule(t *testing.T) {
	patterns, exemptions := ForbiddenNamePatterns(t)
	cases := map[string][]string{
		"client_ip":                       {"ip"},
		"ship":                            nil,
		"Ship_IP":                         {"ip"},
		"ipv6_addr":                       {"ip", "addr"},
		"address":                         nil,
		"remote_addr":                     {"addr"},
		"uploader_ip":                     {"ip", "uploader_ip"},
		"ERROR_CODE_FEE_REJECTED":         nil,
		"fee_amount":                      {"fee"},
		"SIGNATURE_DOMAIN_MEDIA_MANIFEST": nil,
		"media_manifest":                  {"media"},
		"provider_id":                     nil,
		"manifest_sha256":                 nil,
		"key_generation":                  nil,
		"operator_group_id":               nil,
		"submitting_host":                 nil,
		"history_erased":                  nil,
		"content_version":                 nil,
		"IPAddress":                       {"ip"},
		"creator_email":                   {"email"},
		"Username":                        {"username"},
		"watch_event_count":               {"watch_event"},
		"complaint_text":                  {"complaint"},
		"stake":                           {"stake"},
		"mintModuleEnabled":               nil,
		"mint_module_enabled":             nil,
		"geo_region":                      {"geo"},
		"tip":                             nil,
		"IP":                              {"ip"},
		"ipx":                             nil,
		"x_ip":                            {"ip"},
		"package_bytes":                   nil,
		"deadline_unix_seconds":           nil,
		"released_bytes":                  nil,
		"state":                           nil,
	}
	for name, want := range cases {
		got := NameHits(name, patterns, exemptions)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("NameHits(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestFindCanaryForms(t *testing.T) {
	canary := []byte("CANARY-MEDIA-PAYLOAD-7f3a91")
	other := []byte("creator-canary@example.invalid")
	filler := bytes.Repeat([]byte{0x5a, 0x00, 0xff}, 11)

	if _, found := FindCanary(filler, canary, other); found {
		t.Fatal("canary found in filler")
	}
	if form, found := FindCanary(append(append([]byte{}, filler...), canary...), canary); !found || !strings.Contains(form, "raw") {
		t.Fatalf("raw: %q %v", form, found)
	}
	if form, found := FindCanary([]byte("x"+hex.EncodeToString(other)+"y"), canary, other); !found || !strings.Contains(form, "canary 1 (hex)") {
		t.Fatalf("hex: %q %v", form, found)
	}
	if form, found := FindCanary([]byte(strings.ToUpper(hex.EncodeToString(canary))), canary); !found || !strings.Contains(form, "HEX") {
		t.Fatalf("HEX: %q %v", form, found)
	}
	for shift := 0; shift < 3; shift++ {
		buffer := append(append(append([]byte{}, filler[:shift+3]...), canary...), filler...)
		for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding, base64.RawStdEncoding} {
			blob := []byte(`{"value":"` + encoding.EncodeToString(buffer) + `"}`)
			if form, found := FindCanary(blob, canary); !found || !strings.Contains(form, "base64") {
				t.Fatalf("base64 shift %d: %q %v", shift, form, found)
			}
		}
	}
	if form, found := FindCanary([]byte("anything"), canary, nil); !found || !strings.Contains(form, "empty") {
		t.Fatalf("empty canary must fail closed: %q %v", form, found)
	}
}
