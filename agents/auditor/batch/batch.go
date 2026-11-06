// Package batch turns audit reports into signed SubmitAuditBatch messages
// (owner rulings on Q1-Q4; plan sections 16.3 and 16.7).
//
// It builds every signed byte through chain/x/evidence (AuditEnvelope,
// AuditSignBytes, VerifyAuditSignature, IDPrefix, EpochAt): the producer and
// the chain keeper share one implementation, so they cannot drift.
//
//   - evidence_id = IDPrefix(auditor address) + the first 47 lowercase hex
//     characters of SHA-256(report bytes): 16 + 1 + 47 = 64 bytes
//   - observation_digest = the lease's manifest digest for PASS; the full
//     SHA-256 of the report bytes for FAIL (so the chain stores REJECTED,
//     DEC-010); an INCOMPLETE audit produces no item
//   - one batch per provider, at most MaxItems items each, items ordered by
//     evidence id (a batch is atomic on chain, so one provider's lease
//     change fails only that provider's batch)
//   - epoch = EpochAt(header time) of the run; expiry = min(header time +
//     lifetime, lease deadline), lifetime at most
//     evidence.MaxEvidenceLifetimeSeconds
//   - batch_id = IDPrefix + 47 hex of SHA-256 of the SVN-JCS-1 object
//     {"auditor": hex address, "epoch": "<decimal>", "evidence_ids": [...]}
//   - business_id of attempt 0 = IDPrefix + 47 hex of
//     SHA-256("svn-audit-business:" + batch_id + ":0"); a later attempt,
//     made only after the chain answered REPLAY, uses IDPrefix + 47 hex of
//     32 bytes from crypto/rand (RetryBusinessID), so nobody can compute
//     and commit the retry ids before they are broadcast
//
// Eligibility (owner ruling on Q4): only a held lease (ACTIVE or RENEWED,
// reservation held) of the configured id whose deadline is after the run's
// header time plus the epoch guard plus the inclusion timeout, of content
// that is registered and not withdrawn, may be fetched or attested
// (CheckLease, CheckContent; run applies them before any request). Signing
// refuses a round that closes within the guard (RoundOpen) and any expiry
// outside (header time, min(header time + 86400, lease deadline)].
package batch

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/classify"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/report"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Bounds.
const (
	// MaxItems is the protocol ceiling of items per batch (bounds.json
	// safety_max_evidence_batch). The chain's governed max_evidence_batch
	// may be lower; the chain decides (BOUNDS).
	MaxItems = 32
	// SuffixHex is the number of SHA-256 hex characters after the id
	// prefix: 16 + 1 + 47 = 64, the id length bound.
	SuffixHex = msgkit.IDMaxBytes - 2*evidence.IDPrefixBytes - 1
)

// Errors.
var (
	ErrRoundClosing = errors.New("batch: the audit round ends within the epoch guard; run the audit again in the next round")
	ErrIneligible   = errors.New("batch: item is not eligible for signing")
	ErrSignature    = errors.New("batch: signed batch does not verify")
)

func suffix(sum [32]byte) string { return hex.EncodeToString(sum[:])[:SuffixHex] }

// EvidenceID is the evidence id of a report: the signer's id prefix and 47
// hex characters of the report's SHA-256.
func EvidenceID(signer []byte, reportSHA256 [32]byte) string {
	return evidence.IDPrefix(signer) + suffix(reportSHA256)
}

// BatchID is the batch id of a set of evidence ids (in item order) in one
// epoch.
func BatchID(signer []byte, epoch uint64, evidenceIDs []string) (string, error) {
	ids := make([]any, 0, len(evidenceIDs))
	for _, id := range evidenceIDs {
		ids = append(ids, id)
	}
	text, err := jcs.Canonicalize(map[string]any{
		"auditor":      hex.EncodeToString(signer),
		"epoch":        strconv.FormatUint(epoch, 10),
		"evidence_ids": ids,
	})
	if err != nil {
		return "", fmt.Errorf("batch: batch id: %w", err)
	}
	return evidence.IDPrefix(signer) + suffix(sha256.Sum256([]byte(text))), nil
}

// BusinessID is the business id of attempt 0 of a batch, the one the signed
// batch carries. Anyone who sees the batch id can compute it, but the batch
// id is public only once attempt 0 is broadcast.
func BusinessID(signer []byte, batchID string) string {
	sum := sha256.Sum256([]byte("svn-audit-business:" + batchID + ":0"))
	return evidence.IDPrefix(signer) + suffix(sum)
}

// RetryBusinessID draws the business id of a later attempt: IDPrefix and 47
// hex characters of 32 bytes read from r (crypto/rand when r is nil). It is
// not derived from the batch, so another account that sees attempt 0 in the
// mempool cannot commit the retry ids first (security review finding 1). The
// caller stores the transaction bytes, which carry the id, before the
// broadcast; a retry after a stop reads the id back from those bytes.
func RetryBusinessID(signer []byte, r io.Reader) (string, error) {
	if r == nil {
		r = rand.Reader
	}
	var nonce [32]byte
	if _, err := io.ReadFull(r, nonce[:]); err != nil {
		return "", fmt.Errorf("batch: retry business id: %w", err)
	}
	return evidence.IDPrefix(signer) + suffix(nonce), nil
}

// Window holds the producer's time settings in seconds.
type Window struct {
	// Guard: no signing within this many seconds before an epoch boundary.
	Guard int64
	// Inclusion: how long a submission may wait for a committed result.
	Inclusion int64
	// Lifetime: requested evidence lifetime, at most
	// evidence.MaxEvidenceLifetimeSeconds.
	Lifetime int64
}

// Validate checks the window against the chain constants.
func (w Window) Validate() error {
	switch {
	case w.Guard < 1 || w.Guard >= evidence.EpochSeconds:
		return fmt.Errorf("batch: epoch guard must be 1 to %d seconds", evidence.EpochSeconds-1)
	case w.Inclusion < 1:
		return errors.New("batch: inclusion timeout must be positive")
	case w.Lifetime > evidence.MaxEvidenceLifetimeSeconds:
		return fmt.Errorf("batch: lifetime above %d seconds", evidence.MaxEvidenceLifetimeSeconds)
	case w.Lifetime <= w.Guard+w.Inclusion:
		return errors.New("batch: lifetime must exceed the epoch guard plus the inclusion timeout")
	}
	return nil
}

// boundary is the first second of the epoch after the one holding t.
func boundary(t int64) int64 { return (int64(evidence.EpochAt(t)) + 1) * evidence.EpochSeconds }

// RoundOpen refuses (ErrRoundClosing) when the run's header time, or the
// signing host's clock now, is within guard seconds of the end of the
// header time's epoch, or past it. The host clock is not verified; it can
// only add a refusal. The chain decides (STALE_VERSION).
func RoundOpen(headerTime, now, guard int64) error {
	end := boundary(headerTime) - guard
	if headerTime >= end || now >= end {
		return ErrRoundClosing
	}
	return nil
}

// Expiry is min(header time + lifetime, lease deadline).
func Expiry(headerTime, lifetime, deadline int64) int64 { return min(headerTime+lifetime, deadline) }

// held reports whether a lease state holds a reservation.
func held(l *leasesv1.Lease) bool {
	s := l.GetState()
	return (s == leasesv1.LeaseState_LEASE_STATE_ACTIVE || s == leasesv1.LeaseState_LEASE_STATE_RENEWED) && l.GetReservationHeld()
}

// ManifestSHA256 returns the lease's manifest digest when it is a 32-byte
// SHA-256 digest.
func ManifestSHA256(l *leasesv1.Lease) ([32]byte, bool) {
	var out [32]byte
	d := l.GetManifestDigest()
	if d.GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 || len(d.GetValue()) != len(out) {
		return out, false
	}
	copy(out[:], d.GetValue())
	return out, true
}

// LeaseRef names the configured audit.
type LeaseRef struct {
	LeaseID, ProviderID, ContentID string
}

// CheckLease decides whether the held lease the chain returned for the
// (provider, content) pair may be audited at headerTime (owner ruling on
// Q4): it must exist, be the configured lease, be held, carry a SHA-256
// manifest digest and a content version in [1, 2^53-1], and have a deadline
// after headerTime + guard + inclusion. The empty reason means eligible.
func CheckLease(l *leasesv1.Lease, ref LeaseRef, headerTime int64, w Window) classify.Reason {
	if l == nil || l.GetLeaseId() != ref.LeaseID || l.GetProviderId() != ref.ProviderID || l.GetContentId() != ref.ContentID || !held(l) {
		return classify.LeaseNotHeld
	}
	if _, ok := ManifestSHA256(l); !ok {
		return classify.ChainReadFailed
	}
	if v := l.GetContentVersion(); v == 0 || v > uint64(genesis.SafeIntegerMax) {
		return classify.ChainReadFailed
	}
	if l.GetDeadlineUnixSeconds() <= headerTime+w.Guard+w.Inclusion {
		return classify.LeaseNearDeadline
	}
	return ""
}

// CheckContent decides whether the lease's content may be fetched: it must
// be registered at the lease's version, with no tombstone and not
// WITHDRAWN (owner ruling on Q4; DEC-003's removal default: the auditor does
// not download removed media). The empty reason means eligible.
func CheckContent(resp *contentv1.QueryContentResponse, found bool, l *leasesv1.Lease) classify.Reason {
	c := resp.GetContent()
	if !found || c == nil {
		return classify.InputContentNotFound
	}
	if c.GetContentId() != l.GetContentId() || c.GetContentVersion() != l.GetContentVersion() {
		return classify.ChainReadFailed
	}
	if resp.GetTombstone() != nil || c.GetState() == contentv1.ContentState_CONTENT_STATE_WITHDRAWN {
		return classify.InputContentWithdrawn
	}
	return ""
}

// Audit is one PASS or FAIL audit to attest.
type Audit struct {
	// Report is the canonical report bytes as stored.
	Report []byte
	// Deadline is the lease deadline the run read.
	Deadline int64
	// ManifestSHA256 is the lease's manifest digest the run read.
	ManifestSHA256 [32]byte
}

// Item is one attested audit of a batch.
type Item struct {
	EvidenceID     string
	ProviderID     string
	LeaseID        string
	ContentID      string
	ContentVersion uint64
	Result         string
	ReportSHA256   [32]byte
	Observation    [32]byte
	Expiry         int64
}

// Plan holds the signing inputs shared by every batch of a run.
type Plan struct {
	// Signer is the auditor's 20-byte address.
	Signer []byte
	// KeyID is the signer_key_id label.
	KeyID string
	// HeaderTime is the run's committed header time.
	HeaderTime int64
	// MaxItems bounds items per batch (1 to MaxItems).
	MaxItems int
	Window   Window
}

func (p Plan) validate() error {
	if len(p.Signer) != msgkit.AccountBytes {
		return errors.New("batch: signer must be 20 bytes")
	}
	if msgkit.CheckID(p.KeyID) != msgkit.OK {
		return errors.New("batch: key id must match the id pattern")
	}
	if p.HeaderTime < genesis.UnixSecondsMin || p.HeaderTime > genesis.UnixSecondsMax {
		return errors.New("batch: header time out of range")
	}
	if p.MaxItems < 1 || p.MaxItems > MaxItems {
		return fmt.Errorf("batch: max items must be 1 to %d", MaxItems)
	}
	return p.Window.Validate()
}

// NewItem builds the item of one audit and checks it against the plan.
func NewItem(p Plan, a Audit) (Item, error) {
	r, err := report.Parse(a.Report)
	if err != nil {
		return Item{}, err
	}
	sum := report.SHA256(a.Report)
	it := Item{
		EvidenceID: EvidenceID(p.Signer, sum), ProviderID: r.ProviderID, LeaseID: r.LeaseID,
		ContentID: r.ContentID, ContentVersion: r.ContentVersion, Result: r.Result, ReportSHA256: sum,
		Expiry: Expiry(p.HeaderTime, p.Window.Lifetime, a.Deadline),
	}
	if r.AuditorAddress != hex.EncodeToString(p.Signer) || r.AuditorKeyID != p.KeyID {
		return Item{}, fmt.Errorf("%w: report of another auditor or key id", ErrIneligible)
	}
	if r.ExpectedManifestSHA256 != a.ManifestSHA256 {
		return Item{}, fmt.Errorf("%w: report expects another manifest digest than the lease", ErrIneligible)
	}
	switch r.Result {
	case report.ResultPass:
		it.Observation = a.ManifestSHA256
	case report.ResultFail:
		it.Observation = sum
	default:
		return Item{}, fmt.Errorf("%w: %s audits produce no item", ErrIneligible, r.Result)
	}
	if err := checkExpiry(p, a.Deadline, it.Expiry); err != nil {
		return Item{}, err
	}
	return it, nil
}

// checkExpiry refuses an expiry the chain would refuse or that would read
// as current past its lease: it must be after header time + guard +
// inclusion and at most min(header time + 86400, lease deadline).
func checkExpiry(p Plan, deadline, expiry int64) error {
	ceiling := min(p.HeaderTime+evidence.MaxEvidenceLifetimeSeconds, deadline)
	if expiry > ceiling {
		return fmt.Errorf("%w: expiry %d above min(header time + %d, lease deadline)", ErrIneligible, expiry, evidence.MaxEvidenceLifetimeSeconds)
	}
	if expiry <= p.HeaderTime+p.Window.Guard+p.Window.Inclusion {
		return fmt.Errorf("%w: expiry %d too close to header time %d", ErrIneligible, expiry, p.HeaderTime)
	}
	return nil
}

// Batch is one unsigned or signed batch.
type Batch struct {
	ProviderID string
	// Items are in message order (by evidence id).
	Items []Item
	// Msg carries business id attempt 0 and, once signed, the signature.
	Msg *evidencev1.MsgSubmitAuditBatch
	// Supersedes is the id of the batch this one was rebuilt from, or "".
	Supersedes string
}

// ID is the batch id.
func (b *Batch) ID() string { return b.Msg.GetBatchId() }

// Epoch is the batch's epoch.
func (b *Batch) Epoch() uint64 { return b.Msg.GetEpoch() }

// Build groups items by provider, orders each group by evidence id, splits
// it into batches of at most p.MaxItems, and builds the unsigned messages.
// Batches are ordered by provider id, then by first evidence id. A
// duplicate evidence id is an error.
func Build(p Plan, items []Item) ([]*Batch, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	byProvider := map[string][]Item{}
	seen := map[string]bool{}
	for _, it := range items {
		if seen[it.EvidenceID] {
			return nil, fmt.Errorf("%w: duplicate evidence id %s", ErrIneligible, it.EvidenceID)
		}
		seen[it.EvidenceID] = true
		byProvider[it.ProviderID] = append(byProvider[it.ProviderID], it)
	}
	providers := make([]string, 0, len(byProvider))
	for id := range byProvider {
		providers = append(providers, id)
	}
	sort.Strings(providers)
	var out []*Batch
	for _, provider := range providers {
		group := byProvider[provider]
		sort.Slice(group, func(i, j int) bool { return group[i].EvidenceID < group[j].EvidenceID })
		for start := 0; start < len(group); start += p.MaxItems {
			b, err := assemble(p, provider, group[start:min(start+p.MaxItems, len(group))], "")
			if err != nil {
				return nil, err
			}
			out = append(out, b)
		}
	}
	return out, nil
}

// assemble builds the unsigned message of one batch.
func assemble(p Plan, provider string, items []Item, supersedes string) (*Batch, error) {
	epoch := evidence.EpochAt(p.HeaderTime)
	ids := make([]string, len(items))
	msgItems := make([]*evidencev1.EvidenceItem, len(items))
	for i, it := range items {
		if it.ProviderID != provider {
			return nil, fmt.Errorf("%w: item of another provider", ErrIneligible)
		}
		ids[i] = it.EvidenceID
		msgItems[i] = &evidencev1.EvidenceItem{
			EvidenceId:     it.EvidenceID,
			Kind:           evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION,
			LeaseId:        it.LeaseID,
			ContentId:      it.ContentID,
			ContentVersion: it.ContentVersion,
			ObservationDigest: &typesv1.Digest{
				Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256,
				Value:     append([]byte(nil), it.Observation[:]...),
			},
			ExpiryUnixSeconds: it.Expiry,
			Attestation:       true,
		}
	}
	batchID, err := BatchID(p.Signer, epoch, ids)
	if err != nil {
		return nil, err
	}
	msg := &evidencev1.MsgSubmitAuditBatch{
		Signer:        append([]byte(nil), p.Signer...),
		BusinessId:    BusinessID(p.Signer, batchID),
		BatchId:       batchID,
		Epoch:         epoch,
		SchemaVersion: evidence.SchemaVersion,
		Items:         msgItems,
		AuditorSignature: &typesv1.InnerSignature{
			Domain:                  typesv1.SignatureDomain_SIGNATURE_DOMAIN_EVIDENCE_ATTESTATION,
			KeyId:                   p.KeyID,
			CanonicalProfileVersion: 1,
		},
	}
	return &Batch{ProviderID: provider, Items: append([]Item(nil), items...), Msg: msg, Supersedes: supersedes}, nil
}

// Signer is the auditor key: keys.Key implements it.
type Signer interface {
	PublicKey() []byte
	Sign(message []byte) []byte
}

// Sign signs b with key and checks the result with
// evidence.VerifyAuditSignature, the chain's own steps 6-8. The key's
// address must be the batch signer.
func Sign(b *Batch, key Signer) error {
	pub := key.PublicKey()
	sig := b.Msg.GetAuditorSignature()
	if sig == nil {
		return fmt.Errorf("%w: no signature field", ErrSignature)
	}
	if string(evidence.AuditorAddress(pub)) != string(b.Msg.GetSigner()) {
		return fmt.Errorf("%w: key address is not the batch signer", ErrSignature)
	}
	sig.PublicKey = pub
	sig.Signature = nil
	preimage, err := evidence.AuditSignBytes(b.Msg)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSignature, err)
	}
	sig.Signature = key.Sign(preimage)
	if code := evidence.VerifyAuditSignature(b.Msg); code != msgkit.OK {
		return fmt.Errorf("%w: %s", ErrSignature, code)
	}
	return nil
}

// WithBusinessID returns a copy of the signed message with another business
// id. The business id is not part of the signed payload, so the auditor
// signature stays valid.
func WithBusinessID(msg *evidencev1.MsgSubmitAuditBatch, businessID string) *evidencev1.MsgSubmitAuditBatch {
	out := proto.Clone(msg).(*evidencev1.MsgSubmitAuditBatch)
	out.BusinessId = businessID
	return out
}

// Envelopes returns the SVN-JCS-1 text of evidence.AuditEnvelope for every
// item, in item order: the bytes the auditor signed inside the batch
// object.
func Envelopes(msg *evidencev1.MsgSubmitAuditBatch) ([][]byte, error) {
	out := make([][]byte, len(msg.GetItems()))
	for i := range msg.GetItems() {
		env, err := evidence.AuditEnvelope(msg, i)
		if err != nil {
			return nil, err
		}
		text, err := msgkit.Canonical(env)
		if err != nil {
			return nil, err
		}
		out[i] = []byte(text)
	}
	return out, nil
}

// Rebuild builds a new batch from the items of b that keep returns true
// for, in the same epoch with the same expiries, as Build would: a batch
// the chain rejected for one item's lease or content change (CONFLICT,
// EXPIRED, WITHDRAWN) is rebuilt without that item. It returns nil when no
// item is kept. The new batch keeps b's signer and key id, is unsigned, and
// names b in Supersedes.
func Rebuild(b *Batch, keep func(Item) bool) (*Batch, []Item, error) {
	var kept, dropped []Item
	for _, it := range b.Items {
		if keep(it) {
			kept = append(kept, it)
		} else {
			dropped = append(dropped, it)
		}
	}
	if len(kept) == 0 {
		return nil, dropped, nil
	}
	p := Plan{Signer: b.Msg.GetSigner(), KeyID: b.Msg.GetAuditorSignature().GetKeyId(), HeaderTime: int64(b.Msg.GetEpoch()) * evidence.EpochSeconds}
	nb, err := assemble(p, b.ProviderID, kept, b.ID())
	if err != nil {
		return nil, nil, err
	}
	return nb, dropped, nil
}
