package devnet_test

// Process control for the broadcaster devnet tests: building the
// svn-broadcaster and svnd binaries, starting broadcaster subcommands under
// a chosen uid, and stopping or killing them. Every process is a real
// binary; nothing here fakes a node, a database or the broadcaster.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// bcIdentity is a process identity: the uid (and gid) a broadcaster
// subcommand runs under.
type bcIdentity struct{ uid uint32 }

// cred is the credential that runs a process as id, with no supplementary
// groups; nil when id is the test's own uid.
func (id bcIdentity) cred() *syscall.Credential {
	if id.uid == uint32(os.Geteuid()) {
		return nil
	}
	return &syscall.Credential{Uid: id.uid, Gid: id.uid, Groups: []uint32{}}
}

// bcUnusedUIDs returns n numeric uids that have no passwd or group entry
// and own no process. The range differs from the command smoke test's, so
// the two never pick the same uid.
func bcUnusedUIDs(t *testing.T, n int) []uint32 {
	t.Helper()
	used := map[uint32]bool{}
	procs, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range procs {
		info, err := os.Stat(filepath.Join("/proc", p.Name()))
		if err != nil {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			used[st.Uid] = true
		}
	}
	var out []uint32
	for uid := uint32(3_730_001); len(out) < n && uid < 3_740_000; uid++ {
		if used[uid] {
			continue
		}
		if _, err := user.LookupId(strconv.FormatUint(uint64(uid), 10)); err == nil {
			continue
		}
		if _, err := user.LookupGroupId(strconv.FormatUint(uint64(uid), 10)); err == nil {
			continue
		}
		out = append(out, uid)
	}
	if len(out) != n {
		t.Fatalf("found %d unused uids, want %d", len(out), n)
	}
	return out
}

// bcBuffer is a bounded bytes.Buffer safe for one writer and concurrent
// readers. It keeps the first bcBufferCap bytes and counts the rest.
type bcBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	dropped int
}

const bcBufferCap = 4 << 20

func (b *bcBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	room := bcBufferCap - b.buf.Len()
	if room < len(p) {
		if room > 0 {
			b.buf.Write(p[:room])
		}
		b.dropped += len(p) - max(room, 0)
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *bcBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// bcProc is one started broadcaster subcommand. Its standard output is kept
// in memory (subcommands print one JSON result); its standard error, the
// redacting JSON log, goes to a file under the run's log directory so the
// log canary scan of phase L reads every byte it wrote.
type bcProc struct {
	name    string
	cmd     *exec.Cmd
	stdout  *bcBuffer
	logPath string
	done    chan struct{}
	err     error
}

// bcLauncher starts broadcaster subcommands of one run.
type bcLauncher struct {
	t      *testing.T
	bin    string
	logDir string
	env    []string
	mu     sync.Mutex
	procs  []*bcProc
	serial int
}

// bcChildEnv is the base environment of every broadcaster child: no PG*
// variable and nothing else of the test's environment.
var bcChildEnv = []string{"PATH=/usr/bin:/bin", "TZ=UTC", "LC_ALL=C.UTF-8"}

// start starts the binary with args under id, with extra environment
// entries appended to the base environment.
func (l *bcLauncher) start(name string, id bcIdentity, extraEnv []string, args ...string) *bcProc {
	l.t.Helper()
	l.mu.Lock()
	l.serial++
	logPath := filepath.Join(l.logDir, fmt.Sprintf("%03d-%s.log", l.serial, name))
	l.mu.Unlock()
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		l.t.Fatal(err)
	}
	p := &bcProc{name: name, stdout: &bcBuffer{}, logPath: logPath, done: make(chan struct{})}
	p.cmd = exec.Command(l.bin, args...)
	p.cmd.Env = append(append([]string(nil), l.env...), extraEnv...)
	p.cmd.Dir = "/"
	p.cmd.Stdout = p.stdout
	p.cmd.Stderr = logFile
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Credential: id.cred(), Setpgid: true, Pdeathsig: syscall.SIGKILL}
	if err := p.cmd.Start(); err != nil {
		_ = logFile.Close()
		l.t.Fatalf("start %s: %v", name, err)
	}
	go func() {
		p.err = p.cmd.Wait()
		_ = logFile.Close()
		close(p.done)
	}()
	l.mu.Lock()
	l.procs = append(l.procs, p)
	l.mu.Unlock()
	l.t.Cleanup(func() {
		select {
		case <-p.done:
			return
		default:
		}
		// A test that stopped early: SIGTERM first, so dev-db removes its
		// cluster and the signer its socket; SIGKILL of the process group
		// after 30 s.
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-p.done:
		case <-time.After(30 * time.Second):
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			<-p.done
		}
	})
	return p
}

// run runs the binary to completion, bounded by two minutes, and returns
// its exit code.
func (l *bcLauncher) run(name string, id bcIdentity, args ...string) (int, *bcProc) {
	l.t.Helper()
	p := l.start(name, id, nil, args...)
	select {
	case <-p.done:
	case <-time.After(2 * time.Minute):
		l.t.Fatalf("%s did not finish: %s", name, p.logTail(20))
	}
	return p.exitCode(), p
}

// exitCode returns a finished process's exit code (-1 for a signal).
func (p *bcProc) exitCode() int {
	if p.err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(p.err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// exited reports whether the process has exited.
func (p *bcProc) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// log returns everything the process wrote to its standard error so far.
func (p *bcProc) log() string {
	raw, err := os.ReadFile(p.logPath)
	if err != nil {
		return fmt.Sprintf("(log %s: %v)", p.logPath, err)
	}
	return string(raw)
}

// logTail returns the last n lines of the process log.
func (p *bcProc) logTail(n int) string {
	lines := strings.Split(strings.TrimRight(p.log(), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// stop sends SIGTERM and waits, bounded by a minute, for the exit code.
func (p *bcProc) stop(t *testing.T) int {
	t.Helper()
	if p.exited() {
		return p.exitCode()
	}
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("signal %s: %v", p.name, err)
	}
	select {
	case <-p.done:
	case <-time.After(time.Minute):
		t.Fatalf("%s did not stop on SIGTERM: %s", p.name, p.logTail(20))
	}
	return p.exitCode()
}

// kill sends SIGKILL to the process group and waits for the exit: a
// process crash, not a host power loss.
func (p *bcProc) kill(t *testing.T) {
	t.Helper()
	if p.exited() {
		t.Fatalf("%s exited before the kill: %d %s", p.name, p.exitCode(), p.logTail(20))
	}
	if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill %s: %v", p.name, err)
	}
	select {
	case <-p.done:
	case <-time.After(30 * time.Second):
		t.Fatalf("%s did not exit on SIGKILL", p.name)
	}
}

// bcWait polls cond every 100 ms until it holds, failing the test after
// timeout or when one of procs exits first.
func bcWait(t *testing.T, what string, timeout time.Duration, cond func() bool, procs ...*bcProc) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		for _, p := range procs {
			if p != nil && p.exited() {
				t.Fatalf("waiting for %s: %s exited %d: %s", what, p.name, p.exitCode(), p.logTail(30))
			}
		}
		if time.Now().After(deadline) {
			var tails []string
			for _, p := range procs {
				if p != nil {
					tails = append(tails, p.name+":\n"+p.logTail(15))
				}
			}
			t.Fatalf("timed out after %s waiting for %s\n%s", timeout, what, strings.Join(tails, "\n"))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// bcBuildBinaries builds svn-broadcaster (with -race when this test binary
// has it, and with -tags svn_faultpoint so the build-tagged fault points of
// phases WP1-WP3 and SG exist) and the production svnd (without -race; the
// validators are not the code under test) into dir. GOPROXY=off: the network
// namespace has no network, so the module cache must hold every module. A
// build failure fails the test; it never skips.
func bcBuildBinaries(t *testing.T, dir string) (broadcaster, svnd string) {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("the go command builds the binaries under test: %v", err)
	}
	module, err := filepath.Abs(filepath.Join("..", "..", "services", "broadcaster"))
	if err != nil {
		t.Fatal(err)
	}
	broadcaster = filepath.Join(dir, "svn-broadcaster")
	svnd = filepath.Join(dir, "svnd")
	args := []string{"build", "-o", broadcaster, "-tags", "svn_faultpoint"}
	if broadcasterRace {
		args = append(args, "-race")
	}
	builds := []*exec.Cmd{
		exec.Command(goTool, append(args, "./cmd/svn-broadcaster")...),
		exec.Command(goTool, "build", "-o", svnd, "../cmd/svnd"),
	}
	builds[0].Dir = module
	started := time.Now()
	errs := make([]error, len(builds))
	var wg sync.WaitGroup
	for i, cmd := range builds {
		cmd.Env = append(os.Environ(), "GOPROXY=off")
		wg.Add(1)
		go func() {
			defer wg.Done()
			if out, err := cmd.CombinedOutput(); err != nil {
				errs[i] = fmt.Errorf("%s: %v\n%s", strings.Join(cmd.Args, " "), err, out)
			}
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, bin := range []string{broadcaster, svnd} {
		if err := os.Chmod(bin, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fmt.Printf("phase=build seconds=%.1f broadcaster_race=%t broadcaster_tags=svn_faultpoint svnd_race=false\n",
		time.Since(started).Seconds(), broadcasterRace)
	return broadcaster, svnd
}

// bcMkdir creates dir with mode, owned by id.
func bcMkdir(t *testing.T, dir string, mode os.FileMode, id bcIdentity) {
	t.Helper()
	if err := os.Mkdir(dir, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
	if id.uid != uint32(os.Geteuid()) {
		if err := os.Chown(dir, int(id.uid), int(id.uid)); err != nil {
			t.Fatal(err)
		}
	}
}

// bcWriteFile writes data to a new file with mode, owned by id.
func bcWriteFile(t *testing.T, path string, data []byte, mode os.FileMode, id bcIdentity) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Chmod(mode)
	}
	if werr == nil && id.uid != uint32(os.Geteuid()) {
		werr = f.Chown(int(id.uid), int(id.uid))
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		t.Fatal(werr)
	}
}

// bcWriteJSON writes v as a new 0644 configuration file owned by the test.
func bcWriteJSON(t *testing.T, path string, v any) {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	bcWriteFile(t, path, raw, 0o644, bcIdentity{uint32(os.Geteuid())})
}
