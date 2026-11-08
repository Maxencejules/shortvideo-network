package policy

import (
	"bytes"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// canonicalOf returns the canonical payload of chain-built sign bytes:
// signBytes is UTF-8(domain) || 0x00 || canonical (msgkit.SignBytes). The
// domain string comes from the chain's own table, so this package holds no
// copy of any signed preimage (TestNoLocalPreimageCopy).
func canonicalOf(d typesv1.SignatureDomain, signBytes []byte) (string, error) {
	domain, ok := msgkit.DomainString(d)
	if !ok {
		return "", errors.New("policy: unknown signature domain")
	}
	prefix := append([]byte(domain), 0)
	if !bytes.HasPrefix(signBytes, prefix) {
		return "", errors.New("policy: sign bytes do not start with their domain")
	}
	return string(signBytes[len(prefix):]), nil
}

// IntentSignBytes rebuilds the bytes a hosting-intent key signs for msg (a
// MsgRegisterContent or MsgWithdrawContent) with host as the submitting
// host and the devnet role DEVNET_TEST_ONLY, with the chain's exported
// builders (chain/x/content/intent.go).
func IntentSignBytes(msg sdk.Msg, host []byte) ([]byte, *typesv1.InnerSignature, error) {
	if len(host) != genesis.AccountBytes {
		return nil, nil, refuse(CodeInvalid, "submitting host is not a 20-byte address")
	}
	switch m := msg.(type) {
	case *contentv1.MsgRegisterContent:
		manifest, code := msgkit.CheckDigest(m.GetManifestDigest())
		if code != msgkit.OK {
			return nil, nil, refuse(CodeInvalid, "manifest digest: %s", code)
		}
		sb, err := content.RegisterIntentSignBytes(host, m.GetContentId(), m.GetContentVersion(), manifest, m.GetPackageBytes(), content.DevnetRoleName)
		if err != nil {
			return nil, nil, refuse(CodeInvalid, "register intent payload does not build")
		}
		return sb, m.GetHostingIntent(), nil
	case *contentv1.MsgWithdrawContent:
		reason, code := msgkit.CheckDigest(m.GetReasonDigest())
		if code != msgkit.OK {
			return nil, nil, refuse(CodeInvalid, "reason digest: %s", code)
		}
		sb, err := content.WithdrawIntentSignBytes(host, m.GetContentId(), m.GetContentVersion(), reason, content.DevnetRoleName)
		if err != nil {
			return nil, nil, refuse(CodeInvalid, "withdraw intent payload does not build")
		}
		return sb, m.GetHostingIntent(), nil
	}
	return nil, nil, refuse(CodeInvalid, "message carries no hosting intent")
}

// VerifyHostingIntent checks the hosting intent of msg as the chain's
// devnet rule does (shape, domain SVN hosting intent, ed25519 over the
// payload rebuilt for submitting host host and role DEVNET_TEST_ONLY) and
// returns the intent's public key. The caller then checks that key
// against the registry: CheckIntentRegistered in the signer,
// CheckPublication in serve.
func VerifyHostingIntent(msg sdk.Msg, host []byte) ([]byte, error) {
	signBytes, sig, err := IntentSignBytes(msg, host)
	if err != nil {
		return nil, err
	}
	if code := msgkit.CheckInnerSignature(sig, typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT); code != msgkit.OK {
		return nil, refuse(CodeIntentSignature, "hosting intent shape or domain: %s", code)
	}
	canonical, err := canonicalOf(typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT, signBytes)
	if err != nil {
		return nil, refuse(CodeIntentSignature, "%v", err)
	}
	if !msgkit.VerifyWithKey(sig.GetPublicKey(), typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT, canonical, sig.GetSignature()) {
		return nil, refuse(CodeIntentSignature, "hosting intent does not verify for this host, content version and role")
	}
	return bytes.Clone(sig.GetPublicKey()), nil
}

// CheckIntentRegistered is the signer's hosting-intent check (plan 7.2):
// the intent verifies for the signer's own address as submitting host,
// and its public key is registered for some principal.
func CheckIntentRegistered(msg sdk.Msg, host []byte, reg *Registry) error {
	if reg == nil {
		return refuse(CodeIntentKeyUnregistered, "no intent-key registry loaded")
	}
	pub, err := VerifyHostingIntent(msg, host)
	if err != nil {
		return err
	}
	if !reg.Contains(pub) {
		return refuse(CodeIntentKeyUnregistered, "hosting intent key is not in the operator's registry")
	}
	return nil
}
