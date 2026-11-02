package app_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// pubN is a 32-byte test key that encodes n in its last two bytes.
func pubN(n int) []byte {
	out := make([]byte, 32)
	binary.BigEndian.PutUint16(out[30:], uint16(n))
	out[0] = 0x5a
	return out
}

// TestSeatQuotaBoundaryCommitted (DR-06-03): one seat may hold 32 open
// proposals on the current base policy version. The 31st and 32nd commit;
// the 33rd is ERROR_CODE_BOUNDS and leaves the store byte-identical; another
// seat can still propose. When the other seats execute a proposal, the
// version moves to 2, the first seat's 32 proposals on base 1 can never
// execute, and they stop counting: the seat proposes again. Fails on the
// round-3 review tree, which kept one count per seat until each proposal's
// expiry.
func TestSeatQuotaBoundaryCommitted(t *testing.T) {
	c := newCouncil(t, nil)
	s1, s2 := c.seat(0), c.seat(1)
	proposeOn := func(key cryptotypes.PrivKey, i int, base uint64) *authorityv1.MsgProposeChange {
		return change{id: fmt.Sprintf("p-%s-%d-%02d", hexAddr(key)[:4], base, i), base: base, node: fmt.Sprintf("n%02d", i), pub: pubN(i), power: 1, height: 1000}.msg(t, key)
	}
	propose := func(key cryptotypes.PrivKey, i int) sdk.Msg { return proposeOn(key, i, 1) }
	limit := authority.MaxOpenProposalsPerSeat
	for start := 0; start < limit-2; start += 8 {
		var keys []cryptotypes.PrivKey
		var msgs []sdk.Msg
		for i := start; i < start+8 && i < limit-2; i++ {
			keys = append(keys, s1)
			msgs = append(msgs, propose(s1, i))
		}
		for i, result := range c.sendMany(t, keys, msgs) {
			if result.Code != 0 {
				t.Fatalf("proposal %d code %d log %s", start+i+1, result.Code, result.Log)
			}
		}
	}
	for _, i := range []int{limit - 2, limit - 1} {
		before := c.snapshot()
		c.ok(t, s1, propose(s1, i))
		if bytes.Equal(before, c.snapshot()) {
			t.Fatalf("proposal %d did not change the store", i+1)
		}
	}
	c.reject(t, rejectRow{name: "33rd open proposal of one seat", key: s1, msg: propose(s1, limit),
		space: "svn", code: 19, proto: authority.CodeBounds, advances: true})
	other := proposeOn(s2, 0, 1)
	c.queue(t, 1, other, 1, 2, 3)
	c.ok(t, c.keys.authority, executeMsg(c.keys.authority, "ex-other", other.GetProposalId()))
	stale := proposeOn(s1, 0, 1).GetProposalId()
	c.ok(t, c.seat(1), approveMsg(c.seat(1), "ap-stale-1", stale))
	c.ok(t, c.seat(2), approveMsg(c.seat(2), "ap-stale-2", stale))
	c.ok(t, c.seat(3), approveMsg(c.seat(3), "ap-stale-3", stale))
	c.reject(t, rejectRow{name: "execute of a stale proposal", key: c.keys.authority, msg: executeMsg(c.keys.authority, "ex-stale", stale),
		space: "svn", code: 5, proto: authority.CodeStale, advances: true})
	c.ok(t, s1, proposeOn(s1, limit, 2))
}

// TestCommittedGasDoesNotGrowWithHistory: the gas of one committed propose
// is the same before and after 300 terminal proposals. The two measured
// transactions read and write keys and values of the same lengths: the same
// seat with a stored public key at sequence 1 and 2 (one varint byte), ids
// of the same width, expiry and activation of the same digit count, policy
// version 1 (no execute, so 0x04 is absent), the seat's open count absent
// (its earlier proposal expired). The bound is exact equality.
func TestCommittedGasDoesNotGrowWithHistory(t *testing.T) {
	c := newCouncil(t, nil)
	s1 := c.seat(0)
	others := []cryptotypes.PrivKey{c.seat(1), c.seat(2), c.seat(3)}

	// Warm-up: the first signature stores seat-1's public key. NOT_FOUND
	// passes the ante, so the sequence moves to 1 and nothing opens.
	if warm := c.send(t, s1, approveMsg(s1, "w-0000", "x-0000")); warm.Codespace != "svn" || warm.Code != 16 {
		t.Fatalf("warm-up codespace %q code %d", warm.Codespace, warm.Code)
	}
	measure := func(n int) uint64 {
		t.Helper()
		expiry := c.run.when.Unix() + 10
		msg := change{id: fmt.Sprintf("m-%04d", n), business: fmt.Sprintf("mb-%04d", n), base: 1, node: "v5", pub: bytes32(9), power: 1, height: 900000, expiry: expiry}.msg(t, s1)
		return uint64(c.ok(t, s1, msg).GasUsed)
	}
	baseline := measure(0)

	terminal := 0
	for round := 0; round < 4; round++ {
		var keys []cryptotypes.PrivKey
		var msgs []sdk.Msg
		var ids []string
		expiry := c.run.when.Unix() + 10
		for i := 0; i < 75; i++ {
			n := round*75 + i
			key := others[i%3]
			id := fmt.Sprintf("h-%04d", n)
			ids = append(ids, id)
			keys = append(keys, key)
			msgs = append(msgs, change{id: id, business: fmt.Sprintf("hb-%04d", n), base: 1, node: fmt.Sprintf("n%04d", n), pub: pubN(n), power: 1, height: 900000, expiry: expiry}.msg(t, key))
		}
		if round == 0 {
			// seat-1's measured proposal expires in this round too, so its
			// open count is absent again before the second measurement.
			ids = append(ids, "m-0000")
		}
		for start := 0; start < 75; start += 25 {
			for i, result := range c.sendMany(t, keys[start:start+25], msgs[start:start+25]) {
				if result.Code != 0 {
					t.Fatalf("history proposal %s code %d log %s", ids[start+i], result.Code, result.Log)
				}
			}
		}
		c.run.when = c.run.when.Add(20 * time.Second)
		keys = keys[:0]
		msgs = msgs[:0]
		for i, id := range ids {
			key := others[i%3]
			keys = append(keys, key)
			msgs = append(msgs, approveMsg(key, "ha-"+id, id))
		}
		for start := 0; start < len(msgs); start += 25 {
			end := min(start+25, len(msgs))
			for i, result := range c.sendMany(t, keys[start:end], msgs[start:end]) {
				if result.Code != 0 || authorityResult(t, result)["code"] != authority.CodeExpired {
					t.Fatalf("expiring %s: code %d log %s", ids[start+i], result.Code, result.Log)
				}
				terminal++
			}
		}
	}
	if terminal < 300 {
		t.Fatalf("%d terminal proposals, want at least 300", terminal)
	}
	after := measure(1)
	t.Logf("propose gas: %d before, %d after %d terminal proposals", baseline, after, terminal)
	if after != baseline {
		t.Fatalf("propose gas grew with history: %d before, %d after", baseline, after)
	}
}

// TestZeroFeeTransactionIsMetered (REQ-CHAIN-010): a transaction carries no
// fee amount, and its gas is still metered and limited.
func TestZeroFeeTransactionIsMetered(t *testing.T) {
	c := newCouncil(t, nil)
	s1 := c.seat(0)
	msg := change{id: "metered", base: 1, node: "v5", pub: bytes32(9), power: 1, height: 1000}.msg(t, s1)
	c.reject(t, rejectRow{name: "fee-less propose with gas limit 60,000", key: s1, msg: msg, gas: 60_000,
		space: "sdk", code: 11, advances: true})
	raw := c.sign(t, s1, msg)
	decoded, err := c.run.app.EncodingConfig().TxConfig.TxDecoder()(raw)
	if err != nil {
		t.Fatal(err)
	}
	feeTx, ok := decoded.(sdk.FeeTx)
	if !ok || !feeTx.GetFee().IsZero() {
		t.Fatal("the transaction carries a fee amount")
	}
	result := c.run.deliver(t, raw)
	c.seq[hexAddr(s1)]++
	if result.Code != 0 {
		t.Fatalf("code %d log %s", result.Code, result.Log)
	}
	if result.GasUsed <= 50_000 || result.GasWanted != defaultGas {
		t.Fatalf("gas used %d wanted %d", result.GasUsed, result.GasWanted)
	}
}
