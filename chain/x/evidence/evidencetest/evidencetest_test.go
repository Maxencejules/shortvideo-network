package evidencetest_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence/evidencetest"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Minimal unit-test fakes: an in-memory lease surface and read surfaces
// that answer nothing. The self-tests build a store with InitGenesis and
// then break one invariant at a time. TEST-ONLY keys derived from labels.

type surfaces struct{ leases map[string]*leasesv1.Lease }

func (s *surfaces) Role(sdk.Context, []byte) typesv1.SubmitterRole    { return 0 }
func (s *surfaces) GovernedLimit(sdk.Context, string) (uint64, bool)  { return 0, false }
func (s *surfaces) CheckNotWithdrawn(sdk.Context, string) msgkit.Code { return msgkit.OK }
func (s *surfaces) Lease(_ sdk.Context, id string) (*leasesv1.Lease, bool) {
	l, ok := s.leases[id]
	if !ok {
		return nil, false
	}
	return proto.Clone(l).(*leasesv1.Lease), true
}

func pubKey(label string) []byte {
	seed := sha256.Sum256([]byte("svn-evidence-test-seed:" + label))
	return []byte(ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey))
}

func digest(label string) *typesv1.Digest {
	sum := sha256.Sum256([]byte(label))
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sum[:]}
}

const (
	now      = msgkittest.BlockUnixSeconds + 600
	deadline = msgkittest.BlockUnixSeconds + 30*86400
)

var epoch = evidence.EpochAt(now)

func record(label, id, batchID, leaseID, contentID string, state evidencev1.EvidenceState, observation *typesv1.Digest) *evidencev1.EvidenceRecord {
	prefix := evidence.IDPrefix(evidence.AuditorAddress(pubKey(label)))
	return &evidencev1.EvidenceRecord{
		EvidenceId: prefix + id, BatchId: prefix + batchID, Kind: evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION,
		State: state, SignerPubkey: pubKey(label), LeaseId: leaseID, ContentId: contentID, ContentVersion: 1,
		ObservationDigest: observation, Epoch: epoch, ExpiryUnixSeconds: now + 3600, SchemaVersion: 1, Attestation: true,
	}
}

type fixture struct {
	ctx     sdk.Context
	k       evidence.Keeper
	s       *surfaces
	records []*evidencev1.EvidenceRecord
}

func (f *fixture) store() storetypes.KVStore { return f.ctx.KVStore(f.k.StoreKey()) }

func (f *fixture) check() error { return evidencetest.CheckInvariants(f.ctx, f.k, f.s) }

func (f *fixture) put(r *evidencev1.EvidenceRecord) {
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(r)
	if err != nil {
		panic(err)
	}
	f.store().Set(evidence.RecordKey(r.GetEvidenceId()), raw)
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, evidence.ModuleName)
	ctx = ctx.WithBlockTime(time.Unix(now, 0).UTC()).WithGasMeter(storetypes.NewInfiniteGasMeter())
	s := &surfaces{leases: map[string]*leasesv1.Lease{}}
	for _, id := range []string{"1", "2"} {
		s.leases["l"+id] = &leasesv1.Lease{
			LeaseId: "l" + id, ContentId: "c" + id, ContentVersion: 1, ManifestDigest: digest("manifest:c" + id),
			DeadlineUnixSeconds: deadline, State: leasesv1.LeaseState_LEASE_STATE_ACTIVE, ReservationHeld: true,
		}
	}
	k := evidence.NewKeeper(keys[evidence.ModuleName], evidence.Deps{Roles: s, Limits: s, Leases: s, Content: s})
	records := []*evidencev1.EvidenceRecord{
		record("auditor-1", "e1", "b1", "l1", "c1", evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED, digest("manifest:c1")),
		record("auditor-1", "e2", "b1", "l2", "c2", evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED, digest("other")),
		record("auditor-2", "e3", "b2", "l1", "c1", evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED, digest("manifest:c1")),
	}
	if err := k.InitGenesis(ctx, &evidencev1.GenesisState{Records: records}); err != nil {
		t.Fatal(err)
	}
	f := &fixture{ctx: ctx, k: k, s: s, records: records}
	if err := f.check(); err != nil {
		t.Fatalf("valid store: %v", err)
	}
	return f
}

func edited(r *evidencev1.EvidenceRecord, edit func(*evidencev1.EvidenceRecord)) *evidencev1.EvidenceRecord {
	c := proto.Clone(r).(*evidencev1.EvidenceRecord)
	edit(c)
	return c
}

// TestCheckInvariantsDetectsEachViolation breaks each checked invariant
// once and expects CheckInvariants to name it.
func TestCheckInvariantsDetectsEachViolation(t *testing.T) {
	window1 := evidence.AuditorWindowKey(evidence.AuditorAddress(pubKey("auditor-1")))
	cases := []struct {
		name    string
		want    string
		corrupt func(f *fixture)
	}{
		{"no-layout-key", "layout:", func(f *fixture) { f.store().Delete(msgkit.LayoutVersionKey) }},
		{"layout-version-1", "layout:", func(f *fixture) { msgkit.WriteLayoutVersion(f.store(), 1) }},
		{"unknown-prefix", "layout:", func(f *fixture) { f.store().Set([]byte{0x09, 'x'}, []byte{1}) }},
		{"business-row-31-bytes", "layout:", func(f *fixture) { f.store().Set([]byte{evidence.BusinessPrefix, 'b'}, make([]byte, 31)) }},
		{"business-row-off-pattern-id", "layout:", func(f *fixture) { f.store().Set([]byte{evidence.BusinessPrefix, 'B'}, make([]byte, 32)) }},
		{"record-does-not-decode", "I7:", func(f *fixture) { f.store().Set(evidence.RecordKey(f.records[0].GetEvidenceId()), []byte{0xff, 0xff}) }},
		{"record-under-another-id", "I7:", func(f *fixture) {
			raw := f.store().Get(evidence.RecordKey(f.records[0].GetEvidenceId()))
			f.store().Set(evidence.RecordKey(f.records[0].GetEvidenceId()+"x"), raw)
		}},
		{"attestation-false", "I7:", func(f *fixture) {
			f.put(edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.Attestation = false }))
		}},
		{"schema-version-2", "I7:", func(f *fixture) {
			f.put(edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.SchemaVersion = 2 }))
		}},
		{"kind-provider-receipt", "I7:", func(f *fixture) { f.put(edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.Kind = 1 })) }},
		{"signer-pubkey-31", "I7:", func(f *fixture) {
			f.put(edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.SignerPubkey = r.SignerPubkey[:31] }))
		}},
		{"digest-31-bytes", "I7:", func(f *fixture) {
			f.put(edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.ObservationDigest.Value = r.ObservationDigest.Value[:31] }))
		}},
		{"content-version-0", "I7:", func(f *fixture) {
			f.put(edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.ContentVersion = 0 }))
		}},
		{"expiry-0", "I7:", func(f *fixture) {
			f.put(edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.ExpiryUnixSeconds = 0 }))
		}},
		{"expiry-above-9999", "I7:", func(f *fixture) {
			f.put(edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.ExpiryUnixSeconds = genesis.UnixSecondsMax + 1 }))
		}},
		{"batch-id-pattern", "I7:", func(f *fixture) { f.put(edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.BatchId = "B" })) }},
		{"state-unspecified", "I4:", func(f *fixture) { f.put(edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.State = 0 })) }},
		{"state-unknown", "I4:", func(f *fixture) { f.put(edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.State = 9 })) }},
		{"evidence-id-without-prefix", "I12:", func(f *fixture) {
			f.put(edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.EvidenceId = "plain" }))
			f.store().Set(evidence.LeaseIndexKey("l1", "plain"), evidence.Marker)
		}},
		{"batch-id-of-another-auditor", "I12:", func(f *fixture) {
			f.put(edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.BatchId = f.records[2].GetBatchId() }))
		}},
		{"lease-index-row-missing", "I1:", func(f *fixture) {
			f.store().Delete(evidence.LeaseIndexKey("l1", f.records[0].GetEvidenceId()))
		}},
		{"lease-index-row-without-record", "I1:", func(f *fixture) {
			f.store().Set(evidence.LeaseIndexKey("l1", f.records[0].GetEvidenceId()+"ghost"), evidence.Marker)
		}},
		{"lease-index-row-other-lease", "I1:", func(f *fixture) {
			f.store().Set(evidence.LeaseIndexKey("l2", f.records[0].GetEvidenceId()), evidence.Marker)
		}},
		{"lease-index-row-value", "I1:", func(f *fixture) {
			f.store().Set(evidence.LeaseIndexKey("l1", f.records[0].GetEvidenceId()), []byte{2})
		}},
		{"lease-index-row-without-separator", "I1:", func(f *fixture) {
			f.store().Set(append([]byte{evidence.LeaseIndexPrefix}, "l1"...), evidence.Marker)
		}},
		{"batch-marker-missing", "I2:", func(f *fixture) { f.store().Delete(evidence.BatchKey(f.records[2].GetBatchId())) }},
		{"batch-marker-without-record", "I2:", func(f *fixture) {
			f.store().Set(evidence.BatchKey(f.records[2].GetBatchId()+"x"), evidence.Marker)
		}},
		{"batch-marker-value", "I2:", func(f *fixture) { f.store().Set(evidence.BatchKey(f.records[2].GetBatchId()), []byte{2}) }},
		{"batch-mixes-epochs", "I2:", func(f *fixture) {
			f.put(edited(f.records[1], func(r *evidencev1.EvidenceRecord) { r.Epoch-- }))
		}},
		{"batch-above-32-records", "I2:", func(f *fixture) {
			// 31 more records of auditor-1's batch b1 (2 already): 33, each
			// with its lease index row, so I1 holds and I2 decides.
			for i := 0; i < 31; i++ {
				r := edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.EvidenceId += fmt.Sprintf("-x%02d", i) })
				f.put(r)
				f.store().Set(evidence.LeaseIndexKey(r.GetLeaseId(), r.GetEvidenceId()), evidence.Marker)
			}
		}},
		{"batch-mixes-signer-keys", "I2:", func(f *fixture) {
			// Two keys whose addresses share their id prefix: both records
			// carry the prefix of their own key (I12 holds), so only the
			// batch rule refuses them.
			a, b := evidencetest.PrefixCollisionKeys()
			p := evidence.IDPrefix(evidence.AuditorAddress(a))
			for i, key := range [][]byte{a, b} {
				r := edited(f.records[0], func(r *evidencev1.EvidenceRecord) {
					r.EvidenceId, r.BatchId, r.SignerPubkey = p+fmt.Sprintf("m%d", i), p+"mixed", key
				})
				f.put(r)
				f.store().Set(evidence.LeaseIndexKey(r.GetLeaseId(), r.GetEvidenceId()), evidence.Marker)
			}
			f.store().Set(evidence.BatchKey(p+"mixed"), evidence.Marker)
		}},
		{"window-missing", "I8:", func(f *fixture) { f.store().Delete(window1) }},
		{"window-count", "I8:", func(f *fixture) { f.store().Set(window1, evidence.EncodeWindow(epoch, 3)) }},
		{"window-epoch", "I8:", func(f *fixture) { f.store().Set(window1, evidence.EncodeWindow(epoch+1, 2)) }},
		{"window-count-0", "I8:", func(f *fixture) { f.store().Set(window1, evidence.EncodeWindow(epoch, 0)) }},
		{"window-15-bytes", "I8:", func(f *fixture) { f.store().Set(window1, make([]byte, 15)) }},
		{"window-count-above-cap", "I8:", func(f *fixture) {
			// 1023 more records of auditor-1 in the epoch (2 already), in
			// batches of at most 32 with markers and index rows, and a window
			// that counts them: only the cap clause refuses the window.
			p := evidence.IDPrefix(evidence.AuditorAddress(pubKey("auditor-1")))
			for i := 0; i < int(evidence.MaxEvidencePerAuditorPerEpoch)-1; i++ {
				r := edited(f.records[0], func(r *evidencev1.EvidenceRecord) {
					r.EvidenceId, r.BatchId = p+fmt.Sprintf("cap%04d", i), p+fmt.Sprintf("capb%02d", i/32)
				})
				f.put(r)
				f.store().Set(evidence.LeaseIndexKey(r.GetLeaseId(), r.GetEvidenceId()), evidence.Marker)
				f.store().Set(evidence.BatchKey(r.GetBatchId()), evidence.Marker)
			}
			f.store().Set(window1, evidence.EncodeWindow(epoch, evidence.MaxEvidencePerAuditorPerEpoch+1))
		}},
		{"window-without-records", "I8:", func(f *fixture) {
			f.store().Set(evidence.AuditorWindowKey(evidence.AuditorAddress(pubKey("auditor-3"))), evidence.EncodeWindow(epoch, 1))
		}},
		{"recorded-with-other-digest", "I5:", func(f *fixture) {
			f.put(edited(f.records[1], func(r *evidencev1.EvidenceRecord) { r.State = evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED }))
		}},
		{"rejected-with-manifest-digest", "I5:", func(f *fixture) {
			f.put(edited(f.records[0], func(r *evidencev1.EvidenceRecord) { r.State = evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED }))
		}},
		{"lease-absent", "I6:", func(f *fixture) { delete(f.s.leases, "l2") }},
		{"lease-content-differs", "I6:", func(f *fixture) { f.s.leases["l2"].ContentId = "c9" }},
		{"lease-content-version-differs", "I6:", func(f *fixture) { f.s.leases["l2"].ContentVersion = 2 }},
		{"lease-deadline-before-expiry", "I13:", func(f *fixture) { f.s.leases["l1"].DeadlineUnixSeconds = now }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.corrupt(f)
			err := f.check()
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("error %v, want prefix %q", err, tc.want)
			}
		})
	}
	t.Run("nil-lease-surface", func(t *testing.T) {
		f := newFixture(t)
		if evidencetest.CheckInvariants(f.ctx, f.k, nil) == nil {
			t.Fatal("nil lease surface accepted")
		}
	})
}

// TestPrefixCollisionKeys: the two synthetic keys are distinct 32-byte
// values whose addresses differ and share their id prefix; every call
// returns fresh copies.
func TestPrefixCollisionKeys(t *testing.T) {
	a, b := evidencetest.PrefixCollisionKeys()
	if len(a) != msgkit.PublicKeyBytes || len(b) != msgkit.PublicKeyBytes || bytes.Equal(a, b) {
		t.Fatalf("keys %x %x", a, b)
	}
	sa, sb := sha256.Sum256(a), sha256.Sum256(b)
	addrA, addrB := evidence.AuditorAddress(a), evidence.AuditorAddress(b)
	if !bytes.Equal(sa[:evidence.IDPrefixBytes], sb[:evidence.IDPrefixBytes]) || bytes.Equal(addrA, addrB) {
		t.Fatalf("addresses %x %x", addrA, addrB)
	}
	if p := evidence.IDPrefix(addrA); p != evidence.IDPrefix(addrB) || p != "5a48c07a2b7ecf90." {
		t.Fatalf("prefixes %q %q", p, evidence.IDPrefix(addrB))
	}
	a[0] ^= 1
	if again, _ := evidencetest.PrefixCollisionKeys(); bytes.Equal(a, again) {
		t.Fatal("PrefixCollisionKeys returned a shared slice")
	}
}

// TestStoreSnapshot: equal stores give equal bytes; a write, a delete, or a
// different store name changes them; a cached branch is read through.
func TestStoreSnapshot(t *testing.T) {
	f := newFixture(t)
	key := f.k.StoreKey()
	before := evidencetest.StoreSnapshot(f.ctx, key)
	if !bytes.Equal(before, evidencetest.StoreSnapshot(f.ctx, key)) {
		t.Fatal("snapshot is not stable")
	}
	cached, _ := f.ctx.CacheContext()
	cached.KVStore(key).Set([]byte{0x02, 'z'}, evidence.Marker)
	if bytes.Equal(before, evidencetest.StoreSnapshot(cached, key)) {
		t.Fatal("a write on a cached branch is not seen")
	}
	if !bytes.Equal(before, evidencetest.StoreSnapshot(f.ctx, key)) {
		t.Fatal("the cached write leaked")
	}
	f.store().Delete(msgkit.LayoutVersionKey)
	if bytes.Equal(before, evidencetest.StoreSnapshot(f.ctx, key)) {
		t.Fatal("a delete is not seen")
	}
	if !bytes.HasPrefix(evidencetest.StoreSnapshot(f.ctx, key)[8:], []byte(evidence.ModuleName)) {
		t.Fatal("snapshot does not start with the store name")
	}
}
