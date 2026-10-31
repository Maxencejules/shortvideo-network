package app

import (
	"context"
	"fmt"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/gogoproto/proto"

	"github.com/cosmos/cosmos-sdk/baseapp"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// consensusParamKey stores CometBFT block and evidence bounds.
// It is not a validator-power record. Voting power is only in x/authority.
const consensusParamKey byte = 0x03

type consensusParamsStore struct {
	key *storetypes.KVStoreKey
}

func newConsensusParamsStore(key *storetypes.KVStoreKey) baseapp.ParamStore {
	return consensusParamsStore{key: key}
}

func (s consensusParamsStore) Get(ctx context.Context) (cmtproto.ConsensusParams, error) {
	raw := sdk.UnwrapSDKContext(ctx).KVStore(s.key).Get([]byte{consensusParamKey})
	if len(raw) == 0 {
		return cmtproto.ConsensusParams{}, fmt.Errorf("consensus params are not stored")
	}
	var params cmtproto.ConsensusParams
	if err := proto.Unmarshal(raw, &params); err != nil {
		return cmtproto.ConsensusParams{}, err
	}
	return params, nil
}

func (s consensusParamsStore) Has(ctx context.Context) (bool, error) {
	return sdk.UnwrapSDKContext(ctx).KVStore(s.key).Has([]byte{consensusParamKey}), nil
}

func (s consensusParamsStore) Set(ctx context.Context, params cmtproto.ConsensusParams) error {
	raw, err := proto.Marshal(&params)
	if err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).KVStore(s.key).Set([]byte{consensusParamKey}, raw)
	return nil
}
