package chain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
)

// TestBuildTxMatchesChainDecoder: a transaction built by Prepare and
// Assemble decodes with a fresh copy of the application's strict decoder
// (app.MakeEncoding) to exactly one message equal to the input, the key's
// address as the only signer, the declared gas, the timeout height, no
// memo, no fee, no fee payer or granter, no timeout timestamp, ordered,
// and the sequence. Its signature verifies over the SIGN_MODE_DIRECT bytes
// recomputed from the decoded transaction for the configured chain id and
// account number, and not for another chain id or account number. Its
// hash is SHA-256 of the bytes. Signing is deterministic (RFC 6979): the
// same inputs give the same bytes, and another timeout height gives
// another hash.
func TestBuildTxMatchesChainDecoder(t *testing.T) {
	c := testCodec(t)
	key := testKey("service")
	signer := key.PubKey().Address()
	enc, err := app.MakeEncoding()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range sixMsgs(signer) {
		t.Run(tc.name, func(t *testing.T) {
			in := stdInput(3)
			raw, hash := signed(t, c, key, tc.msg, in)
			if hash != sha256.Sum256(raw) {
				t.Fatal("hash is not SHA-256 of the bytes")
			}
			tx, err := enc.TxConfig.TxDecoder()(raw)
			if err != nil {
				t.Fatalf("strict decode: %v", err)
			}
			full := tx.(authsigning.Tx)
			msgs := full.GetMsgs()
			if len(msgs) != 1 || !proto.Equal(msgs[0].(proto.Message), tc.msg.(proto.Message)) {
				t.Fatalf("decoded messages %v", msgs)
			}
			signers, err := full.GetSigners()
			if err != nil || len(signers) != 1 || !bytes.Equal(signers[0], signer) {
				t.Fatalf("signers %x %v", signers, err)
			}
			if full.GetGas() != in.Gas || full.GetTimeoutHeight() != in.TimeoutHeight || full.GetMemo() != "" ||
				!full.GetFee().IsZero() || len(full.FeeGranter()) != 0 || !bytes.Equal(full.FeePayer(), signer) ||
				!full.GetTimeoutTimeStamp().IsZero() || full.GetUnordered() {
				t.Fatalf("envelope gas %d timeout %d memo %q fee %v", full.GetGas(), full.GetTimeoutHeight(), full.GetMemo(), full.GetFee())
			}
			sigs, err := full.GetSignaturesV2()
			if err != nil || len(sigs) != 1 || sigs[0].Sequence != in.Sequence {
				t.Fatalf("signatures %v %v", sigs, err)
			}
			data := sigs[0].Data.(*signingtypes.SingleSignatureData)
			if data.SignMode != signingtypes.SignMode_SIGN_MODE_DIRECT {
				t.Fatalf("sign mode %v", data.SignMode)
			}
			bech, err := c.Bech32(signer)
			if err != nil {
				t.Fatal(err)
			}
			verify := func(chainID string, number uint64) bool {
				signBytes, err := authsigning.GetSignBytesAdapter(context.Background(), enc.TxConfig.SignModeHandler(), signingtypes.SignMode_SIGN_MODE_DIRECT,
					authsigning.SignerData{Address: bech, ChainID: chainID, AccountNumber: number, Sequence: in.Sequence, PubKey: key.PubKey()}, tx)
				if err != nil {
					t.Fatal(err)
				}
				return key.PubKey().VerifySignature(signBytes, data.Signature)
			}
			if !verify(testChainID, in.AccountNumber) {
				t.Fatal("signature does not verify for the configured chain id")
			}
			if verify("shortvideo-dev-ffffffffffffffff", in.AccountNumber) || verify(testChainID, in.AccountNumber+1) {
				t.Fatal("signature verifies for another chain id or account number")
			}
			again, _ := signed(t, c, key, tc.msg, in)
			if !bytes.Equal(again, raw) {
				t.Fatal("same inputs gave other bytes")
			}
			later := in
			later.TimeoutHeight++
			if _, other := signed(t, c, key, tc.msg, later); other == hash {
				t.Fatal("another timeout height gave the same hash")
			}
			decoded, err := c.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			own, err := decoded.Own()
			if err != nil || own.Msg.TypeURL != sdk.MsgTypeURL(tc.msg) || own.Signer.Sequence != in.Sequence {
				t.Fatalf("own %+v %v", own, err)
			}
		})
	}
}

// TestBuildTxRefusesForeignSigner: Prepare refuses a public key whose
// address is not the message signer, a signer that is not 20 bytes, a
// message that is not an svn message, gas outside [1, 20,000,000], and a
// timeout height of 0; Assemble refuses a signature by another key and a
// signature over other bytes. A change to the caller's message after
// Prepare does not reach the assembled transaction.
func TestBuildTxRefusesForeignSigner(t *testing.T) {
	c := testCodec(t)
	key, other := testKey("service"), testKey("other")
	msg := offerMsg(key.PubKey().Address())
	refuse := func(name string, m sdk.Msg, pub cryptotypes.PubKey, in TxInput, want string) {
		t.Helper()
		if _, err := c.Prepare(m, pub, in); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: got %v, want %q", name, err, want)
		}
	}
	refuse("foreign key", msg, other.PubKey(), stdInput(0), "not the message signer")
	refuse("ed25519 key of another address", msg, ed25519.GenPrivKeyFromSecret([]byte("x")).PubKey(), stdInput(0), "not the message signer")
	refuse("19-byte signer", offerMsg(key.PubKey().Address()[:19]), key.PubKey(), stdInput(0), "signer")
	refuse("auth message", &authtypes.MsgUpdateParams{Authority: "x"}, key.PubKey(), stdInput(0), "not an svn message")
	refuse("no public key", msg, nil, stdInput(0), "nil message or public key")
	for _, gas := range []uint64{0, genesis.MaxGasPerTx + 1} {
		in := stdInput(0)
		in.Gas = gas
		refuse("gas", msg, key.PubKey(), in, "gas limit")
	}
	in := stdInput(0)
	in.TimeoutHeight = 0
	refuse("no timeout", msg, key.PubKey(), in, "timeout height")

	u, err := c.Prepare(msg, key.PubKey(), stdInput(4))
	if err != nil {
		t.Fatal(err)
	}
	foreignSig, err := other.Sign(u.SignBytes())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := u.Assemble(foreignSig); err == nil {
		t.Fatal("Assemble accepted a signature by another key")
	}
	otherBytes, err := key.Sign(append(u.SignBytes(), 0))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := u.Assemble(otherBytes); err == nil {
		t.Fatal("Assemble accepted a signature over other bytes")
	}
	msg.OfferId = "changed-after-prepare"
	sig, err := key.Sign(u.SignBytes())
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := u.Assemble(sig)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := c.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.Msgs[0].Msg.(interface{ GetOfferId() string }).GetOfferId(); got != "o1" {
		t.Fatalf("assembled offer id %q", got)
	}
}

// TestSimulationTxShape: the simulation transaction carries the message,
// the public key, the sequence and the timeout height, declares gas 0, and
// has an empty signature, so it cannot be the signed transaction.
func TestSimulationTxShape(t *testing.T) {
	c := testCodec(t)
	key := testKey("service")
	msg := withdrawMsg(key.PubKey().Address())
	raw, err := c.SimulationTx(msg, key.PubKey(), 9, 150)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := c.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	own, err := decoded.Own()
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Gas != 0 || decoded.TimeoutHeight != 150 || own.Signer.Sequence != 9 || !own.Signer.PubKey.Equals(key.PubKey()) ||
		!proto.Equal(own.Msg.Msg.(proto.Message), msg) {
		t.Fatalf("simulation transaction %+v", decoded)
	}
	enc, err := app.MakeEncoding()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := enc.TxConfig.TxDecoder()(raw)
	if err != nil {
		t.Fatal(err)
	}
	sigs, err := tx.(authsigning.Tx).GetSignaturesV2()
	if err != nil || len(sigs) != 1 || len(sigs[0].Data.(*signingtypes.SingleSignatureData).Signature) != 0 {
		t.Fatalf("simulation signature %v %v", sigs, err)
	}
	if _, err := c.SimulationTx(msg, testKey("other").PubKey(), 9, 150); err == nil {
		t.Fatal("simulation transaction for another key")
	}
	if _, err := c.SimulationTx(msg, key.PubKey(), 9, 0); err == nil {
		t.Fatal("simulation transaction without a timeout height")
	}
}

// TestDecodeAndOwnShape: Decode refuses empty, oversize and undecodable
// bytes; Own refuses every shape the broadcaster never builds.
func TestDecodeAndOwnShape(t *testing.T) {
	c := testCodec(t)
	key := testKey("service")
	for _, raw := range [][]byte{nil, make([]byte, genesis.MaxTxBytes+1), []byte("not a transaction")} {
		if _, err := c.Decode(raw); err == nil {
			t.Fatalf("decoded %d bytes", len(raw))
		}
	}
	two := signedMulti(t, c, key, 1, offerMsg(key.PubKey().Address()), acceptMsg(key.PubKey().Address()))
	decoded, err := c.Decode(two)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Msgs) != 2 {
		t.Fatalf("decoded %d messages", len(decoded.Msgs))
	}
	if _, err := decoded.Own(); err == nil {
		t.Fatal("Own accepted two messages")
	}
	raw, _ := signed(t, c, key, offerMsg(key.PubKey().Address()), stdInput(1))
	base, err := c.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*DecodedTx){
		"memo":         func(d *DecodedTx) { d.Memo = "intent-1" },
		"fee":          func(d *DecodedTx) { d.FeeZero = false },
		"fee payer":    func(d *DecodedTx) { d.FeePayer = "svn1payer" },
		"fee granter":  func(d *DecodedTx) { d.FeeGranter = "svn1granter" },
		"timestamp":    func(d *DecodedTx) { d.TimeoutTimestamp = true },
		"unordered":    func(d *DecodedTx) { d.Unordered = true },
		"no timeout":   func(d *DecodedTx) { d.TimeoutHeight = 0 },
		"other signer": func(d *DecodedTx) { d.Signers = []SignerInfo{{Address: testKey("other").PubKey().Address()}} },
	} {
		d := base
		d.Msgs = append([]DecodedMsg(nil), base.Msgs...)
		d.Signers = append([]SignerInfo(nil), base.Signers...)
		mutate(&d)
		if _, err := d.Own(); err == nil {
			t.Fatalf("Own accepted %s", name)
		}
	}
	if _, err := base.Own(); err != nil {
		t.Fatalf("Own refused the built shape: %v", err)
	}
	audit := &evidencev1.MsgSubmitAuditBatch{Signer: key.PubKey().Address(), BusinessId: "au-1"}
	if _, err := c.Prepare(audit, key.PubKey(), stdInput(1)); err != nil {
		t.Fatalf("Prepare refused a registered svn message: %v", err)
	}
}

// TestNewAttemptTimeoutAboveEarlier: the timeout height is latest + K and
// strictly above every earlier attempt's timeout height for the same
// signer and sequence; when latest + K is not above one, the caller waits
// (ready false) instead of reaching further ahead. K stays within its hard
// bounds and the sum cannot overflow.
func TestNewAttemptTimeoutAboveEarlier(t *testing.T) {
	cases := []struct {
		name    string
		latest  int64
		k       uint64
		earlier []uint64
		want    uint64
		ready   bool
		err     bool
	}{
		{"first attempt", 100, 40, nil, 140, true, false},
		{"earlier below", 100, 40, []uint64{120, 139}, 140, true, false},
		{"earlier equal waits", 100, 40, []uint64{140}, 0, false, false},
		{"earlier above waits", 100, 40, []uint64{90, 141}, 0, false, false},
		{"one block later", 101, 40, []uint64{140}, 141, true, false},
		{"lower bound K", 1, MinTimeoutOffset, nil, 1 + MinTimeoutOffset, true, false},
		{"upper bound K", 1, MaxTimeoutOffset, nil, 1 + MaxTimeoutOffset, true, false},
		{"K below bound", 100, MinTimeoutOffset - 1, nil, 0, false, true},
		{"K above bound", 100, MaxTimeoutOffset + 1, nil, 0, false, true},
		{"no committed block", 0, 40, nil, 0, false, true},
		{"overflow", 1<<63 - 10, 40, nil, 0, false, true},
		{"too many earlier", 100, 40, make([]uint64, MaxEarlierAttempts+1), 0, false, true},
	}
	for _, tc := range cases {
		got, ready, err := NextTimeoutHeight(tc.latest, tc.k, tc.earlier)
		if (err != nil) != tc.err || got != tc.want || ready != tc.ready {
			t.Fatalf("%s: got %d %t %v, want %d %t err=%t", tc.name, got, ready, err, tc.want, tc.ready, tc.err)
		}
	}
}

// TestGasWithMargin: simulated gas plus the margin, capped at the chain's
// gas bound; a zero simulation, a simulation above the bound, and a margin
// above 100 percent are refused.
func TestGasWithMargin(t *testing.T) {
	cases := []struct {
		sim, margin, want uint64
		err               bool
	}{
		{100_000, 30, 130_000, false},
		{100_000, 0, 100_000, false},
		{100_000, 100, 200_000, false},
		{18_000_000, 30, genesis.MaxGasPerTx, false},
		{genesis.MaxGasPerTx, 0, genesis.MaxGasPerTx, false},
		{0, 30, 0, true},
		{genesis.MaxGasPerTx + 1, 0, 0, true},
		{100_000, 101, 0, true},
	}
	for _, tc := range cases {
		got, err := GasWithMargin(tc.sim, tc.margin)
		if (err != nil) != tc.err || got != tc.want {
			t.Fatalf("GasWithMargin(%d, %d) = %d %v", tc.sim, tc.margin, got, err)
		}
	}
}

// TestNewCodecChainID: the chain id is required and bounded.
func TestNewCodecChainID(t *testing.T) {
	if _, err := NewCodec(""); err == nil {
		t.Fatal("empty chain id accepted")
	}
	if _, err := NewCodec(strings.Repeat("a", genesis.MaxChainIDLen+1)); err == nil {
		t.Fatal("oversize chain id accepted")
	}
	if c := testCodec(t); c.ChainID() != testChainID {
		t.Fatalf("chain id %q", c.ChainID())
	}
}
