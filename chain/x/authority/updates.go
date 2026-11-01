package authority

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"slices"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtencoding "github.com/cometbft/cometbft/crypto/encoding"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// InitialUpdates builds the genesis validator set.
//
// EndBlock calls Changeset, which uses the same validatorUpdate constructor,
// when a scheduled change is due. No other module package may build an
// abci.ValidatorUpdate.
func InitialUpdates(validators []*authorityv1.ValidatorPower) ([]abci.ValidatorUpdate, error) {
	if len(validators) == 0 {
		return nil, fmt.Errorf("validator set is empty")
	}
	ordered := slices.Clone(validators)
	slices.SortFunc(ordered, func(a, b *authorityv1.ValidatorPower) int {
		return bytes.Compare(a.GetConsensusPubkey(), b.GetConsensusPubkey())
	})
	updates := make([]abci.ValidatorUpdate, 0, len(ordered))
	var previous []byte
	for _, row := range ordered {
		pub := row.GetConsensusPubkey()
		if len(pub) != ed25519.PubKeySize {
			return nil, fmt.Errorf("consensus pubkey length")
		}
		if bytes.Equal(previous, pub) {
			return nil, fmt.Errorf("duplicate validator key")
		}
		previous = bytes.Clone(pub)
		if row.GetPower() <= 0 {
			return nil, fmt.Errorf("nonpositive validator power")
		}
		update, err := validatorUpdate(pub, row.GetPower())
		if err != nil {
			return nil, err
		}
		updates = append(updates, update)
	}
	return updates, nil
}

// validatorUpdate is the only composite literal of abci.ValidatorUpdate.
// InitialUpdates and Changeset both call it. No other module may.
func validatorUpdate(pub []byte, power int64) (abci.ValidatorUpdate, error) {
	encoded, err := cmtencoding.PubKeyToProto(ed25519.PubKey(pub))
	if err != nil {
		return abci.ValidatorUpdate{}, err
	}
	return abci.ValidatorUpdate{PubKey: encoded, Power: power}, nil
}

// CometEffectiveDelay is the height gap in CometBFT v0.40.
//
// state/execution.go updateState copies the pre-update NextValidators into
// Validators and applies ValidatorUpdates to the following NextValidators.
// The comment there is "only applies to the next next height", and
// LastHeightValidatorsChanged is header.Height+2. A changeset returned from
// FinalizeBlock at height H is the voting set that signs height H+2.
const CometEffectiveDelay int64 = 2

// Changeset is the difference the application returns at the activation
// height. Pubkeys that disappear are power 0. Power and key rotations are
// not the same update: a new consensus key is an add plus a removal.
func Changeset(before, after []*authorityv1.ValidatorPower) ([]abci.ValidatorUpdate, error) {
	prev := map[string]int64{}
	for _, row := range before {
		prev[string(row.GetConsensusPubkey())] = row.GetPower()
	}
	next := map[string]int64{}
	for _, row := range after {
		next[string(row.GetConsensusPubkey())] = row.GetPower()
	}
	keys := make([]string, 0, len(prev)+len(next))
	seen := map[string]struct{}{}
	for key := range prev {
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	for key := range next {
		if _, ok := seen[key]; ok {
			continue
		}
		keys = append(keys, key)
	}
	slices.Sort(keys)
	updates := make([]abci.ValidatorUpdate, 0)
	for _, key := range keys {
		oldPower, had := prev[key]
		newPower, has := next[key]
		if had && has && oldPower == newPower {
			continue
		}
		power := newPower
		if !has {
			power = 0
		}
		update, err := validatorUpdate([]byte(key), power)
		if err != nil {
			return nil, err
		}
		updates = append(updates, update)
	}
	return updates, nil
}

// PubkeyHex returns the canonical hex form of a consensus public key.
func PubkeyHex(raw []byte) string {
	return hex.EncodeToString(raw)
}
