package request

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/config/strictjson"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// Decoded is one request converted for the broadcaster.
type Decoded struct {
	Kind policy.Kind
	// Msg is the chain message with signer and business_id empty (nil for
	// FORWARD_PROVIDER_TX). The broadcaster sets offer term_version 1 and
	// EndLease authority_end false; nothing else is filled in.
	Msg sdk.Msg
	// Raw is the provider-signed transaction of FORWARD_PROVIDER_TX,
	// exactly as received.
	Raw []byte
	// Object is the chain object the request acts on; for
	// FORWARD_PROVIDER_TX it is set after the transaction is decoded
	// (ForwardObject).
	Object store.ObjectRef
	// ProviderID and ContentID name the (provider, content) pair whose held
	// lease a renew or end acts on (no chain query returns a lease by id,
	// DR-08-08). They are optional for an accept and must then match the
	// open offer. A forward names the provider; its content id follows the
	// same rule as the message it carries.
	ProviderID string
	ContentID  string
}

// Stored returns the bytes the outbox stores as the intent's request and
// the request digest covers: the deterministic protobuf encoding of Msg
// (signer and business_id empty), or Raw for a forward.
func (d Decoded) Stored() ([]byte, error) {
	if d.Kind == policy.KindForwardProviderTx {
		if len(d.Raw) == 0 || uint64(len(d.Raw)) > genesis.MaxTxBytes {
			return nil, errors.New("request: forwarded transaction size")
		}
		return append([]byte(nil), d.Raw...), nil
	}
	m, ok := d.Msg.(proto.Message)
	if !ok {
		return nil, errors.New("request: message is not a protobuf message")
	}
	out, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		return nil, errors.New("request: message does not encode")
	}
	if len(out) == 0 || uint64(len(out)) > genesis.MaxTxBytes {
		return nil, errors.New("request: encoded message size")
	}
	return out, nil
}

// WithSigner returns a copy of Msg with signer as its signer, the message
// the policy checks see and the worker signs (after it adds the business
// id). It is nil for a forward.
func (d Decoded) WithSigner(signer []byte) sdk.Msg {
	s := append([]byte(nil), signer...)
	switch msg := d.Msg.(type) {
	case *contentv1.MsgRegisterContent:
		c := proto.CloneOf(msg)
		c.Signer = s
		return c
	case *contentv1.MsgWithdrawContent:
		c := proto.CloneOf(msg)
		c.Signer = s
		return c
	case *leasesv1.MsgOfferLease:
		c := proto.CloneOf(msg)
		c.Signer = s
		return c
	case *leasesv1.MsgAcceptLease:
		c := proto.CloneOf(msg)
		c.Signer = s
		return c
	case *leasesv1.MsgRenewLease:
		c := proto.CloneOf(msg)
		c.Signer = s
		return c
	case *leasesv1.MsgEndLease:
		c := proto.CloneOf(msg)
		c.Signer = s
		return c
	}
	return nil
}

// innerSignature is the JSON of an InnerSignature: a hosting intent or a
// provider's consent, carried as the signer produced it. The domain must be
// the one the kind requires.
type innerSignature struct {
	Domain                  string `json:"domain"`
	KeyID                   string `json:"key_id"`
	PublicKeyHex            string `json:"public_key_hex"`
	SignatureHex            string `json:"signature_hex"`
	CanonicalProfileVersion uint32 `json:"canonical_profile_version"`
}

func (s *innerSignature) proto(field string, want typesv1.SignatureDomain) (*typesv1.InnerSignature, error) {
	if s == nil {
		return nil, invalid(field, "missing")
	}
	domain, ok := typesv1.SignatureDomain_value[s.Domain]
	if !ok || typesv1.SignatureDomain(domain) != want {
		return nil, invalid(field+".domain", "not the domain this kind carries")
	}
	pub, err := hexField(field+".public_key_hex", s.PublicKeyHex, msgkit.PublicKeyBytes)
	if err != nil {
		return nil, err
	}
	sig, err := hexField(field+".signature_hex", s.SignatureHex, msgkit.SignatureBytes)
	if err != nil {
		return nil, err
	}
	out := &typesv1.InnerSignature{
		Domain: want, PublicKey: pub, Signature: sig, KeyId: s.KeyID, CanonicalProfileVersion: s.CanonicalProfileVersion,
	}
	if code := msgkit.CheckInnerSignature(out, want); code != msgkit.OK {
		return nil, invalid(field, "key id, sizes or profile version break the inner signature rules")
	}
	return out, nil
}

// hexField decodes lowercase hex of exactly n bytes.
func hexField(field, s string, n int) ([]byte, error) {
	if len(s) != 2*n {
		return nil, invalid(field, "not the expected number of hex digits")
	}
	b, err := hex.DecodeString(s)
	if err != nil || hex.EncodeToString(b) != s {
		return nil, invalid(field, "not lowercase hex")
	}
	return b, nil
}

func digestField(field, s string) (*typesv1.Digest, error) {
	v, err := hexField(field, s, msgkit.DigestBytes)
	if err != nil {
		return nil, err
	}
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: v}, nil
}

func idField(field, s string) error {
	if !msgkit.ValidID(s) {
		return invalid(field, "missing or outside the id pattern")
	}
	return nil
}

// idFields checks (field, id) pairs in order and returns the first failure.
func idFields(pairs ...string) error {
	for i := 0; i+1 < len(pairs); i += 2 {
		if err := idField(pairs[i], pairs[i+1]); err != nil {
			return err
		}
	}
	return nil
}

// optionalID allows an empty id.
func optionalID(field, s string) error {
	if s == "" {
		return nil
	}
	return idField(field, s)
}

func uintField(field string, v, lo, hi uint64) error {
	if v < lo || v > hi {
		return invalid(field, "outside the chain bounds")
	}
	return nil
}

func unixField(field string, v int64) error {
	if v < genesis.UnixSecondsMin || v > genesis.UnixSecondsMax {
		return invalid(field, "outside the chain's unix-seconds bounds")
	}
	return nil
}

// Bounds of the integer fields (protocol v0): a content version and a term
// version are SVN-JCS-1 safe integers; package_bytes is at most the chain's
// MaxPackageBytes. A renewal's term version is at least 2 (the lease's
// first term is 1).
var safeMax = uint64(genesis.SafeIntegerMax)

// The per-kind request objects. Field names are the chain message's,
// with digests, keys and signatures as lowercase hex.

type registerContentJSON struct {
	ContentID         string          `json:"content_id"`
	ContentVersion    uint64          `json:"content_version"`
	ManifestDigestHex string          `json:"manifest_digest_hex"`
	PackageBytes      uint64          `json:"package_bytes"`
	HostingIntent     *innerSignature `json:"hosting_intent"`
}

type withdrawContentJSON struct {
	ContentID       string          `json:"content_id"`
	ContentVersion  uint64          `json:"content_version"`
	ReasonDigestHex string          `json:"reason_digest_hex"`
	HostingIntent   *innerSignature `json:"hosting_intent"`
}

type offerLeaseJSON struct {
	OfferID             string `json:"offer_id"`
	ContentID           string `json:"content_id"`
	ContentVersion      uint64 `json:"content_version"`
	ProviderID          string `json:"provider_id"`
	ManifestDigestHex   string `json:"manifest_digest_hex"`
	PackageBytes        uint64 `json:"package_bytes"`
	TermsDigestHex      string `json:"terms_digest_hex"`
	StartUnixSeconds    int64  `json:"start_unix_seconds"`
	DeadlineUnixSeconds int64  `json:"deadline_unix_seconds"`
	ExpiryUnixSeconds   int64  `json:"expiry_unix_seconds"`
}

type relayAcceptJSON struct {
	OfferID            string          `json:"offer_id"`
	LeaseID            string          `json:"lease_id"`
	ProviderAcceptance *innerSignature `json:"provider_acceptance"`
	ProviderID         string          `json:"provider_id"`
	ContentID          string          `json:"content_id"`
}

// relayRenewJSON has no evidence_id: plan 5.1 relays renewals without one,
// so the field is refused as unknown.
type relayRenewJSON struct {
	LeaseID             string          `json:"lease_id"`
	TermVersion         uint64          `json:"term_version"`
	DeadlineUnixSeconds int64           `json:"deadline_unix_seconds"`
	ManifestDigestHex   string          `json:"manifest_digest_hex"`
	PackageBytes        uint64          `json:"package_bytes"`
	ProviderSignature   *innerSignature `json:"provider_signature"`
	ProviderID          string          `json:"provider_id"`
	ContentID           string          `json:"content_id"`
}

type relayEndJSON struct {
	LeaseID           string          `json:"lease_id"`
	ProviderSignature *innerSignature `json:"provider_signature"`
	ProviderID        string          `json:"provider_id"`
	ContentID         string          `json:"content_id"`
}

type forwardJSON struct {
	TxBase64   string `json:"tx_base64"`
	ProviderID string `json:"provider_id"`
	ContentID  string `json:"content_id"`
}

// DecodeRequest decodes the request object of kind and converts it.
func DecodeRequest(kind policy.Kind, raw json.RawMessage) (Decoded, error) {
	if _, err := LookupKind(string(kind)); err != nil {
		return Decoded{}, err
	}
	if uint64(len(raw)) > genesis.MaxTxBytes {
		return Decoded{}, malformed("request", "above the size bound")
	}
	switch kind {
	case policy.KindRegisterContent:
		var r registerContentJSON
		if err := decodeObject(raw, &r); err != nil {
			return Decoded{}, err
		}
		return r.decoded()
	case policy.KindWithdrawContent:
		var r withdrawContentJSON
		if err := decodeObject(raw, &r); err != nil {
			return Decoded{}, err
		}
		return r.decoded()
	case policy.KindOfferLease:
		var r offerLeaseJSON
		if err := decodeObject(raw, &r); err != nil {
			return Decoded{}, err
		}
		return r.decoded()
	case policy.KindRelayAcceptLease:
		var r relayAcceptJSON
		if err := decodeObject(raw, &r); err != nil {
			return Decoded{}, err
		}
		return r.decoded()
	case policy.KindRelayRenewLease:
		var r relayRenewJSON
		if err := decodeObject(raw, &r); err != nil {
			return Decoded{}, err
		}
		return r.decoded()
	case policy.KindRelayEndLease:
		var r relayEndJSON
		if err := decodeObject(raw, &r); err != nil {
			return Decoded{}, err
		}
		return r.decoded()
	case policy.KindForwardProviderTx:
		var r forwardJSON
		if err := decodeObject(raw, &r); err != nil {
			return Decoded{}, err
		}
		return r.decoded()
	}
	return Decoded{}, &Error{Class: UnknownKind, Field: "kind", Rule: "not a routed kind"}
}

func decodeObject(raw json.RawMessage, v any) error {
	if err := strictjson.Decode(raw, v); err != nil {
		return malformed("request", "not the request object of this kind")
	}
	return nil
}

func (r registerContentJSON) decoded() (Decoded, error) {
	if err := idField("request.content_id", r.ContentID); err != nil {
		return Decoded{}, err
	}
	if err := uintField("request.content_version", r.ContentVersion, 1, safeMax); err != nil {
		return Decoded{}, err
	}
	manifest, err := digestField("request.manifest_digest_hex", r.ManifestDigestHex)
	if err != nil {
		return Decoded{}, err
	}
	if err := uintField("request.package_bytes", r.PackageBytes, 1, genesis.MaxPackageBytes); err != nil {
		return Decoded{}, err
	}
	intent, err := r.HostingIntent.proto("request.hosting_intent", typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT)
	if err != nil {
		return Decoded{}, err
	}
	return Decoded{
		Kind: policy.KindRegisterContent,
		Msg: &contentv1.MsgRegisterContent{
			ContentId: r.ContentID, ContentVersion: r.ContentVersion, ManifestDigest: manifest,
			PackageBytes: r.PackageBytes, HostingIntent: intent,
		},
		Object: store.ObjectRef{Type: store.ObjectContent, ID: r.ContentID, Version: r.ContentVersion},
	}, nil
}

func (r withdrawContentJSON) decoded() (Decoded, error) {
	if err := idField("request.content_id", r.ContentID); err != nil {
		return Decoded{}, err
	}
	if err := uintField("request.content_version", r.ContentVersion, 1, safeMax); err != nil {
		return Decoded{}, err
	}
	reason, err := digestField("request.reason_digest_hex", r.ReasonDigestHex)
	if err != nil {
		return Decoded{}, err
	}
	intent, err := r.HostingIntent.proto("request.hosting_intent", typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT)
	if err != nil {
		return Decoded{}, err
	}
	return Decoded{
		Kind: policy.KindWithdrawContent,
		Msg: &contentv1.MsgWithdrawContent{
			ContentId: r.ContentID, ContentVersion: r.ContentVersion, ReasonDigest: reason, HostingIntent: intent,
		},
		Object: store.ObjectRef{Type: store.ObjectContent, ID: r.ContentID, Version: r.ContentVersion},
	}, nil
}

func (r offerLeaseJSON) decoded() (Decoded, error) {
	if err := idFields("request.offer_id", r.OfferID, "request.content_id", r.ContentID,
		"request.provider_id", r.ProviderID); err != nil {
		return Decoded{}, err
	}
	if err := uintField("request.content_version", r.ContentVersion, 1, safeMax); err != nil {
		return Decoded{}, err
	}
	manifest, err := digestField("request.manifest_digest_hex", r.ManifestDigestHex)
	if err != nil {
		return Decoded{}, err
	}
	terms, err := digestField("request.terms_digest_hex", r.TermsDigestHex)
	if err != nil {
		return Decoded{}, err
	}
	if err := uintField("request.package_bytes", r.PackageBytes, 1, genesis.MaxPackageBytes); err != nil {
		return Decoded{}, err
	}
	if err := unixField("request.start_unix_seconds", r.StartUnixSeconds); err != nil {
		return Decoded{}, err
	}
	if err := unixField("request.deadline_unix_seconds", r.DeadlineUnixSeconds); err != nil {
		return Decoded{}, err
	}
	if err := unixField("request.expiry_unix_seconds", r.ExpiryUnixSeconds); err != nil {
		return Decoded{}, err
	}
	if r.DeadlineUnixSeconds <= r.StartUnixSeconds {
		return Decoded{}, invalid("request.deadline_unix_seconds", "not after the start")
	}
	return Decoded{
		Kind: policy.KindOfferLease,
		Msg: &leasesv1.MsgOfferLease{
			OfferId: r.OfferID, ContentId: r.ContentID, ContentVersion: r.ContentVersion, ProviderId: r.ProviderID,
			ManifestDigest: manifest, PackageBytes: r.PackageBytes, TermsDigest: terms,
			StartUnixSeconds: r.StartUnixSeconds, DeadlineUnixSeconds: r.DeadlineUnixSeconds,
			ExpiryUnixSeconds: r.ExpiryUnixSeconds, TermVersion: 1,
		},
		Object: store.ObjectRef{Type: store.ObjectOffer, ID: r.OfferID},
	}, nil
}

func (r relayAcceptJSON) decoded() (Decoded, error) {
	if err := idField("request.offer_id", r.OfferID); err != nil {
		return Decoded{}, err
	}
	if err := idField("request.lease_id", r.LeaseID); err != nil {
		return Decoded{}, err
	}
	if err := optionalID("request.provider_id", r.ProviderID); err != nil {
		return Decoded{}, err
	}
	if err := optionalID("request.content_id", r.ContentID); err != nil {
		return Decoded{}, err
	}
	sig, err := r.ProviderAcceptance.proto("request.provider_acceptance", typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT)
	if err != nil {
		return Decoded{}, err
	}
	return Decoded{
		Kind:       policy.KindRelayAcceptLease,
		Msg:        &leasesv1.MsgAcceptLease{OfferId: r.OfferID, LeaseId: r.LeaseID, ProviderAcceptance: sig},
		Object:     store.ObjectRef{Type: store.ObjectOffer, ID: r.OfferID},
		ProviderID: r.ProviderID, ContentID: r.ContentID,
	}, nil
}

func (r relayRenewJSON) decoded() (Decoded, error) {
	if err := idFields("request.lease_id", r.LeaseID, "request.provider_id", r.ProviderID,
		"request.content_id", r.ContentID); err != nil {
		return Decoded{}, err
	}
	if err := uintField("request.term_version", r.TermVersion, 2, safeMax); err != nil {
		return Decoded{}, err
	}
	if err := unixField("request.deadline_unix_seconds", r.DeadlineUnixSeconds); err != nil {
		return Decoded{}, err
	}
	manifest, err := digestField("request.manifest_digest_hex", r.ManifestDigestHex)
	if err != nil {
		return Decoded{}, err
	}
	if err := uintField("request.package_bytes", r.PackageBytes, 1, genesis.MaxPackageBytes); err != nil {
		return Decoded{}, err
	}
	sig, err := r.ProviderSignature.proto("request.provider_signature", typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW)
	if err != nil {
		return Decoded{}, err
	}
	return Decoded{
		Kind: policy.KindRelayRenewLease,
		Msg: &leasesv1.MsgRenewLease{
			LeaseId: r.LeaseID, TermVersion: r.TermVersion, DeadlineUnixSeconds: r.DeadlineUnixSeconds,
			ManifestDigest: manifest, PackageBytes: r.PackageBytes, ProviderSignature: sig,
		},
		Object:     store.ObjectRef{Type: store.ObjectLease, ID: r.LeaseID},
		ProviderID: r.ProviderID, ContentID: r.ContentID,
	}, nil
}

func (r relayEndJSON) decoded() (Decoded, error) {
	if err := idFields("request.lease_id", r.LeaseID, "request.provider_id", r.ProviderID,
		"request.content_id", r.ContentID); err != nil {
		return Decoded{}, err
	}
	sig, err := r.ProviderSignature.proto("request.provider_signature", typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END)
	if err != nil {
		return Decoded{}, err
	}
	return Decoded{
		Kind:       policy.KindRelayEndLease,
		Msg:        &leasesv1.MsgEndLease{LeaseId: r.LeaseID, ProviderSignature: sig, AuthorityEnd: false},
		Object:     store.ObjectRef{Type: store.ObjectLease, ID: r.LeaseID},
		ProviderID: r.ProviderID, ContentID: r.ContentID,
	}, nil
}

func (r forwardJSON) decoded() (Decoded, error) {
	if err := idField("request.provider_id", r.ProviderID); err != nil {
		return Decoded{}, err
	}
	if err := optionalID("request.content_id", r.ContentID); err != nil {
		return Decoded{}, err
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(r.TxBase64)
	if err != nil || len(raw) == 0 || uint64(len(raw)) > genesis.MaxTxBytes ||
		base64.StdEncoding.EncodeToString(raw) != r.TxBase64 {
		return Decoded{}, invalid("request.tx_base64", "not padded standard base64 of 1 to 65,536 bytes")
	}
	return Decoded{Kind: policy.KindForwardProviderTx, Raw: raw, ProviderID: r.ProviderID, ContentID: r.ContentID}, nil
}

// ForwardObject returns the object and business id of a forwarded
// provider message (MsgAcceptLease acts on its offer, MsgRenewLease and
// MsgEndLease on their lease).
func ForwardObject(msg sdk.Msg) (store.ObjectRef, string, error) {
	switch m := msg.(type) {
	case *leasesv1.MsgAcceptLease:
		return store.ObjectRef{Type: store.ObjectOffer, ID: m.GetOfferId()}, m.GetBusinessId(), nil
	case *leasesv1.MsgRenewLease:
		return store.ObjectRef{Type: store.ObjectLease, ID: m.GetLeaseId()}, m.GetBusinessId(), nil
	case *leasesv1.MsgEndLease:
		return store.ObjectRef{Type: store.ObjectLease, ID: m.GetLeaseId()}, m.GetBusinessId(), nil
	}
	return store.ObjectRef{}, "", invalid("request.tx_base64", "the forwarded message is not a provider lease message")
}
