package signer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/keys"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
)

// signReq is a raw sign request for msg with valid envelope fields.
func signReq(t testing.TB, key string, msg sdk.Msg) request {
	t.Helper()
	value, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg.(proto.Message))
	if err != nil {
		t.Fatal(err)
	}
	return request{Version: ProtocolVersion, Op: OpSign, KeyName: key, ChainID: testChainID, AccountNumber: 3, Sequence: 9,
		Gas: 2_000_000, TimeoutHeight: 140, TypeURL: sdk.MsgTypeURL(msg), Value: value}
}

func wantRefusal(t *testing.T, err error, want policy.Code) {
	t.Helper()
	if got := policy.CodeOf(err); got != want {
		t.Fatalf("error = %v (code %q), want code %q", err, got, want)
	}
}

func wantRaw(t *testing.T, resp response, want policy.Code) {
	t.Helper()
	if got := refusalCode(resp); got != want {
		t.Fatalf("response refusal %q (%+v), want %q", got, resp.Refusal, want)
	}
}

// TestSignerSignsAndClientVerifies is the happy path: each key signs its
// own kinds; the transaction decodes with the application's strict
// decoder to exactly the requested message and envelope; the client
// accepts it only because it rebuilds the same bytes.
func TestSignerSignsAndClientVerifies(t *testing.T) {
	f := newFixture(t)
	_, client := f.run(t)
	ctx := context.Background()

	infos, err := client.Keys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 || infos[0].Name != "service" || infos[0].Class != policy.ClassService ||
		!bytes.Equal(infos[0].Address, f.service.PubKey().Address()) || !infos[1].PubKey.Equals(f.broadcaster.PubKey()) {
		t.Fatalf("keys = %+v", infos)
	}
	codec, err := chain.NewCodec(testChainID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		key string
		msg sdk.Msg
	}{
		{"service", f.offer()},
		{"broadcaster", f.accept()},
		{"service", f.register(t, f.intentPriv, f.service.PubKey().Address())},
		{"service", f.withdraw(t, f.intentPriv, f.service.PubKey().Address())},
	} {
		signed, err := client.RequestSignature(ctx, SignRequest{KeyName: tc.key, AccountNumber: 3, Sequence: 9, Gas: 2_000_000, TimeoutHeight: 140, Msg: tc.msg})
		if err != nil {
			t.Fatalf("%s %T: %v", tc.key, tc.msg, err)
		}
		if signed.Hash != sha256.Sum256(signed.Bytes) {
			t.Fatal("hash is not SHA-256 of the bytes")
		}
		decoded, err := codec.Decode(signed.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		own, err := decoded.Own()
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(own.Msg.Msg.(proto.Message), tc.msg.(proto.Message)) || own.Signer.Sequence != 9 ||
			decoded.Gas != 2_000_000 || decoded.TimeoutHeight != 140 {
			t.Fatalf("%T: decoded transaction differs from the request", tc.msg)
		}
	}
}

// TestSignerRefusesOtherTypeURLs sends the signer, for each key, every
// message type the application registers except the key class's three
// (plan 13.1). Each is refused with TYPE_NOT_ALLOWED, and so are type URLs
// the application does not register.
func TestSignerRefusesOtherTypeURLs(t *testing.T) {
	f := newFixture(t)
	_, _ = f.run(t)
	enc, err := app.MakeEncoding()
	if err != nil {
		t.Fatal(err)
	}
	registered := enc.InterfaceRegistry.ListImplementations(sdk.MsgInterfaceProtoName)
	for _, key := range []struct {
		name  string
		class policy.Class
		addr  []byte
	}{{"service", policy.ClassService, f.service.PubKey().Address()}, {"broadcaster", policy.ClassBroadcaster, f.broadcaster.PubKey().Address()}} {
		refused := 0
		isRegistered := map[string]bool{}
		for _, u := range registered {
			isRegistered[u] = true
		}
		for _, typeURL := range append(registered, "/svn.leases.v1.MsgUnknown", "", "/cosmos.bank.v1beta1.MsgSend") {
			if policy.Allowed(key.class, typeURL) {
				continue
			}
			value := []byte{0x0a, 0x14}
			value = append(value, key.addr...)
			req := request{Version: ProtocolVersion, Op: OpSign, KeyName: key.name, ChainID: testChainID, Gas: 1000,
				TimeoutHeight: 140, TypeURL: typeURL, Value: value}
			resp := rawCall(t, f.cfg.SocketPath, req)
			wantRaw(t, resp, policy.CodeTypeNotAllowed)
			// The refusal is the allowlist's, before the bytes are decoded
			// as the type: a registered type is refused by name.
			if wantDetail := map[bool]string{true: "may not sign", false: "not registered"}[isRegistered[typeURL]]; !strings.Contains(resp.Refusal.Detail, wantDetail) {
				t.Fatalf("%s: refusal detail %q, want %q", typeURL, resp.Refusal.Detail, wantDetail)
			}
			refused++
		}
		if refused < 12 {
			t.Fatalf("%s: only %d type URLs tried; the registry lists %d", key.name, refused, len(registered))
		}
	}
}

func TestSignerRefusesAuthorityEnd(t *testing.T) {
	f := newFixture(t)
	_, client := f.run(t)
	end := &leasesv1.MsgEndLease{Signer: f.broadcaster.PubKey().Address(), BusinessId: "bcast-end-1", LeaseId: "lease-1", AuthorityEnd: true}
	wantRaw(t, rawCall(t, f.cfg.SocketPath, signReq(t, "broadcaster", end)), policy.CodeAuthorityEnd)
	_, err := client.RequestSignature(context.Background(), SignRequest{KeyName: "broadcaster", Gas: 1, TimeoutHeight: 140, Msg: end})
	wantRefusal(t, err, policy.CodeAuthorityEnd)
	end.AuthorityEnd = false
	if _, err := client.RequestSignature(context.Background(), SignRequest{KeyName: "broadcaster", Gas: 1, TimeoutHeight: 140, Msg: end}); err != nil {
		t.Fatalf("provider end: %v", err)
	}
}

// TestClientChecksBeforeAsking pins the client side of plan 7.1 (checked in
// policy and again in the signer): RequestSignature runs
// policy.CheckSignable before it connects, so a type the key's class may
// not sign, an EndLease with authority_end, and a message whose signer is
// not the key are refused without reaching the socket. The client knows
// its keys but no signer listens at its socket path, so a request that
// passes the check fails to dial.
func TestClientChecksBeforeAsking(t *testing.T) {
	f := newFixture(t)
	codec, err := chain.NewCodec(testChainID)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(filepath.Join(filepath.Dir(f.cfg.SocketPath), "absent.sock"), codec, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	service, relay := f.service.PubKey(), f.broadcaster.PubKey()
	client.keys = map[string]KeyInfo{
		"service":     {Name: "service", Class: policy.ClassService, Algorithm: keys.Secp256k1, Address: service.Address(), PubKey: service},
		"broadcaster": {Name: "broadcaster", Class: policy.ClassBroadcaster, Algorithm: keys.Secp256k1, Address: relay.Address(), PubKey: relay},
	}
	relayOffer := f.offer()
	relayOffer.Signer = relay.Address()
	cases := []struct {
		name, key string
		msg       sdk.Msg
		want      policy.Code
	}{
		{"authority end", "broadcaster", &leasesv1.MsgEndLease{Signer: relay.Address(), BusinessId: "bcast-end-1", LeaseId: "lease-1",
			AuthorityEnd: true}, policy.CodeAuthorityEnd},
		{"type the class may not sign", "broadcaster", relayOffer, policy.CodeTypeNotAllowed},
		{"signer is not the key", "service", relayOffer, policy.CodeSignerMismatch},
	}
	for _, tc := range cases {
		_, err := client.RequestSignature(context.Background(), SignRequest{KeyName: tc.key, Gas: 1, TimeoutHeight: 140, Msg: tc.msg})
		if policy.CodeOf(err) != tc.want {
			t.Fatalf("%s: %v, want %s before any connection", tc.name, err, tc.want)
		}
	}
	_, err = client.RequestSignature(context.Background(), SignRequest{KeyName: "service", Gas: 1, TimeoutHeight: 140, Msg: f.offer()})
	if err == nil || policy.CodeOf(err) != "" || !strings.Contains(err.Error(), "signer: dial") {
		t.Fatalf("allowed request: %v, want a dial error (it would have connected)", err)
	}
}

func TestSignerRefusesForeignChainID(t *testing.T) {
	f := newFixture(t)
	_, _ = f.run(t)
	req := signReq(t, "service", f.offer())
	req.ChainID = "shortvideo-dev-ffffffffffffffff"
	wantRaw(t, rawCall(t, f.cfg.SocketPath, req), policy.CodeChainID)
	req.ChainID = ""
	wantRaw(t, rawCall(t, f.cfg.SocketPath, req), policy.CodeChainID)

	// A node of another chain id refuses the start.
	g := newFixture(t)
	g.node.chainID = "shortvideo-dev-ffffffffffffffff"
	if _, err := start(context.Background(), g.cfg, g.node, uint32(os.Geteuid())); !errors.Is(err, chain.ErrChainID) {
		t.Fatalf("start against another chain = %v", err)
	}
	// A client of another chain id builds bytes the signer refuses.
	codec, err := chain.NewCodec("shortvideo-dev-ffffffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	other, err := NewClient(f.cfg.SocketPath, codec, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, err = other.RequestSignature(context.Background(), SignRequest{KeyName: "service", Gas: 1, TimeoutHeight: 140, Msg: f.offer()})
	wantRefusal(t, err, policy.CodeChainID)
}

// fakeClock is a goroutine-safe settable clock.
type fakeClock struct{ ns atomic.Int64 }

func (c *fakeClock) Now() time.Time      { return time.Unix(0, c.ns.Load()) }
func (c *fakeClock) Add(d time.Duration) { c.ns.Add(int64(d)) }
func setClock(s *Server, c *fakeClock) {
	s.heights.mu.Lock()
	defer s.heights.mu.Unlock()
	s.heights.now = c.Now
	s.heights.lastTry, s.heights.readAt, s.heights.height = time.Time{}, time.Time{}, 0
}

// TestSignerRefusesUnboundedTimeout pins plan 7.2 (M3): timeout height 0,
// a timeout height above the signer's own fresh height plus K_max, and
// any request while the signer has no fresh height are refused.
func TestSignerRefusesUnboundedTimeout(t *testing.T) {
	f := newFixture(t)
	f.cfg.MaxTimeoutOffset = 40
	srv, _ := f.run(t)
	clock := &fakeClock{}
	clock.ns.Store(time.Unix(1_900_000_000, 0).UnixNano())
	setClock(srv, clock)

	req := signReq(t, "service", f.offer())
	req.TimeoutHeight = 0
	wantRaw(t, rawCall(t, f.cfg.SocketPath, req), policy.CodeTimeoutZero)
	req.TimeoutHeight = 140 // node height 100 + K_max 40
	if resp := rawCall(t, f.cfg.SocketPath, req); resp.Refusal != nil {
		t.Fatalf("timeout at the bound: %+v", resp.Refusal)
	}
	req.TimeoutHeight = 141
	wantRaw(t, rawCall(t, f.cfg.SocketPath, req), policy.CodeTimeoutTooFar)
	req.TimeoutHeight = 1 << 62
	wantRaw(t, rawCall(t, f.cfg.SocketPath, req), policy.CodeTimeoutTooFar)

	// The node advances: the bound follows the signer's own read after a
	// poll interval, not what serve says.
	f.node.setHeight(150)
	req.TimeoutHeight = 190
	wantRaw(t, rawCall(t, f.cfg.SocketPath, req), policy.CodeTimeoutTooFar)
	clock.Add(f.cfg.PollInterval)
	if resp := rawCall(t, f.cfg.SocketPath, req); resp.Refusal != nil {
		t.Fatalf("timeout at the new bound: %+v", resp.Refusal)
	}

	// The node stops answering: the last height stays usable for the
	// freshness window, then signing stops.
	f.node.setStatusErr(errors.New("connection refused"))
	clock.Add(time.Duration(f.cfg.FreshnessPolls) * f.cfg.PollInterval)
	if resp := rawCall(t, f.cfg.SocketPath, req); resp.Refusal != nil {
		t.Fatalf("within the freshness window: %+v", resp.Refusal)
	}
	clock.Add(time.Millisecond)
	wantRaw(t, rawCall(t, f.cfg.SocketPath, req), policy.CodeNoFreshHeight)
	f.node.setStatusErr(nil)
	clock.Add(f.cfg.PollInterval)
	if resp := rawCall(t, f.cfg.SocketPath, req); resp.Refusal != nil {
		t.Fatalf("after the node answers again: %+v", resp.Refusal)
	}
	// A node that reports another chain or is catching up gives no height.
	g := newFixture(t)
	gs, _ := g.run(t)
	gclock := &fakeClock{}
	setClock(gs, gclock)
	g.node.mu.Lock()
	g.node.chainID = "shortvideo-dev-ffffffffffffffff"
	g.node.mu.Unlock()
	wantRaw(t, rawCall(t, g.cfg.SocketPath, signReq(t, "service", g.offer())), policy.CodeNoFreshHeight)
}

func TestSignerRefusesUnregisteredIntentKey(t *testing.T) {
	f := newFixture(t)
	_, client := f.run(t)
	host := f.service.PubKey().Address()
	for _, msg := range []sdk.Msg{f.register(t, f.strayPriv, host), f.withdraw(t, f.strayPriv, host)} {
		wantRaw(t, rawCall(t, f.cfg.SocketPath, signReq(t, "service", msg)), policy.CodeIntentKeyUnregistered)
		_, err := client.RequestSignature(context.Background(), SignRequest{KeyName: "service", Gas: 1, TimeoutHeight: 140, Msg: msg})
		wantRefusal(t, err, policy.CodeIntentKeyUnregistered)
	}
}

func TestSignerVerifiesIntentSignature(t *testing.T) {
	f := newFixture(t)
	_, _ = f.run(t)
	host := f.service.PubKey().Address()
	if resp := rawCall(t, f.cfg.SocketPath, signReq(t, "service", f.register(t, f.intentPriv, host))); resp.Refusal != nil {
		t.Fatalf("registered intent: %+v", resp.Refusal)
	}
	otherHost := f.register(t, f.intentPriv, f.broadcaster.PubKey().Address())
	wantRaw(t, rawCall(t, f.cfg.SocketPath, signReq(t, "service", otherHost)), policy.CodeIntentSignature)
	tampered := f.register(t, f.intentPriv, host)
	tampered.HostingIntent.Signature[0] ^= 1
	wantRaw(t, rawCall(t, f.cfg.SocketPath, signReq(t, "service", tampered)), policy.CodeIntentSignature)
	changed := f.register(t, f.intentPriv, host)
	changed.PackageBytes++
	wantRaw(t, rawCall(t, f.cfg.SocketPath, signReq(t, "service", changed)), policy.CodeIntentSignature)
	withdraw := f.withdraw(t, f.intentPriv, host)
	withdraw.ReasonDigest = digest("another reason")
	wantRaw(t, rawCall(t, f.cfg.SocketPath, signReq(t, "service", withdraw)), policy.CodeIntentSignature)
}

// TestSignerPeerUID pins plan 7.2: a connection whose SO_PEERCRED uid is
// not the configured serve uid gets PEER_REFUSED and nothing else. The
// test process is the only peer it can be, so the configured uid is moved
// away from it and back; the svn_roottest tests run real distinct uids.
func TestSignerPeerUID(t *testing.T) {
	f := newFixture(t)
	srv, client := f.run(t)
	if _, err := client.Keys(context.Background()); err != nil {
		t.Fatalf("own uid: %v", err)
	}
	srv.peerUID.Store(uint32(os.Geteuid()) + 4242)
	_, err := client.Keys(context.Background())
	wantRefusal(t, err, CodePeerRefused)
	wantRaw(t, rawCall(t, f.cfg.SocketPath, signReq(t, "service", f.offer())), CodePeerRefused)
	srv.peerUID.Store(uint32(os.Geteuid()))
	if _, err := client.Keys(context.Background()); err != nil {
		t.Fatalf("own uid again: %v", err)
	}
}

func TestSignerRefusesMalformedRequests(t *testing.T) {
	f := newFixture(t)
	_, client := f.run(t)
	sock := f.cfg.SocketPath

	unknownKey := signReq(t, "service", f.offer())
	unknownKey.KeyName = "authority"
	wantRaw(t, rawCall(t, sock, unknownKey), CodeUnknownKey)
	_, err := client.RequestSignature(context.Background(), SignRequest{KeyName: "authority", Gas: 1, TimeoutHeight: 140, Msg: f.offer()})
	wantRefusal(t, err, CodeUnknownKey)

	mismatch := f.offer()
	mismatch.Signer = sdksecp.GenPrivKey().PubKey().Address()
	wantRaw(t, rawCall(t, sock, signReq(t, "service", mismatch)), policy.CodeSignerMismatch)
	wrongKey := signReq(t, "broadcaster", f.accept())
	wrongKey.KeyName = "service"
	wantRaw(t, rawCall(t, sock, wrongKey), policy.CodeTypeNotAllowed)

	for _, gas := range []uint64{0, 20_000_001} {
		req := signReq(t, "service", f.offer())
		req.Gas = gas
		wantRaw(t, rawCall(t, sock, req), policy.CodeGasBounds)
	}

	unknownField := signReq(t, "service", f.offer())
	unknownField.Value = append(unknownField.Value, 0xf8, 0x06, 0x01) // field 111, varint 1
	wantRaw(t, rawCall(t, sock, unknownField), policy.CodeInvalid)
	nested := f.accept()
	nestedReq := signReq(t, "broadcaster", nested)
	sigBytes, err := proto.Marshal(nested.GetProviderAcceptance())
	if err != nil {
		t.Fatal(err)
	}
	sigBytes = append(sigBytes, 0xf8, 0x06, 0x01)
	nestedReq.Value = protowire(t, nested, sigBytes)
	wantRaw(t, rawCall(t, sock, nestedReq), policy.CodeInvalid)
	duplicate := signReq(t, "service", f.offer())
	duplicate.Value = append(duplicate.Value, 0x22, 0x01, 'x') // content_id again
	wantRaw(t, rawCall(t, sock, duplicate), policy.CodeInvalid)
	garbage := signReq(t, "service", f.offer())
	garbage.Value = []byte{0xff, 0xff, 0xff}
	wantRaw(t, rawCall(t, sock, garbage), policy.CodeInvalid)
	empty := signReq(t, "service", f.offer())
	empty.Value = nil
	wantRaw(t, rawCall(t, sock, empty), policy.CodeInvalid)

	badVersion := signReq(t, "service", f.offer())
	badVersion.Version = 2
	wantRaw(t, rawCall(t, sock, badVersion), CodeMalformed)
	wantRaw(t, rawCall(t, sock, request{Version: ProtocolVersion, Op: "export-key"}), CodeMalformed)
	wantRaw(t, rawCall(t, sock, map[string]any{"v": 1, "op": "keys", "extra": true}), CodeMalformed)

	for _, frame := range [][]byte{{0, 0, 0, 0}, binary.BigEndian.AppendUint32(nil, MaxRequestBytes+1), append(binary.BigEndian.AppendUint32(nil, 4), []byte("{}{}")...)} {
		conn, err := net.Dial("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(frame); err != nil {
			t.Fatal(err)
		}
		var resp response
		if err := readFrame(conn, &resp, MaxResponseBytes); err != nil {
			t.Fatal(err)
		}
		conn.Close()
		wantRaw(t, resp, CodeMalformed)
	}
}

// protowire re-encodes an accept message with its provider_acceptance
// field (5) replaced by raw bytes.
func protowire(t *testing.T, m *leasesv1.MsgAcceptLease, sig []byte) []byte {
	t.Helper()
	without := proto.Clone(m).(*leasesv1.MsgAcceptLease)
	without.ProviderAcceptance = nil
	out, err := proto.MarshalOptions{Deterministic: true}.Marshal(without)
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, 0x2a, byte(len(sig)))
	return append(out, sig...)
}

// TestSignerStartupRefusals pins the start-up refusals of plan 7.1 and
// 7.2 the unit tests can reach as one uid: configuration bounds and the
// uid rule, keys of the wrong role, unsafe key files, a writable registry,
// an unsafe socket directory, and a foreign file at the socket path.
func TestSignerStartupRefusals(t *testing.T) {
	euid := uint32(os.Geteuid())
	cfgCases := map[string]struct {
		edit func(*fixture)
		want string
	}{
		"chain id without the devnet prefix": {func(f *fixture) { f.cfg.ChainID, f.node.chainID = "shortvideo-1", "shortvideo-1" }, "prefix"},
		"empty chain id":                     {func(f *fixture) { f.cfg.ChainID = "" }, "chain id must be"},
		"remote rpc":                         {func(f *fixture) { f.cfg.RPCURL = "http://10.0.0.1:26657" }, "loopback"},
		"https rpc":                          {func(f *fixture) { f.cfg.RPCURL = "https://127.0.0.1:26657" }, "loopback"},
		"rpc host name":                      {func(f *fixture) { f.cfg.RPCURL = "http://localhost:26657" }, "loopback IP"},
		"rpc timeout":                        {func(f *fixture) { f.cfg.RPCTimeout = 0 }, "rpc timeout"},
		"poll interval":                      {func(f *fixture) { f.cfg.PollInterval = time.Millisecond }, "poll interval"},
		"freshness":                          {func(f *fixture) { f.cfg.FreshnessPolls = 1 }, "freshness"},
		"K_max low":                          {func(f *fixture) { f.cfg.MaxTimeoutOffset = 4 }, "K_max"},
		"K_max high":                         {func(f *fixture) { f.cfg.MaxTimeoutOffset = 1001 }, "K_max"},
		"relative socket":                    {func(f *fixture) { f.cfg.SocketPath = "signer.sock" }, "socket path"},
		"long socket":                        {func(f *fixture) { f.cfg.SocketPath = "/" + strings.Repeat("s", 120) }, "socket path"},
		"relative registry":                  {func(f *fixture) { f.cfg.RegistryPath = "intent-keys.json" }, "registry path"},
		"no keys":                            {func(f *fixture) { f.cfg.Keys = nil }, "one or two keys"},
		"three keys":                         {func(f *fixture) { f.cfg.Keys = append(f.cfg.Keys, f.cfg.Keys[0]) }, "one or two keys"},
		"unknown class":                      {func(f *fixture) { f.cfg.Keys[0].Class = "INTENT" }, "class SERVICE or BROADCASTER"},
		"relative key path":                  {func(f *fixture) { f.cfg.Keys[0].Path = "service.hex" }, "absolute path"},
		"unknown boundary":                   {func(f *fixture) { f.cfg.Boundary = 0 }, "unknown process boundary"},
		"same uid with another serve uid":    {func(f *fixture) { f.cfg.ServeUID = euid + 1 }, "same-uid"},
		"distinct uids with the same uid":    {func(f *fixture) { f.cfg.Boundary, f.cfg.ServeUID = policy.DistinctUIDs, euid }, "distinct uids are configured"},
	}
	for name, tc := range cfgCases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			tc.edit(f)
			if _, err := start(context.Background(), f.cfg, f.node, euid); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("start = %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
	t.Run("uid rule", func(t *testing.T) {
		f := newFixture(t)
		cfg := f.cfg
		cfg.Boundary, cfg.ServeUID = policy.DistinctUIDs, 1001
		if err := cfg.validate(0); err == nil || !strings.Contains(err.Error(), "root") {
			t.Fatalf("distinct uids as root: %v", err)
		}
		if err := cfg.validate(1001); err == nil {
			t.Fatal("distinct uids with the serve uid accepted")
		}
		cfg.ServeUID = 0
		if err := cfg.validate(1002); err == nil {
			t.Fatal("distinct uids with serve as root accepted")
		}
		cfg.ServeUID = 1001
		if err := cfg.validate(1002); err != nil {
			t.Fatalf("distinct uids 1002/1001: %v", err)
		}
	})

	roleCases := map[string]typesv1.SubmitterRole{
		"authority": typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY,
		"auditor":   typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR,
		"provider":  typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER,
		"no role":   typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED,
	}
	for name, role := range roleCases {
		t.Run("service key with role "+name, func(t *testing.T) {
			f := newFixture(t)
			f.node.submitters[0].Role = role
			_, err := start(context.Background(), f.cfg, f.node, euid)
			wantRefusal(t, err, policy.CodeRole)
		})
	}
	t.Run("same key for both classes", func(t *testing.T) {
		f := newFixture(t)
		f.cfg.Keys[1].Path = f.cfg.Keys[0].Path
		_, err := start(context.Background(), f.cfg, f.node, euid)
		wantRefusal(t, err, policy.CodeRole)
	})
	t.Run("duplicate key names", func(t *testing.T) {
		f := newFixture(t)
		f.cfg.Keys[1].Name = "service"
		if _, err := start(context.Background(), f.cfg, f.node, euid); err == nil {
			t.Fatal("two keys named service accepted")
		}
	})
	t.Run("group-readable key file", func(t *testing.T) {
		f := newFixture(t)
		if err := os.Chmod(f.cfg.Keys[0].Path, 0o640); err != nil {
			t.Fatal(err)
		}
		if _, err := start(context.Background(), f.cfg, f.node, euid); err == nil {
			t.Fatal("a group-readable key file accepted")
		}
	})
	t.Run("key directory open to others", func(t *testing.T) {
		f := newFixture(t)
		if err := os.Chmod(f.keyDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := start(context.Background(), f.cfg, f.node, euid); err == nil {
			t.Fatal("a key directory readable by others accepted")
		}
	})
	t.Run("writable registry", func(t *testing.T) {
		f := newFixture(t)
		if err := os.Chmod(f.cfg.RegistryPath, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := start(context.Background(), f.cfg, f.node, euid); !errors.Is(err, policy.ErrRegistryWritable) {
			t.Fatalf("start = %v, want ErrRegistryWritable", err)
		}
	})
	t.Run("socket directory writable by others", func(t *testing.T) {
		f := newFixture(t)
		if err := os.Chmod(filepath.Dir(f.cfg.SocketPath), 0o777); err != nil {
			t.Fatal(err)
		}
		if _, err := start(context.Background(), f.cfg, f.node, euid); err == nil {
			t.Fatal("a world-writable socket directory accepted")
		}
	})
	t.Run("regular file at the socket path", func(t *testing.T) {
		f := newFixture(t)
		if err := os.WriteFile(f.cfg.SocketPath, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := start(context.Background(), f.cfg, f.node, euid); err == nil {
			t.Fatal("a regular file at the socket path was replaced")
		}
	})
	t.Run("stale socket of a killed signer is replaced", func(t *testing.T) {
		f := newFixture(t)
		l, err := net.ListenUnix("unix", &net.UnixAddr{Name: f.cfg.SocketPath, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		l.SetUnlinkOnClose(false)
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
		_, client := f.run(t)
		if _, err := client.Keys(context.Background()); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(f.cfg.SocketPath)
		if err != nil || info.Mode().Perm() != 0o666 {
			t.Fatalf("socket mode = %v, %v", info, err)
		}
	})
	t.Run("node unreachable", func(t *testing.T) {
		f := newFixture(t)
		f.node.statusErr = errors.New("connection refused")
		if _, err := start(context.Background(), f.cfg, f.node, euid); err == nil {
			t.Fatal("start without a node accepted")
		}
	})
}

// TestClientRejectsAlteredTransaction runs the client against a signer
// stand-in that answers with a transaction for another request (another
// sequence, another message, a wrong hash): the client must refuse each.
func TestClientRejectsAlteredTransaction(t *testing.T) {
	f := newFixture(t)
	srv, _ := f.run(t)
	sock := filepath.Join(filepath.Dir(f.cfg.SocketPath), "standin.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var mode atomic.Int32
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			var req request
			if err := readFrame(conn, &req, MaxRequestBytes); err != nil {
				conn.Close()
				continue
			}
			var resp response
			switch req.Op {
			case OpKeys:
				resp.Keys = srv.keyBodies()
			case OpSign:
				switch mode.Load() {
				case 0:
					req.Sequence++
				case 1:
					other := f.offer()
					other.OfferId = "offer-2"
					value, _ := proto.MarshalOptions{Deterministic: true}.Marshal(other)
					req.Value = value
				}
				raw, hash, err := srv.sign(context.Background(), req)
				if err != nil {
					resp.Refusal = &refusalBody{Code: CodeInternal}
					break
				}
				resp.TxBytes, resp.TxHash = raw, hash[:]
				if mode.Load() == 2 {
					resp.TxHash = make([]byte, 32)
				}
			}
			_ = writeFrame(conn, resp, MaxResponseBytes)
			conn.Close()
		}
	}()
	codec, err := chain.NewCodec(testChainID)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(sock, codec, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for m := int32(0); m < 3; m++ {
		mode.Store(m)
		_, err := client.RequestSignature(context.Background(), SignRequest{KeyName: "service", AccountNumber: 3, Sequence: 9, Gas: 2_000_000, TimeoutHeight: 140, Msg: f.offer()})
		if err == nil || policy.CodeOf(err) != "" {
			t.Fatalf("mode %d: client accepted or refused for another reason: %v", m, err)
		}
	}
	l.Close()
	wg.Wait()
}

func TestHeightSourceFreshness(t *testing.T) {
	node := &fakeNode{chainID: testChainID, height: 10}
	codec, err := chain.NewCodec(testChainID)
	if err != nil {
		t.Fatal(err)
	}
	client, err := chain.New(node, codec)
	if err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{}
	h := newHeightSource(client, time.Second, 4*time.Second)
	h.now = clock.Now
	ctx := context.Background()
	if got, ok := h.fresh(ctx); !ok || got != 10 || node.statusCalls() != 1 {
		t.Fatalf("first read: %d %v calls %d", got, ok, node.statusCalls())
	}
	node.setHeight(11)
	clock.Add(999 * time.Millisecond)
	if got, _ := h.fresh(ctx); got != 10 || node.statusCalls() != 1 {
		t.Fatalf("within the poll interval the node was read again: %d calls %d", got, node.statusCalls())
	}
	clock.Add(time.Millisecond)
	if got, _ := h.fresh(ctx); got != 11 || node.statusCalls() != 2 {
		t.Fatalf("after the poll interval: %d calls %d", got, node.statusCalls())
	}
	node.setStatusErr(errors.New("down"))
	clock.Add(4 * time.Second)
	if got, ok := h.fresh(ctx); !ok || got != 11 {
		t.Fatalf("at the freshness edge: %d %v", got, ok)
	}
	clock.Add(time.Second)
	if _, ok := h.fresh(ctx); ok {
		t.Fatal("a height older than the freshness window is fresh")
	}
	node.setStatusErr(nil)
	node.setHeight(0)
	clock.Add(time.Second)
	if _, ok := h.fresh(ctx); ok {
		t.Fatal("a node without a committed block gave a fresh height")
	}
}

// TestSignerLogsNoSecrets checks the signer's own log lines: a signed and
// a refused request leave no key, message or transaction bytes in the
// log, only names, codes and the hash.
func TestSignerLogsNoSecrets(t *testing.T) {
	f := newFixture(t)
	var buf syncBuffer
	f.cfg.Logger = newTestLogger(&buf)
	_, client := f.run(t)
	signed, err := client.RequestSignature(context.Background(), SignRequest{KeyName: "service", Gas: 1, TimeoutHeight: 140, Msg: f.offer()})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = client.RequestSignature(context.Background(), SignRequest{KeyName: "service", Gas: 1, TimeoutHeight: 140, Msg: f.register(t, f.strayPriv, f.service.PubKey().Address())})
	out := buf.String()
	for name, secret := range map[string][]byte{"service key": f.service.Bytes(), "broadcaster key": f.broadcaster.Bytes(), "tx bytes": signed.Bytes} {
		if strings.Contains(out, string(secret)) || containsHexOrBase64(out, secret) {
			t.Fatalf("log carries the %s", name)
		}
	}
	if !strings.Contains(out, "signer signed") || !strings.Contains(out, string(policy.CodeIntentKeyUnregistered)) {
		t.Fatalf("log lacks the expected records: %s", out)
	}
}
