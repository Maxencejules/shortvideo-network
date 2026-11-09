// Package engine catches the projection up to the read floor and reconciles it.
// It never indexes a height above the floor, so the newest saved block, which
// the application may still be executing, is not treated as final.
package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/project"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/reconcile"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/store"
)

// Source is the chain read the engine uses.
type Source interface {
	Status(ctx context.Context) (chain.Status, error)
	Block(ctx context.Context, height int64) (project.Block, error)
	reconcile.Querier
}

// Engine applies committed blocks and reconciles.
type Engine struct {
	Chain   Source
	Store   *store.Store
	ChainID string
}

// Result is one catch-up pass.
type Result struct {
	Indexed int64
	Floor   int64
	// Gap is set when a height in the range was not returned. The pass stops.
	// It does not apply a later height in place of the missing one.
	Gap bool
}

// CatchUp indexes every height from the checkpoint through the floor.
func (e *Engine) CatchUp(ctx context.Context) (Result, error) {
	st, err := e.Chain.Status(ctx)
	if err != nil {
		_ = e.Store.Observe(ctx, 0, 0, time.Unix(0, 0).UTC(), false, false, "unreachable")
		return Result{}, err
	}
	if st.ChainID != e.ChainID {
		return Result{}, fmt.Errorf("%w: node %q", chain.ErrChainID, st.ChainID)
	}
	floor := st.AppFloor()
	if err := e.Store.Observe(ctx, st.Height, floor, st.Time, st.CatchingUp, false, ""); err != nil {
		return Result{}, err
	}
	next, err := e.Store.NextHeight(ctx)
	if err != nil {
		return Result{}, err
	}
	for height := next; height <= floor; height++ {
		block, err := e.Chain.Block(ctx, height)
		if err != nil {
			code := "unreachable"
			gap := false
			if errors.Is(err, chain.ErrHistoryUnavailable) {
				code = "history_unavailable"
				gap = true
			} else if errors.Is(err, chain.ErrChainID) {
				code = "chain_id"
			} else if errors.Is(err, chain.ErrForged) {
				code = "forged"
			}
			_ = e.Store.Observe(ctx, st.Height, floor, st.Time, st.CatchingUp, gap, code)
			return Result{Indexed: height - 1, Floor: floor, Gap: gap}, err
		}
		effects, err := project.Decode(block)
		if err != nil {
			code := "malformed"
			if errors.Is(err, project.ErrEventVersion) {
				code = "event_version"
			}
			_ = e.Store.Observe(ctx, st.Height, floor, st.Time, st.CatchingUp, false, code)
			return Result{Indexed: height - 1, Floor: floor}, err
		}
		if err := e.Store.Apply(ctx, block, effects); err != nil {
			return Result{Indexed: height - 1, Floor: floor}, err
		}
	}
	cp, err := e.Store.Checkpoint(ctx)
	if err != nil {
		return Result{}, err
	}
	indexed := int64(0)
	if cp.Found {
		indexed = cp.Height
	}
	return Result{Indexed: indexed, Floor: floor}, nil
}

// Reconcile compares the projection to queries at the floor. It does not
// change a lease term or a content row.
func (e *Engine) Reconcile(ctx context.Context, floor int64) (string, error) {
	proj, err := e.Store.Load(ctx)
	if err != nil {
		return "", err
	}
	return reconcile.Run(ctx, e.Chain, e.Store, proj, floor)
}
