package authority

import (
	"google.golang.org/protobuf/proto"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

func cloneValidators(rows []*authorityv1.ValidatorPower) []*authorityv1.ValidatorPower {
	out := make([]*authorityv1.ValidatorPower, len(rows))
	for i, row := range rows {
		out[i] = proto.Clone(row).(*authorityv1.ValidatorPower)
	}
	return out
}
