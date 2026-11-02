package app_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// defaultGas is the gas limit of the test transactions. Measured on this
// tree: a propose uses about 133,000 gas with its first signature (the
// public key is stored) and about 119,000 after; an approve about 135,000;
// an execute about 150,000. 200,000 covers every authority message.
const defaultGas = 200000

func TestCommittedAuthorityFlowAndReplay(t *testing.T) {
	doc, seats, authorityKey := councilDocument()
	first := boot(t, doc)
	second := boot(t, doc)
	numbers := accountNumbers(doc)
	activation := uint64(10)
	first.deliver(t, nil)
	second.deliver(t, nil)
	sameSnapshot := func(when string) {
		t.Helper()
		if !bytes.Equal(first.app.AuthoritySnapshotForTest(), second.app.AuthoritySnapshotForTest()) {
			t.Fatalf("authority stores diverged %s", when)
		}
	}
	sameSnapshot("after the first block")
	msg := powerProposal(t, seats[0].key, "add-v5", 1, "v5", bytes32(9), 1, activation)
	steps := []struct {
		key    cryptotypes.PrivKey
		number uint64
		msg    sdk.Msg
	}{
		{seats[0].key, numbers[seats[0].addr], msg},
		{seats[1].key, numbers[seats[1].addr], approveMsg(seats[1].key, "ap-1", "add-v5")},
		{seats[2].key, numbers[seats[2].addr], approveMsg(seats[2].key, "ap-2", "add-v5")},
		{seats[3].key, numbers[seats[3].addr], approveMsg(seats[3].key, "ap-3", "add-v5")},
		{authorityKey, numbers[hex.EncodeToString(authorityKey.PubKey().Address())], executeMsg(authorityKey, "ex-1", "add-v5")},
	}
	raws := make([][]byte, len(steps))
	for i, step := range steps {
		raws[i] = signAny(t, first.app, doc.ChainID, step.key, step.number, 0, step.msg)
		result := first.deliver(t, raws[i])
		if result.Code != 0 {
			t.Fatalf("step %d code %d log %s", i, result.Code, result.Log)
		}
		same := second.deliver(t, raws[i])
		if same.Code != 0 {
			t.Fatalf("replay step %d code %d log %s", i, same.Code, same.Log)
		}
		sameSnapshot(fmt.Sprintf("after step %d", i))
	}
	for first.height < int64(activation) {
		if len(first.last.ValidatorUpdates) != 0 {
			t.Fatalf("updates at height %d before activation", first.height)
		}
		first.deliver(t, nil)
		second.deliver(t, nil)
	}
	if len(first.last.ValidatorUpdates) != 1 || first.last.ValidatorUpdates[0].Power != 1 {
		t.Fatalf("height %d updates %+v", first.height, first.last.ValidatorUpdates)
	}
	if first.height != int64(activation) {
		t.Fatalf("activation height %d", first.height)
	}
	// CometBFT v0.40 applies this changeset at height+2. The application
	// returns it on the activation block.
	if authority.CometEffectiveDelay != 2 {
		t.Fatal(authority.CometEffectiveDelay)
	}
	sameSnapshot("after the activation block")
	if !bytes.Equal(first.app.LastCommitID().Hash, second.app.LastCommitID().Hash) {
		t.Fatal("deterministic replay diverged")
	}
}

type seatKey struct {
	key  *sdked.PrivKey
	addr string
}

// councilDocument is sampleDocument with an authority account, a service
// account, and four devnet council seats. Each seat address is also a
// BROADCASTER submitter: that row is the only way a seat gets an auth
// account and passes the ante (DR-06-06).
func councilDocument() (genesis.Document, []seatKey, *sdksecp.PrivKey) {
	doc, keys := councilFixture()
	return doc, keys.seats, keys.authority
}

type councilKeys struct {
	seats     []seatKey
	authority *sdksecp.PrivKey
	service   *sdksecp.PrivKey
}

func councilFixture() (genesis.Document, councilKeys) {
	doc := sampleDocument()
	keys := councilKeys{authority: sdksecp.GenPrivKey(), service: sdksecp.GenPrivKey()}
	doc.Submitters = []genesis.Submitter{
		{AddressHex: hex.EncodeToString(keys.authority.PubKey().Address()), Role: genesis.RoleAuthority},
		{AddressHex: hex.EncodeToString(keys.service.PubKey().Address()), Role: genesis.RoleAdmittedService},
	}
	keys.seats = make([]seatKey, 4)
	doc.DevnetTestSeats = make([]genesis.DevnetTestSeat, 4)
	for i := range keys.seats {
		key := sdked.GenPrivKey()
		keys.seats[i] = seatKey{key: key, addr: hex.EncodeToString(key.PubKey().Address())}
		doc.DevnetTestSeats[i] = genesis.DevnetTestSeat{
			SeatID:       "seat-" + string(rune('1'+i)),
			PublicKeyHex: hex.EncodeToString(key.PubKey().Bytes()),
		}
		doc.Submitters = append(doc.Submitters, genesis.Submitter{
			AddressHex: keys.seats[i].addr,
			Role:       genesis.RoleBroadcaster,
		})
	}
	doc.DevnetTestParameters = []genesis.DevnetTestParameter{{
		Label: genesis.DevnetTestLabel, DecisionID: "DEC-008", NumericValue: 3,
	}}
	return doc, keys
}

func accountNumbers(doc genesis.Document) map[string]uint64 {
	addrs := make([]string, len(doc.Submitters))
	for i, row := range doc.Submitters {
		addrs[i] = row.AddressHex
	}
	return numbersIncluding(addrs)
}

func numbersIncluding(addrs []string) map[string]uint64 {
	sorted := append([]string(nil), addrs...)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	out := map[string]uint64{}
	for i, addr := range sorted {
		out[addr] = uint64(i)
	}
	return out
}

// change is one ValidatorPowerChange proposal. unix != 0 selects a time
// activation; otherwise height is the activation height.
type change struct {
	id       string
	business string
	base     uint64
	node     string
	pub      []byte
	power    int64
	height   uint64
	unix     int64
	expiry   int64
}

func (c change) msg(t *testing.T, key cryptotypes.PrivKey) *authorityv1.MsgProposeChange {
	t.Helper()
	activation := &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: c.height}}
	if c.unix != 0 {
		activation = &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_UnixSeconds{UnixSeconds: c.unix}}
	}
	body := &authorityv1.ValidatorPowerChange{
		ConsensusPubkey: c.pub,
		Power:           c.power,
		NodeId:          c.node,
		ReasonDigest:    &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: bytes32(7)},
		Activation:      activation,
	}
	payload := &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ValidatorPower{ValidatorPower: body}}
	digest, out := authority.PayloadDigest(payload)
	if !out.Success() {
		t.Fatal(out.Code)
	}
	business := c.business
	if business == "" {
		business = "b-" + c.id
	}
	expiry := c.expiry
	if expiry == 0 {
		expiry = 1_800_000_000
	}
	return &authorityv1.MsgProposeChange{
		Signer:            key.PubKey().Address(),
		BusinessId:        business,
		ProposalId:        c.id,
		ParameterKey:      "validator-" + c.node,
		BasePolicyVersion: c.base,
		Payload:           payload,
		PayloadDigest:     &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: digest},
		ExpiryUnixSeconds: expiry,
	}
}

func powerProposal(t *testing.T, key cryptotypes.PrivKey, id string, base uint64, node string, pub []byte, power int64, height uint64) *authorityv1.MsgProposeChange {
	t.Helper()
	return change{id: id, base: base, node: node, pub: pub, power: power, height: height}.msg(t, key)
}

func approveMsg(key cryptotypes.PrivKey, id, proposal string) sdk.Msg {
	return &authorityv1.MsgApproveChange{Signer: key.PubKey().Address(), BusinessId: id, ProposalId: proposal}
}

func executeMsg(key cryptotypes.PrivKey, id, proposal string) sdk.Msg {
	return &authorityv1.MsgExecuteChange{Signer: key.PubKey().Address(), BusinessId: id, ProposalId: proposal}
}

func signAny(t *testing.T, chainApp *app.App, chainID string, key cryptotypes.PrivKey, number, sequence uint64, msg sdk.Msg) []byte {
	t.Helper()
	return signFull(t, chainApp, chainID, key, number, sequence, defaultGas, nil, msg)
}

func TestWrongSignatureDoesNotMutate(t *testing.T) {
	doc, seats, _ := councilDocument()
	running := boot(t, doc)
	running.deliver(t, nil)
	msg := powerProposal(t, seats[0].key, "sig", 1, "v5", bytes32(9), 1, 12)
	raw := signAny(t, running.app, doc.ChainID, seats[0].key, 0, 0, msg)
	raw[len(raw)-1] ^= 0xff
	before := running.app.AuthoritySnapshotForTest()
	result := running.deliver(t, raw)
	if result.Code == 0 {
		t.Fatal("corrupt signature was accepted")
	}
	if !bytes.Equal(before, running.app.AuthoritySnapshotForTest()) {
		t.Fatal("corrupt signature changed the authority snapshot")
	}
}

// council is a booted chain with its signers and their sequences.
type council struct {
	doc     genesis.Document
	keys    councilKeys
	run     *running
	numbers map[string]uint64
	seq     map[string]uint64
}

func newCouncil(t *testing.T, edit func(*genesis.Document)) *council {
	t.Helper()
	doc, keys := councilFixture()
	if edit != nil {
		edit(&doc)
	}
	c := &council{doc: doc, keys: keys, run: boot(t, doc), numbers: accountNumbers(doc), seq: map[string]uint64{}}
	c.run.deliver(t, nil)
	return c
}

func hexAddr(key cryptotypes.PrivKey) string {
	return hex.EncodeToString(key.PubKey().Address())
}

func (c *council) seat(i int) cryptotypes.PrivKey { return c.keys.seats[i].key }

// sign signs msg at the signer's tracked sequence.
func (c *council) sign(t *testing.T, key cryptotypes.PrivKey, msg sdk.Msg) []byte {
	t.Helper()
	return signFull(t, c.run.app, c.doc.ChainID, key, c.numbers[hexAddr(key)], c.seq[hexAddr(key)], defaultGas, nil, msg)
}

// send delivers one transaction in its own block and moves the tracked
// sequence. Use it only when the ante passes; reject rows use reject.
func (c *council) send(t *testing.T, key cryptotypes.PrivKey, msg sdk.Msg) *abci.ExecTxResult {
	t.Helper()
	result := c.run.deliver(t, c.sign(t, key, msg))
	c.seq[hexAddr(key)]++
	return result
}

// ok delivers msg and requires code 0.
func (c *council) ok(t *testing.T, key cryptotypes.PrivKey, msg sdk.Msg) *abci.ExecTxResult {
	t.Helper()
	result := c.send(t, key, msg)
	if result.Code != 0 {
		t.Fatalf("%T code %d codespace %s log %s", msg, result.Code, result.Codespace, result.Log)
	}
	return result
}

// sendMany delivers one block with one transaction per message. Every
// signer's ante passes, so each sequence moves once per message.
func (c *council) sendMany(t *testing.T, keys []cryptotypes.PrivKey, msgs []sdk.Msg) []*abci.ExecTxResult {
	t.Helper()
	raws := make([][]byte, len(msgs))
	for i, msg := range msgs {
		raws[i] = c.sign(t, keys[i], msg)
		c.seq[hexAddr(keys[i])]++
	}
	return c.run.deliverMany(t, raws)
}

// queue proposes msg from seat proposer and approves it with three seats.
func (c *council) queue(t *testing.T, proposer int, msg *authorityv1.MsgProposeChange, approvers ...int) {
	t.Helper()
	c.ok(t, c.seat(proposer), msg)
	if len(approvers) == 0 {
		approvers = []int{0, 1, 2}
	}
	for _, i := range approvers {
		c.ok(t, c.seat(i), approveMsg(c.seat(i), fmt.Sprintf("%s-ap-%d", msg.GetProposalId(), i), msg.GetProposalId()))
	}
}

func (c *council) snapshot() []byte { return c.run.app.AuthoritySnapshotForTest() }

// rejectRow is one rejected transaction. The full authority store, and every
// other module store except the account store, must be byte-identical after
// it.
type rejectRow struct {
	name    string
	key     cryptotypes.PrivKey
	msg     sdk.Msg
	gas     uint64 // 0: defaultGas
	zeroGas bool
	fee     sdk.Coins
	stale   bool // sign one sequence below the account's
	corrupt bool // flip the last signature byte
	space   string
	code    uint32
	// proto is the protocol code the log must name ("" for SDK codes).
	proto string
	// advances: the ante passed, so the account sequence moved by one.
	advances bool
}

func (c *council) reject(t *testing.T, row rejectRow) {
	t.Helper()
	addr := row.key.PubKey().Address()
	seqBefore, known := c.run.app.AccountSequenceForTest(addr)
	gas := row.gas
	if gas == 0 && !row.zeroGas {
		gas = defaultGas
	}
	sequence := c.seq[hexAddr(row.key)]
	if row.stale {
		sequence--
	}
	raw := signFull(t, c.run.app, c.doc.ChainID, row.key, c.numbers[hexAddr(row.key)], sequence, gas, row.fee, row.msg)
	if row.corrupt {
		raw[len(raw)-1] ^= 0xff
	}
	before := c.snapshot()
	full := c.run.app.FullSnapshot()
	result := c.run.deliver(t, raw)
	if result.Codespace != row.space || result.Code != row.code {
		t.Fatalf("%s: codespace %q code %d, want %q %d; log %s", row.name, result.Codespace, result.Code, row.space, row.code, result.Log)
	}
	if row.proto != "" && !strings.Contains(result.Log, row.proto) {
		t.Fatalf("%s: log %q does not name %s", row.name, result.Log, row.proto)
	}
	if after := c.snapshot(); !bytes.Equal(before, after) {
		t.Fatalf("%s changed the authority store\nbefore % x\nafter  % x", row.name, before, after)
	}
	// Every row is a reject, so no registry or other module store changes
	// either. The account store is excluded: the ante may move the sequence.
	if !bytes.Equal(full, c.run.app.FullSnapshot()) {
		t.Fatalf("%s changed a module store", row.name)
	}
	seqAfter, knownAfter := c.run.app.AccountSequenceForTest(addr)
	if knownAfter != known {
		t.Fatalf("%s: account existence changed", row.name)
	}
	want := seqBefore
	if row.advances {
		want++
		c.seq[hexAddr(row.key)]++
	}
	if known && seqAfter != want {
		t.Fatalf("%s: sequence %d, want %d", row.name, seqAfter, want)
	}
}

// TestRejectedMessagesLeaveTheFullAuthorityState sends one transaction per
// reject class. Each row asserts the exact codespace, code, and protocol
// code, and that the whole authority store (every key: config, admission
// rows, pending entries, proposal records, business ids, counts) is
// byte-identical afterwards. Setup steps between rows must succeed and must
// change the store.
func TestRejectedMessagesLeaveTheFullAuthorityState(t *testing.T) {
	c := newCouncil(t, func(doc *genesis.Document) { doc.DevnetTestParameters[0].DelaySeconds = 100 })
	s1, s2, s3, s4 := c.seat(0), c.seat(1), c.seat(2), c.seat(3)
	auth, service := c.keys.authority, c.keys.service
	outsider := sdksecp.GenPrivKey()
	rows := 0
	reject := func(row rejectRow) {
		t.Helper()
		c.reject(t, row)
		rows++
	}
	step := func(key cryptotypes.PrivKey, msg sdk.Msg) {
		t.Helper()
		before := c.snapshot()
		c.ok(t, key, msg)
		if bytes.Equal(before, c.snapshot()) {
			t.Fatalf("%T did not change the authority store", msg)
		}
	}
	valid := func(key cryptotypes.PrivKey, id string) *authorityv1.MsgProposeChange {
		return change{id: id, base: 1, node: "v5", pub: bytes32(9), power: 1, height: 1000}.msg(t, key)
	}
	edited := func(key cryptotypes.PrivKey, id string, edit func(*authorityv1.MsgProposeChange)) sdk.Msg {
		msg := valid(key, id)
		edit(msg)
		return msg
	}
	const big = 70_000

	reject(rejectRow{name: "outsider propose", key: outsider, msg: valid(outsider, "out"), space: "sdk", code: 9})
	reject(rejectRow{name: "corrupt signature", key: s1, msg: valid(s1, "bad-sig"), corrupt: true, space: "sdk", code: 4})
	reject(rejectRow{name: "malformed propose", key: s1, msg: &authorityv1.MsgProposeChange{Signer: s1.PubKey().Address()},
		space: "svn", code: 7, proto: authority.CodeMalformed, advances: true})
	reject(rejectRow{name: "stale sequence", key: s1, msg: valid(s1, "stale-seq"), stale: true, space: "sdk", code: 32})
	reject(rejectRow{name: "out of gas", key: s1, msg: valid(s1, "oog"), gas: 60_000, space: "sdk", code: 11, advances: true})
	reject(rejectRow{name: "gas limit 0", key: s2, msg: valid(s2, "gas0"), zeroGas: true, space: "svn", code: 19, proto: authority.CodeBounds})
	reject(rejectRow{name: "tx bytes above 65536", key: s2, msg: edited(s2, "huge", func(m *authorityv1.MsgProposeChange) {
		m.BusinessId = strings.Repeat("a", big)
	}), space: "svn", code: 19, proto: authority.CodeBounds})
	reject(rejectRow{name: "fee amount", key: s2, msg: valid(s2, "fee"), fee: sdk.NewCoins(sdk.NewInt64Coin("reject", 1)),
		space: "svn", code: 21, proto: "ERROR_CODE_FEE_REJECTED"})
	reject(rejectRow{name: "wrong-role propose (authority)", key: auth, msg: valid(auth, "by-auth"),
		space: "svn", code: 1, proto: authority.CodeUnauthorized, advances: true})
	reject(rejectRow{name: "wrong-role propose (service)", key: service, msg: valid(service, "by-svc"),
		space: "svn", code: 1, proto: authority.CodeUnauthorized, advances: true})
	reject(rejectRow{name: "bad id pattern", key: s2, msg: edited(s2, "bad-id", func(m *authorityv1.MsgProposeChange) {
		m.ProposalId = "Bad-Id"
	}), space: "svn", code: 19, proto: authority.CodeBounds, advances: true})
	reject(rejectRow{name: "dec- parameter key", key: s2, msg: edited(s2, "dec-key", func(m *authorityv1.MsgProposeChange) {
		m.ParameterKey = "dec-008"
	}), space: "svn", code: 2, proto: authority.CodeForbidden, advances: true})
	reject(rejectRow{name: "wrong payload digest", key: s2, msg: edited(s2, "digest", func(m *authorityv1.MsgProposeChange) {
		m.PayloadDigest.Value = bytes32(1)
	}), space: "svn", code: 18, proto: authority.CodeIntegrity, advances: true})
	reject(rejectRow{name: "activation not in the future", key: s2,
		msg:   change{id: "past", base: 1, node: "v5", pub: bytes32(9), power: 1, height: 1}.msg(t, s2),
		space: "svn", code: 9, proto: authority.CodeOutOfRange, advances: true})
	reject(rejectRow{name: "expiry not after block time", key: s2,
		msg:   change{id: "expired", base: 1, node: "v5", pub: bytes32(9), power: 1, height: 1000, expiry: c.run.when.Unix()}.msg(t, s2),
		space: "svn", code: 6, proto: authority.CodeExpired, advances: true})
	reject(rejectRow{name: "stale base policy version", key: s2,
		msg:   change{id: "stale", base: 2, node: "v5", pub: bytes32(9), power: 1, height: 1000}.msg(t, s2),
		space: "svn", code: 5, proto: authority.CodeStale, advances: true})
	reject(rejectRow{name: "parameter key not bound to the payload", key: s2, msg: edited(s2, "unbound", func(m *authorityv1.MsgProposeChange) {
		m.ParameterKey = "max_page_limit"
	}), space: "svn", code: 7, proto: authority.CodeMalformed, advances: true})
	reject(rejectRow{name: "digest algorithm unspecified", key: s2, msg: edited(s2, "no-algo", func(m *authorityv1.MsgProposeChange) {
		m.PayloadDigest.Algorithm = typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED
	}), space: "svn", code: 7, proto: authority.CodeMalformed, advances: true})
	reject(rejectRow{name: "expiry 0", key: s2, msg: edited(s2, "exp-0", func(m *authorityv1.MsgProposeChange) {
		m.ExpiryUnixSeconds = 0
	}), space: "svn", code: 19, proto: authority.CodeBounds, advances: true})
	reject(rejectRow{name: "expiry one above unix max", key: s2, msg: edited(s2, "exp-max", func(m *authorityv1.MsgProposeChange) {
		m.ExpiryUnixSeconds = genesis.UnixSecondsMax + 1
	}), space: "svn", code: 19, proto: authority.CodeBounds, advances: true})

	// Proposal A: add v5 at height 1000.
	step(s3, valid(s3, "add-v5"))
	reject(rejectRow{name: "reused business id, other payload", key: s4,
		msg:   change{id: "other", business: "b-add-v5", base: 1, node: "v6", pub: bytes32(10), power: 1, height: 1000}.msg(t, s4),
		space: "svn", code: 3, proto: authority.CodeReplay, advances: true})
	reject(rejectRow{name: "duplicate proposal id, fresh business id", key: s4,
		msg:   change{id: "add-v5", business: "b-dup", base: 1, node: "v9", pub: bytes32(11), power: 1, height: 1000}.msg(t, s4),
		space: "svn", code: 4, proto: authority.CodeDuplicate, advances: true})
	reject(rejectRow{name: "wrong-role approve (authority)", key: auth, msg: approveMsg(auth, "ap-auth", "add-v5"),
		space: "svn", code: 1, proto: authority.CodeUnauthorized, advances: true})
	reject(rejectRow{name: "seat approve of a missing proposal", key: s4, msg: approveMsg(s4, "ap-missing", "missing"),
		space: "svn", code: 16, proto: authority.CodeNotFound, advances: true})
	step(s1, approveMsg(s1, "ap-1", "add-v5"))
	reject(rejectRow{name: "repeated approval, same seat, new business id", key: s1, msg: approveMsg(s1, "ap-1b", "add-v5"),
		space: "svn", code: 3, proto: authority.CodeReplay, advances: true})
	reject(rejectRow{name: "execute of an OPEN proposal", key: auth, msg: executeMsg(auth, "ex-open", "add-v5"),
		space: "svn", code: 14, proto: authority.CodeConflict, advances: true})
	step(s2, approveMsg(s2, "ap-2", "add-v5"))
	step(s4, approveMsg(s4, "ap-4", "add-v5"))
	// Two more QUEUED proposals on base 1, from seat 4: another payload for
	// validator-v5, and one for validator-v6. They go stale when add-v5
	// executes. Seat 4 fills its base-2 quota below; these two no longer
	// count against it.
	step(s4, change{id: "v5-alt", base: 1, node: "v5", pub: bytes32(13), power: 1, height: 1000}.msg(t, s4))
	step(s4, change{id: "v6-old", base: 1, node: "v6", pub: bytes32(10), power: 1, height: 1000}.msg(t, s4))
	for _, id := range []string{"v5-alt", "v6-old"} {
		for i, seat := range []cryptotypes.PrivKey{s1, s2, s3} {
			step(seat, approveMsg(seat, fmt.Sprintf("%s-ap-%d", id, i), id))
		}
	}
	reject(rejectRow{name: "approve of a QUEUED proposal", key: s3, msg: approveMsg(s3, "ap-3q", "add-v5"),
		space: "svn", code: 14, proto: authority.CodeConflict, advances: true})
	// A seat is a BROADCASTER submitter (DR-06-06), so check_role says
	// FORBIDDEN, not UNAUTHORIZED.
	reject(rejectRow{name: "seat-signed execute", key: s2, msg: executeMsg(s2, "ex-seat", "add-v5"),
		space: "svn", code: 2, proto: authority.CodeForbidden, advances: true})
	reject(rejectRow{name: "service-signed execute", key: service, msg: executeMsg(service, "ex-svc", "add-v5"),
		space: "svn", code: 2, proto: authority.CodeForbidden, advances: true})
	reject(rejectRow{name: "early execute (delay 100)", key: auth, msg: executeMsg(auth, "ex-early", "add-v5"),
		space: "svn", code: 9, proto: authority.CodeOutOfRange, advances: true})

	c.run.when = c.run.when.Add(200 * time.Second)
	step(auth, executeMsg(auth, "ex-a", "add-v5"))
	// Policy version is 2 now. A height entry for v5 is pending.
	reject(rejectRow{name: "execute on an older base, parameter since executed with another digest", key: auth,
		msg: executeMsg(auth, "ex-alt", "v5-alt"), space: "svn", code: 14, proto: authority.CodeConflict, advances: true})
	reject(rejectRow{name: "execute on an older base", key: auth,
		msg: executeMsg(auth, "ex-old", "v6-old"), space: "svn", code: 5, proto: authority.CodeStale, advances: true})
	step(s1, change{id: "dup-key", base: 2, node: "v6", pub: bytes32(1), power: 1, height: 1000}.msg(t, s1))
	step(s2, change{id: "by-time", base: 2, node: "v7", pub: bytes32(12), power: 1, unix: 1_750_000_000}.msg(t, s2))
	step(s1, change{id: "same-node", base: 2, node: "v5", pub: bytes32(14), power: 1, height: 1000}.msg(t, s1))
	step(s2, change{id: "same-key", base: 2, node: "v8", pub: bytes32(9), power: 1, height: 1000}.msg(t, s2))
	for _, id := range []string{"dup-key", "by-time", "same-node", "same-key"} {
		for i, seat := range []cryptotypes.PrivKey{s1, s3, s4} {
			step(seat, approveMsg(seat, fmt.Sprintf("%s-ap-%d", id, i), id))
		}
	}
	c.run.when = c.run.when.Add(200 * time.Second)
	reject(rejectRow{name: "execute of a duplicate-consensus-key add", key: auth, msg: executeMsg(auth, "ex-dup", "dup-key"),
		space: "svn", code: 4, proto: authority.CodeDuplicate, advances: true})
	reject(rejectRow{name: "execute of a time activation while a height entry is pending", key: auth, msg: executeMsg(auth, "ex-mixed", "by-time"),
		space: "svn", code: 14, proto: authority.CodeConflict, advances: true})
	reject(rejectRow{name: "execute of a second change for the same node at one height", key: auth, msg: executeMsg(auth, "ex-same-node", "same-node"),
		space: "svn", code: 14, proto: authority.CodeConflict, advances: true})
	reject(rejectRow{name: "execute of a change that reuses a scheduled key at one height", key: auth, msg: executeMsg(auth, "ex-same-key", "same-key"),
		space: "svn", code: 14, proto: authority.CodeConflict, advances: true})

	// Seat 4 fills its open-proposal quota, eight proposals per block.
	for start := 0; start < authority.MaxOpenProposalsPerSeat; start += 8 {
		keys := make([]cryptotypes.PrivKey, 0, 8)
		msgs := make([]sdk.Msg, 0, 8)
		for i := start; i < start+8; i++ {
			keys = append(keys, s4)
			msgs = append(msgs, change{id: fmt.Sprintf("q-%02d", i), base: 2, node: fmt.Sprintf("q%02d", i), pub: bytes32(byte(100 + i)), power: 1, height: 1000}.msg(t, s4))
		}
		for i, result := range c.sendMany(t, keys, msgs) {
			if result.Code != 0 {
				t.Fatalf("quota fill %d code %d log %s", start+i, result.Code, result.Log)
			}
		}
	}
	reject(rejectRow{name: "seat at its open limit proposes", key: s4,
		msg:   change{id: "q-over", base: 2, node: "q99", pub: bytes32(250), power: 1, height: 1000}.msg(t, s4),
		space: "svn", code: 19, proto: authority.CodeBounds, advances: true})

	if rows != 37 {
		t.Fatalf("%d reject rows, want 37", rows)
	}
}

// TestRemovalBindHaltLeavesTheSnapshot writes a due removal with the wrong
// key straight into the store. EndBlock must halt the block. What each
// assertion shows:
//   - FinalizeBlock returns an error that names ERROR_CODE_CONFLICT. CometBFT
//     does not commit a block whose FinalizeBlock fails; this test never calls
//     Commit, so it does not assert the committed height or hash.
//   - The authority store of the finalize-block branch, which holds every
//     write the failed block made, equals the store before the block: the
//     halting EndBlock wrote nothing and the entry is still there.
//   - A second FinalizeBlock at the same height fails the same way. It runs
//     in the same process and reuses the failed attempt's finalize branch, so
//     it is not a restart.
func TestRemovalBindHaltLeavesTheSnapshot(t *testing.T) {
	doc, _, _ := councilDocument()
	running := boot(t, doc)
	running.deliver(t, nil)
	if err := running.app.InstallMismatchedRemovalForTest(); err != nil {
		t.Fatal(err)
	}
	before := running.app.AuthoritySnapshotForTest()
	running.height++
	attempt := func(which string) {
		t.Helper()
		_, err := running.app.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: running.height,
			Time:   running.when,
			Hash:   bytes32(byte(running.height)),
		})
		if err == nil {
			t.Fatalf("%s: the mismatched removal was finalized", which)
		}
		if !strings.Contains(err.Error(), authority.CodeConflict) {
			t.Fatalf("%s: error %v does not name %s", which, err, authority.CodeConflict)
		}
		if !bytes.Equal(before, running.app.FinalizeAuthoritySnapshotForTest()) {
			t.Fatalf("%s: the halting block wrote to the authority store", which)
		}
	}
	attempt("first attempt")
	// The second FinalizeBlock at the same height runs in the same process
	// and reuses the failed attempt's finalize state. It is not a restart;
	// it shows only that the entry is still there and fails the same way.
	attempt("second attempt")
}

func signFull(t *testing.T, chainApp *app.App, chainID string, key cryptotypes.PrivKey, number, sequence, gas uint64, fee sdk.Coins, msg sdk.Msg) []byte {
	t.Helper()
	builder := chainApp.EncodingConfig().TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		t.Fatal(err)
	}
	builder.SetGasLimit(gas)
	if fee != nil {
		builder.SetFeeAmount(fee)
	}
	signer := authsigning.SignerData{
		Address: sdk.AccAddress(key.PubKey().Address()).String(), ChainID: chainID,
		AccountNumber: number, Sequence: sequence, PubKey: key.PubKey(),
	}
	placeholder := signingtypes.SignatureV2{
		PubKey: key.PubKey(), Sequence: sequence,
		Data: &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT},
	}
	if err := builder.SetSignatures(placeholder); err != nil {
		t.Fatal(err)
	}
	signature, err := tx.SignWithPrivKey(context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT, signer, builder, key, chainApp.EncodingConfig().TxConfig, sequence)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.SetSignatures(signature); err != nil {
		t.Fatal(err)
	}
	raw, err := chainApp.EncodingConfig().TxConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
