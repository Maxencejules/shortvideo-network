// Package pgdev runs a disposable PostgreSQL cluster for development and
// tests: initdb, then the postgres server as a direct child process.
//
// This is the goal 11 cluster starter, copied into the indexer module so
// the indexer does not import the broadcaster. The rules are unchanged:
// a directory under os.TempDir(), a Unix socket only, and no TCP port.
//
// Layout. Start creates one directory with os.MkdirTemp(os.TempDir(),
// "svn-pg-*") and refuses any other place. Inside it are data/ (PGDATA,
// created by initdb), run/ (the Unix socket directory) and postgres.log (the
// server's standard error). The server listens on its Unix socket only
// (listen_addresses is empty), so it opens no TCP port.
//
// Identity. When the calling process has euid 0, initdb and postgres run as
// the operating-system user "postgres" through SysProcAttr.Credential with an
// empty supplementary group list, because both refuse to run as root, and the
// directory is chowned to that user. Otherwise they run as the calling user.
// Start refuses when an ancestor of the directory is not traversable by the
// server user, for example a TMPDIR under a mode-0700 directory of another
// user.
//
// Lifetime. postgres starts in its own process group with Pdeathsig set to
// SIGKILL, from a goroutine locked to its OS thread that stays alive until the
// server exits: the kernel kills the server when the thread that started it
// dies, which happens when the calling process dies. Stop sends SIGINT, which
// is PostgreSQL's fast shutdown, and waits. Remove stops the server and
// deletes the directory. This package has no crash hooks; immediate stops and
// SIGKILL of the server are test code in internal/pgtest.
//
// Logging (decision request DR-11-37). The server runs with
// log_min_error_statement=panic, log_statement=none and
// log_parameter_max_length_on_error=0, so it never logs statement text or bind
// parameters, and with log_error_verbosity=terse and log_min_messages=fatal,
// so error messages, which can quote an input value (for example "invalid
// input syntax for type interval"), are returned to the client and not
// written to the server log.
//
// Credentials. Start generates the superuser password (24 random bytes, hex),
// hands it to initdb through a mode-0600 file that is removed right after, and
// keeps it in memory only. CreateLoginRole generates role passwords the same
// way and returns them; nothing is written to disk.
package pgdev

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lib/pq"
)

const (
	// DirPrefix is the name prefix of every cluster directory under
	// os.TempDir().
	DirPrefix = "svn-pg-"
	// Superuser is the cluster superuser that initdb creates.
	Superuser = "svn_super"
	// Port is the port number in the socket file name (.s.PGSQL.5432). No TCP
	// port is opened.
	Port = 5432
	// ServerUser is the operating-system user the server runs as when the
	// calling process is root.
	ServerUser = "postgres"
	// MinStartTimeout and MaxStartTimeout bound Options.StartTimeout.
	MinStartTimeout = time.Second
	MaxStartTimeout = 5 * time.Minute

	// stopTimeout bounds a fast stop before the process group is killed.
	stopTimeout = 30 * time.Second
	// socketPathMax is the longest socket path a sockaddr_un holds, without
	// its terminating NUL.
	socketPathMax = 107
	// outputCap bounds the initdb and version output kept for error messages.
	outputCap = 16 << 10
	// logTailBytes is how much of the server log a start error quotes.
	logTailBytes = 2 << 10
	// passwordBytes is the number of random bytes in a generated password.
	passwordBytes = 24
)

// Errors returned by this package. Their text never includes a password.
var (
	ErrOptions        = errors.New("pgdev: invalid options")
	ErrDirectory      = errors.New("pgdev: cluster directory must be os.TempDir()/" + DirPrefix + "*")
	ErrNotTraversable = errors.New("pgdev: an ancestor directory is not traversable by the server user")
	ErrRunning        = errors.New("pgdev: server is running")
	ErrNotRunning     = errors.New("pgdev: server is not running")
	ErrName           = errors.New("pgdev: role or database name outside ^[a-z_][a-z0-9_]{0,62}$")
	ErrStart          = errors.New("pgdev: server did not become ready")
)

// nameRE is the rule for role and database names this package creates.
var nameRE = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// serverSettings are the -c settings of every server start.
var serverSettings = []string{
	"listen_addresses=",
	"port=" + strconv.Itoa(Port),
	"unix_socket_permissions=0777",
	"fsync=on",
	"logging_collector=off",
	"log_destination=stderr",
	"log_min_messages=fatal",
	"log_min_error_statement=panic",
	"log_statement=none",
	"log_parameter_max_length_on_error=0",
	"log_error_verbosity=terse",
	"log_connections=off",
	"log_disconnections=off",
}

// childEnv is the whole environment of initdb and postgres. Nothing of the
// caller's environment is passed on.
var childEnv = []string{"LC_ALL=C.UTF-8", "TZ=UTC"}

// Options configures Start.
type Options struct {
	// BinDir is the absolute directory that holds initdb and postgres.
	BinDir string
	// StartTimeout bounds initdb and each wait for the server to accept
	// connections; it must be within [MinStartTimeout, MaxStartTimeout].
	StartTimeout time.Duration
}

// Cluster is one disposable cluster.
type Cluster struct {
	version   string
	binDir    string
	dir       string
	dataDir   string
	socketDir string
	logPath   string
	timeout   time.Duration
	cred      *syscall.Credential
	superPass string

	mu      sync.Mutex
	proc    *process
	removed bool
}

// process is one running child.
type process struct {
	pid    int
	exited chan struct{}
	err    error // written before exited is closed
}

// Start creates the directory, runs initdb and starts the server. On any
// error it removes everything it created.
func Start(ctx context.Context, opts Options) (*Cluster, error) {
	if !filepath.IsAbs(opts.BinDir) || filepath.Clean(opts.BinDir) != opts.BinDir {
		return nil, fmt.Errorf("%w: BinDir must be a clean absolute path", ErrOptions)
	}
	if opts.StartTimeout < MinStartTimeout || opts.StartTimeout > MaxStartTimeout {
		return nil, fmt.Errorf("%w: StartTimeout outside [%s, %s]", ErrOptions, MinStartTimeout, MaxStartTimeout)
	}
	version, err := Version(ctx, opts.BinDir)
	if err != nil {
		return nil, err
	}
	cred, err := serverCredential()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", DirPrefix)
	if err != nil {
		return nil, fmt.Errorf("pgdev: create cluster directory: %w", err)
	}
	c := &Cluster{
		version:   version,
		binDir:    opts.BinDir,
		dir:       dir,
		dataDir:   filepath.Join(dir, "data"),
		socketDir: filepath.Join(dir, "run"),
		logPath:   filepath.Join(dir, "postgres.log"),
		timeout:   opts.StartTimeout,
		cred:      cred,
	}
	if err := c.create(ctx); err != nil {
		if rmErr := c.Remove(); rmErr != nil {
			return nil, errors.Join(err, rmErr)
		}
		return nil, err
	}
	return c, nil
}

// create prepares the directory, runs initdb and starts the server.
func (c *Cluster) create(ctx context.Context) error {
	if err := ValidateDir(c.dir); err != nil {
		return err
	}
	uid, gid, groups := c.serverIDs()
	if err := checkTraversable(c.dir, uid, gid, groups); err != nil {
		return err
	}
	if err := os.Chmod(c.dir, 0o711); err != nil {
		return fmt.Errorf("pgdev: chmod cluster directory: %w", err)
	}
	if err := os.Mkdir(c.socketDir, 0o711); err != nil {
		return fmt.Errorf("pgdev: create socket directory: %w", err)
	}
	if err := os.Chmod(c.socketDir, 0o711); err != nil {
		return fmt.Errorf("pgdev: chmod socket directory: %w", err)
	}
	pass, err := newPassword()
	if err != nil {
		return err
	}
	c.superPass = pass
	pwfile := filepath.Join(c.dir, "pwfile")
	if err := os.WriteFile(pwfile, []byte(pass+"\n"), 0o600); err != nil {
		return fmt.Errorf("pgdev: write initdb password file: %w", err)
	}
	if c.cred != nil {
		for _, p := range []string{c.dir, c.socketDir, pwfile} {
			if err := os.Chown(p, int(c.cred.Uid), int(c.cred.Gid)); err != nil {
				return fmt.Errorf("pgdev: chown %s: %w", filepath.Base(p), err)
			}
		}
	}
	initErr := c.initdb(ctx, pwfile)
	if err := os.Remove(pwfile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.Join(initErr, fmt.Errorf("pgdev: remove initdb password file: %w", err))
	}
	if initErr != nil {
		return initErr
	}
	return c.Start(ctx)
}

// initdb runs initdb with the start.sh flags and waits for it.
func (c *Cluster) initdb(ctx context.Context, pwfile string) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	out := &capBuffer{max: outputCap}
	cmd := exec.Command(filepath.Join(c.binDir, "initdb"),
		"-D", c.dataDir,
		"--username="+Superuser,
		"--pwfile="+pwfile,
		"--auth-local=scram-sha-256",
		"--auth-host=scram-sha-256",
		"--encoding=UTF8",
		"--locale=C.UTF-8",
	)
	cmd.Dir = c.dir
	cmd.Env = childEnv
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.SysProcAttr = c.sysProcAttr()
	p, err := startLocked(cmd)
	if err != nil {
		return fmt.Errorf("pgdev: start initdb: %w", err)
	}
	select {
	case <-p.exited:
	case <-ctx.Done():
		if err := killGroup(p.pid); err != nil {
			return errors.Join(ctx.Err(), err)
		}
		<-p.exited
		return fmt.Errorf("pgdev: initdb: %w", ctx.Err())
	}
	if p.err != nil {
		return fmt.Errorf("pgdev: initdb: %w: %s", p.err, out.String())
	}
	return nil
}

// Start starts the server on the existing data directory and waits until it
// accepts connections. It is used by the first start and by restarts.
func (c *Cluster) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.removed {
		return fmt.Errorf("pgdev: cluster removed")
	}
	if c.proc != nil {
		select {
		case <-c.proc.exited:
		default:
			return ErrRunning
		}
	}
	logFile, err := os.OpenFile(c.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("pgdev: open server log: %w", err)
	}
	args := []string{"-D", c.dataDir, "-c", "unix_socket_directories=" + c.socketDir}
	for _, s := range serverSettings {
		args = append(args, "-c", s)
	}
	cmd := exec.Command(filepath.Join(c.binDir, "postgres"), args...)
	cmd.Dir = c.dir
	cmd.Env = childEnv
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = c.sysProcAttr()
	p, err := startLocked(cmd)
	closeErr := logFile.Close()
	if err != nil {
		return errors.Join(fmt.Errorf("pgdev: start postgres: %w", err), closeErr)
	}
	c.proc = p
	if closeErr != nil {
		return errors.Join(fmt.Errorf("pgdev: close server log: %w", closeErr), c.stopLocked())
	}
	if err := c.waitReady(ctx, p); err != nil {
		return errors.Join(err, c.stopLocked())
	}
	return nil
}

// waitReady polls a superuser connection until it succeeds, the server exits,
// or the start timeout passes.
func (c *Cluster) waitReady(ctx context.Context, p *process) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	for {
		db, err := c.superuserDB("postgres")
		if err != nil {
			return err
		}
		pingCtx, pingCancel := context.WithTimeout(ctx, time.Second)
		pingErr := db.PingContext(pingCtx)
		pingCancel()
		closeErr := db.Close()
		if pingErr == nil {
			return closeErr
		}
		select {
		case <-p.exited:
			return fmt.Errorf("%w: server exited (%v); log tail: %s", ErrStart, p.err, c.logTail())
		case <-ctx.Done():
			return fmt.Errorf("%w: %v; log tail: %s", ErrStart, ctx.Err(), c.logTail())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// Stop shuts the server down with SIGINT (fast shutdown) and waits for it to
// exit; after stopTimeout it kills the process group.
func (c *Cluster) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stopLocked()
}

func (c *Cluster) stopLocked() error {
	p := c.proc
	if p == nil {
		return nil
	}
	select {
	case <-p.exited:
		return nil
	default:
	}
	if err := syscall.Kill(p.pid, syscall.SIGINT); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("pgdev: signal postgres: %w", err)
	}
	select {
	case <-p.exited:
		return nil
	case <-time.After(stopTimeout):
	}
	if err := killGroup(p.pid); err != nil {
		return err
	}
	<-p.exited
	return nil
}

// Remove stops the server and deletes the cluster directory.
func (c *Cluster) Remove() error {
	stopErr := c.Stop()
	c.mu.Lock()
	c.removed = true
	c.mu.Unlock()
	if err := os.RemoveAll(c.dir); err != nil {
		return errors.Join(stopErr, fmt.Errorf("pgdev: remove cluster directory: %w", err))
	}
	return stopErr
}

// Postmaster returns the pid of the running server and a channel closed when
// it exits. ok is false when no server was started.
func (c *Cluster) Postmaster() (pid int, exited <-chan struct{}, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.proc == nil {
		return 0, nil, false
	}
	return c.proc.pid, c.proc.exited, true
}

// Version returns the output of `postgres --version`, for example
// "postgres (PostgreSQL) 18.6 (Ubuntu 18.6-1.pgdg24.04+2)".
func Version(ctx context.Context, binDir string) (string, error) {
	out := &capBuffer{max: outputCap}
	cmd := exec.CommandContext(ctx, filepath.Join(binDir, "postgres"), "--version")
	cmd.Env = childEnv
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("pgdev: postgres --version: %w: %s", err, out.String())
	}
	return strings.TrimSpace(out.String()), nil
}

// Version returns the server version string read at Start.
func (c *Cluster) Version() string { return c.version }

// Dir returns the cluster directory.
func (c *Cluster) Dir() string { return c.dir }

// SocketDir returns the Unix socket directory.
func (c *Cluster) SocketDir() string { return c.socketDir }

// LogPath returns the server log file.
func (c *Cluster) LogPath() string { return c.logPath }

// SuperuserDB opens a connection pool to database as the cluster superuser.
// Only cluster bootstrap and tests use it.
func (c *Cluster) SuperuserDB(database string) (*sql.DB, error) {
	if !nameRE.MatchString(database) {
		return nil, ErrName
	}
	return c.superuserDB(database)
}

func (c *Cluster) superuserDB(database string) (*sql.DB, error) {
	connector, err := pq.NewConnectorConfig(pq.Config{
		Host:               c.socketDir,
		Port:               Port,
		Database:           database,
		User:               Superuser,
		Password:           c.superPass,
		ApplicationName:    "pgdev",
		SSLMode:            pq.SSLModeDisable,
		ClientEncoding:     "UTF8",
		Datestyle:          "ISO, MDY",
		MinProtocolVersion: pq.ProtocolVersion30,
		MaxProtocolVersion: pq.ProtocolVersion30,
		ConnectTimeout:     10 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("pgdev: connector: %w", err)
	}
	return sql.OpenDB(connector), nil
}

// CreateLoginRole creates a LOGIN role with a new random password and returns
// the password. The role name is passed as a bind parameter and quoted by
// format(%I) on the server.
func (c *Cluster) CreateLoginRole(ctx context.Context, name string) (string, error) {
	if !nameRE.MatchString(name) {
		return "", ErrName
	}
	pass, err := newPassword()
	if err != nil {
		return "", err
	}
	if err := c.execFormatted(ctx, "postgres", "CREATE ROLE %I LOGIN PASSWORD %L", name, pass); err != nil {
		return "", err
	}
	return pass, nil
}

// CreateDatabase creates database owned by owner and revokes every PUBLIC
// privilege on it. CREATE DATABASE cannot run inside a function, so the two
// names, checked against ^[a-z_][a-z0-9_]{0,62}$, are quoted with
// pq.QuoteIdentifier into the statement text; this is the only statement of
// the package built that way.
func (c *Cluster) CreateDatabase(ctx context.Context, name, owner string) error {
	if !nameRE.MatchString(name) || !nameRE.MatchString(owner) {
		return ErrName
	}
	db, err := c.superuserDB("postgres")
	if err != nil {
		return err
	}
	_, execErr := db.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(name)+" OWNER "+pq.QuoteIdentifier(owner))
	if err := db.Close(); err != nil {
		return errors.Join(execErr, err)
	}
	if execErr != nil {
		return fmt.Errorf("pgdev: create database: %w", execErr)
	}
	return c.execFormatted(ctx, "postgres", "REVOKE ALL ON DATABASE %I FROM PUBLIC", name)
}

// GrantConnect grants CONNECT on database to role.
func (c *Cluster) GrantConnect(ctx context.Context, database, role string) error {
	if !nameRE.MatchString(database) || !nameRE.MatchString(role) {
		return ErrName
	}
	return c.execFormatted(ctx, "postgres", "GRANT CONNECT ON DATABASE %I TO %I", database, role)
}

// execFormatted runs EXECUTE format(f, args...) as the superuser in database.
// f is a constant of this package; args are bind parameters, so identifiers
// and literals are quoted by the server's format(%I, %L).
func (c *Cluster) execFormatted(ctx context.Context, database, f string, args ...string) error {
	db, err := c.superuserDB(database)
	if err != nil {
		return err
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return errors.Join(fmt.Errorf("pgdev: connect: %w", err), db.Close())
	}
	_, err = conn.ExecContext(ctx, `CREATE FUNCTION pg_temp.svn_exec(f text, a text[]) RETURNS void
LANGUAGE plpgsql AS $fn$ BEGIN EXECUTE format(f, VARIADIC a); END $fn$`)
	if err == nil {
		_, err = conn.ExecContext(ctx, `SELECT pg_temp.svn_exec($1, $2)`, f, pq.Array(args))
	}
	if err != nil {
		err = fmt.Errorf("pgdev: %s: %w", strings.Fields(f)[0], err)
	}
	return errors.Join(err, conn.Close(), db.Close())
}

// sysProcAttr is the process attributes of initdb and postgres.
func (c *Cluster) sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Credential: c.cred,
		Setpgid:    true,
		Pdeathsig:  syscall.SIGKILL,
	}
}

// serverIDs returns the uid, gid and supplementary groups the server runs
// with.
func (c *Cluster) serverIDs() (int, int, []int) {
	if c.cred != nil {
		return int(c.cred.Uid), int(c.cred.Gid), nil
	}
	groups, err := os.Getgroups()
	if err != nil {
		groups = nil
	}
	return os.Geteuid(), os.Getegid(), groups
}

// logTail returns the last logTailBytes of the server log.
func (c *Cluster) logTail() string {
	f, err := os.Open(c.logPath)
	if err != nil {
		return "(no log: " + err.Error() + ")"
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "(no log: " + err.Error() + ")"
	}
	off := fi.Size() - logTailBytes
	if off < 0 {
		off = 0
	}
	buf := make([]byte, fi.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
		return "(log read: " + err.Error() + ")"
	}
	return strings.TrimSpace(string(buf))
}

// serverCredential returns the credential of ServerUser when the process is
// root, and nil otherwise.
func serverCredential() (*syscall.Credential, error) {
	if os.Geteuid() != 0 {
		return nil, nil
	}
	u, err := user.Lookup(ServerUser)
	if err != nil {
		return nil, fmt.Errorf("pgdev: look up %q: %w", ServerUser, err)
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("pgdev: uid of %q: %w", ServerUser, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("pgdev: gid of %q: %w", ServerUser, err)
	}
	if uid == 0 {
		return nil, fmt.Errorf("pgdev: %q has uid 0", ServerUser)
	}
	return &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}}, nil
}

// ValidateDir reports whether dir is a cluster directory this package may
// use: a direct child of os.TempDir() named DirPrefix*, a real directory (not
// a symlink), and short enough that its socket path fits a sockaddr_un.
func ValidateDir(dir string) error {
	tmp := filepath.Clean(os.TempDir())
	clean := filepath.Clean(dir)
	if !filepath.IsAbs(clean) || clean != dir || filepath.Dir(clean) != tmp || !strings.HasPrefix(filepath.Base(clean), DirPrefix) {
		return fmt.Errorf("%w: %s", ErrDirectory, dir)
	}
	socket := filepath.Join(clean, "run", ".s.PGSQL."+strconv.Itoa(Port))
	if len(socket) > socketPathMax {
		return fmt.Errorf("%w: socket path longer than %d bytes", ErrDirectory, socketPathMax)
	}
	fi, err := os.Lstat(clean)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDirectory, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%w: not a directory: %s", ErrDirectory, dir)
	}
	return nil
}

// checkTraversable checks the search (x) permission of every ancestor of dir
// for a process with uid, gid and supplementary groups, with the kernel's
// owner, then group, then other rule.
func checkTraversable(dir string, uid, gid int, groups []int) error {
	for p := filepath.Dir(dir); ; p = filepath.Dir(p) {
		fi, err := os.Stat(p)
		if err != nil {
			return fmt.Errorf("%w: %s: %v", ErrNotTraversable, p, err)
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("%w: %s: no stat data", ErrNotTraversable, p)
		}
		if !canSearch(fi.Mode().Perm(), int(st.Uid), int(st.Gid), uid, gid, groups) {
			return fmt.Errorf("%w: %s", ErrNotTraversable, p)
		}
		if p == filepath.Dir(p) {
			return nil
		}
	}
}

// canSearch applies the owner, group, other permission rule for the x bit.
func canSearch(perm os.FileMode, ownerUID, ownerGID, uid, gid int, groups []int) bool {
	if uid == ownerUID {
		return perm&0o100 != 0
	}
	inGroup := gid == ownerGID
	for _, g := range groups {
		if g == ownerGID {
			inGroup = true
		}
	}
	if inGroup {
		return perm&0o010 != 0
	}
	return perm&0o001 != 0
}

// startLocked starts cmd from a goroutine locked to its OS thread and keeps
// that goroutine, and so the thread, alive until the child exits. Pdeathsig
// fires when the starting thread exits, so the thread must outlive the child.
func startLocked(cmd *exec.Cmd) (*process, error) {
	p := &process{exited: make(chan struct{})}
	started := make(chan error, 1)
	go func() {
		// Never unlocked: when this goroutine returns, the runtime ends the
		// thread, after the child has exited.
		runtime.LockOSThread()
		if err := cmd.Start(); err != nil {
			started <- err
			return
		}
		p.pid = cmd.Process.Pid
		started <- nil
		p.err = cmd.Wait()
		close(p.exited)
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	return p, nil
}

// killGroup sends SIGKILL to the process group led by pid.
func killGroup(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("pgdev: kill process group: %w", err)
	}
	return nil
}

// newPassword returns passwordBytes random bytes in lowercase hex.
func newPassword() (string, error) {
	b := make([]byte, passwordBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("pgdev: random password: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// capBuffer keeps the first max bytes written to it and drops the rest.
type capBuffer struct {
	mu  sync.Mutex
	max int
	buf bytes.Buffer
}

func (b *capBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if room := b.max - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *capBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
