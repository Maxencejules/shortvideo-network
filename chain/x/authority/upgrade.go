package authority

import (
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BumpPolicyVersionForUpgrade adds one to the policy version (0x04). The
// upgrade runner calls it when an upgrade applies (DR-10-38, now the
// owner's Q2 ruling "Council plan, fail closed (Recommended)": approved but
// unexecuted proposals become stale at the upgrade); no other code may
// (TestBumpPolicyVersionCallSites). Every proposal on the old base version
// then fails its execute with STALE_VERSION or CONFLICT, and stops counting
// against its seat's open quota (0x14 is keyed by base version). Changes an
// execute already scheduled are not touched.
//
// It checks before it writes: a stored version that is not 8 bytes, or is
// below 2, is ERROR_CODE_INTEGRITY, and version 2^64-1 is
// ERROR_CODE_OVERFLOW; neither writes anything.
func (k Keeper) BumpPolicyVersionForUpgrade(ctx sdk.Context) error {
	kv := ctx.KVStore(k.storeKey)
	version, out := readPolicyVersion(kv)
	if !out.Success() {
		return sdkError(out.Code)
	}
	if version == math.MaxUint64 {
		return sdkError(CodeOverflow)
	}
	kv.Set([]byte{policyKey}, encodeU64(version+1))
	return nil
}
