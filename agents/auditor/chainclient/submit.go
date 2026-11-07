package chainclient

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/batch"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/keys"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/report"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/store"
	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
)

// State is the submission state of one batch. Only COMMITTED says that a
// transaction carrying the batch is in a block with code 0; an
// acknowledgement is never reported as anything but SUBMITTED.
type State string

// Batch states.
const (
	// NotSubmitted: no submission is stored and no record of the batch is
	// on chain.
	NotSubmitted State = "NOT_SUBMITTED"
	// NotAccepted: every stored submission was refused by CheckTx (the
	// ante chain); nothing can commit from them.
	NotAccepted State = "NOT_ACCEPTED"
	// Submitted: a submission was acknowledged by CheckTx, or its answer is
	// unknown, and no committed result is visible. It is not a commit. A
	// rebroadcast that CheckTx refuses while an earlier submission waits
	// leaves the batch SUBMITTED (warning REBROADCAST_NOT_ACCEPTED).
	Submitted State = "SUBMITTED"
	// Committed: a stored submission is in a block with code 0; the items
	// come from the evidence records read back afterwards.
	Committed State = "COMMITTED"
	// Failed: the latest committed submission has a non-zero code and no
	// submission is waiting.
	Failed State = "FAILED"
	// RecordsFound: no committed submission of this store is visible, but
	// the chain holds records under the batch's evidence ids; the items say
	// whether they are this batch's.
	RecordsFound State = "RECORDS_FOUND"
)

// ItemState is the state of one item after a read-back.
type ItemState string

// Item states.
const (
	// The record matches the batch item; its reported state.
	ItemRecorded ItemState = "RECORDED"
	ItemRejected ItemState = "REJECTED"
	ItemExpired  ItemState = "EXPIRED"
	// ItemIDTaken: the evidence id holds a record that differs from the
	// batch item (signer key, batch id, lease, content, version, kind,
	// digest, epoch, or expiry). Never reported as recorded.
	ItemIDTaken ItemState = "ID_TAKEN"
	// ItemNotOnChain: no record holds the evidence id.
	ItemNotOnChain ItemState = "NOT_ON_CHAIN"
	// ItemFailed is the index state of an item of a committed failure.
	ItemFailed ItemState = "FAILED"
)

// Bounds.
const (
	// MaxSubmissions bounds the stored submissions of one batch.
	MaxSubmissions = 16
	// maxRounds bounds broadcast rounds of one Submit call.
	maxRounds = 4
	// SubmissionFormat is the stored submission record.
	SubmissionFormat = "svn-audit-submission-proposed-v0"
	// WarnEpochStale is printed when a fresh header shows another epoch
	// than the batch's: the chain will answer STALE_VERSION.
	WarnEpochStale = "EPOCH_LIKELY_STALE"
	// WarnRebroadcastNotAccepted is printed when CheckTx refused a
	// rebroadcast while an earlier submission of the batch was waiting:
	// the earlier one may still commit, so the batch stays SUBMITTED. On a
	// node the usual answer is sdk/32, because the waiting transaction
	// holds the account sequence.
	WarnRebroadcastNotAccepted = "REBROADCAST_NOT_ACCEPTED"
	// Acknowledgement values of a submission record.
	AckAccepted    = "ACCEPTED"
	AckNotAccepted = "NOT_ACCEPTED"
	AckUnknown     = "UNKNOWN"
)

// ItemResult is one item of a Result.
type ItemResult struct {
	EvidenceID string    `json:"evidence_id"`
	State      ItemState `json:"state"`
}

// Result is the submission state of one batch.
type Result struct {
	BatchID string `json:"batch_id"`
	State   State  `json:"state"`
	// Codespace and Code are the CheckTx answer for NOT_ACCEPTED and the
	// committed answer for COMMITTED and FAILED.
	Codespace string `json:"codespace"`
	Code      uint32 `json:"code"`
	TxHash    string `json:"tx_hash"`
	Height    int64  `json:"height"`
	// Ack is the CheckTx answer of the broadcast this call made, if any.
	Ack      *Ack         `json:"ack,omitempty"`
	Items    []ItemResult `json:"items"`
	Warnings []string     `json:"warnings"`
	// Next is the batch rebuilt from this one after a lease or content
	// change, and Dropped the evidence ids left out of it (LEASE_CHANGED).
	Next    *Result  `json:"next,omitempty"`
	Dropped []string `json:"dropped"`
}

// Label is the state with its code, as the operator reads it: for example
// COMMITTED(0), FAILED(5), NOT_ACCEPTED(sdk/32), SUBMITTED.
func (r Result) Label() string {
	switch r.State {
	case Committed, Failed:
		if r.Codespace == "" || r.Codespace == "svn" {
			return fmt.Sprintf("%s(%d)", r.State, r.Code)
		}
		return fmt.Sprintf("%s(%s/%d)", r.State, r.Codespace, r.Code)
	case NotAccepted:
		return fmt.Sprintf("%s(%s/%d)", r.State, r.Codespace, r.Code)
	}
	return string(r.State)
}

// Recorded reports whether the batch, or the batch rebuilt from it, is
// committed with code 0 and every item read back as this batch's record.
func (r Result) Recorded() bool {
	if r.Next != nil {
		return r.Next.Recorded()
	}
	if r.State != Committed || len(r.Items) == 0 {
		return false
	}
	for _, it := range r.Items {
		if it.State != ItemRecorded && it.State != ItemRejected && it.State != ItemExpired {
			return false
		}
	}
	return true
}

// Submission is a stored submission record:
// batches/<name>.json, with the transaction bytes in batches/<name>.pb.
// The bytes are stored before the broadcast, the record once the broadcast
// answer (or its failure) is known. Bytes without a record are a
// submission whose answer is UNKNOWN (Submitter.unrecorded). The
// transaction hash is the SHA-256 of the stored bytes.
type Submission struct {
	Format          string `json:"format"`
	BatchID         string `json:"batch_id"`
	Number          int64  `json:"number"`
	BusinessAttempt int64  `json:"business_attempt"`
	BusinessID      string `json:"business_id"`
	ChainID         string `json:"chain_id"`
	AccountNumber   string `json:"account_number"`
	Sequence        string `json:"sequence"`
	TxHash          string `json:"tx_hash"`
	Ack             string `json:"ack"`
	AckCodespace    string `json:"ack_codespace"`
	AckCode         int64  `json:"ack_code"`
}

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (s Submission) object() map[string]any {
	return map[string]any{
		"format": s.Format, "batch_id": s.BatchID, "number": s.Number, "business_attempt": s.BusinessAttempt,
		"business_id": s.BusinessID, "chain_id": s.ChainID, "account_number": s.AccountNumber, "sequence": s.Sequence,
		"tx_hash": s.TxHash, "ack": s.Ack, "ack_codespace": s.AckCodespace, "ack_code": s.AckCode,
	}
}

func (s Submission) encode() ([]byte, error) {
	if s.Format != SubmissionFormat || !msgkit.ValidID(s.BatchID) || !msgkit.ValidID(s.BusinessID) || !hex64.MatchString(s.TxHash) ||
		s.Number < 0 || s.Number >= MaxSubmissions || s.BusinessAttempt < 0 || s.BusinessAttempt > MaxSubmissions ||
		s.AckCode < 0 || s.AckCode > 1<<32-1 || s.ChainID == "" || len(s.ChainID) > 64 || len(s.AckCodespace) > 64 {
		return nil, errors.New("chainclient: submission record fields")
	}
	switch s.Ack {
	case AckAccepted, AckNotAccepted, AckUnknown:
	default:
		return nil, errors.New("chainclient: submission ack")
	}
	for _, n := range []string{s.AccountNumber, s.Sequence} {
		if v, err := strconv.ParseUint(n, 10, 64); err != nil || strconv.FormatUint(v, 10) != n {
			return nil, errors.New("chainclient: submission numbers")
		}
	}
	text, err := jcs.Canonicalize(s.object())
	if err != nil {
		return nil, fmt.Errorf("chainclient: submission: %w", err)
	}
	return []byte(text), nil
}

func parseSubmission(raw []byte) (Submission, error) {
	canonical, err := jcs.ParseAndCanonicalize(string(raw))
	if err != nil || canonical != string(raw) {
		return Submission{}, errors.New("chainclient: submission record is not canonical SVN-JCS-1")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var s Submission
	if err := dec.Decode(&s); err != nil {
		return Submission{}, fmt.Errorf("chainclient: submission record: %w", err)
	}
	again, err := s.encode()
	if err != nil || !bytes.Equal(again, raw) {
		return Submission{}, errors.New("chainclient: submission record is not in its format")
	}
	return s, nil
}

// SubmissionName is the store name of submission n of a batch: the batch
// id's auditor prefix and 47 hex characters of SHA-256 of
// "svn-audit-submission:" + batch id + ":" + n.
func SubmissionName(batchID string, n int) string {
	prefixLen := 2*evidence.IDPrefixBytes + 1
	sum := sha256.Sum256([]byte("svn-audit-submission:" + batchID + ":" + strconv.Itoa(n)))
	return batchID[:prefixLen] + hex.EncodeToString(sum[:])[:batch.SuffixHex]
}

// Submitter submits stored batches and reports their state.
type Submitter struct {
	Client *Client
	Store  *store.Store
	// Key signs outer transactions and rebuilt batches. Status needs none.
	Key *keys.Key
	// Gas is the declared gas limit of each transaction.
	Gas uint64
	// Inclusion bounds the wait for a committed result after a broadcast.
	Inclusion time.Duration
	// Interval is the polling interval of that wait.
	Interval time.Duration
	// Window holds the eligibility settings used when a batch is rebuilt.
	Window batch.Window
	// Rand is the source of retry business ids (batch.RetryBusinessID);
	// nil means crypto/rand.
	Rand io.Reader
}

type stored struct {
	Submission
	raw []byte
}

// submissions loads the stored submissions of a batch, in order. A record
// without transaction bytes is an error; bytes without a record are an
// unrecorded submission.
func (s *Submitter) submissions(b *batch.Batch) ([]stored, error) {
	batchID := b.ID()
	if len(batchID) < 2*evidence.IDPrefixBytes+1 {
		return nil, errors.New("chainclient: batch id without an auditor prefix")
	}
	var out []stored
	for n := 0; n < MaxSubmissions; n++ {
		name := SubmissionName(batchID, n)
		txRaw, err := s.Store.Get(store.Batches, name, ".pb")
		if errors.Is(err, store.ErrNotFound) {
			if _, err := s.Store.Get(store.Batches, name, ".json"); !errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("chainclient: submission %d has a record without transaction bytes", n)
			}
			break
		}
		if err != nil {
			return nil, fmt.Errorf("chainclient: submission %d transaction: %w", n, err)
		}
		raw, err := s.Store.Get(store.Batches, name, ".json")
		if errors.Is(err, store.ErrNotFound) {
			var prev *Submission
			if len(out) > 0 {
				prev = &out[len(out)-1].Submission
			}
			sub, err := s.unrecorded(b, n, txRaw, prev)
			if err != nil {
				return nil, err
			}
			out = append(out, stored{Submission: sub, raw: txRaw})
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("chainclient: submission %d: %w", n, err)
		}
		sub, err := parseSubmission(raw)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(txRaw)
		if sub.BatchID != batchID || sub.Number != int64(n) || sub.TxHash != hex.EncodeToString(sum[:]) {
			return nil, fmt.Errorf("chainclient: submission %d does not match its batch or bytes", n)
		}
		out = append(out, stored{Submission: sub, raw: txRaw})
	}
	return out, nil
}

// unrecorded is submission n of b from transaction bytes stored without a
// record: the process stopped, or the record write failed, between storing
// the bytes and recording the broadcast answer. The answer is UNKNOWN: the
// transaction may have been sent and may still commit, so state queries
// its hash. The bytes must carry exactly b's message under a business id of
// b's signer: submission 0 (prev nil) carries attempt 0, the signed batch's
// own business id; a later submission carries the business id of the one
// before it (a rebroadcast of the same attempt) or a new id with the
// signer's prefix (the next attempt, made only after REPLAY).
func (s *Submitter) unrecorded(b *batch.Batch, n int, txRaw []byte, prev *Submission) (Submission, error) {
	msg, err := s.Client.TxMessage(txRaw)
	if err != nil {
		return Submission{}, fmt.Errorf("chainclient: submission %d transaction: %w", n, err)
	}
	id := msg.GetBusinessId()
	if !proto.Equal(msg, batch.WithBusinessID(b.Msg, id)) {
		return Submission{}, fmt.Errorf("chainclient: submission %d transaction does not carry its batch", n)
	}
	var attempt int64
	switch {
	case prev == nil:
		if id != b.Msg.GetBusinessId() {
			return Submission{}, fmt.Errorf("chainclient: submission %d transaction does not carry its batch's first business id", n)
		}
	case id == prev.BusinessID:
		attempt = prev.BusinessAttempt
	case msgkit.ValidID(id) && evidence.OwnsID(b.Msg.GetSigner(), id) && prev.BusinessAttempt < MaxSubmissions:
		attempt = prev.BusinessAttempt + 1
	default:
		return Submission{}, fmt.Errorf("chainclient: submission %d transaction carries a business id that is not its signer's", n)
	}
	sum := sha256.Sum256(txRaw)
	return Submission{
		Format: SubmissionFormat, BatchID: b.ID(), Number: int64(n), BusinessAttempt: attempt,
		BusinessID: id, TxHash: hex.EncodeToString(sum[:]), Ack: AckUnknown,
	}, nil
}

// matches reports whether a record is the batch item's: the same signer
// key, batch id, lease, content, version, kind, digest, epoch, and expiry.
func matches(rec *evidencev1.EvidenceRecord, msg *evidencev1.MsgSubmitAuditBatch, it *evidencev1.EvidenceItem) bool {
	d, want := rec.GetObservationDigest(), it.GetObservationDigest()
	return bytes.Equal(rec.GetSignerPubkey(), msg.GetAuditorSignature().GetPublicKey()) &&
		rec.GetBatchId() == msg.GetBatchId() && rec.GetLeaseId() == it.GetLeaseId() &&
		rec.GetContentId() == it.GetContentId() && rec.GetContentVersion() == it.GetContentVersion() &&
		rec.GetKind() == it.GetKind() && d.GetAlgorithm() == want.GetAlgorithm() && bytes.Equal(d.GetValue(), want.GetValue()) &&
		rec.GetEpoch() == msg.GetEpoch() && rec.GetExpiryUnixSeconds() == it.GetExpiryUnixSeconds()
}

// readBack reads every item's record and compares it with the batch.
func (s *Submitter) readBack(ctx context.Context, b *batch.Batch) ([]ItemResult, bool, error) {
	out := make([]ItemResult, 0, len(b.Msg.GetItems()))
	any := false
	for _, it := range b.Msg.GetItems() {
		rec, found, err := s.Client.Evidence(ctx, it.GetEvidenceId())
		if err != nil {
			return nil, false, err
		}
		state := ItemNotOnChain
		if found {
			any = true
			state = ItemIDTaken
			if matches(rec, b.Msg, it) {
				switch rec.GetState() {
				case evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED:
					state = ItemRecorded
				case evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED:
					state = ItemRejected
				case evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED:
					state = ItemExpired
				}
			}
		}
		out = append(out, ItemResult{EvidenceID: it.GetEvidenceId(), State: state})
	}
	return out, any, nil
}

// Status reports the state of a stored batch from the chain: a committed
// transaction result of a stored submission, then the records read back.
// It broadcasts nothing. A committed result it reads is indexed once, as
// Submit does.
func (s *Submitter) Status(ctx context.Context, b *batch.Batch) (Result, error) {
	res, _, err := s.state(ctx, b)
	if err != nil {
		return Result{}, err
	}
	return res, s.index(b, res)
}

func (s *Submitter) state(ctx context.Context, b *batch.Batch) (Result, []stored, error) {
	subs, err := s.submissions(b)
	if err != nil {
		return Result{}, nil, err
	}
	res := Result{BatchID: b.ID(), State: NotSubmitted}
	var ok, failed *TxResult
	var okHash, failedHash string
	waiting := false
	for _, sub := range subs {
		if sub.Ack == AckNotAccepted {
			continue
		}
		hash, _ := hex.DecodeString(sub.TxHash)
		c, found, err := s.Client.CommittedTx(ctx, hash)
		if err != nil {
			return Result{}, nil, err
		}
		switch {
		case !found:
			waiting = true
		case c.Code == 0 && ok == nil:
			ok, okHash = &c, sub.TxHash
		case c.Code != 0:
			failed, failedHash = &c, sub.TxHash
		}
	}
	items, anyRecord, err := s.readBack(ctx, b)
	if err != nil {
		return Result{}, nil, err
	}
	res.Items = items
	switch {
	case ok != nil:
		res.State, res.Code, res.Codespace, res.Height, res.TxHash = Committed, 0, ok.Codespace, ok.Height, okHash
	case waiting && anyRecord:
		res.State = RecordsFound
	case waiting:
		res.State = Submitted
	case failed != nil:
		res.State, res.Code, res.Codespace, res.Height, res.TxHash = Failed, failed.Code, failed.Codespace, failed.Height, failedHash
	case anyRecord:
		res.State = RecordsFound
	case len(subs) > 0:
		last := subs[len(subs)-1]
		res.State, res.Codespace, res.Code, res.TxHash = NotAccepted, last.AckCodespace, uint32(last.AckCode), last.TxHash
	}
	return res, subs, nil
}

// lastAttempt is the business attempt of the latest submission and its
// business id; attempt 0 and the signed batch's business id when nothing
// was submitted.
func lastAttempt(b *batch.Batch, subs []stored) (uint64, string) {
	attempt, id := uint64(0), b.Msg.GetBusinessId()
	for _, sub := range subs {
		if uint64(sub.BusinessAttempt) >= attempt {
			attempt, id = uint64(sub.BusinessAttempt), sub.BusinessID
		}
	}
	return attempt, id
}

// Submit broadcasts a stored batch unless it is already committed, waits
// for a committed result within Inclusion, and reads the records back:
//
//   - COMMITTED or RECORDS_FOUND: nothing is broadcast
//   - FAILED(REPLAY): the business id was taken; the next business
//     attempt is broadcast with a business id from Rand (the auditor
//     signature does not cover it)
//   - FAILED(CONFLICT, EXPIRED, WITHDRAWN): the items whose lease or
//     content changed, and FAIL items without a bound endpoint source, are
//     dropped and the rest is rebuilt, signed, stored, and submitted as a
//     new batch in the same epoch (Result.Next)
//   - any other FAILED code: returned (STALE_VERSION needs a new audit)
//   - NOT_SUBMITTED, NOT_ACCEPTED, SUBMITTED: broadcast with the current
//     account sequence; a SUBMITTED batch whose epoch has passed is not
//     broadcast again, and one whose rebroadcast CheckTx refuses stays
//     SUBMITTED with the warning REBROADCAST_NOT_ACCEPTED (the earlier
//     submission may still commit)
//
// Before a broadcast it reads a fresh header: another epoch than the
// batch's adds the warning EPOCH_LIKELY_STALE, and a batch never submitted
// is still broadcast (the chain decides; a rejected transaction changes no
// state). An acknowledgement alone leaves the batch SUBMITTED. Before
// each broadcast it also reads the stored report of every FAIL item and
// returns an error, broadcasting nothing, when one does not say its
// endpoint came from a bound capability manifest (batch.CheckFailSources,
// N2); a committed or recorded batch is still reported.
func (s *Submitter) Submit(ctx context.Context, b *batch.Batch, runID string) (Result, error) {
	return s.submit(ctx, b, runID, true)
}

func (s *Submitter) submit(ctx context.Context, b *batch.Batch, runID string, mayRebuild bool) (Result, error) {
	if s.Key == nil {
		return Result{}, errors.New("chainclient: submit needs the auditor key")
	}
	if !bytes.Equal(s.Key.Address(), b.Msg.GetSigner()) {
		return Result{}, errors.New("chainclient: the key's address is not the batch signer")
	}
	var warnings []string
	var ack *Ack
	for round := 0; round < maxRounds; round++ {
		res, subs, err := s.state(ctx, b)
		if err != nil {
			return Result{}, err
		}
		res.Warnings, res.Ack = warnings, ack
		attempt, businessID := lastAttempt(b, subs)
		switch res.State {
		case Committed:
			return res, s.index(b, res)
		case RecordsFound:
			return res, nil
		case Failed:
			if err := s.index(b, res); err != nil {
				return res, err
			}
			switch SVNCode(res.Codespace, res.Code) {
			case msgkit.Replay:
				attempt++
				if businessID, err = batch.RetryBusinessID(b.Msg.GetSigner(), s.Rand); err != nil {
					return res, err
				}
			case msgkit.Conflict, msgkit.Expired, msgkit.Withdrawn:
				if !mayRebuild {
					return res, nil
				}
				return s.rebuild(ctx, b, runID, res)
			default:
				return res, nil
			}
		}
		header, err := s.Client.Header(ctx)
		if err != nil {
			return res, err
		}
		if evidence.EpochAt(header.Time) != b.Epoch() {
			if len(warnings) == 0 {
				warnings = append(warnings, WarnEpochStale)
				res.Warnings = warnings
			}
			if res.State == Submitted {
				return res, nil
			}
		}
		if len(subs) >= MaxSubmissions {
			return res, fmt.Errorf("chainclient: batch %s has %d submissions, the bound", b.ID(), MaxSubmissions)
		}
		// N2: a FAIL item is broadcast only when its stored report says
		// its endpoint came from a bound capability manifest.
		if err := batch.CheckFailSources(s.Store, b); err != nil {
			return res, err
		}
		sub, a, err := s.broadcast(ctx, b, header.ChainID, len(subs), attempt, businessID)
		if err != nil {
			return res, err
		}
		ack = a
		if sub.Ack == AckNotAccepted {
			res.Ack = ack
			if res.State == Submitted {
				// An earlier submission is still waiting and may commit;
				// CheckTx refused only this one.
				res.Warnings = append(warnings, WarnRebroadcastNotAccepted)
				return res, nil
			}
			res.State, res.Codespace, res.Code, res.TxHash = NotAccepted, sub.AckCodespace, uint32(sub.AckCode), sub.TxHash
			return res, nil
		}
		committed, err := s.wait(ctx, sub.TxHash)
		if err != nil {
			return res, err
		}
		if !committed {
			res.State, res.TxHash, res.Ack = Submitted, sub.TxHash, ack
			return res, nil
		}
	}
	res, _, err := s.state(ctx, b)
	if err != nil {
		return Result{}, err
	}
	res.Warnings, res.Ack = warnings, ack
	return res, nil
}

// broadcast builds and signs submission n of b, business attempt attempt
// with businessID, stores its bytes, sends it, and then stores the
// submission record with
// the CheckTx answer. A transport error stores the answer UNKNOWN: the
// transaction may still commit. The bytes are stored first so that a stop
// or a store error after the send still leaves the hash to query.
func (s *Submitter) broadcast(ctx context.Context, b *batch.Batch, chainID string, n int, attempt uint64, businessID string) (Submission, *Ack, error) {
	number, sequence, err := s.Client.Account(ctx, b.Msg.GetSigner())
	if err != nil {
		return Submission{}, nil, err
	}
	msg := batch.WithBusinessID(b.Msg, businessID)
	raw, hash, err := s.Client.BuildTx(msg, s.Key.SDK(), TxInput{ChainID: chainID, AccountNumber: number, Sequence: sequence, Gas: s.Gas})
	if err != nil {
		return Submission{}, nil, err
	}
	sub := Submission{
		Format: SubmissionFormat, BatchID: b.ID(), Number: int64(n), BusinessAttempt: int64(attempt),
		BusinessID: msg.GetBusinessId(), ChainID: chainID, AccountNumber: strconv.FormatUint(number, 10),
		Sequence: strconv.FormatUint(sequence, 10), TxHash: hex.EncodeToString(hash[:]), Ack: AckUnknown,
	}
	name := SubmissionName(b.ID(), n)
	if err := s.Store.Put(store.Batches, name, ".pb", raw); err != nil {
		return Submission{}, nil, fmt.Errorf("chainclient: store transaction: %w", err)
	}
	a, berr := s.Client.Broadcast(ctx, raw)
	var ack *Ack
	if berr == nil {
		ack = &a
		sub.Ack, sub.AckCodespace, sub.AckCode = AckAccepted, a.Codespace, int64(a.Code)
		if a.Code != 0 {
			sub.Ack = AckNotAccepted
		}
	}
	text, err := sub.encode()
	if err != nil {
		return Submission{}, nil, err
	}
	if err := s.Store.Put(store.Batches, name, ".json", text); err != nil {
		return Submission{}, nil, fmt.Errorf("chainclient: store submission: %w", err)
	}
	// After a transport error the answer is unknown (ack nil, record
	// UNKNOWN): the transaction may still commit, and the caller polls.
	return sub, ack, nil
}

// wait polls the committed result of a transaction until a block holds it
// or Inclusion passes. It returns true when committed.
func (s *Submitter) wait(ctx context.Context, txHash string) (bool, error) {
	hash, _ := hex.DecodeString(txHash)
	deadline := time.Now().Add(s.Inclusion)
	for {
		_, found, err := s.Client.CommittedTx(ctx, hash)
		if err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
		if !time.Now().Before(deadline) {
			return false, nil
		}
		timer := time.NewTimer(s.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
}

// rebuild drops the items of a batch the chain refused for a lease or
// content change and submits the rest as a new batch in the same epoch. It
// also drops a FAIL item whose stored report does not say its endpoint came
// from a bound capability manifest (N2), so no such item is signed again.
func (s *Submitter) rebuild(ctx context.Context, b *batch.Batch, runID string, res Result) (Result, error) {
	header, err := s.Client.Header(ctx)
	if err != nil {
		return res, err
	}
	if evidence.EpochAt(header.Time) != b.Epoch() {
		res.Warnings = append(res.Warnings, WarnEpochStale)
		return res, nil
	}
	keep := map[string]bool{}
	for _, it := range b.Items {
		if batch.CheckFailSource(s.Store, it) != nil {
			keep[it.EvidenceID] = false
			continue
		}
		ok, err := s.stillEligible(ctx, b.ProviderID, it, header.Time)
		if err != nil {
			return res, err
		}
		keep[it.EvidenceID] = ok
	}
	nb, dropped, err := batch.Rebuild(b, func(it batch.Item) bool { return keep[it.EvidenceID] })
	if err != nil {
		return res, err
	}
	for _, it := range dropped {
		res.Dropped = append(res.Dropped, it.EvidenceID)
	}
	if nb == nil {
		return res, nil
	}
	if err := batch.Sign(nb, s.Key); err != nil {
		return res, err
	}
	if err := batch.Persist(s.Store, nb, runID); err != nil {
		return res, err
	}
	next, err := s.submit(ctx, nb, runID, false)
	res.Next = &next
	return res, err
}

// stillEligible re-reads an item's lease and content at header time: the
// lease must still be the held, eligible lease of its pair with a deadline
// at or after the item's expiry, the expiry must be after header time plus
// the inclusion timeout, and the content must not be withdrawn.
func (s *Submitter) stillEligible(ctx context.Context, provider string, it batch.Item, now int64) (bool, error) {
	lease, found, err := s.Client.HeldLease(ctx, provider, it.ContentID)
	if err != nil {
		return false, err
	}
	ref := batch.LeaseRef{LeaseID: it.LeaseID, ProviderID: provider, ContentID: it.ContentID}
	if !found || batch.CheckLease(lease, ref, now, s.Window) != "" || lease.GetContentVersion() != it.ContentVersion ||
		it.Expiry > lease.GetDeadlineUnixSeconds() || it.Expiry <= now+s.Window.Inclusion {
		return false, nil
	}
	if it.Result == report.ResultPass {
		manifest, _ := batch.ManifestSHA256(lease)
		if manifest != it.Observation {
			return false, nil
		}
	}
	content, found, err := s.Client.Content(ctx, it.ContentID, it.ContentVersion)
	if err != nil {
		return false, err
	}
	return batch.CheckContent(content, found, lease) == "", nil
}

// index appends one index line per item of a committed result, once:
// evidence id, full report SHA-256, batch id, transaction hash, height,
// code, and item state. Nothing is indexed before a committed result.
func (s *Submitter) index(b *batch.Batch, res Result) error {
	if res.State != Committed && res.State != Failed {
		return nil
	}
	existing := map[string]bool{}
	if err := s.Store.ScanIndex(func(line []byte) error {
		existing[string(line)] = true
		return nil
	}); err != nil {
		return err
	}
	for i, it := range b.Items {
		state := ItemFailed
		if res.State == Committed {
			state = res.Items[i].State
		}
		text, err := jcs.Canonicalize(map[string]any{
			"evidence_id": it.EvidenceID, "report_sha256": hex.EncodeToString(it.ReportSHA256[:]),
			"batch_id": b.ID(), "tx_hash": res.TxHash, "height": res.Height,
			"codespace": res.Codespace, "code": int64(res.Code), "state": string(state),
		})
		if err != nil {
			return fmt.Errorf("chainclient: index line: %w", err)
		}
		if existing[text] {
			continue
		}
		if err := s.Store.AppendIndex([]byte(text)); err != nil {
			return err
		}
		existing[text] = true
	}
	return nil
}

// IndexLine is one parsed index line.
type IndexLine struct {
	EvidenceID   string `json:"evidence_id"`
	ReportSHA256 string `json:"report_sha256"`
	BatchID      string `json:"batch_id"`
	TxHash       string `json:"tx_hash"`
	Height       int64  `json:"height"`
	Codespace    string `json:"codespace"`
	Code         int64  `json:"code"`
	State        string `json:"state"`
}

// ParseIndexLine reads one index line written by Submitter.
func ParseIndexLine(line []byte) (IndexLine, error) {
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	var l IndexLine
	if err := dec.Decode(&l); err != nil {
		return IndexLine{}, fmt.Errorf("chainclient: index line: %w", err)
	}
	if !msgkit.ValidID(l.EvidenceID) || !hex64.MatchString(l.ReportSHA256) {
		return IndexLine{}, errors.New("chainclient: index line fields")
	}
	return l, nil
}
