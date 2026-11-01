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

// sideDocument is the authority state that has no field in the frozen
// authority proto. It is JSON with sorted rows, written only by save.
//
// Goal 07 moved provider rows to x/providers. The document no longer has
// "providers" or "prev_provider"; encoding/json ignores those keys when an
// older document is decoded, and the next save drops them.
type sideDocument struct {
	PolicyVersion uint64        `json:"policy_version"`
	Payloads      []namedText   `json:"payloads,omitempty"`
	Decoded       []decodedWire `json:"decoded,omitempty"`
	Business      []namedBytes  `json:"business,omitempty"`
	Executed      []namedBytes  `json:"executed,omitempty"`
	Governed      []namedUint   `json:"governed,omitempty"`
	PrevCouncil   [][]byte      `json:"prev_council,omitempty"`
	PrevService   [][]byte      `json:"prev_service,omitempty"`
}

type namedText struct {
	ID    string `json:"id"`
	Value string `json:"value"`
}

// decodedWire is one storedChange. A force_withdraw row keeps its reason
// digest value in reason, next to content_id and content_version.
type decodedWire struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	NodeID         string `json:"node_id,omitempty"`
	PubKey         []byte `json:"pub_key,omitempty"`
	Power          int64  `json:"power"`
	Reason         []byte `json:"reason,omitempty"`
	ByHeight       bool   `json:"by_height,omitempty"`
	Height         uint64 `json:"height,omitempty"`
	Unix           int64  `json:"unix,omitempty"`
	LimitKey       string `json:"limit_key,omitempty"`
	LimitValue     uint64 `json:"limit_value,omitempty"`
	ProviderID     string `json:"provider_id,omitempty"`
	ContentID      string `json:"content_id,omitempty"`
	ContentVersion uint64 `json:"content_version,omitempty"`
}

type namedBytes struct {
	ID    string `json:"id"`
	Value []byte `json:"value"`
}

type namedUint struct {
	ID    string `json:"id"`
	Value uint64 `json:"value"`
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
		Decoded:       map[string]storedChange{},
		Business:      map[string][]byte{},
		Executed:      map[string][]byte{},
		Governed:      map[string]uint64{},
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
	for _, row := range side.Decoded {
		st.Decoded[row.ID] = storedChange{
			Kind: row.Kind, NodeID: row.NodeID, PubKey: row.PubKey, Power: row.Power, Reason: row.Reason,
			ByHeight: row.ByHeight, Height: row.Height, Unix: row.Unix, LimitKey: row.LimitKey,
			LimitValue: row.LimitValue, ProviderID: row.ProviderID, ContentID: row.ContentID,
			ContentVersion: row.ContentVersion,
		}
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
	st.PrevCouncil = side.PrevCouncil
	st.PrevService = side.PrevService
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
	for id, row := range st.Decoded {
		doc.Decoded = append(doc.Decoded, decodedWire{
			ID: id, Kind: row.Kind, NodeID: row.NodeID, PubKey: row.PubKey, Power: row.Power, Reason: row.Reason,
			ByHeight: row.ByHeight, Height: row.Height, Unix: row.Unix, LimitKey: row.LimitKey,
			LimitValue: row.LimitValue, ProviderID: row.ProviderID, ContentID: row.ContentID,
			ContentVersion: row.ContentVersion,
		})
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
	doc.PrevCouncil = st.PrevCouncil
	doc.PrevService = st.PrevService
	slices.SortFunc(doc.Payloads, func(a, b namedText) int { return bytes.Compare([]byte(a.ID), []byte(b.ID)) })
	slices.SortFunc(doc.Decoded, func(a, b decodedWire) int { return bytes.Compare([]byte(a.ID), []byte(b.ID)) })
	slices.SortFunc(doc.Business, func(a, b namedBytes) int { return bytes.Compare([]byte(a.ID), []byte(b.ID)) })
	slices.SortFunc(doc.Executed, func(a, b namedBytes) int { return bytes.Compare([]byte(a.ID), []byte(b.ID)) })
	slices.SortFunc(doc.Governed, func(a, b namedUint) int { return bytes.Compare([]byte(a.ID), []byte(b.ID)) })
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
