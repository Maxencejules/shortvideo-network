package difftest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Step op names: the spec op of a transaction, or a fixture name.
const (
	opPropose          = "ProposeChange"
	opApprove          = "ApproveChange"
	opExecute          = "ExecuteChange"
	opRegisterProvider = "RegisterProvider"
	opRegisterContent  = "RegisterContent"
	fxBlock            = "Block"
)

// unixMax is the largest protocol time (bounds.json unix_seconds_max).
const unixMax int64 = 253402300799

// Key ids name exactly one key: the key is derived from the id. No private
// key is written anywhere; nothing here is a real key.
const intentKeyID = "intent-k1"

func keyFor(keyID string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("svn-authority-difftest-key:" + keyID))
	return ed25519.NewKeyFromSeed(seed[:])
}

func pubFor(keyID string) []byte { return []byte(keyFor(keyID).Public().(ed25519.PublicKey)) }

func sha(label string) []byte {
	sum := sha256.Sum256([]byte(label))
	return sum[:]
}

func hexOf(b []byte) string { return hex.EncodeToString(b) }

func digest(value []byte) *typesv1.Digest {
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: bytes.Clone(value)}
}

func manifestFor(contentID string) []byte  { return sha("manifest:" + contentID) }
func endpointFor(providerID string) []byte { return sha("endpoint:" + providerID) }

// step is one generated step: a transaction message with the spec dict the
// oracle receives, or a Block fixture.
type step struct {
	op string
	// tag labels the generator variant for coverage counts only; it never
	// decides an expected outcome.
	tag        string
	msg        proto.Message
	spec       map[string]any
	headerTime int64
}

// council reports whether the step is a council transaction.
func (s *step) council() bool {
	switch s.op {
	case opPropose, opApprove, opExecute:
		return true
	}
	return false
}

// payload is one proposal payload: the protobuf message the chain receives
// and its proto3 JSON form, built here field by field without the chain's
// canonicalizer. A zero integer and an empty string are proto3 defaults and
// are omitted, as proto3 JSON omits them; validator power 0 (a removal) is
// the only zero the generator sends (G-2).
type payload struct {
	msg  *authorityv1.ProposalPayload
	json map[string]any
	key  string // the parameter key the payload binds
	kind string // a coverage label
}

// digestJSON is the proto3 JSON of a Digest; the default algorithm and an
// empty value are omitted.
func digestJSON(d *typesv1.Digest) map[string]any {
	out := map[string]any{}
	if d.GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED {
		out["algorithm"] = d.GetAlgorithm().String()
	}
	if len(d.GetValue()) > 0 {
		out["value"] = base64.StdEncoding.EncodeToString(d.GetValue())
	}
	return out
}

func governedPayload(key string, value uint64) payload {
	body := map[string]any{}
	if key != "" {
		body["limitKey"] = key
	}
	if value != 0 {
		body["value"] = strconv.FormatUint(value, 10)
	}
	return payload{
		msg: &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_GovernedLimit{
			GovernedLimit: &authorityv1.GovernedLimitChange{LimitKey: key, Value: value},
		}},
		json: map[string]any{"governedLimit": body},
		key:  key,
		kind: "governed",
	}
}

// activation is a validator activation point: a height, or a unix time.
type activation struct {
	byHeight bool
	height   uint64
	unix     int64
}

func atHeight(h uint64) activation { return activation{byHeight: true, height: h} }
func atTime(t int64) activation    { return activation{unix: t} }

func (a activation) proto() *authorityv1.ActivationPoint {
	if a.byHeight {
		return &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: a.height}}
	}
	return &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_UnixSeconds{UnixSeconds: a.unix}}
}

// json is the proto3 JSON of the activation oneof: a set oneof member is
// always present, also when it is 0.
func (a activation) json() map[string]any {
	if a.byHeight {
		return map[string]any{"height": strconv.FormatUint(a.height, 10)}
	}
	return map[string]any{"unixSeconds": strconv.FormatInt(a.unix, 10)}
}

func validatorPayload(node string, pub []byte, power int64, reason *typesv1.Digest, at activation) payload {
	body := map[string]any{"activation": at.json()}
	if len(pub) > 0 {
		body["consensusPubkey"] = base64.StdEncoding.EncodeToString(pub)
	}
	if node != "" {
		body["nodeId"] = node
	}
	if reason != nil {
		body["reasonDigest"] = digestJSON(reason)
	}
	if power != 0 {
		body["power"] = strconv.FormatInt(power, 10)
	}
	return payload{
		msg: &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ValidatorPower{
			ValidatorPower: &authorityv1.ValidatorPowerChange{
				ConsensusPubkey: bytes.Clone(pub), Power: power, NodeId: node,
				ReasonDigest: reason, Activation: at.proto(),
			},
		}},
		json: map[string]any{"validatorPower": body},
		key:  "validator-" + node,
		kind: "validator",
	}
}

// idBody is the proto3 JSON body of a one-id payload; an empty id is omitted.
func idBody(field, id string) map[string]any {
	if id == "" {
		return map[string]any{}
	}
	return map[string]any{field: id}
}

func admitPayload(providerID string) payload {
	return payload{
		msg: &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_AdmitProvider{
			AdmitProvider: &authorityv1.AdmitProviderChange{ProviderId: providerID},
		}},
		json: map[string]any{"admitProvider": idBody("providerId", providerID)},
		key:  "admit-" + providerID,
		kind: "admit",
	}
}

func disablePayload(providerID string) payload {
	return payload{
		msg: &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_DisableProvider{
			DisableProvider: &authorityv1.DisableProviderChange{ProviderId: providerID},
		}},
		json: map[string]any{"disableProvider": idBody("providerId", providerID)},
		key:  "disable-" + providerID,
		kind: "disable",
	}
}

func withdrawPayload(contentID string, version uint64) payload {
	return withdrawWith(contentID, version, digest(sha("reason:"+contentID)))
}

// withdrawWith is a force-withdraw payload with any reason digest (nil:
// absent); an empty id and version 0 are omitted from the JSON.
func withdrawWith(contentID string, version uint64, reason *typesv1.Digest) payload {
	body := idBody("contentId", contentID)
	if version != 0 {
		body["contentVersion"] = strconv.FormatUint(version, 10)
	}
	if reason != nil {
		body["reasonDigest"] = digestJSON(reason)
	}
	return payload{
		msg: &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ForceWithdraw{
			ForceWithdraw: &authorityv1.ForceWithdrawChange{ContentId: contentID, ContentVersion: version, ReasonDigest: reason},
		}},
		json: map[string]any{"forceWithdraw": body},
		key:  "withdraw-" + contentID,
		kind: "withdraw",
	}
}

// payloadDigest is SHA-256 of the SVN-JCS-1 text of the payload's proto3
// JSON (chain/jcs, the goal-02 canonicalizer port), computed without the
// x/authority payload code, so a canonicalization difference there shows
// up as a divergence (the chain answers INTEGRITY, the spec does not).
func payloadDigest(p payload) []byte {
	canon, err := jcs.Canonicalize(p.json)
	if err != nil {
		panic(fmt.Sprintf("difftest: canonicalize payload: %v", err))
	}
	return sha(canon)
}

// proposeParams is one propose message.
type proposeParams struct {
	signer       []byte
	business     string
	proposalID   string
	parameterKey string
	base         uint64
	expiry       int64
	payload      payload
	// digest overrides the payload digest value (nil: payloadDigest).
	digest []byte
	// wrongAlgorithm sends the digest with DIGEST_ALGORITHM_UNSPECIFIED
	// instead of SHA-256 (G-1).
	wrongAlgorithm bool
}

func buildPropose(p proposeParams, tag string) *step {
	value := p.digest
	if value == nil {
		value = payloadDigest(p.payload)
	}
	algorithm := typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256
	if p.wrongAlgorithm {
		algorithm = typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED
	}
	key := p.parameterKey
	if key == "" {
		key = p.payload.key
	}
	msg := &authorityv1.MsgProposeChange{
		Signer: bytes.Clone(p.signer), BusinessId: p.business, ProposalId: p.proposalID, ParameterKey: key,
		BasePolicyVersion: p.base, Payload: p.payload.msg,
		PayloadDigest:     &typesv1.Digest{Algorithm: algorithm, Value: bytes.Clone(value)},
		ExpiryUnixSeconds: p.expiry,
	}
	spec := map[string]any{
		"op": opPropose, "signer": hexOf(p.signer), "business_id": p.business, "proposal_id": p.proposalID,
		"parameter_key": key, "base_policy_version": p.base, "payload": p.payload.json,
		"payload_digest": hexOf(value), "expiry_unix_seconds": p.expiry,
	}
	if algorithm != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 {
		spec["digest_algorithm"] = algorithm.String()
	}
	return &step{op: opPropose, tag: tag, msg: msg, spec: spec}
}

func buildApprove(signer []byte, business, proposalID string) *step {
	return &step{op: opApprove, msg: &authorityv1.MsgApproveChange{
		Signer: bytes.Clone(signer), BusinessId: business, ProposalId: proposalID,
	}, spec: map[string]any{
		"op": opApprove, "signer": hexOf(signer), "business_id": business, "proposal_id": proposalID,
	}}
}

func buildExecute(signer []byte, business, proposalID string) *step {
	return &step{op: opExecute, msg: &authorityv1.MsgExecuteChange{
		Signer: bytes.Clone(signer), BusinessId: business, ProposalId: proposalID,
	}, spec: map[string]any{
		"op": opExecute, "signer": hexOf(signer), "business_id": business, "proposal_id": proposalID,
	}}
}

func blockStep(headerTime int64) *step {
	return &step{op: fxBlock, headerTime: headerTime, spec: map[string]any{"op": fxBlock, "header_time": headerTime}}
}

func buildRegisterProvider(signer []byte, business, providerID string) *step {
	keyID := providerID + "-k1"
	msg := &providersv1.MsgRegisterProvider{
		Signer: bytes.Clone(signer), BusinessId: business, ProviderId: providerID, SigningPubkey: pubFor(keyID),
		SigningKeyId: keyID, EndpointManifestDigest: digest(endpointFor(providerID)),
		DeclaredCapacityBytes: 1_000_000, OperatorGroupId: providers.DevnetOperatorGroup,
	}
	return &step{op: opRegisterProvider, msg: msg, spec: map[string]any{
		"op": opRegisterProvider, "signer": hexOf(signer), "business_id": business, "provider_id": providerID,
		"signing_key_id": keyID, "signing_pubkey": hexOf(pubFor(keyID)),
		"endpoint_manifest_digest": hexOf(endpointFor(providerID)), "declared_capacity_bytes": 1_000_000,
		"operator_group_id": providers.DevnetOperatorGroup,
	}}
}

// buildRegisterContent registers version 1 of contentID with a valid
// hosting intent by the intent key (DEC-001 devnet role).
func buildRegisterContent(signer []byte, business, contentID string, size uint64) *step {
	manifest := manifestFor(contentID)
	signBytes, err := content.RegisterIntentSignBytes(signer, contentID, 1, manifest, size, content.DevnetRoleName)
	if err != nil {
		panic(fmt.Sprintf("difftest: register intent: %v", err))
	}
	domain := typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT
	domainName, ok := msgkit.DomainString(domain)
	if !ok {
		panic("difftest: hosting intent domain has no name")
	}
	msg := &contentv1.MsgRegisterContent{
		Signer: bytes.Clone(signer), BusinessId: business, ContentId: contentID, ContentVersion: 1, PackageBytes: size,
		ManifestDigest: digest(manifest),
		HostingIntent: &typesv1.InnerSignature{
			Domain: domain, PublicKey: pubFor(intentKeyID), Signature: ed25519.Sign(keyFor(intentKeyID), signBytes),
			KeyId: intentKeyID, CanonicalProfileVersion: 1,
		},
	}
	return &step{op: opRegisterContent, msg: msg, spec: map[string]any{
		"op": opRegisterContent, "signer": hexOf(signer), "business_id": business, "content_id": contentID,
		"content_version": 1, "package_bytes": size, "manifest_digest": hexOf(manifest),
		"hosting_intent": map[string]any{
			"domain": domainName, "key_id": intentKeyID, "valid": true, "role_name": content.DevnetRoleName,
		},
	}}
}

// seedRecord is a provider written by providers.InitGenesis before the
// first step, in state (ADMITTED or DRAINING), mirrored to the oracle as a
// SeedProvider fixture.
func seedRecord(providerID string, acct []byte, state providersv1.ProviderState) *providersv1.Provider {
	keyID := providerID + "-k1"
	return &providersv1.Provider{
		ProviderId: providerID, Account: bytes.Clone(acct), SigningPubkey: pubFor(keyID), SigningKeyId: keyID,
		KeyGeneration: 1, EndpointManifestDigest: digest(endpointFor(providerID)), State: state,
		AdmittedCapacityBytes: 1_000_000, OperatorGroupId: providers.DevnetOperatorGroup,
	}
}

func seedSpec(r *providersv1.Provider) map[string]any {
	state := r.GetState().String()[len("PROVIDER_STATE_"):]
	return map[string]any{
		"op": "SeedProvider", "provider_id": r.GetProviderId(), "account": hexOf(r.GetAccount()),
		"signing_key_id": r.GetSigningKeyId(), "signing_pubkey": hexOf(r.GetSigningPubkey()),
		"endpoint_manifest_digest": hexOf(r.GetEndpointManifestDigest().GetValue()), "state": state,
		"admitted_capacity_bytes": r.GetAdmittedCapacityBytes(), "reserved_bytes": r.GetReservedBytes(),
		"operator_group_id": r.GetOperatorGroupId(),
	}
}

// seatAddress is the account of a council key (authority.SeatAddress).
func seatAddress(pub []byte) []byte {
	addr := authority.SeatAddress(pub)
	if len(addr) != msgkit.AccountBytes {
		panic("difftest: seat key is not 32 bytes")
	}
	return addr
}
