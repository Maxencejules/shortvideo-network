package authority

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// governanceQuery calls Query/Governance through the hand-written service
// descriptor, as the SDK query router does: the request is marshalled and
// decoded by the handler.
func governanceQuery(t *testing.T, k Keeper, ctx sdk.Context, limit uint32, key []byte) (*authorityv1.QueryGovernanceResponse, error) {
	t.Helper()
	desc := QueryServiceDesc(k)
	if desc.ServiceName != "svn.authority.v1.Query" || len(desc.Methods) != 1 || desc.Methods[0].MethodName != "Governance" {
		t.Fatalf("service descriptor %s %v", desc.ServiceName, desc.Methods)
	}
	raw, err := proto.Marshal(&authorityv1.QueryGovernanceRequest{Page: &typesv1.PageRequest{Limit: limit, PageKey: key}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := desc.Methods[0].Handler(nil, ctx, func(v any) error { return proto.Unmarshal(raw, v.(proto.Message)) }, nil)
	if err != nil {
		return nil, err
	}
	return out.(*authorityv1.QueryGovernanceResponse), nil
}

// governanceItems names every item of one page in walk order.
func governanceItems(state *authorityv1.AuthorityState) []string {
	var out []string
	for _, row := range state.GetValidators() {
		out = append(out, "validator:"+hex.EncodeToString(row.GetConsensusPubkey()))
	}
	for _, row := range state.GetSubmitters() {
		out = append(out, "submitter:"+hex.EncodeToString(row.GetAddress()))
	}
	for _, row := range state.GetDevnetTestSeats() {
		out = append(out, "seat:"+row.GetSeatId())
	}
	for _, row := range state.GetPendingValidatorPower() {
		out = append(out, "pending:"+row.GetProposalId())
	}
	for _, row := range state.GetProposals() {
		out = append(out, "proposal:"+row.GetProposalId())
	}
	return out
}

// governanceEnv is a keeper with every section of the walk filled: four
// validators, two submitters, four seats, two genesis pending entries and
// one scheduled by an execute, and proposals in the OPEN, QUEUED and
// EXECUTED states.
func governanceEnv(t *testing.T) (*keeperEnv, []string) {
	t.Helper()
	gen, seats, authority := readyGenesis()
	gen.Authority.Validators[0], gen.Authority.Validators[3] = gen.Authority.Validators[3], gen.Authority.Validators[0]
	gen.Authority.Submitters[0], gen.Authority.Submitters[1] = gen.Authority.Submitters[1], gen.Authority.Submitters[0]
	gen.Authority.DevnetTestSeats[0], gen.Authority.DevnetTestSeats[2] = gen.Authority.DevnetTestSeats[2], gen.Authority.DevnetTestSeats[0]
	gen.Authority.PendingValidatorPower = []*authorityv1.PendingValidatorPower{
		heightEntry("g-v6", "v6", pub(6), 1, 60),
		heightEntry("g-v5", "v5", pub(5), 1, 50),
	}
	e := newKeeperEnv(t, gen, seats, authority)
	e.queue(t, "q-exec", powerMsg("v7", pub(7), 1, 70))
	if _, err := e.keeper.Execute(e.txCtx(), execute(authority, "ex-q-exec", "q-exec")); err != nil {
		t.Fatal(err)
	}
	e.queue(t, "q-queued", governedMsg(msgkit.LimitMaxOpenOffers, 5))
	if _, err := e.keeper.Propose(e.txCtx(), propose(e.seats[1].Signer, "a-open", 2, governedMsg(msgkit.LimitMaxEvidenceBatch, 4))); err != nil {
		t.Fatal(err)
	}
	want := []string{}
	for i := 1; i <= 4; i++ {
		want = append(want, "validator:"+hex.EncodeToString(pub(byte(i))))
	}
	want = append(want, "submitter:"+hex.EncodeToString(bytes20(1)), "submitter:"+hex.EncodeToString(bytes20(2)))
	for i := 1; i <= 4; i++ {
		want = append(want, fmt.Sprintf("seat:seat-%d", i))
	}
	want = append(want, "pending:g-v5", "pending:g-v6", "pending:q-exec", "proposal:a-open", "proposal:q-exec", "proposal:q-queued")
	return e, want
}

// TestGovernanceQueryPaging (D-9, DR-10-23): one ordered walk over
// validators by key, submitters by address, seats by id, pending changes in
// store-key order, and proposals by id. Every item counts against the page
// limit, the first one included, so a walk with limit L returns
// ceil(n/L) pages of L items (the last one shorter) whose concatenation is
// the full walk, and the last page has an empty next key. A nil page uses
// the cap. A governed max_page_limit lowers the cap. The proposal view
// matches the stored record.
func TestGovernanceQueryPaging(t *testing.T) {
	e, want := governanceEnv(t)
	ctx := e.blockCtx()
	full, err := governanceQuery(t, e.keeper, ctx, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := governanceItems(full.GetState()); !slices.Equal(got, want) {
		t.Fatalf("full walk\n got %v\nwant %v", got, want)
	}
	if len(full.GetPage().GetNextPageKey()) != 0 || full.GetState().GetQuorumLoss() != authorityv1.QuorumLossBehavior_QUORUM_LOSS_BEHAVIOR_UNSET ||
		len(full.GetState().GetApprovedCouncil()) != 0 {
		t.Fatalf("full page %v", full)
	}
	for _, view := range full.GetState().GetProposals() {
		rec := recordOf(t, e.kv(), view.GetProposalId())
		if !proto.Equal(view, proposalView(rec)) || view.GetPayloadDigest().GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 ||
			!bytes.Equal(view.GetPayloadDigest().GetValue(), rec.PayloadDigest) || len(view.GetApproverKeys()) != len(rec.ApproverKeys) {
			t.Fatalf("proposal view %v", view)
		}
	}
	for _, limit := range []uint32{1, 2, 3, 4, 5, 7, uint32(len(want)), uint32(len(want)) + 1, 100} {
		var got []string
		var key []byte
		pages := 0
		for {
			page, err := governanceQuery(t, e.keeper, ctx, limit, key)
			if err != nil {
				t.Fatalf("limit %d after %v: %v", limit, got, err)
			}
			items := governanceItems(page.GetState())
			remaining := len(want) - len(got)
			if len(items) != min(int(limit), remaining) {
				t.Fatalf("limit %d page %d: %d items with %d remaining", limit, pages, len(items), remaining)
			}
			got = append(got, items...)
			pages++
			key = page.GetPage().GetNextPageKey()
			if len(key) == 0 {
				break
			}
			if len(key) > msgkit.PageKeyMaxBytes {
				t.Fatalf("limit %d: next key of %d bytes", limit, len(key))
			}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("limit %d walk\n got %v\nwant %v", limit, got, want)
		}
		if wantPages := (len(want) + int(limit) - 1) / int(limit); pages != wantPages {
			t.Fatalf("limit %d: %d pages, want %d", limit, pages, wantPages)
		}
	}

	// A key cursor resumes at the first item at or after it.
	page, err := governanceQuery(t, e.keeper, ctx, 1, append([]byte{pageSeats}, "seat-2a"...))
	if err != nil || !slices.Equal(governanceItems(page.GetState()), []string{"seat:seat-3"}) {
		t.Fatalf("seat cursor between ids: %v %v", page, err)
	}
	page, err = governanceQuery(t, e.keeper, ctx, 2, []byte("q"))
	if err != nil || !slices.Equal(governanceItems(page.GetState()), []string{"proposal:q-exec", "proposal:q-queued"}) || len(page.GetPage().GetNextPageKey()) != 0 {
		t.Fatalf("proposal cursor: %v %v", page, err)
	}

	// A governed max_page_limit of 3 is the cap: limit 0 returns 3 items,
	// limit 3 is accepted and limit 4 is BOUNDS.
	e.queue(t, "q-page", governedMsg(msgkit.LimitMaxPageLimit, 3))
	if _, err := e.keeper.Execute(e.txCtx(), execute(e.authority, "ex-q-page", "q-page")); err != nil {
		t.Fatal(err)
	}
	ctx = e.blockCtx()
	for _, limit := range []uint32{0, 3} {
		page, err := governanceQuery(t, e.keeper, ctx, limit, nil)
		if err != nil || len(governanceItems(page.GetState())) != 3 {
			t.Fatalf("governed cap, limit %d: %v %v", limit, page, err)
		}
	}
	if _, err := governanceQuery(t, e.keeper, ctx, 4, nil); queryCode(err) != msgkit.Bounds {
		t.Fatalf("limit above the governed cap: %v", err)
	}
}

func queryCode(err error) msgkit.Code {
	code, _ := msgkit.QueryCode(err)
	return code
}

// TestGovernanceQueryRejects: every malformed page request is BOUNDS with a
// gRPC status that carries the protocol code; a stored record that does not
// decode is a plain error; a context that is not an SDK context is
// MALFORMED.
func TestGovernanceQueryRejects(t *testing.T) {
	e, _ := governanceEnv(t)
	ctx := e.blockCtx()
	rows := []struct {
		name  string
		limit uint32
		key   []byte
	}{
		{"limit above the protocol cap", genesis.MaxPageLimit + 1, nil},
		{"key over 64 bytes", 1, []byte(strings.Repeat("a", 65))},
		{"tag 0x00", 1, []byte{0x00, 'a'}},
		{"tag 0x05", 1, []byte{0x05, 'a'}},
		{"tag 0xff", 1, []byte{0xff}},
		{"bare id outside the pattern", 1, []byte("Proposal")},
		{"validator cursor of 31 bytes", 1, append([]byte{pageValidators}, make([]byte, 31)...)},
		{"validator cursor of 33 bytes", 1, append([]byte{pageValidators}, make([]byte, 33)...)},
		{"submitter cursor of 19 bytes", 1, append([]byte{pageSubmitters}, make([]byte, 19)...)},
		{"seat cursor outside the pattern", 1, append([]byte{pageSeats}, "Seat"...)},
		{"empty seat cursor", 1, []byte{pageSeats}},
		{"pending cursor of 2 bytes", 1, []byte{pagePending, 0, 0}},
		{"pending cursor past the cap", 1, []byte{pagePending, MaxPendingChanges}},
		{"pending cursor past the entries", 1, []byte{pagePending, 3}},
	}
	for _, r := range rows {
		_, err := governanceQuery(t, e.keeper, ctx, r.limit, r.key)
		if queryCode(err) != msgkit.Bounds {
			t.Fatalf("%s: %v", r.name, err)
		}
	}
	if _, err := governanceQuery(t, e.keeper, ctx, 1, []byte{pagePending, 2}); err != nil {
		t.Fatalf("pending cursor at the last entry: %v", err)
	}
	if _, err := e.keeper.Governance(t.Context(), &authorityv1.QueryGovernanceRequest{}); err == nil {
		t.Fatal("a plain context was served")
	}
	e.kv().Set(proposalKey("zz"), []byte(`{}`))
	_, err := governanceQuery(t, e.keeper, ctx, 0, nil)
	if err == nil {
		t.Fatal("a damaged record was served")
	}
	if _, isProtocol := msgkit.QueryCode(err); isProtocol {
		t.Fatalf("a damaged record is a protocol code: %v", err)
	}
}

// TestGovernanceQueryResponseBound: the largest page is limit items of the
// largest item kind. The test computes that bound from a maximal item of
// every kind (64-character ids, three approver keys, maximal integers),
// builds a page of 100 maximal proposal records, and checks the encoded
// response stays within the bound, which is itself below 65,536 bytes
// (genesis.MaxTxBytes, used here as a size yardstick).
func TestGovernanceQueryResponseBound(t *testing.T) {
	id := func(prefix string, i int) string {
		s := fmt.Sprintf("%s%03d", prefix, i)
		return s + strings.Repeat("x", 64-len(s))
	}
	key := func(b byte) []byte { return bytes.Repeat([]byte{b}, 32) }
	digest := &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: key(9)}
	maxProposal := &authorityv1.Proposal{
		ProposalId: id("p", 0), ParameterKey: id("k", 0), BasePolicyVersion: math.MaxUint64, PayloadDigest: digest,
		State: authorityv1.ProposalState_PROPOSAL_STATE_REJECTED, ExpiryUnixSeconds: unixMax, ExecuteAfterUnixSeconds: unixMax,
		ApproverKeys: [][]byte{key(1), key(2), key(3)}, BusinessId: id("b", 0),
	}
	items := []*authorityv1.AuthorityState{
		{Validators: []*authorityv1.ValidatorPower{{ConsensusPubkey: key(1), Power: safeMax, NodeId: id("n", 0), ReasonDigest: digest}}},
		{Submitters: []*typesv1.Submitter{{Address: bytes.Repeat([]byte{1}, 20), Role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER}}},
		{DevnetTestSeats: []*authorityv1.DevnetTestSeat{{SeatId: strings.Repeat("s", MaxSeatIDLength), PublicKey: key(2)}}},
		{PendingValidatorPower: []*authorityv1.PendingValidatorPower{{
			ProposalId: id("p", 0), ConsensusPubkey: key(3), Power: safeMax, NodeId: id("n", 0), ReasonDigest: digest,
			Activation: &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: uint64(safeMax)}},
		}}},
		{Proposals: []*authorityv1.Proposal{maxProposal}},
	}
	maxItem := 0
	for _, item := range items {
		maxItem = max(maxItem, proto.Size(item))
	}
	// Every page carries the devnet test rows in effect (schema version 2).
	// They are fixed at genesis; this genesis has one row per decision id,
	// each with the largest numeric value and delay.
	gen, seats, authority := readyGenesis()
	for _, decision := range genesis.DecisionIDs {
		if decision != "DEC-008" {
			gen.DevnetTestParameters = append(gen.DevnetTestParameters, &typesv1.DevnetTestParameter{
				Label: genesis.DevnetTestLabel, DecisionId: decision, NumericValue: math.MaxUint64,
				RoleName: devnetTestRole, DelaySeconds: unixMax,
			})
		}
	}
	fixed := proto.Size(&authorityv1.QueryGovernanceResponse{
		State:                &authorityv1.AuthorityState{QuorumLoss: authorityv1.QuorumLossBehavior_QUORUM_LOSS_BEHAVIOR_UNSET},
		Page:                 &typesv1.PageResponse{NextPageKey: make([]byte, msgkit.PageKeyMaxBytes)},
		DevnetTestParameters: gen.GetDevnetTestParameters(),
	}) + 3 // the state's length prefix grows from 1 to at most 3 bytes
	limit := int(genesis.MaxPageLimit)
	bound := limit*maxItem + fixed

	e := newKeeperEnv(t, gen, seats, authority)
	for i := 0; i <= limit; i++ {
		rec := &proposalRecord{
			ProposalID: id("p", i), ParameterKey: id("k", i), BasePolicyVersion: math.MaxUint64, PayloadDigest: key(9),
			State: int32(authorityv1.ProposalState_PROPOSAL_STATE_REJECTED), ExpiryUnixSeconds: unixMax, ExecuteAfterUnixSeconds: unixMax,
			ApproverKeys: [][]byte{key(1), key(2), key(3)}, BusinessID: id("b", i), Proposer: "seat-1",
			Approvers: []string{"seat-1", "seat-2", "seat-3"}, Payload: "{}", Decoded: decodedWire{Kind: kindGovernedLimit},
		}
		raw, err := encodeRecord(rec)
		if err != nil {
			t.Fatal(err)
		}
		e.kv().Set(proposalKey(rec.ProposalID), raw)
	}
	page, err := governanceQuery(t, e.keeper, e.blockCtx(), uint32(limit), []byte(id("p", 0)))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.GetState().GetProposals()) != limit || string(page.GetPage().GetNextPageKey()) != id("p", limit) {
		t.Fatalf("%d proposals, next %q", len(page.GetState().GetProposals()), page.GetPage().GetNextPageKey())
	}
	if len(page.GetDevnetTestParameters()) != len(genesis.DecisionIDs) {
		t.Fatalf("%d devnet rows on the page, want %d", len(page.GetDevnetTestParameters()), len(genesis.DecisionIDs))
	}
	size := proto.Size(page)
	t.Logf("maximal page: %d bytes; bound %d (%d items of at most %d bytes + %d)", size, bound, limit, maxItem, fixed)
	if size > bound || bound > int(genesis.MaxTxBytes) {
		t.Fatalf("page %d bytes, bound %d, tx cap %d", size, bound, genesis.MaxTxBytes)
	}
	if size < limit*proto.Size(items[len(items)-1]) {
		t.Fatalf("page %d bytes is smaller than %d maximal proposals", size, limit)
	}
}
