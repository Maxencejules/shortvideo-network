package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// statusResult is status's output on standard output.
type statusResult struct {
	ChainID string `json:"chain_id"`
	// Schema is "current": status refuses a schema that is not the
	// binary's before it reads anything else.
	Schema   string           `json:"schema"`
	Signers  []signerStatus   `json:"signers"`
	Observer *observerStatus  `json:"observer"`
	Stall    *stallEpisodeOut `json:"stall_episode"`
}

type signerStatus struct {
	Address        string  `json:"address"`
	Class          string  `json:"class"`
	Name           string  `json:"name"`
	AccountNumber  *uint64 `json:"account_number"`
	NextSequence   *uint64 `json:"next_sequence"`
	ObservedHeight uint64  `json:"observed_height"`
	HaltedReason   string  `json:"halted_reason,omitempty"`
}

type observerStatus struct {
	LatestHeight uint64    `json:"latest_height"`
	LatestTime   time.Time `json:"latest_time"`
	SeenAt       time.Time `json:"seen_at"`
	AgeMillis    int64     `json:"age_ms"`
	// Fresh is the API's rule: the record is at most the configured
	// freshness (poll interval times freshness polls) old.
	Fresh    bool   `json:"fresh"`
	Instance string `json:"instance"`
}

type stallEpisodeOut struct {
	ID            int64     `json:"id"`
	Reason        string    `json:"reason"`
	StartedHeight uint64    `json:"started_height"`
	StartedAt     time.Time `json:"started_at"`
}

// runStatus is the status subcommand. It reads the outbox only through
// the store's read functions (checked by a static test), under either
// database role, and never reaches the node or the signer.
func runStatus(ctx context.Context, args []string, env processEnv, log *slog.Logger) error {
	fs := newFlags("status", env)
	cfgPath := configFlag(fs)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	c, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	if err := c.RequireDatabase(); err != nil {
		return refusedError(err)
	}
	codec, err := chain.NewCodec(c.ChainID)
	if err != nil {
		return refusedError(err)
	}
	conn, err := c.ConnConfig()
	if err != nil {
		return refusedError(err)
	}
	db, err := store.OpenDB(conn)
	if err != nil {
		return refusedError(err)
	}
	defer db.Close()
	if err := checkSchema(ctx, db); err != nil {
		return err
	}
	if err := store.CheckChain(ctx, db, c.ChainID); err != nil {
		if errors.Is(err, store.ErrChainMismatch) || errors.Is(err, store.ErrChainUnbound) {
			return refusedError(err)
		}
		return err
	}
	instance, err := instanceID("status")
	if err != nil {
		return err
	}
	st, err := store.New(db, c.StoreLimits(), instance)
	if err != nil {
		return refusedError(err)
	}
	res, err := readStatus(ctx, st, codec, c.Numbers.ObserverFreshness())
	if err != nil {
		return err
	}
	res.ChainID = c.ChainID
	log.Info("status read", "signers", int64(len(res.Signers)), "stall_open", res.Stall != nil)
	return json.NewEncoder(env.stdout).Encode(res)
}

// readStatus reads the signers, the observer record and the open stall
// episode.
func readStatus(ctx context.Context, st *store.Store, codec *chain.Codec, freshness time.Duration) (statusResult, error) {
	res := statusResult{Schema: "current", Signers: []signerStatus{}}
	signers, err := st.KeySigners(ctx)
	if err != nil {
		return res, fmt.Errorf("read signers: %w", err)
	}
	for _, s := range signers {
		addr, err := codec.Bech32(s.Address)
		if err != nil {
			return res, err
		}
		res.Signers = append(res.Signers, signerStatus{Address: addr, Class: string(s.Class), Name: s.KeyName,
			AccountNumber: s.AccountNumber, NextSequence: s.NextSequence, ObservedHeight: s.ObservedHeight,
			HaltedReason: s.HaltedReason})
	}
	h, err := st.ReadChainHealth(ctx)
	switch {
	case err == nil:
		res.Observer = &observerStatus{LatestHeight: h.LatestHeight, LatestTime: h.LatestTime.UTC(), SeenAt: h.SeenAt.UTC(),
			AgeMillis: h.Age.Milliseconds(), Fresh: h.Age >= 0 && h.Age <= freshness, Instance: h.Observer}
	case !errors.Is(err, store.ErrNotFound):
		return res, fmt.Errorf("read chain health: %w", err)
	}
	ep, err := st.OpenStallEpisode(ctx)
	switch {
	case err == nil:
		res.Stall = &stallEpisodeOut{ID: ep.ID, Reason: string(ep.Reason), StartedHeight: ep.StartedHeight, StartedAt: ep.StartedAt.UTC()}
	case !errors.Is(err, store.ErrNotFound):
		return res, fmt.Errorf("read stall episode: %w", err)
	}
	return res, nil
}
