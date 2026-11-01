package providers

import (
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Inner signature payloads. The signed bytes are
// UTF-8(domain) || 0x00 || UTF-8(SVN-JCS-1 canonical JSON) (protocol.md
// "Signature domains"). Keys are lowerCamelCase; uint64 values are decimal
// strings; byte strings are lowercase hex. There is no chain id in these
// payloads (open question DR-07-01).
//
//	SVN-PROVIDER-ROTATE-1  {"expectedGeneration","newSigningKeyId","newSigningPubkey","providerId"}
//	SVN-PROVIDER-DRAIN-1   {"keyGeneration","providerId"}
//
// The rotate object is unchanged from goal 06. The drain object is a
// reversible goal-07 choice; keyGeneration is the stored generation of the
// signing key, so a drain signature names the key generation it was made
// with.

// rotateCanonical is the SVN-JCS-1 text of a rotation payload.
func rotateCanonical(providerID, newSigningKeyID string, newSigningPubkey []byte, expectedGeneration uint64) (string, error) {
	return msgkit.Canonical(map[string]any{
		"expectedGeneration": msgkit.Dec(expectedGeneration),
		"newSigningKeyId":    newSigningKeyID,
		"newSigningPubkey":   msgkit.Hex(newSigningPubkey),
		"providerId":         providerID,
	})
}

// drainCanonical is the SVN-JCS-1 text of a drain payload.
func drainCanonical(providerID string, keyGeneration uint64) (string, error) {
	return msgkit.Canonical(map[string]any{
		"keyGeneration": msgkit.Dec(keyGeneration),
		"providerId":    providerID,
	})
}

// RotateSignBytes returns the bytes the current provider key signs for
// MsgRotateProviderKey: the SVN-PROVIDER-ROTATE-1 domain, 0x00, and the
// canonical rotation payload. Provider agents and tests use it.
func RotateSignBytes(providerID, newSigningKeyID string, newSigningPubkey []byte, expectedGeneration uint64) ([]byte, error) {
	canonical, err := rotateCanonical(providerID, newSigningKeyID, newSigningPubkey, expectedGeneration)
	if err != nil {
		return nil, err
	}
	return msgkit.SignBytes(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE, canonical), nil
}

// DrainSignBytes returns the bytes the current provider key signs for
// MsgDrainProvider: the SVN-PROVIDER-DRAIN-1 domain, 0x00, and the canonical
// drain payload. keyGeneration is the provider's current key_generation.
func DrainSignBytes(providerID string, keyGeneration uint64) ([]byte, error) {
	canonical, err := drainCanonical(providerID, keyGeneration)
	if err != nil {
		return nil, err
	}
	return msgkit.SignBytes(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN, canonical), nil
}
