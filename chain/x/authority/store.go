package authority

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

const sideKey byte = 0x20

type sideDocument struct {
	PolicyVersion uint64         `json:"policy_version"`
	Payloads      []namedText    `json:"payloads,omitempty"`
	Business      []namedBytes   `json:"business,omitempty"`
	Executed      []namedBytes   `json:"executed,omitempty"`
	Governed      []namedUint    `json:"governed,omitempty"`
	Providers     []providerWire `json:"providers,omitempty"`
	PrevCouncil   [][]byte       `json:"prev_council,omitempty"`
	PrevService   [][]byte       `json:"prev_service,omitempty"`
	PrevProvider  []namedList    `json:"prev_provider,omitempty"`
}

type namedText struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

type namedBytes struct {
	ID    string `json:"id"`
	Value []byte `json:"value"`
}

type namedUint struct {
	ID    string `json:"id"`
	Value uint64 `json:"value"`
}

type namedList struct {
	ID    string   `json:"id"`
	Value [][]byte `json:"value"`
}

type providerWire struct {
	ID            string `json:"id"`
	Account       []byte `json:"account"`
	SigningPub    []byte `json:"signing_pub"`
	SigningKeyID  string `json:"signing_key_id"`
	Generation    uint64 `json:"generation"`
	State         string `json:"state"`
	OperatorGroup string `json:"operator_group"`
	Endpoint      []byte `json:"endpoint"`
	Capacity      uint64 `json:"capacity"`
}

func (k Keeper) load(ctx sdk.Context) (*State, error) {
	raw := ctx.KVStore(k.storeKey).Get([]byte{stateKey})
	if raw == nil {
		return nil, errMissing
	}
	var genesis authorityv1.GenesisState
	if err := proto.Unmarshal(raw, &genesis); err != nil {
		return nil, err
	}
	st := &State{
		PolicyVersion: 1,
		Validators:    genesis.GetAuthority().GetValidators(),
		Submitters:    genesis.GetAuthority().GetSubmitters(),
		Seats:         genesis.GetAuthority().GetDevnetTestSeats(),
		Proposals:     genesis.GetAuthority().GetProposals(),
		Pending:       genesis.GetAuthority().GetPendingValidatorPower(),
		Params:        genesis.GetDevnetTestParameters(),
		Payloads:      map[string]string{},
		Business:      map[string][]byte{},
		Executed:      map[string][]byte{},
		Providers:     map[string]*ProviderRecord{},
		Governed:      map[string]uint64{},
		PrevProvider:  map[string][][]byte{},
	}
	sideRaw := ctx.KVStore(k.storeKey).Get([]byte{sideKey})
	if sideRaw == nil {
		return st, nil
	}
	var side sideDocument
	if err := json.Unmarshal(sideRaw, &side); err != nil {
		return nil, err
	}
	if side.PolicyVersion != 0 {
		st.PolicyVersion = side.PolicyVersion
	}
	for _, row := range side.Payloads {
		st.Payloads[row.ID] = row.Value
	}
	for _, row := range side.Business {
		st.Business[row.ID] = row.Value
	}
	for _, row := range side.Executed {
		st.Executed[row.ID] = row.Value
	}
	for _, row := range side.Governed {
		st.Governed[row.ID] = row.Value
	}
	for _, row := range side.Providers {
		st.Providers[row.ID] = &ProviderRecord{
			ID: row.ID, Account: row.Account, SigningPub: row.SigningPub, SigningKeyID: row.SigningKeyID,
			Generation: row.Generation, State: row.State, OperatorGroup: row.OperatorGroup,
			Endpoint: row.Endpoint, Capacity: row.Capacity,
		}
	}
	st.PrevCouncil = side.PrevCouncil
	st.PrevService = side.PrevService
	for _, row := range side.PrevProvider {
		st.PrevProvider[row.ID] = row.Value
	}
	return st, nil
}

func (k Keeper) save(ctx sdk.Context, st *State) error {
	current, err := k.Export(ctx)
	if err != nil {
		return err
	}
	if current.Authority == nil {
		current.Authority = &authorityv1.AuthorityState{}
	}
	current.Authority.Validators = st.Validators
	current.Authority.Submitters = st.Submitters
	current.Authority.DevnetTestSeats = st.Seats
	current.Authority.Proposals = st.Proposals
	current.Authority.PendingValidatorPower = st.Pending
	current.Authority.QuorumLoss = authorityv1.QuorumLossBehavior_QUORUM_LOSS_BEHAVIOR_UNSET
	raw, err := marshalState(current)
	if err != nil {
		return err
	}
	ctx.KVStore(k.storeKey).Set([]byte{stateKey}, raw)
	side, err := json.Marshal(sideFrom(st))
	if err != nil {
		return err
	}
	ctx.KVStore(k.storeKey).Set([]byte{sideKey}, side)
	k.reindexSubmitters(ctx, st)
	return nil
}

func sideFrom(st *State) sideDocument {
	doc := sideDocument{PolicyVersion: st.PolicyVersion}
	for id, value := range st.Payloads {
		doc.Payloads = append(doc.Payloads, namedText{ID: id, Value: value})
	}
	for id, value := range st.Business {
		doc.Business = append(doc.Business, namedBytes{ID: id, Value: value})
	}
	for id, value := range st.Executed {
		doc.Executed = append(doc.Executed, namedBytes{ID: id, Value: value})
	}
	for id, value := range st.Governed {
		doc.Governed = append(doc.Governed, namedUint{ID: id, Value: value})
	}
	for id, row := range st.Providers {
		doc.Providers = append(doc.Providers, providerWire{
			ID: id, Account: row.Account, SigningPub: row.SigningPub, SigningKeyID: row.SigningKeyID,
			Generation: row.Generation, State: row.State, OperatorGroup: row.OperatorGroup,
			Endpoint: row.Endpoint, Capacity: row.Capacity,
		})
	}
	doc.PrevCouncil = st.PrevCouncil
	doc.PrevService = st.PrevService
	for id, rows := range st.PrevProvider {
		doc.PrevProvider = append(doc.PrevProvider, namedList{ID: id, Value: rows})
	}
	slices.SortFunc(doc.Payloads, func(a, b namedText) int { return bytes.Compare([]byte(a.ID), []byte(b.ID)) })
	slices.SortFunc(doc.Business, func(a, b namedBytes) int { return bytes.Compare([]byte(a.ID), []byte(b.ID)) })
	slices.SortFunc(doc.Executed, func(a, b namedBytes) int { return bytes.Compare([]byte(a.ID), []byte(b.ID)) })
	slices.SortFunc(doc.Governed, func(a, b namedUint) int { return bytes.Compare([]byte(a.ID), []byte(b.ID)) })
	slices.SortFunc(doc.Providers, func(a, b providerWire) int { return bytes.Compare([]byte(a.ID), []byte(b.ID)) })
	slices.SortFunc(doc.PrevProvider, func(a, b namedList) int { return bytes.Compare([]byte(a.ID), []byte(b.ID)) })
	return doc
}

func (k Keeper) reindexSubmitters(ctx sdk.Context, st *State) {
	store := ctx.KVStore(k.storeKey)
	for _, old := range st.PrevService {
		if len(old) == 20 {
			store.Delete(admitKeyBytes(old))
		}
	}
	for _, row := range st.Submitters {
		if len(row.GetAddress()) == 20 {
			store.Set(admitKeyBytes(row.GetAddress()), []byte{byte(row.GetRole())})
		}
	}
}

var errMissing = fmt.Errorf("authority state is missing")
