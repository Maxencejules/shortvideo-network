package content

import (
	"bytes"
	"context"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// v0 content records use content_version 1 (protocol.md integers section).
// A second encoding is a new content_id until a recorded decision says
// otherwise.
const v0ContentVersion uint64 = 1

// sdkContext returns the SDK context a router placed in goCtx. A context
// without one is a wiring error.
func sdkContext(goCtx context.Context) (sdk.Context, error) {
	if ctx, ok := goCtx.(sdk.Context); ok {
		return ctx, nil
	}
	if ctx, ok := goCtx.Value(sdk.SdkContextKey).(sdk.Context); ok {
		return ctx, nil
	}
	return sdk.Context{}, svnerr.ErrMalformed.Wrap("content: no SDK context")
}

// RegisterContent is the committed RegisterContent handler. The ante
// handler has verified the outer signature. Checks run in the order of
// op_register_content and all of them run before the first write:
//
//  0. msgkit.Enforce: gas limit and tx size (BOUNDS), signer size (MALFORMED)
//  1. business_id, content_id, manifest_digest, hosting_intent present (MALFORMED)
//  2. signer role: none UNAUTHORIZED; not ADMITTED_SERVICE FORBIDDEN
//  3. content_id outside the id pattern: BOUNDS
//  4. content_version other than 1: OUT_OF_RANGE
//  5. package_bytes outside 1..effective max_package_bytes: BOUNDS
//  6. manifest_digest: MALFORMED or BOUNDS (SHA-256, 32 bytes)
//  7. hosting intent (checkIntent)
//  8. business gate: same payload idempotent, other payload REPLAY
//  9. any record of content_id, in any state: DUPLICATE_ID (no revival)
//
// Success writes one PENDING record and the business id, and emits
// ContentRegistered. It writes no reservation, lease, offer, or
// publication state: a registry entry is not a hosting claim.
func (k Keeper) RegisterContent(goCtx context.Context, msg *contentv1.MsgRegisterContent) (*contentv1.MsgRegisterContentResponse, error) {
	ctx, err := sdkContext(goCtx)
	if err != nil {
		return nil, err
	}
	out := k.registerContent(ctx, msg)
	if out.Code != msgkit.OK {
		return nil, out.Code.Err()
	}
	if out.Idempotent {
		msgkit.EmitIdempotent(ctx, ModuleName)
	}
	return &contentv1.MsgRegisterContentResponse{}, nil
}

func (k Keeper) registerContent(ctx sdk.Context, msg *contentv1.MsgRegisterContent) msgkit.Outcome {
	if msg == nil {
		return failed(msgkit.Malformed)
	}
	if code := msgkit.Enforce(ctx, msg.GetSigner()); code != msgkit.OK {
		return failed(code)
	}
	if msg.GetBusinessId() == "" || msg.GetContentId() == "" || msg.GetManifestDigest() == nil || msg.GetHostingIntent() == nil {
		return failed(msgkit.Malformed)
	}
	if code := k.checkServiceRole(ctx, msg.GetSigner()); code != msgkit.OK {
		return failed(code)
	}
	if !msgkit.ValidID(msg.GetContentId()) {
		return failed(msgkit.Bounds)
	}
	if msg.GetContentVersion() != v0ContentVersion {
		return failed(msgkit.OutOfRange)
	}
	if code := k.checkPackageBytes(ctx, msg.GetPackageBytes()); code != msgkit.OK {
		return failed(code)
	}
	manifest, code := msgkit.CheckDigest(msg.GetManifestDigest())
	if code != msgkit.OK {
		return failed(code)
	}
	intent := func(role string) (string, error) {
		return registerIntentCanonical(msg.GetSigner(), msg.GetContentId(), msg.GetContentVersion(), manifest, msg.GetPackageBytes(), role)
	}
	if code := k.checkIntent(ctx, msg.GetHostingIntent(), intent); code != msgkit.OK {
		return failed(code)
	}
	business, code := msgkit.BusinessDigest(map[string]any{
		"content_id":      msg.GetContentId(),
		"content_version": msgkit.Dec(msg.GetContentVersion()),
		"hosting_intent":  msgkit.SignatureObject(msg.GetHostingIntent()),
		"manifest_digest": msgkit.DigestObject(msg.GetManifestDigest()),
		"package_bytes":   msgkit.Dec(msg.GetPackageBytes()),
	})
	if code != msgkit.OK {
		return failed(code)
	}
	if gate := k.ledger.Gate(ctx, msg.GetBusinessId(), business); gate.Code != msgkit.OK || gate.Idempotent {
		return gate
	}
	if _, exists := k.contentByID(ctx, msg.GetContentId()); exists {
		return failed(msgkit.DuplicateID)
	}

	record := &contentv1.Content{
		ContentId:      msg.GetContentId(),
		ContentVersion: msg.GetContentVersion(),
		ManifestDigest: sha256Digest(manifest),
		PackageBytes:   msg.GetPackageBytes(),
		SubmittingHost: append([]byte(nil), msg.GetSigner()...),
		State:          contentv1.ContentState_CONTENT_STATE_PENDING,
	}
	err := k.commit(ctx, func(cc sdk.Context) error {
		k.ledger.Commit(cc, msg.GetBusinessId(), business)
		k.setContent(cc, record)
		return msgkit.EmitTyped(cc, &eventsv1.ContentRegistered{
			ContentId:      record.GetContentId(),
			ContentVersion: record.GetContentVersion(),
			ManifestSha256: msgkit.Hex(manifest),
			PackageBytes:   msgkit.Dec(record.GetPackageBytes()),
		})
	})
	if err != nil {
		// An event encoding failure carries no svn code: MALFORMED.
		// Nothing of the branch was written.
		return failed(codeOf(err))
	}
	return msgkit.Outcome{}
}

// WithdrawContent is the committed WithdrawContent handler. Checks run in
// the order of op_withdraw_content and withdraw_content, all before the
// first write:
//
//  0. msgkit.Enforce
//  1. business_id, content_id, reason_digest, hosting_intent present (MALFORMED)
//  2. signer role: none UNAUTHORIZED; not ADMITTED_SERVICE FORBIDDEN
//  3. reason_digest: MALFORMED or BOUNDS
//  4. hosting intent over the withdraw payload (checkIntent)
//  5. business gate: same payload idempotent, other payload REPLAY
//  6. no record of content_id: NOT_FOUND (an id outside the pattern has none)
//  7. signer is not the record's submitting host: FORBIDDEN (cross-owner)
//  8. record already WITHDRAWN: WITHDRAWN
//  9. content_version is not the record's version: STALE_VERSION
//
// Success marks the record WITHDRAWN, writes its tombstone, runs the
// withdrawal hook if one is wired, and emits ContentWithdrawn. The record
// and its history stay. Accepted reservations are not released.
func (k Keeper) WithdrawContent(goCtx context.Context, msg *contentv1.MsgWithdrawContent) (*contentv1.MsgWithdrawContentResponse, error) {
	ctx, err := sdkContext(goCtx)
	if err != nil {
		return nil, err
	}
	out := k.withdrawContent(ctx, msg)
	if out.Code != msgkit.OK {
		return nil, out.Code.Err()
	}
	if out.Idempotent {
		msgkit.EmitIdempotent(ctx, ModuleName)
	}
	return &contentv1.MsgWithdrawContentResponse{}, nil
}

func (k Keeper) withdrawContent(ctx sdk.Context, msg *contentv1.MsgWithdrawContent) msgkit.Outcome {
	if msg == nil {
		return failed(msgkit.Malformed)
	}
	if code := msgkit.Enforce(ctx, msg.GetSigner()); code != msgkit.OK {
		return failed(code)
	}
	if msg.GetBusinessId() == "" || msg.GetContentId() == "" || msg.GetReasonDigest() == nil || msg.GetHostingIntent() == nil {
		return failed(msgkit.Malformed)
	}
	if code := k.checkServiceRole(ctx, msg.GetSigner()); code != msgkit.OK {
		return failed(code)
	}
	reason, code := msgkit.CheckDigest(msg.GetReasonDigest())
	if code != msgkit.OK {
		return failed(code)
	}
	intent := func(role string) (string, error) {
		return withdrawIntentCanonical(msg.GetSigner(), msg.GetContentId(), msg.GetContentVersion(), reason, role)
	}
	if code := k.checkIntent(ctx, msg.GetHostingIntent(), intent); code != msgkit.OK {
		return failed(code)
	}
	business, code := msgkit.BusinessDigest(map[string]any{
		"content_id":      msg.GetContentId(),
		"content_version": msgkit.Dec(msg.GetContentVersion()),
		"hosting_intent":  msgkit.SignatureObject(msg.GetHostingIntent()),
		"reason_digest":   msgkit.DigestObject(msg.GetReasonDigest()),
	})
	if code != msgkit.OK {
		return failed(code)
	}
	if gate := k.ledger.Gate(ctx, msg.GetBusinessId(), business); gate.Code != msgkit.OK || gate.Idempotent {
		return gate
	}
	record, found := k.contentByID(ctx, msg.GetContentId())
	if !found {
		return failed(msgkit.NotFound)
	}
	if !bytes.Equal(record.GetSubmittingHost(), msg.GetSigner()) {
		return failed(msgkit.Forbidden)
	}
	if code := checkWithdrawable(record, msg.GetContentVersion()); code != msgkit.OK {
		return failed(code)
	}
	if code := checkTombstoneTime(ctx); code != msgkit.OK {
		return failed(code)
	}
	err := k.commit(ctx, func(cc sdk.Context) error {
		k.ledger.Commit(cc, msg.GetBusinessId(), business)
		return k.writeWithdrawal(cc, record, reason)
	})
	if err != nil {
		// A hook or event failure: its svn code, or MALFORMED for an error
		// that carries none, as ForceWithdrawByCouncil maps it. Nothing of
		// the branch was written.
		return failed(codeOf(err))
	}
	return msgkit.Outcome{}
}

// ForceWithdrawByCouncil applies an executed council ForceWithdrawChange
// (DEC-003, DEC-009). x/authority calls it from ExecuteChange after the
// council checks pass and passes devnet = true only when the DEC-003 devnet
// test parameter names DEVNET_TEST_ONLY. reasonSHA256 is the payload's
// 32-byte reason digest value. The order follows the spec's
// apply_proposal_payload force-withdraw branch:
//
//  0. reason not 32 bytes: MALFORMED (a wiring error; authority validated it)
//  1. no record of contentID: NOT_FOUND
//  2. devnet false: DECISION_BLOCKED
//  3. record already WITHDRAWN: WITHDRAWN
//  4. version is not the record's version: STALE_VERSION
//
// There is no submitting-host check: the council acts for no host. Success
// writes the same tombstone as WithdrawContent and emits ContentWithdrawn.
// A failure writes nothing. A withdrawal hook error returns the hook's svn
// code when it carries one and MALFORMED otherwise; nothing is written.
func (k Keeper) ForceWithdrawByCouncil(ctx sdk.Context, contentID string, version uint64, reasonSHA256 []byte, devnet bool) msgkit.Code {
	if len(reasonSHA256) != msgkit.DigestBytes {
		return msgkit.Malformed
	}
	record, found := k.contentByID(ctx, contentID)
	if !found {
		return msgkit.NotFound
	}
	if !devnet {
		return msgkit.DecisionBlocked
	}
	if code := checkWithdrawable(record, version); code != msgkit.OK {
		return code
	}
	if code := checkTombstoneTime(ctx); code != msgkit.OK {
		return code
	}
	reason := append([]byte(nil), reasonSHA256...)
	if err := k.commit(ctx, func(cc sdk.Context) error { return k.writeWithdrawal(cc, record, reason) }); err != nil {
		return codeOf(err)
	}
	return msgkit.OK
}

// checkWithdrawable is the shared tail of withdraw_content: WITHDRAWN
// before the version comparison.
func checkWithdrawable(record *contentv1.Content, version uint64) msgkit.Code {
	if record.GetState() == contentv1.ContentState_CONTENT_STATE_WITHDRAWN {
		return msgkit.Withdrawn
	}
	if version != record.GetContentVersion() {
		return msgkit.StaleVersion
	}
	return msgkit.OK
}

// checkTombstoneTime keeps every tombstone inside the protocol time range,
// so an exported catalogue always validates. CometBFT block times are in
// range; this check runs after every protocol check and changes no spec
// outcome for such a time.
func checkTombstoneTime(ctx sdk.Context) msgkit.Code {
	unix := ctx.BlockTime().Unix()
	if unix < genesis.UnixSecondsMin || unix > genesis.UnixSecondsMax {
		return msgkit.OutOfRange
	}
	return msgkit.OK
}

// writeWithdrawal marks record WITHDRAWN, writes its tombstone, runs the
// hook, and emits ContentWithdrawn. The caller runs it inside commit.
func (k Keeper) writeWithdrawal(ctx sdk.Context, record *contentv1.Content, reasonSHA256 []byte) error {
	withdrawn := &contentv1.Content{
		ContentId:      record.GetContentId(),
		ContentVersion: record.GetContentVersion(),
		ManifestDigest: sha256Digest(record.GetManifestDigest().GetValue()),
		PackageBytes:   record.GetPackageBytes(),
		SubmittingHost: append([]byte(nil), record.GetSubmittingHost()...),
		State:          contentv1.ContentState_CONTENT_STATE_WITHDRAWN,
	}
	k.setContent(ctx, withdrawn)
	k.setTombstone(ctx, &contentv1.Tombstone{
		ContentId:             withdrawn.GetContentId(),
		ContentVersion:        withdrawn.GetContentVersion(),
		CommittedUnixSeconds:  ctx.BlockTime().Unix(),
		ReasonDigest:          sha256Digest(reasonSHA256),
		HistoryErased:         false,
		RetentionDecisionId:   RetentionDecisionID,
		RetentionValueVersion: 0,
	})
	if k.deps.Hook != nil {
		if err := k.deps.Hook.ContentWithdrawn(ctx, withdrawn.GetContentId(), withdrawn.GetContentVersion()); err != nil {
			return err
		}
	}
	return msgkit.EmitTyped(ctx, &eventsv1.ContentWithdrawn{
		ContentId:      withdrawn.GetContentId(),
		ContentVersion: withdrawn.GetContentVersion(),
		HistoryErased:  false,
	})
}

// commit runs write on a cached branch of ctx and writes the branch back,
// with its events, only when write succeeds. So a failed hook or event
// leaves this module's store and the event stream untouched even before
// the SDK discards the message branch.
func (k Keeper) commit(ctx sdk.Context, write func(sdk.Context) error) error {
	cached, flush := ctx.CacheContext()
	if err := write(cached); err != nil {
		return err
	}
	flush()
	return nil
}

func (k Keeper) checkServiceRole(ctx sdk.Context, signer []byte) msgkit.Code {
	switch k.deps.Roles.Role(ctx, signer) {
	case typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE:
		return msgkit.OK
	case typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED:
		return msgkit.Unauthorized
	default:
		return msgkit.Forbidden
	}
}

// checkPackageBytes is check_package: 1..effective max_package_bytes, where
// a governed value applies only within 1..268435456.
func (k Keeper) checkPackageBytes(ctx sdk.Context, n uint64) msgkit.Code {
	governed, found := k.deps.Limits.GovernedLimit(ctx, msgkit.LimitMaxPackageBytes)
	if n < 1 || n > msgkit.Effective(msgkit.LimitMaxPackageBytes, governed, found) {
		return msgkit.Bounds
	}
	return msgkit.OK
}

// codeOf maps an error to its svn protocol code. An error that carries no
// svn code is MALFORMED, so an unknown failure can never become success.
func codeOf(err error) msgkit.Code {
	for n := uint32(1); n <= svnerr.MaxCode; n++ {
		registered, _ := svnerr.ByCode(n)
		if errors.Is(err, registered) {
			return msgkit.Code(typesv1.ErrorCode(n).String())
		}
	}
	return msgkit.Malformed
}

func failed(code msgkit.Code) msgkit.Outcome { return msgkit.Outcome{Code: code} }

func sha256Digest(value []byte) *typesv1.Digest {
	return &typesv1.Digest{
		Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256,
		Value:     append([]byte(nil), value...),
	}
}
