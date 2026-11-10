package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os/exec"
	"time"

	"github.com/lib/pq"
)

// Service is the upload API. Publication is not a status this service can set.
type Service struct {
	store   *Store
	blob    *Blob
	limits  Limits
	verify  Verifier
	now     func() time.Time
	delay   time.Duration
	ffprobe string
	db      *sql.DB
}

// Options is the process configuration. Database is required.
// A production process must pass Verifier and must not pass fixtures.
type Options struct {
	Environment       string
	EnableDevFixtures bool
	PublicLaunch      bool
	Fixtures          []Fixture
	Limits            Limits
	QuarantineDir     string
	Database          pq.Config
	Verifier          Verifier
	ProbeDelay        time.Duration
	Now               func() time.Time
}

// New migrates the schema, provisions fixture allowances, and refuses a public launch.
func New(ctx context.Context, opt Options) (*Service, error) {
	if opt.PublicLaunch {
		return nil, errors.New("public launch is disabled")
	}
	if err := Validate(opt.Limits); err != nil {
		return nil, err
	}
	var verify Verifier
	switch opt.Environment {
	case "production":
		if opt.EnableDevFixtures || len(opt.Fixtures) > 0 || opt.ProbeDelay != 0 {
			return nil, errors.New("production refuses development fixtures")
		}
		if opt.Verifier == nil {
			return nil, errors.New("production session verifier is not configured")
		}
		verify = opt.Verifier
	case "development":
		if !opt.EnableDevFixtures {
			return nil, errors.New("development requires explicit dev fixtures")
		}
		if opt.ProbeDelay < 0 {
			return nil, errors.New("probe delay is negative")
		}
		built, err := NewDevVerifier(opt.Fixtures)
		if err != nil {
			return nil, err
		}
		verify = built
	default:
		return nil, errors.New("environment must be development or production")
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, errors.New("ffprobe is required")
	}
	blob, err := OpenBlob(opt.QuarantineDir)
	if err != nil {
		return nil, err
	}
	db, err := Open(opt.Database)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := Migrate(pingCtx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	store := NewStore(db)
	if opt.Environment == "development" {
		for _, fixture := range opt.Fixtures {
			if fixture.Role != "creator" {
				continue
			}
			if err := store.EnsureAccount(pingCtx, fixture.AccountID, opt.Limits.AccountInputAllowanceBytes, opt.Limits.AccountOutputBudgetBytes); err != nil {
				_ = db.Close()
				return nil, err
			}
		}
	}
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		store:   store,
		blob:    blob,
		limits:  opt.Limits,
		verify:  verify,
		now:     now,
		delay:   opt.ProbeDelay,
		ffprobe: ffprobe,
		db:      db,
	}, nil
}

// Close closes the database pool.
func (s *Service) Close() error { return s.db.Close() }

// Resume probes uploads that were left in PROBING and queues each one at most once.
func (s *Service) Resume(ctx context.Context) error {
	rows, err := s.store.Probing(ctx)
	if err != nil {
		return err
	}
	for _, item := range rows {
		row, err := s.store.Owned(ctx, item.UploadID, item.Owner)
		if err != nil || row.Status != "PROBING" {
			continue
		}
		if err := s.finish(ctx, row); err != nil && !errors.Is(err, errDone) {
			return err
		}
	}
	return nil
}

// Sweep expires abandoned uploads and deletes their quarantine files.
func (s *Service) Sweep(ctx context.Context) (int, error) {
	n, names, err := s.store.Sweep(ctx, s.now())
	if err != nil {
		return 0, err
	}
	for _, name := range names {
		_ = s.blob.Remove(name)
	}
	return n, nil
}

func (s *Service) finish(ctx context.Context, row uploadRow) error {
	path, err := s.blob.Path(row.ObjectName)
	if err != nil {
		return err
	}
	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(s.limits.ProbeTimeoutSeconds)*time.Second)
	defer cancel()
	result, err := ProbeFile(probeCtx, s.ffprobe, path, s.limits)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			if ctx.Err() != nil {
				return err
			}
		}
		_ = s.store.Release(ctx, row.UploadID, row.Owner, "REJECTED", s.now())
		_ = s.blob.Remove(row.ObjectName)
		return err
	}
	_, err = s.store.FinishProbe(ctx, row, result, s.now())
	return err
}

func newID(prefix string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buf), nil
}

func newObject() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func newToken() (string, string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(buf)
	sum := sha256Hex(token)
	return token, sum, nil
}
