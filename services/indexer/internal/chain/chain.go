// Package chain reads committed blocks and application queries from one
// configured CometBFT endpoint.
//
// The endpoint is trusted for the bytes it returns. This package checks that
// those bytes agree with each other: the chain id, the height, the block
// hash and the commit hash. That check is not a light-client proof, and
// comparing two unauthenticated responses would not be one either.
package chain

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"

	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/project"
)

// floorMinus is the number of blocks subtracted from the status height to
// reach the read floor. It is 1: CometBFT can save a block before the
// application has committed it, so the latest saved height is not yet a
// finished projection input.
const floorMinus int64 = 1

var (
	// ErrChainID is a node or a block whose chain id is not the configured one.
	ErrChainID = errors.New("chain: chain id mismatch")
	// ErrHistoryUnavailable is a height the node did not return. It is not
	// an empty block.
	ErrHistoryUnavailable = errors.New("chain: history unavailable")
	// ErrForged is a response that disagrees with itself.
	ErrForged = errors.New("chain: forged response")
	// ErrQuery is a query the node rejected or answered below the floor.
	ErrQuery = errors.New("chain: query rejected")
	// ErrCatchingUp is a node that reports it is still catching up.
	ErrCatchingUp = errors.New("chain: node is catching up")
)

// Status is the latest header the node reported.
type Status struct {
	ChainID    string
	Height     int64
	Time       time.Time
	CatchingUp bool
}

// AppFloor is the highest height this process will index from s. The
// subtraction is floorMinus.
func (s Status) AppFloor() int64 {
	return max(int64(1), s.Height-floorMinus)
}

// Header is the committed header of one height, tied to its commit hash.
type Header struct {
	ChainID    string
	Height     int64
	Hash       []byte
	AppHash    []byte
	Time       time.Time
	CommitHash []byte
}

// Results is the application output of one committed height.
type Results struct {
	Height   int64
	AppHash  []byte
	Txs      []project.Tx
	Finalize []abci.Event
}

// Reader is the raw endpoint. A missing height returns ErrHistoryUnavailable.
type Reader interface {
	Status(ctx context.Context) (Status, error)
	Header(ctx context.Context, height int64) (Header, error)
	Results(ctx context.Context, height int64) (Results, error)
	Query(ctx context.Context, path string, data []byte, height int64) (QueryResult, error)
}

// QueryResult is one ABCI query answer.
type QueryResult struct {
	Code      uint32
	Codespace string
	Log       string
	Value     []byte
	Height    int64
	Found     bool
}

// Client reads one chain through one Reader.
type Client struct {
	chainID string
	raw     Reader
}

// New returns a client for chainID.
func New(chainID string, raw Reader) (*Client, error) {
	if chainID == "" || raw == nil {
		return nil, errors.New("chain: chain id and reader are required")
	}
	return &Client{chainID: chainID, raw: raw}, nil
}

// ChainID is the id the client was configured with.
func (c *Client) ChainID() string { return c.chainID }

// Status reads the node and refuses a different chain id.
func (c *Client) Status(ctx context.Context) (Status, error) {
	st, err := c.raw.Status(ctx)
	if err != nil {
		return Status{}, err
	}
	if st.ChainID != c.chainID {
		return Status{}, fmt.Errorf("%w: node %q configured %q", ErrChainID, st.ChainID, c.chainID)
	}
	if st.Height < 1 {
		return Status{}, fmt.Errorf("%w: status height %d", ErrForged, st.Height)
	}
	return st, nil
}

// Block reads one committed height and checks the response against itself.
// A missing height is ErrHistoryUnavailable. The caller must not treat that
// as a block with no events.
func (c *Client) Block(ctx context.Context, height int64) (project.Block, error) {
	if height < 1 {
		return project.Block{}, fmt.Errorf("%w: height %d", ErrForged, height)
	}
	h, err := c.raw.Header(ctx, height)
	if err != nil {
		if errors.Is(err, ErrHistoryUnavailable) {
			return project.Block{}, err
		}
		return project.Block{}, fmt.Errorf("%w: header %d: %v", ErrHistoryUnavailable, height, err)
	}
	res, err := c.raw.Results(ctx, height)
	if err != nil {
		if errors.Is(err, ErrHistoryUnavailable) {
			return project.Block{}, err
		}
		return project.Block{}, fmt.Errorf("%w: results %d: %v", ErrHistoryUnavailable, height, err)
	}
	if err := verify(c.chainID, height, h, res); err != nil {
		return project.Block{}, err
	}
	return project.Block{
		ChainID: h.ChainID, Height: h.Height,
		Hash: append([]byte(nil), h.Hash...), AppHash: append([]byte(nil), res.AppHash...),
		TimeUnix: h.Time.UTC().Unix(), Txs: res.Txs, Finalize: res.Finalize,
	}, nil
}

// verify checks one header against the commit and the block results.
// A mismatch is a forged response. A missing piece is not an empty block;
// the caller reports that before calling verify.
func verify(chainID string, height int64, h Header, res Results) error {
	switch {
	case h.ChainID != chainID:
		return fmt.Errorf("%w: block %q configured %q", ErrChainID, h.ChainID, chainID)
	case h.Height != height || res.Height != height:
		return fmt.Errorf("%w: height header %d results %d want %d", ErrForged, h.Height, res.Height, height)
	case len(h.Hash) != 32 || len(h.CommitHash) != 32 || len(res.AppHash) != 32:
		return fmt.Errorf("%w: hash length", ErrForged)
	case !bytes.Equal(h.Hash, h.CommitHash):
		return fmt.Errorf("%w: block hash differs from commit hash at %d", ErrForged, height)
	}
	// The header app hash is the state before this block (the previous
	// block's result). block_results.app_hash is the state after this
	// block. They are not the same value.
	for _, tx := range res.Txs {
		if len(tx.Hash) != 32 {
			return fmt.Errorf("%w: transaction hash length at %d", ErrForged, height)
		}
	}
	return nil
}
