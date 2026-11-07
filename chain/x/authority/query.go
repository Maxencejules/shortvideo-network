package authority

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Page key tags of Query/Governance (DR-10-23, interim). A page key is a
// tag byte followed by the cursor of the first item of the next page, or a
// bare proposal id. Proposal ids start with [a-z0-9], so they never start
// with a tag byte.
const (
	// pageValidators || consensus key (32 bytes).
	pageValidators byte = 0x01
	// pageSubmitters || address (20 bytes).
	pageSubmitters byte = 0x02
	// pageSeats || seat id.
	pageSeats byte = 0x03
	// pagePending || position (1 byte) in store-key order.
	pagePending byte = 0x04
)

// Sections of the Governance walk, in walk order.
const (
	sectionValidators = iota
	sectionSubmitters
	sectionSeats
	sectionPending
	sectionProposals
)

// governanceCursor is a decoded page key: the section of the next item and
// its cursor (a key for every section but pending, a position for pending).
type governanceCursor struct {
	section int
	key     []byte
	index   int
}

// Governance is svn.authority.v1.Query/Governance (D-9, DR-10-23 interim
// paging). It reads committed state at the query height and never writes.
//
// One ordered walk covers, in this order: validators by consensus key,
// submitters by address, council seats by seat id, pending validator changes
// in store-key order (the spec activation order), then proposal records by
// proposal id. Every item of every section counts against the page limit,
// the first one included. A proposal carries approver_keys only; the proto
// has no field for the approving seat ids. approved_council is always empty
// (DEC-008) and quorum_loss is the stored value on every page.
//
//   - limit: msgkit.CheckPage against the effective max_page_limit (governed
//     value, at most the protocol ceiling 100); 0 or an absent page uses the
//     cap; a limit above the cap is BOUNDS.
//   - page_key: empty starts the walk. A key over 64 bytes, a tag byte with a
//     cursor of the wrong shape (a consensus key that is not 32 bytes, an
//     address that is not 20 bytes, a seat id outside the id pattern, a
//     pending cursor that is not one byte or names no entry), any other first
//     byte, or a bare proposal id outside the id pattern is BOUNDS.
//   - next_page_key: the page key of the first item after the page; empty on
//     the last page. A key cursor resumes at the first item at or after it.
//     A pending position is exact only at one height: a walk that spans
//     heights should query at the height of its first page.
//
// The work of one call is the config key, the pending prefix (at most
// MaxPendingChanges entries), and at most limit+1 proposal records. A
// stored record or entry that does not decode is a plain error.
func (k Keeper) Governance(goCtx context.Context, req *authorityv1.QueryGovernanceRequest) (*authorityv1.QueryGovernanceResponse, error) {
	ctx, err := sdkContext(goCtx)
	if err != nil {
		return nil, err
	}
	limit, _, code := msgkit.CheckPage(&typesv1.PageRequest{Limit: req.GetPage().GetLimit()}, k.pageLimit(ctx))
	if code != msgkit.OK {
		return nil, code.QueryErr()
	}
	cursor, code := parseGovernanceKey(req.GetPage().GetPageKey())
	if code != msgkit.OK {
		return nil, code.QueryErr()
	}
	kv := ctx.KVStore(k.storeKey)
	cfg, err := readConfig(kv)
	if err != nil {
		return nil, fmt.Errorf("authority governance query: %w", err)
	}
	pending, err := readPending(kv)
	if err != nil {
		return nil, fmt.Errorf("authority governance query: %w", err)
	}
	if cursor.section == sectionPending && cursor.index >= len(pending) {
		return nil, msgkit.Bounds.QueryErr()
	}
	page := &governancePage{limit: limit, state: &authorityv1.AuthorityState{QuorumLoss: cfg.GetAuthority().GetQuorumLoss()}}
	if page.walkConfig(cursor, cfg.GetAuthority(), pending) {
		if err := page.walkProposals(kv, cursor); err != nil {
			return nil, err
		}
	}
	return &authorityv1.QueryGovernanceResponse{
		State: page.state,
		Page:  &typesv1.PageResponse{NextPageKey: page.next},
	}, nil
}

// parseGovernanceKey decodes a page key (see Governance).
func parseGovernanceKey(raw []byte) (governanceCursor, msgkit.Code) {
	if len(raw) == 0 {
		return governanceCursor{section: sectionValidators}, msgkit.OK
	}
	if len(raw) > msgkit.PageKeyMaxBytes {
		return governanceCursor{}, msgkit.Bounds
	}
	rest := raw[1:]
	switch raw[0] {
	case pageValidators:
		if len(rest) != 32 {
			return governanceCursor{}, msgkit.Bounds
		}
		return governanceCursor{section: sectionValidators, key: bytes.Clone(rest)}, msgkit.OK
	case pageSubmitters:
		if len(rest) != 20 {
			return governanceCursor{}, msgkit.Bounds
		}
		return governanceCursor{section: sectionSubmitters, key: bytes.Clone(rest)}, msgkit.OK
	case pageSeats:
		if !idPattern.Match(rest) {
			return governanceCursor{}, msgkit.Bounds
		}
		return governanceCursor{section: sectionSeats, key: bytes.Clone(rest)}, msgkit.OK
	case pagePending:
		if len(rest) != 1 || int(rest[0]) >= MaxPendingChanges {
			return governanceCursor{}, msgkit.Bounds
		}
		return governanceCursor{section: sectionPending, index: int(rest[0])}, msgkit.OK
	default:
		if !idPattern.Match(raw) {
			return governanceCursor{}, msgkit.Bounds
		}
		return governanceCursor{section: sectionProposals, key: bytes.Clone(raw)}, msgkit.OK
	}
}

// governancePage collects one page of the walk.
type governancePage struct {
	limit uint32
	count uint32
	next  []byte
	state *authorityv1.AuthorityState
}

// take reports whether the page has room for one more item and counts it.
// When the page is full it records next, the page key of that item, and
// the walk stops.
func (p *governancePage) take(next []byte) bool {
	if p.count == p.limit {
		p.next = next
		return false
	}
	p.count++
	return true
}

// walkConfig adds the validators, submitters, seats, and pending entries at
// or after cursor. It returns false when the page filled up before the
// proposals section.
func (p *governancePage) walkConfig(cursor governanceCursor, auth *authorityv1.AuthorityState, pending []*authorityv1.PendingValidatorPower) bool {
	if cursor.section == sectionValidators {
		rows := cloneValidators(auth.GetValidators())
		slices.SortFunc(rows, func(a, b *authorityv1.ValidatorPower) int {
			return bytes.Compare(a.GetConsensusPubkey(), b.GetConsensusPubkey())
		})
		for _, row := range rows {
			if bytes.Compare(row.GetConsensusPubkey(), cursor.key) < 0 {
				continue
			}
			if !p.take(append([]byte{pageValidators}, row.GetConsensusPubkey()...)) {
				return false
			}
			p.state.Validators = append(p.state.Validators, row)
		}
		cursor = governanceCursor{section: sectionSubmitters}
	}
	if cursor.section == sectionSubmitters {
		rows := make([]*typesv1.Submitter, 0, len(auth.GetSubmitters()))
		for _, row := range auth.GetSubmitters() {
			rows = append(rows, proto.Clone(row).(*typesv1.Submitter))
		}
		slices.SortFunc(rows, func(a, b *typesv1.Submitter) int { return bytes.Compare(a.GetAddress(), b.GetAddress()) })
		for _, row := range rows {
			if bytes.Compare(row.GetAddress(), cursor.key) < 0 {
				continue
			}
			if !p.take(append([]byte{pageSubmitters}, row.GetAddress()...)) {
				return false
			}
			p.state.Submitters = append(p.state.Submitters, row)
		}
		cursor = governanceCursor{section: sectionSeats}
	}
	if cursor.section == sectionSeats {
		rows := make([]*authorityv1.DevnetTestSeat, 0, len(auth.GetDevnetTestSeats()))
		for _, row := range auth.GetDevnetTestSeats() {
			rows = append(rows, proto.Clone(row).(*authorityv1.DevnetTestSeat))
		}
		slices.SortFunc(rows, func(a, b *authorityv1.DevnetTestSeat) int { return strings.Compare(a.GetSeatId(), b.GetSeatId()) })
		for _, row := range rows {
			if row.GetSeatId() < string(cursor.key) {
				continue
			}
			if !p.take(append([]byte{pageSeats}, row.GetSeatId()...)) {
				return false
			}
			p.state.DevnetTestSeats = append(p.state.DevnetTestSeats, row)
		}
		cursor = governanceCursor{section: sectionPending}
	}
	if cursor.section == sectionPending {
		for i := cursor.index; i < len(pending); i++ {
			if !p.take([]byte{pagePending, byte(i)}) {
				return false
			}
			p.state.PendingValidatorPower = append(p.state.PendingValidatorPower, pending[i])
		}
	}
	return true
}

// walkProposals adds proposal records in proposal id order, from the cursor
// id when the cursor is in the proposals section. It visits at most one
// record past the page.
func (p *governancePage) walkProposals(kv KV, cursor governanceCursor) error {
	start := []byte{proposalPrefix}
	if cursor.section == sectionProposals {
		start = proposalKey(string(cursor.key))
	}
	iter := kv.Iterator(start, prefixEnd(proposalPrefix))
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		id := string(iter.Key()[1:])
		if !p.take([]byte(id)) {
			return nil
		}
		rec, err := decodeRecord(id, iter.Value())
		if err != nil {
			return fmt.Errorf("authority governance query: %w", err)
		}
		p.state.Proposals = append(p.state.Proposals, proposalView(rec))
	}
	return nil
}

// proposalView is the protocol Proposal of a stored record.
func proposalView(rec *proposalRecord) *authorityv1.Proposal {
	keys := make([][]byte, len(rec.ApproverKeys))
	for i, key := range rec.ApproverKeys {
		keys[i] = bytes.Clone(key)
	}
	return &authorityv1.Proposal{
		ProposalId:              rec.ProposalID,
		ParameterKey:            rec.ParameterKey,
		BasePolicyVersion:       rec.BasePolicyVersion,
		PayloadDigest:           &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: bytes.Clone(rec.PayloadDigest)},
		State:                   rec.state(),
		ExpiryUnixSeconds:       rec.ExpiryUnixSeconds,
		ExecuteAfterUnixSeconds: rec.ExecuteAfterUnixSeconds,
		ApproverKeys:            keys,
		BusinessId:              rec.BusinessID,
	}
}

// pageLimit is the effective max_page_limit: the governed value (0x13) or
// the protocol ceiling.
func (k Keeper) pageLimit(ctx sdk.Context) uint32 {
	governed, found := k.GovernedLimit(ctx, msgkit.LimitMaxPageLimit)
	return uint32(msgkit.Effective(msgkit.LimitMaxPageLimit, governed, found))
}

// sdkContext unwraps the SDK context of a query.
func sdkContext(goCtx context.Context) (sdk.Context, error) {
	if ctx, ok := goCtx.(sdk.Context); ok {
		return ctx, nil
	}
	if ctx, ok := goCtx.Value(sdk.SdkContextKey).(sdk.Context); ok {
		return ctx, nil
	}
	return sdk.Context{}, svnerr.ErrMalformed.Wrap("authority: no SDK context")
}
