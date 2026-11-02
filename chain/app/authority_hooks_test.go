package app

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Test-only hooks. This is a _test.go file, so none of these symbols is
// linked into svnd. TestLinkedBinaryOmitsTestHooks checks the binary.

func (app *App) authorityStore() storetypes.KVStore {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID()})
	return ctx.KVStore(app.authority.StoreKey())
}

// AuthoritySnapshotForTest is the whole committed authority store in key
// order: for each key, its length (8 bytes, big-endian), the key, the value
// length (8 bytes, big-endian), the value. It covers every prefix: the config
// key, the admission rows, the pending entries, and every per-object key.
// Iterator.Error is not called: the cachekv merge iterator reports an
// exhausted range as an error.
func (app *App) AuthoritySnapshotForTest() []byte {
	return snapshotStore(app.authorityStore())
}

// FinalizeAuthoritySnapshotForTest is the same dump of the authority store
// in the finalize-block branch: the committed store plus every write the
// current block has made. After a FinalizeBlock that returned an error it
// shows what that block wrote before failing. It panics before the first
// FinalizeBlock.
func (app *App) FinalizeAuthoritySnapshotForTest() []byte {
	return snapshotStore(app.GetContextForFinalizeBlock(nil).KVStore(app.authority.StoreKey()))
}

func snapshotStore(store storetypes.KVStore) []byte {
	iter := store.Iterator(nil, nil)
	defer iter.Close()
	var out []byte
	for ; iter.Valid(); iter.Next() {
		out = binary.BigEndian.AppendUint64(out, uint64(len(iter.Key())))
		out = append(out, iter.Key()...)
		out = binary.BigEndian.AppendUint64(out, uint64(len(iter.Value())))
		out = append(out, iter.Value()...)
	}
	return out
}

// InstallMismatchedRemovalForTest writes a pending removal of the first
// validator, due at height 1, whose consensus key has its first byte
// flipped. It writes the store directly: it is not a message, and no
// propose, approve, or execute ran. The next EndBlock must halt with
// ERROR_CODE_CONFLICT.
func (app *App) InstallMismatchedRemovalForTest() error {
	store := app.authorityStore()
	st, err := authority.LoadState(store, app.ChainID(), 0, 0)
	if err != nil {
		return err
	}
	validators := st.Validators()
	if len(validators) == 0 {
		return fmt.Errorf("no validator")
	}
	wrong := bytes.Clone(validators[0].GetConsensusPubkey())
	wrong[0] ^= 0xff
	entry := &authorityv1.PendingValidatorPower{
		ProposalId:      "commit-bad-key",
		ConsensusPubkey: wrong,
		Power:           0,
		NodeId:          validators[0].GetNodeId(),
		ReasonDigest:    &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: make([]byte, 32)},
		Activation:      &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: 1}},
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(entry)
	if err != nil {
		return err
	}
	store.Set(authority.PendingKey(entry), raw)
	return nil
}

// ProposalStateForTest reads the committed state of one council proposal
// from its record (prefix 0x10 || proposal id). The record is JSON; only its
// state field is decoded, into a typed struct. found is false when no record
// exists.
func (app *App) ProposalStateForTest(id string) (state authorityv1.ProposalState, found bool, err error) {
	raw := app.authorityStore().Get(append([]byte{0x10}, id...))
	if raw == nil {
		return 0, false, nil
	}
	var rec struct {
		State int32 `json:"state"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		return 0, true, fmt.Errorf("proposal %s record: %w", id, err)
	}
	return authorityv1.ProposalState(rec.State), true, nil
}

// RevokeAdmissionForTest deletes the ante admission row of addr. No message
// does this. The ante must then refuse that account.
func (app *App) RevokeAdmissionForTest(addr []byte) {
	app.authorityStore().Delete(authority.AdmissionKey(addr))
}

// ExportAuthorityModuleForTest calls the authority module's ExportGenesis
// directly, without the App.Export check, and returns what it panicked with
// (nil when it did not panic).
func (app *App) ExportAuthorityModuleForTest() (raw json.RawMessage, panicked any) {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID()})
	defer func() {
		panicked = recover()
	}()
	mod := app.mm.Modules[authority.ModuleName].(module.HasABCIGenesis)
	return mod.ExportGenesis(ctx, app.encoding.Codec), nil
}

// AccountSequenceForTest is the committed auth sequence of addr, and whether
// the account exists.
func (app *App) AccountSequenceForTest(addr []byte) (uint64, bool) {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID()})
	keeper := app.mm.Modules[authtypes.ModuleName].(authModule).keeper
	account := keeper.GetAccount(ctx, sdk.AccAddress(addr))
	if account == nil {
		return 0, false
	}
	return account.GetSequence(), true
}
