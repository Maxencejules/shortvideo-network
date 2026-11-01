package authority

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"strconv"

	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const (
	safeMax      int64 = 9007199254740991
	unixMin      int64 = 1
	unixMax      int64 = 253402300799
	int64Max     int64 = 1<<63 - 1
	rotateDomain       = "SVN-PROVIDER-ROTATE-1"
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// PowerChange is the normalized validator payload.
type PowerChange struct {
	NodeID    string
	PubKey    []byte
	Power     int64
	Reason    []byte
	Height    uint64
	Unix      int64
	ByHeight  bool
	Canonical string
	Digest    []byte
}

func (p PowerChange) kind() string {
	if p.ByHeight {
		return "height"
	}
	return "time"
}

func (p PowerChange) point() int64 {
	if p.ByHeight {
		return int64(p.Height)
	}
	return p.Unix
}

// PayloadDigest is the SHA-256 of the canonical payload.
func PayloadDigest(payload *authorityv1.ProposalPayload) ([]byte, Outcome) {
	if payload == nil {
		return nil, fail(CodeMalformed)
	}
	var canon string
	var out Outcome
	switch body := payload.GetChange().(type) {
	case *authorityv1.ProposalPayload_ValidatorPower:
		_, canon, out = validatorCanonical(body.ValidatorPower)
	case *authorityv1.ProposalPayload_GovernedLimit:
		canon, out = governedCanonical(body.GovernedLimit)
	case *authorityv1.ProposalPayload_AdmitProvider:
		canon, out = idCanonical("admitProvider", "providerId", body.AdmitProvider.GetProviderId())
	case *authorityv1.ProposalPayload_DisableProvider:
		canon, out = idCanonical("disableProvider", "providerId", body.DisableProvider.GetProviderId())
	case *authorityv1.ProposalPayload_ForceWithdraw:
		canon, out = withdrawCanonical(body.ForceWithdraw)
	default:
		return nil, fail(CodeMalformed)
	}
	if !out.Success() {
		return nil, out
	}
	sum := sha256.Sum256([]byte(canon))
	return sum[:], ok()
}

// CanonicalFromProto rebuilds the SVN-JCS-1 payload and checks the digest.
func CanonicalFromProto(payload *authorityv1.ProposalPayload, digest []byte) (PowerChange, string, string, Outcome) {
	if payload == nil {
		return PowerChange{}, "", "", fail(CodeMalformed)
	}
	switch body := payload.GetChange().(type) {
	case *authorityv1.ProposalPayload_ValidatorPower:
		change, canon, out := validatorCanonical(body.ValidatorPower)
		if !out.Success() {
			return PowerChange{}, "", "", out
		}
		sum := sha256.Sum256([]byte(canon))
		if len(digest) != 32 || !bytes.Equal(sum[:], digest) {
			return PowerChange{}, "", "", fail(CodeIntegrity)
		}
		change.Canonical = canon
		change.Digest = append([]byte(nil), digest...)
		return change, "validator_power", canon, ok()
	case *authorityv1.ProposalPayload_GovernedLimit:
		canon, out := governedCanonical(body.GovernedLimit)
		if !out.Success() {
			return PowerChange{}, "", "", out
		}
		if !digestMatches(canon, digest) {
			return PowerChange{}, "", "", fail(CodeIntegrity)
		}
		return PowerChange{}, "governed_limit", canon, ok()
	case *authorityv1.ProposalPayload_AdmitProvider:
		canon, out := idCanonical("admitProvider", "providerId", body.AdmitProvider.GetProviderId())
		if !out.Success() {
			return PowerChange{}, "", "", out
		}
		if !digestMatches(canon, digest) {
			return PowerChange{}, "", "", fail(CodeIntegrity)
		}
		return PowerChange{}, "admit_provider", canon, ok()
	case *authorityv1.ProposalPayload_DisableProvider:
		canon, out := idCanonical("disableProvider", "providerId", body.DisableProvider.GetProviderId())
		if !out.Success() {
			return PowerChange{}, "", "", out
		}
		if !digestMatches(canon, digest) {
			return PowerChange{}, "", "", fail(CodeIntegrity)
		}
		return PowerChange{}, "disable_provider", canon, ok()
	case *authorityv1.ProposalPayload_ForceWithdraw:
		canon, out := withdrawCanonical(body.ForceWithdraw)
		if !out.Success() {
			return PowerChange{}, "", "", out
		}
		if !digestMatches(canon, digest) {
			return PowerChange{}, "", "", fail(CodeIntegrity)
		}
		return PowerChange{}, "force_withdraw", canon, ok()
	default:
		return PowerChange{}, "", "", fail(CodeMalformed)
	}
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
		if body.Height < 1 || int64(body.Height) > safeMax {
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

func governedCanonical(body *authorityv1.GovernedLimitChange) (string, Outcome) {
	if body == nil || !idPattern.MatchString(body.GetLimitKey()) || body.GetValue() < 1 {
		return "", fail(CodeBounds)
	}
	canon, err := jcs.Canonicalize(map[string]any{
		"governedLimit": map[string]any{
			"limitKey": body.GetLimitKey(),
			"value":    strconv.FormatUint(body.GetValue(), 10),
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

// ParsePayloadJSON parses one bounded SVN-JCS-1 object. Fuzz and the replay
// test use it. Unknown fields and a second alternative are malformed.
func ParsePayloadJSON(raw []byte) (string, Outcome) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", fail(CodeBounds)
	}
	canon, err := jcs.ParseAndCanonicalize(string(raw))
	if err != nil {
		return "", fail(CodeMalformed)
	}
	return canon, ok()
}

func parameterKey(kind, nodeID, limitKey, providerID, contentID string) string {
	switch kind {
	case "validator_power":
		return "validator-" + nodeID
	case "governed_limit":
		return limitKey
	case "admit_provider":
		return "admit-" + providerID
	case "disable_provider":
		return "disable-" + providerID
	default:
		return "withdraw-" + contentID
	}
}

func pubkeyHex(raw []byte) string { return hex.EncodeToString(raw) }

func b64Decode(text string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(text)
}
