package authority

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// ProviderAdmission is the part of x/providers an executed council proposal
// uses. x/providers implements it; authority never imports that package.
// There is no single-signature admit or disable message (DEC-009).
type ProviderAdmission interface {
	// AdmitByCouncil moves an APPLIED provider to ADMITTED. devnet is the
	// DEC-002 flag authority computes. Absent: NOT_FOUND; devnet false:
	// DECISION_BLOCKED; wrong operator group or state: FORBIDDEN.
	AdmitByCouncil(ctx sdk.Context, providerID string, devnet bool) msgkit.Code
	// DisableByCouncil moves an ADMITTED or DRAINING provider to DISABLED.
	// Absent: NOT_FOUND; any other state: CONFLICT.
	DisableByCouncil(ctx sdk.Context, providerID string) msgkit.Code
}

// ContentRemoval is the part of x/content an executed council
// ForceWithdrawChange uses. x/content implements it.
type ContentRemoval interface {
	// ForceWithdrawByCouncil withdraws one content version and writes its
	// tombstone with the 32-byte reason digest value. devnet is the DEC-003
	// flag authority computes. Absent: NOT_FOUND; devnet false:
	// DECISION_BLOCKED; already withdrawn: WITHDRAWN; other version:
	// STALE_VERSION.
	ForceWithdrawByCouncil(ctx sdk.Context, contentID string, version uint64, reason []byte, devnet bool) msgkit.Code
}

// WithRegistry returns a copy of k that executes provider and content
// payloads through p and c. A nil hook is a wiring error and panics before
// the chain starts.
func (k Keeper) WithRegistry(p ProviderAdmission, c ContentRemoval) Keeper {
	if p == nil || c == nil {
		panic("authority: WithRegistry needs both the provider and the content hook")
	}
	k.providers = p
	k.content = c
	return k
}

// WithPeers returns a copy of k whose business gate also consults the
// ledgers in peers. Business ids are one protocol namespace across
// authority, providers, and content. Peers may be filled after this call;
// the pointer is shared. A nil peers set consults no other module.
func (k Keeper) WithPeers(peers *msgkit.Peers) Keeper {
	k.peers = peers
	return k
}

// registry binds the hooks to one transaction context. It returns nil when
// no hook is wired, so the engine reports MALFORMED.
func (k Keeper) registry(ctx sdk.Context) Registry {
	if k.providers == nil || k.content == nil {
		return nil
	}
	return boundRegistry{ctx: ctx, providers: k.providers, content: k.content}
}

// external is the global business-id lookup for the engine. It skips this
// module's own ledger; the engine reads authority's own id from 0x11.
func (k Keeper) external(ctx sdk.Context) func(string) ([]byte, bool) {
	if k.peers == nil {
		return nil
	}
	peers := k.peers
	return func(id string) ([]byte, bool) {
		return peers.Lookup(ctx, id, ModuleName)
	}
}

type boundRegistry struct {
	ctx       sdk.Context
	providers ProviderAdmission
	content   ContentRemoval
}

func (r boundRegistry) Admit(providerID string, devnet bool) Outcome {
	return fromCode(r.providers.AdmitByCouncil(r.ctx, providerID, devnet))
}

func (r boundRegistry) Disable(providerID string) Outcome {
	return fromCode(r.providers.DisableByCouncil(r.ctx, providerID))
}

func (r boundRegistry) ForceWithdraw(contentID string, version uint64, reason []byte, devnet bool) Outcome {
	return fromCode(r.content.ForceWithdrawByCouncil(r.ctx, contentID, version, reason, devnet))
}

func fromCode(code msgkit.Code) Outcome {
	if code == msgkit.OK {
		return ok()
	}
	return fail(string(code))
}
