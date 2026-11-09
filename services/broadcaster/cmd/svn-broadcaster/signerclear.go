package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"regexp"

	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"

	chainaddress "github.com/Maxencejules/shortvideo-network/chain/address"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// clearCommand is the name signer-clear records in signer_events.
const clearCommand = "signer-clear"

// maxAddressLen bounds the --signer flag before it is decoded.
const maxAddressLen = 90

var hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// clearResult is signer-clear's output on standard output.
type clearResult struct {
	Signer      string `json:"signer"`
	Sequence    uint64 `json:"sequence"`
	ForeignHash string `json:"foreign_hash"`
	Cleared     bool   `json:"cleared"`
}

// parseAccount decodes a bech32 account address of the chain's prefix to
// its 20 bytes.
func parseAccount(s string) ([]byte, error) {
	if s == "" || len(s) > maxAddressLen {
		return nil, errors.New("--signer must be a bech32 account address")
	}
	addr, err := addresscodec.NewBech32Codec(chainaddress.Account).StringToBytes(s)
	if err != nil || len(addr) != genesis.AccountBytes {
		return nil, fmt.Errorf("--signer must be a bech32 %s account address of %d bytes", chainaddress.Account, genesis.AccountBytes)
	}
	return addr, nil
}

// runSignerClear is the signer-clear subcommand (DR-11-15): it records
// the operator's acknowledgement of the foreign transaction at a signer's
// sequence. It takes the signer's advisory lock, so it fails with the
// lock-held code while serve or a reconcile pass drives that signer.
func runSignerClear(ctx context.Context, args []string, env processEnv, log *slog.Logger) error {
	fs := newFlags("signer-clear", env)
	cfgPath := configFlag(fs)
	signerFlag := fs.String("signer", "", "bech32 address of the halted key signer (required)")
	sequence := fs.Uint64("sequence", 0, "the signer sequence the foreign transaction used (required)")
	foreign := fs.String("foreign-hash", "", "the foreign transaction's hash, 64 lowercase hex characters (required)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	seqSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "sequence" {
			seqSet = true
		}
	})
	addr, err := parseAccount(*signerFlag)
	if err != nil {
		return usageError(err)
	}
	if !seqSet || *sequence > math.MaxInt64 {
		return usageError(errors.New("--sequence is required: 0 to 9223372036854775807"))
	}
	if !hashRE.MatchString(*foreign) {
		return usageError(errors.New("--foreign-hash must be 64 lowercase hex characters"))
	}
	var hash [32]byte
	if _, err := hex.Decode(hash[:], []byte(*foreign)); err != nil {
		return usageError(fmt.Errorf("--foreign-hash: %w", err))
	}
	c, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	if err := c.RequireRuntimeDatabase(); err != nil {
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
	instance, err := instanceID(clearCommand)
	if err != nil {
		return err
	}
	st, err := store.New(db, c.StoreLimits(), instance)
	if err != nil {
		return refusedError(err)
	}
	if err := clearConflict(ctx, st, addr, *sequence, hash); err != nil {
		return err
	}
	log.Info("signer event cleared", "signer", *signerFlag, "sequence", *sequence, "foreign_hash", *foreign)
	return json.NewEncoder(env.stdout).Encode(clearResult{Signer: *signerFlag, Sequence: *sequence, ForeignHash: *foreign, Cleared: true})
}

// clearConflict takes the signer's lock and clears the event.
func clearConflict(ctx context.Context, st *store.Store, addr []byte, sequence uint64, hash [32]byte) error {
	ss, err := st.LockSigner(ctx, addr)
	switch {
	case errors.Is(err, store.ErrSignerLockHeld):
		return lockHeldError(err)
	case errors.Is(err, store.ErrNotFound):
		return notFoundError(errors.New("no key signer is registered at this address"))
	case errors.Is(err, store.ErrInvalid):
		return refusedError(err)
	case err != nil:
		return err
	}
	clearErr := ss.ClearConflict(ctx, sequence, hash, clearCommand)
	if err := ss.Close(); err != nil && clearErr == nil {
		return fmt.Errorf("close signer session: %w", err)
	}
	if errors.Is(clearErr, store.ErrNotFound) {
		return notFoundError(errors.New("no uncleared signer event matches this signer, sequence and hash"))
	}
	return clearErr
}
