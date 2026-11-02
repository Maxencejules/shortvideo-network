package authority

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// FuzzProposalPayloadProto fuzzes the payload path the chain runs: proto
// bytes of a ProposalPayload, then PayloadDigest and CanonicalFromProto
// (ProposeChange calls the latter). Whenever PayloadDigest accepts a
// payload, CanonicalFromProto must accept it with that digest and give the
// matching kind; the canonical string must hash to the digest and be a fixed
// point of the SVN-JCS-1 parser; a second call must give identical output;
// and the same payload with one digest bit flipped, or a short digest, must
// be refused.
func FuzzProposalPayloadProto(f *testing.F) {
	for _, payload := range payloadProtoSeeds() {
		raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(payload)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 4096 {
			raw = raw[:4096]
		}
		var payload authorityv1.ProposalPayload
		if err := proto.Unmarshal(raw, &payload); err != nil {
			return
		}
		digest, out := PayloadDigest(&payload)
		if !out.Success() {
			if digest != nil {
				t.Fatalf("PayloadDigest refused with %s and returned a digest", out.Code)
			}
			return
		}
		if len(digest) != sha256.Size {
			t.Fatalf("digest has %d bytes", len(digest))
		}
		change, kind, canon, parsed := CanonicalFromProto(&payload, digest)
		if !parsed.Success() {
			t.Fatalf("PayloadDigest accepted the payload, CanonicalFromProto refused it with %s", parsed.Code)
		}
		if want := payloadKind(&payload); kind != want {
			t.Fatalf("kind %q for a %q payload", kind, want)
		}
		if sum := sha256.Sum256([]byte(canon)); !bytes.Equal(sum[:], digest) {
			t.Fatalf("canonical %q does not hash to the digest", canon)
		}
		again, err := jcs.ParseAndCanonicalize(canon)
		if err != nil || again != canon {
			t.Fatalf("canonical %q re-canonicalizes to %q (%v)", canon, again, err)
		}
		if body := payload.GetValidatorPower(); kind == kindValidatorPower && (change.NodeID != body.GetNodeId() ||
			!bytes.Equal(change.PubKey, body.GetConsensusPubkey()) || change.Power != body.GetPower()) {
			t.Fatalf("validator change %s %x %d does not carry the payload fields", change.NodeID, change.PubKey, change.Power)
		}
		digest2, out2 := PayloadDigest(&payload)
		change2, kind2, canon2, parsed2 := CanonicalFromProto(&payload, digest)
		if !out2.Success() || !bytes.Equal(digest2, digest) || !parsed2.Success() || kind2 != kind || canon2 != canon ||
			change2.NodeID != change.NodeID || !bytes.Equal(change2.PubKey, change.PubKey) || change2.Power != change.Power {
			t.Fatalf("a second call differs: %x %q %q", digest2, kind2, canon2)
		}
		flipped := bytes.Clone(digest)
		flipped[int(digest[0])%len(flipped)] ^= 1
		if _, _, _, bad := CanonicalFromProto(&payload, flipped); bad.Code != CodeIntegrity {
			t.Fatalf("a flipped digest bit got %+v", bad)
		}
		if _, _, _, bad := CanonicalFromProto(&payload, digest[:31]); bad.Code != CodeIntegrity {
			t.Fatalf("a 31-byte digest got %+v", bad)
		}
	})
}

// payloadKind is the stored kind of the oneof alternative a payload holds.
func payloadKind(payload *authorityv1.ProposalPayload) string {
	switch payload.GetChange().(type) {
	case *authorityv1.ProposalPayload_ValidatorPower:
		return kindValidatorPower
	case *authorityv1.ProposalPayload_GovernedLimit:
		return kindGovernedLimit
	case *authorityv1.ProposalPayload_AdmitProvider:
		return kindAdmitProvider
	case *authorityv1.ProposalPayload_DisableProvider:
		return kindDisableProvider
	case *authorityv1.ProposalPayload_ForceWithdraw:
		return kindForceWithdraw
	default:
		return ""
	}
}

// payloadProtoSeeds are valid payloads: one of each kind, plus a removal and
// a unix-time activation.
func payloadProtoSeeds() []*authorityv1.ProposalPayload {
	reason := &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: pub(7)}
	return []*authorityv1.ProposalPayload{
		powerMsg("v5", pub(9), 1, 20).GetPayload(),
		removalMsg("v1", pub(1), 20).GetPayload(),
		timePower("v6", pub(10), 2, testTime+60).GetPayload(),
		governedMsg("max_page_limit", 7).GetPayload(),
		{Change: &authorityv1.ProposalPayload_AdmitProvider{AdmitProvider: &authorityv1.AdmitProviderChange{ProviderId: "prov-1"}}},
		{Change: &authorityv1.ProposalPayload_DisableProvider{DisableProvider: &authorityv1.DisableProviderChange{ProviderId: "prov-1"}}},
		{Change: &authorityv1.ProposalPayload_ForceWithdraw{ForceWithdraw: &authorityv1.ForceWithdrawChange{
			ContentId: "content-1", ContentVersion: 1, ReasonDigest: reason,
		}}},
	}
}
