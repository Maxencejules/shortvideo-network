package content

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Hosting intent (DEC-001, decision request DR-07-01).
//
// Protocol v0 has no hosting-intent record schema and InnerSignature has no
// role field. Until the owner decides the record, this module uses a
// devnet-only interim: the intent key signs
//
//	"SVN-HOSTING-INTENT-1" || 0x00 || SVN-JCS-1(payload)
//
// where payload names the action, the message fields, the role, and the
// submitting host (lowercase hex of the outer signer, so one intent
// authorizes one host):
//
//	register: {"action":"register","contentId","contentVersion","manifestSha256",
//	           "packageBytes","roleName","submittingHost"}
//	withdraw: {"action":"withdraw","contentId","contentVersion","reasonSha256",
//	           "roleName","submittingHost"}
//
// uint64 values are decimal strings and digests are lowercase hex. The
// public key is the one carried in the message. A valid signature proves
// the payload was signed by that key; it does not prove a creator
// authorized the host, because no intent key is registered on chain and
// DEC-001 key custody is unset. The chain id is not in the payload.

const (
	actionRegister = "register"
	actionWithdraw = "withdraw"
)

var hostingDomain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT

func registerIntentCanonical(signer []byte, contentID string, version uint64, manifestSHA256 []byte, packageBytes uint64, role string) (string, error) {
	return msgkit.Canonical(map[string]any{
		"action":         actionRegister,
		"contentId":      contentID,
		"contentVersion": msgkit.Dec(version),
		"manifestSha256": msgkit.Hex(manifestSHA256),
		"packageBytes":   msgkit.Dec(packageBytes),
		"roleName":       role,
		"submittingHost": msgkit.Hex(signer),
	})
}

func withdrawIntentCanonical(signer []byte, contentID string, version uint64, reasonSHA256 []byte, role string) (string, error) {
	return msgkit.Canonical(map[string]any{
		"action":         actionWithdraw,
		"contentId":      contentID,
		"contentVersion": msgkit.Dec(version),
		"reasonSha256":   msgkit.Hex(reasonSHA256),
		"roleName":       role,
		"submittingHost": msgkit.Hex(signer),
	})
}

func checkSignBytesInputs(signer, digest []byte, role string) error {
	if len(signer) != msgkit.AccountBytes {
		return fmt.Errorf("content intent: signer must be %d bytes, got %d", msgkit.AccountBytes, len(signer))
	}
	if len(digest) != msgkit.DigestBytes {
		return fmt.Errorf("content intent: digest must be %d bytes, got %d", msgkit.DigestBytes, len(digest))
	}
	if role == "" {
		return fmt.Errorf("content intent: empty role name")
	}
	return nil
}

// RegisterIntentSignBytes returns the bytes a hosting-intent key signs to
// let signer (the 20-byte outer signer, the submitting host) register
// (contentID, version) with the given manifest SHA-256 value and package
// byte count. roleName is DevnetRoleName on the devnet; no other role
// passes. Agents (goal 16) and tests sign these bytes with ed25519.
func RegisterIntentSignBytes(signer []byte, contentID string, version uint64, manifestSHA256 []byte, packageBytes uint64, roleName string) ([]byte, error) {
	if err := checkSignBytesInputs(signer, manifestSHA256, roleName); err != nil {
		return nil, err
	}
	canonical, err := registerIntentCanonical(signer, contentID, version, manifestSHA256, packageBytes, roleName)
	if err != nil {
		return nil, fmt.Errorf("content intent: %w", err)
	}
	return msgkit.SignBytes(hostingDomain, canonical), nil
}

// WithdrawIntentSignBytes returns the bytes a hosting-intent key signs to
// let signer withdraw (contentID, version) with the given reason SHA-256
// value. The withdraw intent is not bound to the key that signed the
// register intent (DR-07-01).
func WithdrawIntentSignBytes(signer []byte, contentID string, version uint64, reasonSHA256 []byte, roleName string) ([]byte, error) {
	if err := checkSignBytesInputs(signer, reasonSHA256, roleName); err != nil {
		return nil, err
	}
	canonical, err := withdrawIntentCanonical(signer, contentID, version, reasonSHA256, roleName)
	if err != nil {
		return nil, fmt.Errorf("content intent: %w", err)
	}
	return msgkit.SignBytes(hostingDomain, canonical), nil
}

// checkIntent is intent_ok from the spec machine, in this order:
//
//  1. structure (msgkit.CheckShape): nil, unknown domain, profile version,
//     key id, public key and signature sizes
//  2. the key signed the payload with role ACCOUNT_HOST_DELEGATE: FORBIDDEN
//     (the spec rejects host custody first; the role is visible only
//     through the signed payload)
//  3. domain other than HOSTING_INTENT: WRONG_DOMAIN
//  4. no DEC-001 devnet parameter naming DEVNET_TEST_ONLY: DECISION_BLOCKED
//     (every non-devnet chain stops here)
//  5. the key did not sign the payload with role DEVNET_TEST_ONLY:
//     UNAUTHORIZED
//
// Deviation from the spec: a payload signed for another role name is
// UNAUTHORIZED, not FORBIDDEN, because an unknown role cannot be recognised
// from a signature. canonical builds the payload for one role name; an
// error there (a field that cannot be canonicalized) is MALFORMED.
func (k Keeper) checkIntent(ctx sdk.Context, sig *typesv1.InnerSignature, canonical func(role string) (string, error)) msgkit.Code {
	if code := msgkit.CheckShape(sig); code != msgkit.OK {
		return code
	}
	delegate, err := canonical(DelegateRoleName)
	if err != nil {
		return msgkit.Malformed
	}
	if msgkit.VerifyWithKey(sig.GetPublicKey(), hostingDomain, delegate, sig.GetSignature()) {
		return msgkit.Forbidden
	}
	if sig.GetDomain() != hostingDomain {
		return msgkit.WrongDomain
	}
	role, found := k.deps.Params.DevnetRole(ctx, HostingDecisionID)
	if !found || role != DevnetRoleName {
		return msgkit.DecisionBlocked
	}
	devnet, err := canonical(DevnetRoleName)
	if err != nil {
		return msgkit.Malformed
	}
	if !msgkit.VerifyWithKey(sig.GetPublicKey(), hostingDomain, devnet, sig.GetSignature()) {
		return msgkit.Unauthorized
	}
	return msgkit.OK
}
