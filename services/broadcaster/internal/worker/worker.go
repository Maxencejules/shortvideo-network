package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/signer"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// batch bounds the intents, attempts or owner rows one pass of a loop reads.
const batch = 64

// Worker is the submission engine. Run drives it until its context ends;
// ReconcileOnce runs one pass of every loop.
type Worker struct {
	cfg     Config
	log     *slog.Logger
	st      *store.Store
	callers map[string]policy.Caller
	keys    map[policy.Class]signer.KeyInfo
	obs     *observer
	// gone records, per intent, the read height at which its object was
	// first seen no longer allowing its effect (checkEffect).
	goneMu sync.Mutex
	gone   map[string]int64
}

// New checks cfg and returns a worker. It does no I/O.
func New(cfg Config) (*Worker, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	log := cfg.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	w := &Worker{cfg: cfg, log: log, st: cfg.Store, callers: map[string]policy.Caller{}, keys: map[policy.Class]signer.KeyInfo{},
		gone: map[string]int64{}}
	for _, c := range cfg.Callers {
		w.callers[c.Principal] = c
	}
	for _, k := range cfg.Keys {
		w.keys[k.Class] = k
	}
	w.obs = &observer{w: w}
	return w, nil
}

// registerSigners registers both key signers with their class, key name
// and lock key, or checks that the stored rows agree (store.ErrSignerConfig
// otherwise).
func (w *Worker) registerSigners(ctx context.Context) error {
	for _, k := range w.cfg.Keys {
		lock, err := LockKeyOf(k.Class)
		if err != nil {
			return err
		}
		if err := w.st.RegisterSigner(ctx, store.SignerKey{Address: k.Address, Class: store.SignerClass(k.Class),
			KeyName: k.Name, LockKey: lock}); err != nil {
			return fmt.Errorf("worker: register signer %s: %w", k.Name, err)
		}
	}
	return nil
}

// Run registers the signers and runs every loop until ctx ends: one loop
// per key signer, the forwarder, housekeeping and the observer. A loop
// logs a failed pass and tries again at the next poll; Run returns when
// ctx ends and every loop has stopped.
func (w *Worker) Run(ctx context.Context) error {
	if err := w.registerSigners(ctx); err != nil {
		return err
	}
	var wg sync.WaitGroup
	loops := []func(context.Context) error{w.obs.pass, w.housekeepingPass}
	sessions := []*session{newSession(w, false, w.keys[policy.ClassService]), newSession(w, false, w.keys[policy.ClassBroadcaster]),
		newSession(w, true, signer.KeyInfo{})}
	for _, s := range sessions {
		loops = append(loops, s.pass)
	}
	for _, loop := range loops {
		wg.Add(1)
		go func(pass func(context.Context) error) {
			defer wg.Done()
			w.loop(ctx, pass)
		}(loop)
	}
	wg.Wait()
	for _, s := range sessions {
		s.close()
	}
	return ctx.Err()
}

// loop runs pass at every poll until ctx ends.
func (w *Worker) loop(ctx context.Context, pass func(context.Context) error) {
	ticker := time.NewTicker(w.cfg.Numbers.PollInterval)
	defer ticker.Stop()
	for {
		if err := pass(ctx); err != nil && ctx.Err() == nil {
			w.log.Warn("worker pass failed", "error", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// ErrLockHeld is returned by ReconcileOnce when another process holds a
// signer's or the forwarder's advisory lock.
var ErrLockHeld = errors.New("worker: another process holds a signer lock")

// ReconcileOnce registers the signers and runs one pass of every loop: the
// observer, housekeeping, both key signers and the forwarder (the
// reconcile-once command). It takes each advisory lock with
// pg_try_advisory_lock and returns ErrLockHeld, wrapped with the name of
// the signer or forwarder session whose lock is held, without running that
// loop's pass, when another process holds it.
func (w *Worker) ReconcileOnce(ctx context.Context) error {
	if err := w.registerSigners(ctx); err != nil {
		return err
	}
	var errs []error
	for _, pass := range []func(context.Context) error{w.obs.pass, w.housekeepingPass} {
		errs = append(errs, pass(ctx))
	}
	for _, s := range []*session{newSession(w, false, w.keys[policy.ClassService]),
		newSession(w, false, w.keys[policy.ClassBroadcaster]), newSession(w, true, signer.KeyInfo{})} {
		if err := s.open(ctx); err != nil {
			if errors.Is(err, store.ErrSignerLockHeld) {
				err = fmt.Errorf("%w (%s)", ErrLockHeld, s.name())
			}
			errs = append(errs, err)
			continue
		}
		errs = append(errs, s.pass(ctx))
		s.close()
	}
	return errors.Join(errs...)
}

// alert logs an observation an operator must see. It never changes a
// state; the decision that raised it says what changes.
func (w *Worker) alert(code string, attrs ...any) {
	w.log.Error("broadcaster alert", append([]any{"alert", code}, attrs...)...)
}
