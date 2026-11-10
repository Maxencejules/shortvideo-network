// Package sign builds provider signatures and the provider's own
// transaction. The inner signature uses the chain's sign-bytes helpers.
// A service key is not an input.
package sign

import (
	"bytes"
	"crypto/ed25519"
	"fmt"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers/capability"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Terms is the offer the provider agrees to. The signed text is
// leases.AcceptSignBytes of these fields and the lease id.
type Terms struct {
	OfferID        string
	ContentID      string
	ContentVersion uint64
	ProviderID     string
	Manifest       [32]byte
	PackageBytes   uint64
	TermsDigest    [32]byte
	Start          int64
	Deadline       int64
	Expiry         int64
	TermVersion    uint64
	Scheduler      []byte
}

// Proto is the offer object the chain stores.
func (t Terms) Proto() *leasesv1.Offer {
	return &leasesv1.Offer{
		OfferId: t.OfferID, ContentId: t.ContentID, ContentVersion: t.ContentVersion,
		ProviderId: t.ProviderID, ManifestDigest: digest(t.Manifest), PackageBytes: t.PackageBytes,
		TermsDigest: digest(t.TermsDigest), StartUnixSeconds: t.Start, DeadlineUnixSeconds: t.Deadline,
		ExpiryUnixSeconds: t.Expiry, TermVersion: t.TermVersion, Scheduler: append([]byte(nil), t.Scheduler...),
	}
}

func digest(sum [32]byte) *typesv1.Digest {
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: append([]byte(nil), sum[:]...)}
}

// Canonical is the SVN-JCS-1 acceptance payload for these terms.
func Canonical(leaseID string, t Terms) (string, error) {
	raw, err := leases.AcceptSignBytes(t.Proto(), leaseID)
	if err != nil {
		return "", err
	}
	return payload(raw)
}

// Accept signs the exact terms with the provider key.
func Accept(priv ed25519.PrivateKey, keyID, leaseID string, t Terms) (*typesv1.InnerSignature, string, error) {
	raw, err := leases.AcceptSignBytes(t.Proto(), leaseID)
	if err != nil {
		return nil, "", err
	}
	canonical, err := payload(raw)
	if err != nil {
		return nil, "", err
	}
	sig, err := inner(priv, keyID, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, canonical)
	if err != nil {
		return nil, "", err
	}
	return sig, canonical, nil
}

// End signs a provider end for the lease's current term version.
func End(priv ed25519.PrivateKey, keyID, leaseID, providerID string, termVersion uint64) (*typesv1.InnerSignature, string, error) {
	raw, err := leases.EndSignBytes(leaseID, providerID, termVersion)
	if err != nil {
		return nil, "", err
	}
	canonical, err := payload(raw)
	if err != nil {
		return nil, "", err
	}
	return mustInner(priv, keyID, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END, canonical)
}

// Rotate signs a key rotation with the current key.
func Rotate(priv ed25519.PrivateKey, keyID, providerID, newKeyID string, newPub []byte, generation uint64) (*typesv1.InnerSignature, string, error) {
	raw, err := providers.RotateSignBytes(providerID, newKeyID, newPub, generation)
	if err != nil {
		return nil, "", err
	}
	canonical, err := payload(raw)
	if err != nil {
		return nil, "", err
	}
	return mustInner(priv, keyID, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE, canonical)
}

// Drain signs the drain payload with the current key.
func Drain(priv ed25519.PrivateKey, keyID, providerID string, generation uint64) (*typesv1.InnerSignature, string, error) {
	raw, err := providers.DrainSignBytes(providerID, generation)
	if err != nil {
		return nil, "", err
	}
	canonical, err := payload(raw)
	if err != nil {
		return nil, "", err
	}
	return mustInner(priv, keyID, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN, canonical)
}

// Receipt signs the transfer-receipt attestation. The field set is interim
// (DR-16-01). It is not a storage proof.
func Receipt(priv ed25519.PrivateKey, keyID string, fields map[string]any) (*typesv1.InnerSignature, string, error) {
	text, err := msgkit.Canonical(fields)
	if err != nil {
		return nil, "", err
	}
	return mustInner(priv, keyID, typesv1.SignatureDomain_SIGNATURE_DOMAIN_TRANSFER_RECEIPT, text)
}

// Inventory signs the inventory attestation. The field set is interim (DR-16-02).
func Inventory(priv ed25519.PrivateKey, keyID string, fields map[string]any) (*typesv1.InnerSignature, string, error) {
	text, err := msgkit.Canonical(fields)
	if err != nil {
		return nil, "", err
	}
	return mustInner(priv, keyID, typesv1.SignatureDomain_SIGNATURE_DOMAIN_INVENTORY, text)
}

// Capability signs a capability manifest with the provider key.
func Capability(priv ed25519.PrivateKey, m capability.Manifest) (capability.Result, *typesv1.InnerSignature, error) {
	res, err := capability.Encode(m)
	if err != nil {
		return capability.Result{}, nil, err
	}
	sig, err := capability.Sign(res, priv)
	if err != nil {
		return capability.Result{}, nil, err
	}
	return res, sig, nil
}

// Verify checks an inner signature against the provider's current public key.
func Verify(sig *typesv1.InnerSignature, domain typesv1.SignatureDomain, keyID string, pub []byte, canonical string) error {
	if code := msgkit.VerifyInner(sig, domain, keyID, pub, canonical); code != msgkit.OK {
		return fmt.Errorf("sign: %s", code)
	}
	return nil
}

func mustInner(priv ed25519.PrivateKey, keyID string, domain typesv1.SignatureDomain, canonical string) (*typesv1.InnerSignature, string, error) {
	sig, err := inner(priv, keyID, domain, canonical)
	if err != nil {
		return nil, "", err
	}
	return sig, canonical, nil
}

func payload(raw []byte) (string, error) {
	i := bytes.IndexByte(raw, 0)
	if i <= 0 || i == len(raw)-1 {
		return "", fmt.Errorf("sign: sign-bytes")
	}
	return string(raw[i+1:]), nil
}

func inner(priv ed25519.PrivateKey, keyID string, domain typesv1.SignatureDomain, canonical string) (*typesv1.InnerSignature, error) {
	if len(priv) != ed25519.PrivateKeySize || !msgkit.ValidID(keyID) {
		return nil, fmt.Errorf("sign: key")
	}
	return &typesv1.InnerSignature{
		Domain:                  domain,
		PublicKey:               append([]byte(nil), priv.Public().(ed25519.PublicKey)...),
		Signature:               ed25519.Sign(priv, msgkit.SignBytes(domain, canonical)),
		KeyId:                   keyID,
		CanonicalProfileVersion: msgkit.CanonicalProfileVersion,
	}, nil
}
