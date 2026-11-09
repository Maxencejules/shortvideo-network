// Package pgtest is the test-only PostgreSQL harness of the broadcaster
// module. Only _test.go files import it; it is never linked into the
// svn-broadcaster binary.
//
// It adds three things to internal/pgdev:
//
//   - A machine-wide lock. Start takes an exclusive flock on
//     os.TempDir()/svn-pgtest.lock before it starts a cluster and Close
//     releases it after the cluster is removed, so test binaries that run in
//     parallel (go test -p N) run one cluster at a time. The kernel releases
//     the lock when a test process dies.
//   - Crash hooks. ImmediateStop sends SIGQUIT to the postmaster, the signal
//     `pg_ctl -m immediate stop` sends; KillPostmaster sends SIGKILL to it.
//     Both wait until the postmaster and every other live process of its
//     process group have exited, so a restart on the same data directory
//     finds no attached shared memory. These are process crashes, not a host
//     power loss: the operating system keeps every write already handed to
//     it.
//   - Helper processes. HelperCommand re-executes the test binary with
//     HelperEnv set; the test package's TestMain runs the named helper
//     instead of the tests, so a test can SIGKILL a real client process.
//
// There is no skip path: a missing PostgreSQL binary fails Start.
package pgtest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/pgdev"
)

const (
	// DefaultBinDir is the PostgreSQL 18 install of scripts/env (goal 04 pin).
	DefaultBinDir = "/usr/lib/postgresql/18/bin"
	// HelperEnv names the helper a re-executed test binary runs.
	HelperEnv = "SVN_PGTEST_HELPER"
	// StartTimeout bounds initdb and each wait for readiness.
	StartTimeout = 2 * time.Minute

	lockName  = "svn-pgtest.lock"
	lockWait  = 15 * time.Minute
	drainWait = 30 * time.Second
)

// Server is a running cluster that holds the machine-wide test lock.
type Server struct {
	Cluster *pgdev.Cluster
	lock    *os.File
}

// Start takes the machine-wide lock, waiting at most lockWait, and starts a
// cluster with the binaries in DefaultBinDir.
func Start(ctx context.Context) (*Server, error) {
	lock, err := acquireLock(ctx)
	if err != nil {
		return nil, err
	}
	c, err := pgdev.Start(ctx, pgdev.Options{BinDir: DefaultBinDir, StartTimeout: StartTimeout})
	if err != nil {
		return nil, errors.Join(err, releaseLock(lock))
	}
	return &Server{Cluster: c, lock: lock}, nil
}

// Close removes the cluster, then releases the lock.
func (s *Server) Close() error {
	rmErr := s.Cluster.Remove()
	return errors.Join(rmErr, releaseLock(s.lock))
}

// NewDatabase creates database owned by owner and grants CONNECT on it to
// each role in connect.
func (s *Server) NewDatabase(ctx context.Context, name, owner string, connect ...string) error {
	if err := s.Cluster.CreateDatabase(ctx, name, owner); err != nil {
		return err
	}
	for _, r := range connect {
		if err := s.Cluster.GrantConnect(ctx, name, r); err != nil {
			return err
		}
	}
	return nil
}

// ImmediateStop sends SIGQUIT to the postmaster and waits until its process
// group has no live process.
func (s *Server) ImmediateStop() error {
	return s.signalAndDrain(syscall.SIGQUIT)
}

// KillPostmaster sends SIGKILL to the postmaster and waits until its process
// group has no live process: the backends exit when they see the postmaster
// is gone.
func (s *Server) KillPostmaster() error {
	return s.signalAndDrain(syscall.SIGKILL)
}

func (s *Server) signalAndDrain(sig syscall.Signal) error {
	pid, exited, ok := s.Cluster.Postmaster()
	if !ok {
		return pgdev.ErrNotRunning
	}
	select {
	case <-exited:
		return pgdev.ErrNotRunning
	default:
	}
	if err := syscall.Kill(pid, sig); err != nil {
		return fmt.Errorf("pgtest: signal postmaster: %w", err)
	}
	select {
	case <-exited:
	case <-time.After(drainWait):
		return fmt.Errorf("pgtest: postmaster %d did not exit after %v", pid, sig)
	}
	return WaitGroupGone(pid, drainWait)
}

// Restart starts the server again on the same data directory.
func (s *Server) Restart(ctx context.Context) error {
	return s.Cluster.Start(ctx)
}

// WaitGroupGone waits until no live (non-zombie) process has process group
// pgid, reading /proc.
func WaitGroupGone(pgid int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		live, err := liveInGroup(pgid)
		if err != nil {
			return err
		}
		if len(live) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("pgtest: process group %d still has live processes %v", pgid, live)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ProcessLive reports whether pid exists and is not a zombie.
func ProcessLive(pid int) (bool, error) {
	_, state, err := readStat(pid)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return state != "Z" && state != "X", nil
}

// liveInGroup lists the pids of live processes in process group pgid.
func liveInGroup(pgid int) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("pgtest: read /proc: %w", err)
	}
	var live []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		pgrp, state, err := readStat(pid)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if pgrp == pgid && state != "Z" && state != "X" {
			live = append(live, pid)
		}
	}
	return live, nil
}

// readStat returns the process group and state of pid from /proc/<pid>/stat,
// "pid (comm) state ppid pgrp ...". comm can hold spaces and parentheses, so
// the fields are read after the last ')'.
func readStat(pid int) (pgrp int, state string, err error) {
	b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ESRCH) {
			return 0, "", os.ErrNotExist
		}
		return 0, "", fmt.Errorf("pgtest: read stat of %d: %w", pid, err)
	}
	end := bytes.LastIndexByte(b, ')')
	if end < 0 {
		return 0, "", fmt.Errorf("pgtest: malformed stat of %d", pid)
	}
	fields := strings.Fields(string(b[end+1:]))
	if len(fields) < 3 {
		return 0, "", fmt.Errorf("pgtest: short stat of %d", pid)
	}
	pgrp, err = strconv.Atoi(fields[2])
	if err != nil {
		return 0, "", fmt.Errorf("pgtest: pgrp of %d: %w", pid, err)
	}
	return pgrp, fields[0], nil
}

// Helper returns the helper name a re-executed test binary must run, or ""
// in a normal test run.
func Helper() string { return os.Getenv(HelperEnv) }

// HelperCommand returns a command that re-executes the running test binary
// as helper. The test package's TestMain must run the helper when Helper()
// is not empty. -test.run=^$ keeps the binary from running tests if TestMain
// does not intercept.
func HelperCommand(ctx context.Context, helper string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), HelperEnv+"="+helper)
	return cmd
}

// acquireLock takes an exclusive flock on the lock file, polling until ctx
// ends or lockWait passes.
func acquireLock(ctx context.Context) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(os.TempDir(), lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("pgtest: open lock file: %w", err)
	}
	deadline := time.Now().Add(lockWait)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.Join(fmt.Errorf("pgtest: flock: %w", err), f.Close())
		}
		if time.Now().After(deadline) {
			return nil, errors.Join(fmt.Errorf("pgtest: another test cluster held the lock for %v", lockWait), f.Close())
		}
		select {
		case <-ctx.Done():
			return nil, errors.Join(ctx.Err(), f.Close())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// releaseLock unlocks and closes the lock file.
func releaseLock(f *os.File) error {
	unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if unlockErr != nil {
		unlockErr = fmt.Errorf("pgtest: unlock: %w", unlockErr)
	}
	return errors.Join(unlockErr, f.Close())
}
