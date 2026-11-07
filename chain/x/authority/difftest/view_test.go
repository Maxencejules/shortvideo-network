package difftest

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// governance is the whole Query/Governance walk at ctx: every page, until
// next_page_key is empty. It reads committed authority state through the
// keeper's query surface (D-9), not through the store layout.
func (h *harness) governance(ctx sdk.Context) *authorityv1.AuthorityState {
	h.tb.Helper()
	out := &authorityv1.AuthorityState{}
	var key []byte
	for pages := 0; ; pages++ {
		if pages > 10_000 {
			h.tb.Fatalf("Query/Governance did not end after %d pages", pages)
		}
		resp, err := h.auth.Governance(ctx, &authorityv1.QueryGovernanceRequest{Page: &typesv1.PageRequest{PageKey: key}})
		if err != nil {
			h.tb.Fatalf("Query/Governance: %v", err)
		}
		state := resp.GetState()
		out.QuorumLoss = state.GetQuorumLoss()
		out.Validators = append(out.Validators, state.GetValidators()...)
		out.Submitters = append(out.Submitters, state.GetSubmitters()...)
		out.DevnetTestSeats = append(out.DevnetTestSeats, state.GetDevnetTestSeats()...)
		out.PendingValidatorPower = append(out.PendingValidatorPower, state.GetPendingValidatorPower()...)
		out.Proposals = append(out.Proposals, state.GetProposals()...)
		out.ApprovedCouncil = append(out.ApprovedCouncil, state.GetApprovedCouncil()...)
		key = resp.GetPage().GetNextPageKey()
		if len(key) == 0 {
			return out
		}
	}
}

// storedRecord is the part of an authority proposal record (0x10, strict
// JSON) the invariant checker reads.
type storedRecord struct {
	ProposalID        string   `json:"proposal_id"`
	ParameterKey      string   `json:"parameter_key"`
	BasePolicyVersion uint64   `json:"base_policy_version"`
	PayloadDigest     []byte   `json:"payload_digest"`
	State             int32    `json:"state"`
	Expiry            int64    `json:"expiry_unix_seconds"`
	ExecuteAfter      int64    `json:"execute_after_unix_seconds"`
	ApproverKeys      [][]byte `json:"approver_keys"`
	Proposer          string   `json:"proposer"`
	Approvers         []string `json:"approvers"`
}

// records decodes every stored proposal record at ctx, in id order.
func (h *harness) records(ctx sdk.Context) []storedRecord {
	h.tb.Helper()
	var out []storedRecord
	eachRow(ctx.KVStore(h.keys[storeAuthority]), prefixProposal, func(key, value []byte) {
		var rec storedRecord
		if err := json.Unmarshal(value, &rec); err != nil {
			h.tb.Fatalf("proposal record %q does not decode: %v", key[1:], err)
		}
		if rec.ProposalID != string(key[1:]) {
			h.tb.Fatalf("proposal record under %q names %q", key[1:], rec.ProposalID)
		}
		out = append(out, rec)
	})
	return out
}

// seatOfKey is the seat id of a council key, or "".
func seatOfKey(pub []byte) string {
	for i := 1; i <= seatCount; i++ {
		if bytes.Equal(seatPub(i), pub) {
			return seatID(i)
		}
	}
	return ""
}

func stateName(s authorityv1.ProposalState) string {
	return strings.TrimPrefix(s.String(), "PROPOSAL_STATE_")
}

// councilProjection is the council part of the Go projection.
func (h *harness) councilProjection(ctx sdk.Context, c *committed) councilView {
	h.tb.Helper()
	gov := c.gov
	v := councilView{
		Height:         dec64(h.height),
		PolicyVersion:  msgkit.Dec(c.version),
		Proposals:      []proposalView{},
		ExecutedParams: []executedView{},
		GovernedLimits: []governedView{},
		Validators:     []validatorView{},
		PendingPower:   []pendingView{},
	}
	for _, p := range gov.GetProposals() {
		approvers := []string{}
		for _, key := range p.GetApproverKeys() {
			seat := seatOfKey(key)
			if seat == "" {
				h.tb.Fatalf("proposal %s approver key %x is not a seat key", p.GetProposalId(), key)
			}
			approvers = append(approvers, seat)
		}
		sort.Strings(approvers)
		v.Proposals = append(v.Proposals, proposalView{
			ProposalID: p.GetProposalId(), ParameterKey: p.GetParameterKey(),
			BasePolicyVersion: msgkit.Dec(p.GetBasePolicyVersion()), PayloadDigest: hexOf(p.GetPayloadDigest().GetValue()),
			State: stateName(p.GetState()), ExpiryUnixSeconds: dec64(p.GetExpiryUnixSeconds()),
			ExecuteAfterUnixSeconds: dec64(p.GetExecuteAfterUnixSeconds()), Approvers: approvers,
		})
	}
	eachRow(ctx.KVStore(h.keys[storeAuthority]), prefixExecuted, func(key, value []byte) {
		v.ExecutedParams = append(v.ExecutedParams, executedView{ParameterKey: string(key[1:]), PayloadDigest: hexOf(value)})
	})
	// The governed values are x/policy's (the authority hook writes them
	// through policy.Keeper.SetLimit); the read surface the registries use
	// must return each stored row.
	eachRow(ctx.KVStore(h.keys[storePolicy]), prefixGoverned, func(key, value []byte) {
		limit := string(key[1:])
		stored := decodeU64(h.tb, "governed value "+limit, value)
		read, found := h.pol.GovernedLimit(ctx, limit)
		if !found || read != stored {
			h.tb.Fatalf("governed row %s=%d but GovernedLimit reads %d, %t", limit, stored, read, found)
		}
		v.GovernedLimits = append(v.GovernedLimits, governedView{LimitKey: limit, Value: msgkit.Dec(stored)})
	})
	for _, row := range gov.GetValidators() {
		var reason *string
		if value := row.GetReasonDigest().GetValue(); len(value) > 0 {
			text := hexOf(value)
			reason = &text
		}
		v.Validators = append(v.Validators, validatorView{
			NodeID: row.GetNodeId(), Power: dec64(row.GetPower()), ConsensusPubkey: hexOf(row.GetConsensusPubkey()), ReasonDigest: reason,
		})
	}
	sort.Slice(v.Validators, func(i, j int) bool { return v.Validators[i].NodeID < v.Validators[j].NodeID })
	for _, item := range gov.GetPendingValidatorPower() {
		act := activationView{Kind: "time", Point: dec64(item.GetActivation().GetUnixSeconds())}
		if point, ok := item.GetActivation().GetPoint().(*authorityv1.ActivationPoint_Height); ok {
			act = activationView{Kind: "height", Point: msgkit.Dec(point.Height)}
		}
		v.PendingPower = append(v.PendingPower, pendingView{
			ProposalID: item.GetProposalId(), NodeID: item.GetNodeId(), Power: dec64(item.GetPower()),
			ConsensusPubkey: hexOf(item.GetConsensusPubkey()), ReasonDigest: hexOf(item.GetReasonDigest().GetValue()),
			Activation: act,
		})
	}
	return v
}

func dec64(v int64) string {
	if v < 0 {
		return "-" + msgkit.Dec(uint64(-v))
	}
	return msgkit.Dec(uint64(v))
}

// projection builds the Go side of the compared state at the committed
// context, from the read of the latest step.
func (h *harness) projection() projection {
	h.tb.Helper()
	ctx := h.unmetered()
	p := projection{
		ChainTime: dec64(h.now), Providers: []providerView{}, Contents: []contentView{},
		Offers: []struct{}{}, Leases: []struct{}{}, Pairs: []struct{}{},
		BusinessIDs: h.businessIDs(ctx), Council: h.councilProjection(ctx, h.last),
	}
	provGenesis, err := h.prov.ExportGenesis(ctx)
	if err != nil {
		h.tb.Fatalf("providers export: %v", err)
	}
	for _, r := range provGenesis.GetProviders() {
		prev := []string{}
		for _, k := range r.GetPreviousSigningKeys() {
			prev = append(prev, hexOf(k.GetPublicKey()))
		}
		p.Providers = append(p.Providers, providerView{
			ProviderID: r.GetProviderId(), Account: hexOf(r.GetAccount()),
			State:        strings.TrimPrefix(r.GetState().String(), "PROVIDER_STATE_"),
			SigningKeyID: r.GetSigningKeyId(), SigningPubkey: hexOf(r.GetSigningPubkey()),
			KeyGeneration:          msgkit.Dec(r.GetKeyGeneration()),
			AdmittedCapacityBytes:  msgkit.Dec(r.GetAdmittedCapacityBytes()),
			ReservedBytes:          msgkit.Dec(r.GetReservedBytes()),
			OperatorGroupID:        r.GetOperatorGroupId(),
			EndpointManifestDigest: hexOf(r.GetEndpointManifestDigest().GetValue()),
			PreviousPubkeys:        prev,
		})
	}
	contentGenesis := h.cont.ExportGenesis(ctx)
	tombs := map[string]*tombstoneView{}
	for _, tomb := range contentGenesis.GetTombstones() {
		tombs[tomb.GetContentId()] = &tombstoneView{
			CommittedUnixSeconds: dec64(tomb.GetCommittedUnixSeconds()), ReasonDigest: hexOf(tomb.GetReasonDigest().GetValue()),
		}
	}
	for _, c := range contentGenesis.GetContents() {
		p.Contents = append(p.Contents, contentView{
			ContentID: c.GetContentId(), ContentVersion: msgkit.Dec(c.GetContentVersion()),
			ManifestDigest: hexOf(c.GetManifestDigest().GetValue()), PackageBytes: msgkit.Dec(c.GetPackageBytes()),
			SubmittingHost: hexOf(c.GetSubmittingHost()),
			State:          strings.TrimPrefix(c.GetState().String(), "CONTENT_STATE_"),
			Tombstone:      tombs[c.GetContentId()],
		})
	}
	return p
}

// businessIDs is the union of the authority, providers, and content
// ledgers. The protocol has one business-id namespace, so an id in two
// ledgers is a failure.
func (h *harness) businessIDs(ctx sdk.Context) []string {
	h.tb.Helper()
	seen := map[string]string{}
	ledgers := []struct {
		name   string
		key    *storetypes.KVStoreKey
		prefix byte
	}{
		{"authority", h.keys[storeAuthority], prefixBusiness},
		{providers.ModuleName, h.keys[storeProviders], providers.BusinessKeyPrefix},
		{content.ModuleName, h.keys[storeContent], content.BusinessPrefix},
	}
	ids := []string{}
	for _, ledger := range ledgers {
		eachRow(ctx.KVStore(ledger.key), ledger.prefix, func(key, value []byte) {
			id := string(key[1:])
			if len(value) != msgkit.DigestBytes {
				h.tb.Fatalf("%s business row %q holds %d bytes", ledger.name, id, len(value))
			}
			if other, dup := seen[id]; dup {
				h.tb.Fatalf("business id %q committed by %s and %s", id, other, ledger.name)
			}
			seen[id] = ledger.name
			ids = append(ids, id)
		})
	}
	sort.Strings(ids)
	return ids
}
