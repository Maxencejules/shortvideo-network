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

// InitialUpdates is the only constructor of consensus validator updates.
//
// Goal 05 calls it from InitGenesis and nowhere else. EndBlock does not call
// it. No other module package may build an abci.ValidatorUpdate. Goal 06
// adds the scheduled-update control and is the next caller of this function.
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
		encoded, err := cmtencoding.PubKeyToProto(ed25519.PubKey(pub))
		if err != nil {
			return nil, err
		}
		updates = append(updates, abci.ValidatorUpdate{
			PubKey: encoded,
			Power:  row.GetPower(),
		})
	}
	return updates, nil
}

// PubkeyHex returns the canonical hex form of a consensus public key.
func PubkeyHex(raw []byte) string {
	return hex.EncodeToString(raw)
}
