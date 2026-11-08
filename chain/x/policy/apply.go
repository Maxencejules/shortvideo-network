package policy

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ApplyDue is the BeginBlock body (deviation D-4, activation part). At block
// height b it applies every pending governed change whose activation height
// is at or below b, in key order: it writes the change's value as the
// governed value of its key (0x02), deletes the pending row (0x03), and
// emits policy_activated. Block b-1 read the old value; block b, and every
// transaction in it, reads the new one. The policy version does not move:
// the execute that scheduled the change already moved it.
//
// It reads every pending row before its first write. A store holds at most
// one pending row per governed limit key, so the work is bounded by the
// number of keys (five). A row that does not decode, names a key that is
// not a governed limit key, or holds a value above its key's ceiling, or a
// sixth row, is an error: ApplyDue writes nothing and emits nothing and
// returns it. The module's BeginBlock returns that error, so FinalizeBlock
// fails on every node alike and the block is not committed (fail closed)
// instead of running the block against a value nobody decided.
func (k Keeper) ApplyDue(ctx sdk.Context) error {
	store := k.kv(ctx)
	rows, err := readPending(store)
	if err != nil {
		return err
	}
	now := blockHeight(ctx)
	for _, row := range rows {
		if row.Activation > now {
			continue
		}
		store.Set(GovernedKey(row.Key), EncodeU64(row.Value))
		store.Delete(PendingKey(row.Key))
		emitActivated(ctx, row)
	}
	return nil
}
