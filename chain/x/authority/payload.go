package authority

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"strconv"

	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const (
	safeMax  int64 = 9007199254740991
	unixMin  int64 = 1
	unixMax  int64 = 253402300799
	int64Max int64 = 1<<63 - 1

	// devnetTestRole is the role name a devnet test parameter carries when
	// it stands in for an unapproved decision on a devnet chain.
	devnetTestRole = "DEVNET_TEST_ONLY"
)

// Payload kinds. They name the oneof alternative of ProposalPayload and are
// stored in proposal records.
const (
	kindValidatorPower  = "validator_power"
	kindGovernedLimit   = "governed_limit"
	kindAdmitProvider   = "admit_provider"
	kindDisableProvider = "disable_provider"
	kindForceWithdraw   = "force_withdraw"

	// Schema version 2 (goal 10, owner ruling Q1 "Minimal additive
	// (Recommended)"). The spec machine parses each of these alternatives as
	// ERROR_CODE_MALFORMED (D-5).
	kindSoftwareUpgrade = "software_upgrade"
	kindCancelUpgrade   = "cancel_upgrade"
	kindCouncilSeat     = "council_seat"
)

// upgradeParameterKey is the parameter key a software upgrade and a cancel
// bind to, so the executed set (0x12) and the stale branch treat every plan
// decision as one parameter.
const upgradeParameterKey = "upgrade"

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// PowerChange is the normalized validator payload.
type PowerChange struct {
	NodeID   string
	PubKey   []byte
	Power    int64
	Reason   []byte
	Height   uint64
	Unix     int64
	ByHeight bool
}

// PayloadDigest is the SHA-256 of the canonical payload.
func PayloadDigest(payload *authorityv1.ProposalPayload) ([]byte, Outcome) {
	_, _, canon, out := canonicalOf(payload)
	if !out.Success() {
		return nil, out
	}
	sum := sha256.Sum256([]byte(canon))
	return sum[:], ok()
}

// CanonicalFromProto rebuilds the SVN-JCS-1 payload and checks the digest.
// It returns the normalized validator change (zero for other kinds), the
// kind, and the canonical text.
func CanonicalFromProto(payload *authorityv1.ProposalPayload, digest []byte) (PowerChange, string, string, Outcome) {
	change, kind, canon, out := canonicalOf(payload)
	if !out.Success() {
		return PowerChange{}, "", "", out
	}
	if !digestMatches(canon, digest) {
		return PowerChange{}, "", "", fail(CodeIntegrity)
	}
	return change, kind, canon, ok()
}

// canonicalOf parses one payload alternative: its field checks
// (ERROR_CODE_MALFORMED or ERROR_CODE_BOUNDS), then its SVN-JCS-1 text. A
// nil payload or an unknown alternative is ERROR_CODE_MALFORMED.
func canonicalOf(payload *authorityv1.ProposalPayload) (PowerChange, string, string, Outcome) {
	if payload == nil {
		return PowerChange{}, "", "", fail(CodeMalformed)
	}
	var change PowerChange
	var kind, canon string
	var out Outcome
	switch body := payload.GetChange().(type) {
	case *authorityv1.ProposalPayload_ValidatorPower:
		kind = kindValidatorPower
		change, canon, out = validatorCanonical(body.ValidatorPower)
	case *authorityv1.ProposalPayload_GovernedLimit:
		kind = kindGovernedLimit
		canon, out = governedCanonical(body.GovernedLimit)
	case *authorityv1.ProposalPayload_AdmitProvider:
		kind = kindAdmitProvider
		canon, out = idCanonical("admitProvider", "providerId", body.AdmitProvider.GetProviderId())
	case *authorityv1.ProposalPayload_DisableProvider:
		kind = kindDisableProvider
		canon, out = idCanonical("disableProvider", "providerId", body.DisableProvider.GetProviderId())
	case *authorityv1.ProposalPayload_ForceWithdraw:
		kind = kindForceWithdraw
		canon, out = withdrawCanonical(body.ForceWithdraw)
	case *authorityv1.ProposalPayload_SoftwareUpgrade:
		kind = kindSoftwareUpgrade
		canon, out = upgradeCanonical(body.SoftwareUpgrade)
	case *authorityv1.ProposalPayload_CancelUpgrade:
		kind = kindCancelUpgrade
		canon, out = idCanonical("cancelUpgrade", "name", body.CancelUpgrade.GetName())
	case *authorityv1.ProposalPayload_CouncilSeat:
		kind = kindCouncilSeat
		canon, out = seatCanonical(body.CouncilSeat)
	default:
		return PowerChange{}, "", "", fail(CodeMalformed)
	}
	if !out.Success() {
		return PowerChange{}, "", "", out
	}
	return change, kind, canon, ok()
}

func digestMatches(canon string, digest []byte) bool {
	sum := sha256.Sum256([]byte(canon))
	return len(digest) == 32 && bytes.Equal(sum[:], digest)
}

func validatorCanonical(body *authorityv1.ValidatorPowerChange) (PowerChange, string, Outcome) {
	if body == nil {
		return PowerChange{}, "", fail(CodeMalformed)
	}
	pub := body.GetConsensusPubkey()
	if len(pub) != 32 {
		return PowerChange{}, "", fail(CodeBounds)
	}
	if !idPattern.MatchString(body.GetNodeId()) {
		return PowerChange{}, "", fail(CodeBounds)
	}
	reason, out := digestField(body.GetReasonDigest())
	if !out.Success() {
		return PowerChange{}, "", out
	}
	act, actJSON, out := activationJSON(body.GetActivation())
	if !out.Success() {
		return PowerChange{}, "", out
	}
	power := body.GetPower()
	if power < 0 || power > int64Max {
		return PowerChange{}, "", fail(CodeBounds)
	}
	rebuilt := map[string]any{
		"activation":      actJSON,
		"consensusPubkey": base64.StdEncoding.EncodeToString(pub),
		"nodeId":          body.GetNodeId(),
		"reasonDigest":    digestJSON(reason),
	}
	if power != 0 {
		rebuilt["power"] = strconv.FormatInt(power, 10)
	}
	canon, err := jcs.Canonicalize(map[string]any{"validatorPower": rebuilt})
	if err != nil {
		return PowerChange{}, "", fail(CodeMalformed)
	}
	change := PowerChange{
		NodeID:   body.GetNodeId(),
		PubKey:   append([]byte(nil), pub...),
		Power:    power,
		Reason:   reason,
		ByHeight: act.byHeight,
		Height:   act.height,
		Unix:     act.unix,
	}
	return change, canon, ok()
}

type activation struct {
	byHeight bool
	height   uint64
	unix     int64
}

func activationJSON(point *authorityv1.ActivationPoint) (activation, map[string]any, Outcome) {
	if point == nil || point.GetPoint() == nil {
		return activation{}, nil, fail(CodeMalformed)
	}
	switch body := point.GetPoint().(type) {
	case *authorityv1.ActivationPoint_Height:
		// Compare as uint64: int64(height) is negative from 2^63 up.
		if body.Height < 1 || body.Height > uint64(safeMax) {
			return activation{}, nil, fail(CodeMalformed)
		}
		return activation{byHeight: true, height: body.Height}, map[string]any{
			"height": strconv.FormatUint(body.Height, 10),
		}, ok()
	case *authorityv1.ActivationPoint_UnixSeconds:
		if body.UnixSeconds < unixMin || body.UnixSeconds > unixMax || body.UnixSeconds > safeMax {
			return activation{}, nil, fail(CodeBounds)
		}
		return activation{unix: body.UnixSeconds}, map[string]any{
			"unixSeconds": strconv.FormatInt(body.UnixSeconds, 10),
		}, ok()
	default:
		return activation{}, nil, fail(CodeMalformed)
	}
}

func digestField(digest *typesv1.Digest) ([]byte, Outcome) {
	if digest == nil || digest.GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 {
		return nil, fail(CodeMalformed)
	}
	if len(digest.GetValue()) != 32 {
		return nil, fail(CodeBounds)
	}
	return append([]byte(nil), digest.GetValue()...), ok()
}

func digestJSON(raw []byte) map[string]any {
	return map[string]any{
		"algorithm": "DIGEST_ALGORITHM_SHA256",
		"value":     base64.StdEncoding.EncodeToString(raw),
	}
}

// governedCanonical is the SVN-JCS-1 text of a governed limit payload. A
// key outside the id pattern or a value below 1 is ERROR_CODE_BOUNDS. An
// activation height of 0 is absent from the text, as proto3 JSON omits a
// zero, so a v0 payload keeps its canonical text and digest (schema version
// 2). The activation window is checked after the parameter key binding.
func governedCanonical(body *authorityv1.GovernedLimitChange) (string, Outcome) {
	if body == nil || !idPattern.MatchString(body.GetLimitKey()) || body.GetValue() < 1 {
		return "", fail(CodeBounds)
	}
	inner := map[string]any{
		"limitKey": body.GetLimitKey(),
		"value":    strconv.FormatUint(body.GetValue(), 10),
	}
	if body.GetActivationHeight() != 0 {
		inner["activationHeight"] = strconv.FormatUint(body.GetActivationHeight(), 10)
	}
	canon, err := jcs.Canonicalize(map[string]any{"governedLimit": inner})
	if err != nil {
		return "", fail(CodeMalformed)
	}
	return canon, ok()
}

// upgradeCanonical is the SVN-JCS-1 text of a software upgrade payload
// (schema version 2). A name outside the id pattern is ERROR_CODE_BOUNDS; a
// release digest that does not name SHA-256 is ERROR_CODE_MALFORMED and one
// that is not 32 bytes ERROR_CODE_BOUNDS. A height of 0 is absent from the
// text, as proto3 JSON omits a zero; the lead window, checked after the
// parameter key binding, refuses it (ERROR_CODE_OUT_OF_RANGE).
func upgradeCanonical(body *authorityv1.SoftwareUpgradeChange) (string, Outcome) {
	if !idPattern.MatchString(body.GetName()) {
		return "", fail(CodeBounds)
	}
	digest, out := digestField(body.GetReleaseDigest())
	if !out.Success() {
		return "", out
	}
	inner := map[string]any{
		"name":          body.GetName(),
		"releaseDigest": digestJSON(digest),
	}
	if body.GetHeight() != 0 {
		inner["height"] = strconv.FormatUint(body.GetHeight(), 10)
	}
	canon, err := jcs.Canonicalize(map[string]any{"softwareUpgrade": inner})
	if err != nil {
		return "", fail(CodeMalformed)
	}
	return canon, ok()
}

// seatCanonical is the SVN-JCS-1 text of a council seat payload (schema
// version 2). A seat id outside the id pattern or a key that is not 32
// bytes is ERROR_CODE_BOUNDS.
func seatCanonical(body *authorityv1.CouncilSeatChange) (string, Outcome) {
	if !idPattern.MatchString(body.GetSeatId()) || len(body.GetNewPublicKey()) != 32 {
		return "", fail(CodeBounds)
	}
	canon, err := jcs.Canonicalize(map[string]any{
		"councilSeat": map[string]any{
			"newPublicKey": base64.StdEncoding.EncodeToString(body.GetNewPublicKey()),
			"seatId":       body.GetSeatId(),
		},
	})
	if err != nil {
		return "", fail(CodeMalformed)
	}
	return canon, ok()
}

func idCanonical(field, inner, id string) (string, Outcome) {
	if !idPattern.MatchString(id) {
		return "", fail(CodeBounds)
	}
	canon, err := jcs.Canonicalize(map[string]any{
		field: map[string]any{inner: id},
	})
	if err != nil {
		return "", fail(CodeMalformed)
	}
	return canon, ok()
}

func withdrawCanonical(body *authorityv1.ForceWithdrawChange) (string, Outcome) {
	if body == nil || !idPattern.MatchString(body.GetContentId()) || body.GetContentVersion() < 1 {
		if body != nil && body.GetContentId() != "" && !idPattern.MatchString(body.GetContentId()) {
			return "", fail(CodeBounds)
		}
		return "", fail(CodeMalformed)
	}
	reason, out := digestField(body.GetReasonDigest())
	if !out.Success() {
		return "", out
	}
	canon, err := jcs.Canonicalize(map[string]any{
		"forceWithdraw": map[string]any{
			"contentId":      body.GetContentId(),
			"contentVersion": strconv.FormatUint(body.GetContentVersion(), 10),
			"reasonDigest":   digestJSON(reason),
		},
	})
	if err != nil {
		return "", fail(CodeMalformed)
	}
	return canon, ok()
}

// parameterKey is the parameter key payload d binds to (MALFORMED on a
// mismatch at propose). The schema version 2 kinds cannot collide with a v0
// key: provider keys start "admit-" or "disable-", validator keys
// "validator-", withdraw keys "withdraw-", and governed keys are the limit
// keys "max_...".
func parameterKey(d decodedWire) string {
	switch d.Kind {
	case kindValidatorPower:
		return "validator-" + d.NodeID
	case kindGovernedLimit:
		return d.LimitKey
	case kindAdmitProvider:
		return "admit-" + d.ProviderID
	case kindDisableProvider:
		return "disable-" + d.ProviderID
	case kindForceWithdraw:
		return "withdraw-" + d.ContentID
	case kindSoftwareUpgrade, kindCancelUpgrade:
		return upgradeParameterKey
	case kindCouncilSeat:
		return "seat-" + d.SeatID
	default:
		return ""
	}
}
