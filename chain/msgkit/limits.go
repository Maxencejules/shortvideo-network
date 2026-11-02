package msgkit

import (
	"sort"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
)

// Governed limit keys. Their ceilings are bounds.json governed_limit_caps.
// A council GovernedLimitChange can lower a limit; nothing can raise it
// above its ceiling.
const (
	LimitMaxEvidenceBatch = "max_evidence_batch"
	LimitMaxPageLimit     = "max_page_limit"
	LimitMaxOpenOffers    = "max_open_offers"
	LimitMaxExpirySteps   = "max_expiry_steps"
	LimitMaxPackageBytes  = "max_package_bytes"
)

var ceilings = map[string]uint64{
	LimitMaxEvidenceBatch: uint64(genesis.MaxEvidenceBatch),
	LimitMaxPageLimit:     uint64(genesis.MaxPageLimit),
	LimitMaxOpenOffers:    uint64(genesis.MaxOpenOffersPerProvider),
	LimitMaxExpirySteps:   uint64(genesis.MaxExpiryStepsPerBlock),
	LimitMaxPackageBytes:  genesis.MaxPackageBytes,
}

// LimitKeys returns the governed limit keys in byte order.
func LimitKeys() []string {
	keys := make([]string, 0, len(ceilings))
	for key := range ceilings {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// Ceiling returns the protocol cap of one governed limit key.
func Ceiling(key string) (uint64, bool) {
	v, ok := ceilings[key]
	return v, ok
}

// Effective is the limit in force (effective_limit in the spec machine):
// the governed value when one is set and 1 <= value <= ceiling, otherwise
// the ceiling. An unknown key returns 0, so a caller that misnames a key
// rejects every positive amount instead of accepting an unbounded one.
func Effective(key string, governed uint64, found bool) uint64 {
	ceiling, ok := Ceiling(key)
	if !ok {
		return 0
	}
	if found && governed >= 1 && governed <= ceiling {
		return governed
	}
	return ceiling
}
