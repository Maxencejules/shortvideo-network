package authority

import (
	"crypto/sha256"
	"encoding/binary"
	"strconv"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
	"google.golang.org/protobuf/proto"

	"github.com/cosmos/cosmos-sdk/store/v2/dbadapter"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Fixed test point. Synthetic values.
const (
	testChainID       = "shortvideo-dev-unit"
	testHeight  int64 = 5
	testTime    int64 = 1_700_000_000
	testExpiry  int64 = 1_800_000_000
)

// testSeat is one devnet council seat: its id, its ed25519 public key and the
// 20-byte account derived from that key.
type testSeat struct {
	ID     string
	Pub    []byte
	Signer []byte
}

// newMemKV is an empty in-memory store with no gas and no cache.
func newMemKV() storetypes.KVStore {
	return dbadapter.Store{DB: dbm.NewMemDB()}
}

// dumpKV is the whole store in key order: for each key, its length (8 bytes,
// big-endian), the key, the value length (8 bytes, big-endian), the value.
// Iterator.Error is not called: the cachekv merge iterator reports an
// exhausted range as an error.
func dumpKV(kv KV) []byte {
	var out []byte
	iter := kv.Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		out = binary.BigEndian.AppendUint64(out, uint64(len(iter.Key())))
		out = append(out, iter.Key()...)
		out = binary.BigEndian.AppendUint64(out, uint64(len(iter.Value())))
		out = append(out, iter.Value()...)
	}
	return out
}

// readyGenesis is the unit-test genesis: validators v1..v4 with power 1 and
// keys pub(1..4); seats seat-1..seat-4 with keys pub(30..33); the authority
// account bytes20(1); the service account bytes20(2); DEC-008 threshold 3,
// delay 0.
func readyGenesis() (*authorityv1.GenesisState, []testSeat, []byte) {
	vals := make([]*authorityv1.ValidatorPower, 4)
	seatRows := make([]*authorityv1.DevnetTestSeat, 4)
	seats := make([]testSeat, 4)
	for i := 0; i < 4; i++ {
		vals[i] = &authorityv1.ValidatorPower{ConsensusPubkey: pub(byte(i + 1)), Power: 1, NodeId: "v" + strconv.Itoa(i+1)}
		key := pub(byte(30 + i))
		id := "seat-" + strconv.Itoa(i+1)
		seatRows[i] = &authorityv1.DevnetTestSeat{SeatId: id, PublicKey: key}
		seats[i] = testSeat{ID: id, Pub: key, Signer: SeatAddress(key)}
	}
	authority := bytes20(1)
	gen := &authorityv1.GenesisState{
		Authority: &authorityv1.AuthorityState{
			Validators: vals,
			Submitters: []*typesv1.Submitter{
				{Address: authority, Role: typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY},
				{Address: bytes20(2), Role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE},
			},
			DevnetTestSeats: seatRows,
			QuorumLoss:      authorityv1.QuorumLossBehavior_QUORUM_LOSS_BEHAVIOR_UNSET,
		},
		DevnetTestParameters: []*typesv1.DevnetTestParameter{{
			Label: "PROPOSED_TEST_PARAMETER", DecisionId: "DEC-008", NumericValue: 3, DelaySeconds: 0,
		}},
	}
	return gen, seats, authority
}

// storeFrom runs InitStore on a fresh memory store.
func storeFrom(t testing.TB, gen *authorityv1.GenesisState) storetypes.KVStore {
	t.Helper()
	kv := newMemKV()
	if err := InitStore(kv, gen); err != nil {
		t.Fatal(err)
	}
	return kv
}

// readyStore is readyGenesis written by InitStore.
func readyStore(t testing.TB) (storetypes.KVStore, []testSeat, []byte) {
	t.Helper()
	gen, seats, authority := readyGenesis()
	return storeFrom(t, gen), seats, authority
}

// loadAt is LoadState on the test chain id at one height and time.
func loadAt(t testing.TB, kv KV, height, unix int64) *State {
	t.Helper()
	st, err := LoadState(kv, testChainID, height, unix)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func propose(signer []byte, id string, base uint64, msg *authorityv1.MsgProposeChange) *authorityv1.MsgProposeChange {
	msg.Signer = signer
	msg.BusinessId = "b-" + id
	msg.ProposalId = id
	msg.BasePolicyVersion = base
	msg.ExpiryUnixSeconds = testExpiry
	return msg
}

func approve(signer []byte, id, proposal string) *authorityv1.MsgApproveChange {
	return &authorityv1.MsgApproveChange{Signer: signer, BusinessId: id, ProposalId: proposal}
}

func execute(signer []byte, id, proposal string) *authorityv1.MsgExecuteChange {
	return &authorityv1.MsgExecuteChange{Signer: signer, BusinessId: id, ProposalId: proposal}
}

func validatorProposal(body *authorityv1.ValidatorPowerChange) *authorityv1.MsgProposeChange {
	_, canon, out := validatorCanonical(body)
	if !out.Success() {
		panic(out.Code)
	}
	sum := sha256.Sum256([]byte(canon))
	return &authorityv1.MsgProposeChange{
		ParameterKey:  "validator-" + body.GetNodeId(),
		Payload:       &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ValidatorPower{ValidatorPower: body}},
		PayloadDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sum[:]},
	}
}

// powerMsg is a validator change that activates at a height.
func powerMsg(node string, key []byte, power int64, height uint64) *authorityv1.MsgProposeChange {
	return validatorProposal(&authorityv1.ValidatorPowerChange{
		ConsensusPubkey: key, Power: power, NodeId: node,
		ReasonDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: pub(7)},
		Activation:   &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: height}},
	})
}

// timePower is a validator change that activates at a unix time.
func timePower(node string, key []byte, power, unix int64) *authorityv1.MsgProposeChange {
	return validatorProposal(&authorityv1.ValidatorPowerChange{
		ConsensusPubkey: key, Power: power, NodeId: node,
		ReasonDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: pub(7)},
		Activation:   &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_UnixSeconds{UnixSeconds: unix}},
	})
}

// removalMsg removes node, bound to key, at a height.
func removalMsg(node string, key []byte, height uint64) *authorityv1.MsgProposeChange {
	return powerMsg(node, key, 0, height)
}

// payloadMsg wraps any other payload with its digest and parameter key.
func payloadMsg(payload *authorityv1.ProposalPayload, parameter string) *authorityv1.MsgProposeChange {
	digest, out := PayloadDigest(payload)
	if !out.Success() {
		panic(out.Code)
	}
	return &authorityv1.MsgProposeChange{
		ParameterKey:  parameter,
		Payload:       payload,
		PayloadDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: digest},
	}
}

// governedMsg changes one governed limit.
func governedMsg(key string, value uint64) *authorityv1.MsgProposeChange {
	return payloadMsg(&authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_GovernedLimit{
		GovernedLimit: &authorityv1.GovernedLimitChange{LimitKey: key, Value: value},
	}}, key)
}

func pub(last byte) []byte {
	out := make([]byte, 32)
	out[31] = last
	return out
}

func bytes20(last byte) []byte {
	out := make([]byte, 20)
	out[19] = last
	return out
}

// queueAt proposes id from seats[0] and approves it with seats 0..2.
func queueAt(t testing.TB, kv KV, seats []testSeat, height, unix int64, id string, msg *authorityv1.MsgProposeChange) {
	t.Helper()
	st := loadAt(t, kv, height, unix)
	if out := st.ProposeChange(propose(seats[0].Signer, id, st.PolicyVersion(), msg)); !out.Success() {
		t.Fatalf("propose %s: %+v", id, out)
	}
	for i := 0; i < ApprovedThreshold; i++ {
		if out := st.ApproveChange(approve(seats[i].Signer, "ap-"+id+"-"+strconv.Itoa(i), id)); !out.Success() {
			t.Fatalf("approve %s seat %d: %+v", id, i, out)
		}
	}
	if got := recordOf(t, kv, id).state(); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		t.Fatalf("%s is %v", id, got)
	}
}

// recordOf reads one proposal record.
func recordOf(t testing.TB, kv KV, id string) *proposalRecord {
	t.Helper()
	raw := kv.Get(proposalKey(id))
	if raw == nil {
		t.Fatalf("proposal %s missing", id)
	}
	rec, err := decodeRecord(id, raw)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

// pendingOf lists the pending entries in key order.
func pendingOf(t testing.TB, kv KV) []*authorityv1.PendingValidatorPower {
	t.Helper()
	out, err := readPending(kv)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// PolicyVersion is a test accessor: the policy version, 1 until the first
// execute, or 0 when the stored value is damaged. Production code reads it
// through policy(); this method is not linked into svnd.
func (s *State) PolicyVersion() uint64 {
	version, out := s.policy()
	if !out.Success() {
		return 0
	}
	return version
}

// openAt is the stored open count of one seat for one base version (0 when
// the key is absent).
func openAt(t testing.TB, kv KV, seatID string, version uint64) uint64 {
	t.Helper()
	raw := kv.Get(openKey(seatID, version))
	if raw == nil {
		return 0
	}
	count, good := decodeU64(raw)
	if !good {
		t.Fatalf("open count of %s at version %d is %x", seatID, version, raw)
	}
	return count
}

// openOf is the sum of one seat's stored open counts over every base
// version: its OPEN and QUEUED proposals, current or stale.
func openOf(t testing.TB, kv KV, seatID string) uint64 {
	t.Helper()
	iter := kv.Iterator([]byte{openPrefix}, prefixEnd(openPrefix))
	defer iter.Close()
	var total uint64
	for ; iter.Valid(); iter.Next() {
		key := iter.Key()
		if len(key) < 9 || string(key[9:]) != seatID {
			continue
		}
		count, good := decodeU64(iter.Value())
		if !good {
			t.Fatalf("open count key %x holds %x", key, iter.Value())
		}
		total += count
	}
	return total
}

// countPrefix counts the keys under one prefix byte.
func countPrefix(kv KV, prefix byte) int {
	iter := kv.Iterator([]byte{prefix}, prefixEnd(prefix))
	defer iter.Close()
	n := 0
	for ; iter.Valid(); iter.Next() {
		n++
	}
	return n
}

// mutateConfig rewrites the config key of a test store.
func mutateConfig(t testing.TB, kv KV, edit func(*authorityv1.GenesisState)) {
	t.Helper()
	cfg, err := readConfig(kv)
	if err != nil {
		t.Fatal(err)
	}
	edit(cfg)
	raw, err := marshalState(cfg)
	if err != nil {
		t.Fatal(err)
	}
	kv.Set([]byte{configKey}, raw)
}

// putPendingRaw writes one pending entry straight into the store, the way a
// damaged store or a test hook would. No message does this.
func putPendingRaw(t testing.TB, kv KV, entry *authorityv1.PendingValidatorPower) {
	t.Helper()
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	kv.Set(PendingKey(entry), raw)
}

// wantUnchanged fails when the store differs from before.
func wantUnchanged(t testing.TB, kv KV, before []byte, what string) {
	t.Helper()
	if got := dumpKV(kv); string(got) != string(before) {
		t.Fatalf("%s changed the authority store (%d bytes before, %d after)", what, len(before), len(got))
	}
}

func heightEntry(id, node string, key []byte, power int64, height uint64) *authorityv1.PendingValidatorPower {
	return &authorityv1.PendingValidatorPower{
		ProposalId: id, NodeId: node, ConsensusPubkey: key, Power: power,
		ReasonDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: pub(7)},
		Activation:   &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: height}},
	}
}

func timeEntry(id, node string, key []byte, power, unix int64) *authorityv1.PendingValidatorPower {
	entry := heightEntry(id, node, key, power, 1)
	entry.Activation = &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_UnixSeconds{UnixSeconds: unix}}
	return entry
}
