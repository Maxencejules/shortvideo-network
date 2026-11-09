package policy

// Test fakes and fixtures. fakeChain is an in-process implementation of
// chain.RPC for unit tests only: it answers the gRPC-routed queries the
// policy reads (Provider, ActiveLeases, Deadlines, Governance, Account)
// from in-memory state, so the real internal/chain client and its height
// and shape checks run. It is a fake; it is never end-to-end evidence.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	"github.com/cometbft/cometbft/p2p"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
)

const testChainID = "shortvideo-dev-00112233aabbccdd"

// sdkMsg shortens sdk.Msg in test tables.
type sdkMsg = sdk.Msg

// fakeAccount is one auth account of the fake chain.
type fakeAccount struct {
	number, sequence uint64
}

// fakeChain is the in-memory chain state behind the fake RPC.
type fakeChain struct {
	mu         sync.Mutex
	height     int64
	time       time.Time
	providers  map[string]*providersv1.Provider
	leases     []*leasesv1.Lease
	offers     []*leasesv1.Offer
	submitters []*typesv1.Submitter
	seats      []*authorityv1.DevnetTestSeat
	accounts   map[string]fakeAccount
	codec      *chain.Codec
	// pageSize caps the entries of one Deadlines or Governance page, so
	// tests reach the page budget with few entries.
	pageSize int
	// failPath makes every query of that path fail with a transport error.
	failPath string
	calls    map[string]int
}

func newFakeChain(t testing.TB) *fakeChain {
	t.Helper()
	codec, err := chain.NewCodec(testChainID)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeChain{
		height: 100, time: time.Unix(1_900_000_000, 0).UTC(), providers: map[string]*providersv1.Provider{},
		accounts: map[string]fakeAccount{}, pageSize: 100, calls: map[string]int{}, codec: codec,
	}
}

func (f *fakeChain) client(t testing.TB) *chain.Client {
	t.Helper()
	c, err := chain.New(f, f.codec)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f *fakeChain) Status(context.Context) (*coretypes.ResultStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &coretypes.ResultStatus{
		NodeInfo: p2p.DefaultNodeInfo{Network: testChainID},
		SyncInfo: coretypes.SyncInfo{LatestBlockHeight: f.height, LatestBlockTime: f.time},
	}, nil
}

func (f *fakeChain) BroadcastTxSync(context.Context, cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	return nil, errors.New("fake chain: broadcast is not used by policy tests")
}

func (f *fakeChain) Tx(context.Context, []byte, bool) (*coretypes.ResultTx, error) {
	return nil, errors.New("fake chain: tx is not used by policy tests")
}

func (f *fakeChain) TxSearch(context.Context, string, bool, *int, *int, string) (*coretypes.ResultTxSearch, error) {
	return nil, errors.New("fake chain: tx_search is not used by policy tests")
}

func (f *fakeChain) Commit(context.Context, *int64) (*coretypes.ResultCommit, error) {
	return nil, errors.New("fake chain: commit is not used by policy tests")
}

// answer is a found response, or not found when resp is nil.
func (f *fakeChain) answer(resp proto.Message) (*coretypes.ResultABCIQuery, error) {
	if resp == nil {
		return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{
			Code: sdkerrors.ErrKeyNotFound.ABCICode(), Codespace: sdkerrors.ErrKeyNotFound.Codespace(), Height: f.height,
		}}, nil
	}
	value, err := proto.MarshalOptions{Deterministic: true}.Marshal(resp)
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{Value: value, Height: f.height}}, nil
}

// pageStart decodes a fake page key (a decimal index).
func pageStart(key []byte) int {
	if len(key) == 0 {
		return 0
	}
	n, err := strconv.Atoi(string(key))
	if err != nil {
		return -1
	}
	return n
}

func (f *fakeChain) ABCIQueryWithOptions(_ context.Context, path string, data cmtbytes.HexBytes, _ rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[path]++
	if path == f.failPath {
		return nil, errors.New("fake chain: connection refused")
	}
	switch path {
	case "/svn.providers.v1.Query/Provider":
		var req providersv1.QueryProviderRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		p, ok := f.providers[req.GetProviderId()]
		if !ok {
			return f.answer(nil)
		}
		return f.answer(&providersv1.QueryProviderResponse{Provider: p})
	case "/svn.leases.v1.Query/ActiveLeases":
		var req leasesv1.QueryActiveLeasesRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		resp := &leasesv1.QueryActiveLeasesResponse{}
		for _, l := range f.leases {
			if l.GetProviderId() == req.GetProviderId() && l.GetContentId() == req.GetContentId() {
				resp.Leases = append(resp.Leases, l)
			}
		}
		return f.answer(resp)
	case "/svn.leases.v1.Query/Deadlines":
		var req leasesv1.QueryDeadlinesRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		start := pageStart(req.GetPage().GetPageKey())
		if start < 0 || start > len(f.offers) {
			return nil, errors.New("fake chain: bad page key")
		}
		end := min(start+min(f.pageSize, int(req.GetPage().GetLimit())), len(f.offers))
		resp := &leasesv1.QueryDeadlinesResponse{Offers: f.offers[start:end], Page: &typesv1.PageResponse{}}
		if end < len(f.offers) {
			resp.Page.NextPageKey = []byte(strconv.Itoa(end))
		}
		return f.answer(resp)
	case "/svn.authority.v1.Query/Governance":
		var req authorityv1.QueryGovernanceRequest
		if err := proto.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		start := pageStart(req.GetPage().GetPageKey())
		total := len(f.submitters) + len(f.seats)
		if start < 0 || start > total {
			return nil, errors.New("fake chain: bad page key")
		}
		end := min(start+min(f.pageSize, int(req.GetPage().GetLimit())), total)
		state := &authorityv1.AuthorityState{}
		for i := start; i < end; i++ {
			if i < len(f.submitters) {
				state.Submitters = append(state.Submitters, f.submitters[i])
			} else {
				state.DevnetTestSeats = append(state.DevnetTestSeats, f.seats[i-len(f.submitters)])
			}
		}
		resp := &authorityv1.QueryGovernanceResponse{State: state, Page: &typesv1.PageResponse{}}
		if end < total {
			resp.Page.NextPageKey = []byte(strconv.Itoa(end))
		}
		return f.answer(resp)
	case "/cosmos.auth.v1beta1.Query/Account":
		var req authtypes.QueryAccountRequest
		if err := gogoproto.Unmarshal(data, &req); err != nil {
			return nil, err
		}
		enc, err := app.MakeEncoding()
		if err != nil {
			return nil, err
		}
		for addr, acct := range f.accounts {
			bech, err := f.codec.Bech32([]byte(addr))
			if err != nil || bech != req.Address {
				continue
			}
			base := authtypes.NewBaseAccount([]byte(addr), nil, acct.number, acct.sequence)
			packed, err := codectypes.NewAnyWithValue(base)
			if err != nil {
				return nil, err
			}
			value, err := enc.Codec.Marshal(&authtypes.QueryAccountResponse{Account: packed})
			if err != nil {
				return nil, err
			}
			return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{Value: value, Height: f.height}}, nil
		}
		return f.answer(nil)
	}
	return nil, fmt.Errorf("fake chain: no handler for %s", path)
}

// innerKey is a provider-style ed25519 signing key of a test.
type innerKey struct {
	id   string
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

func newInnerKey(t testing.TB, id string) innerKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return innerKey{id: id, pub: pub, priv: priv}
}

// innerSig signs chain-built sign bytes (domain || 0x00 || canonical) as
// an InnerSignature of domain d.
func (k innerKey) innerSig(d typesv1.SignatureDomain, signBytes []byte) *typesv1.InnerSignature {
	return &typesv1.InnerSignature{
		Domain: d, PublicKey: append([]byte(nil), k.pub...), Signature: ed25519.Sign(k.priv, signBytes),
		KeyId: k.id, CanonicalProfileVersion: msgkit.CanonicalProfileVersion,
	}
}

func digest(label string) *typesv1.Digest {
	sum := sha256.Sum256([]byte(label))
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sum[:]}
}

func addr(label string) []byte {
	sum := sha256.Sum256([]byte("addr:" + label))
	return sum[:20]
}

// world is the fixture of the relay and forward tests: two admitted
// providers, provider-1 rotated once (its first key is retired), one open
// offer for provider-1 behind filler offers, and one held lease of
// provider-1. The provider-1 account key is a secp256k1 account key that
// signs forwarded transactions in the forward tests.
type world struct {
	chain                   *fakeChain
	p1, p1Old, p2           innerKey
	service, broadcasterKey innerKey
	random                  innerKey
	p1Account               cryptotypes.PrivKey
	p2Account               cryptotypes.PrivKey
	scheduler               []byte
	offer                   *leasesv1.Offer
	lease                   *leasesv1.Lease
}

const (
	worldOfferID   = "offer-1"
	worldLeaseID   = "lease-new"
	worldHeldLease = "lease-held"
	worldContent   = "content-1"
	worldContent2  = "content-2"
	p1AccountNum   = 7
	p1AccountSeq   = 3
)

func newWorld(t testing.TB) *world {
	t.Helper()
	w := &world{
		chain: newFakeChain(t),
		p1:    newInnerKey(t, "p1-key-2"), p1Old: newInnerKey(t, "p1-key-1"), p2: newInnerKey(t, "p2-key-1"),
		service: newInnerKey(t, "p1-key-2"), broadcasterKey: newInnerKey(t, "p1-key-2"), random: newInnerKey(t, "p1-key-2"),
		p1Account: sdksecp.GenPrivKey(), p2Account: sdksecp.GenPrivKey(), scheduler: addr("service"),
	}
	f := w.chain
	f.providers["provider-1"] = &providersv1.Provider{
		ProviderId: "provider-1", Account: w.p1Account.PubKey().Address(), SigningPubkey: w.p1.pub, SigningKeyId: w.p1.id,
		KeyGeneration: 2, State: providersv1.ProviderState_PROVIDER_STATE_ADMITTED, AdmittedCapacityBytes: 1 << 30,
		PreviousSigningKeys: []*providersv1.PreviousSigningKey{{PublicKey: w.p1Old.pub, Generation: 1}},
	}
	f.providers["provider-2"] = &providersv1.Provider{
		ProviderId: "provider-2", Account: w.p2Account.PubKey().Address(), SigningPubkey: w.p2.pub, SigningKeyId: w.p2.id,
		KeyGeneration: 1, State: providersv1.ProviderState_PROVIDER_STATE_ADMITTED, AdmittedCapacityBytes: 1 << 30,
	}
	expiry := f.time.Unix() + 3600
	for i := 0; i < 5; i++ {
		f.offers = append(f.offers, &leasesv1.Offer{OfferId: fmt.Sprintf("filler-%d", i), ContentId: worldContent2, ContentVersion: 1,
			ProviderId: "provider-2", ManifestDigest: digest("m2"), PackageBytes: 10, TermsDigest: digest("t2"),
			StartUnixSeconds: f.time.Unix(), DeadlineUnixSeconds: f.time.Unix() + 60 + int64(i), ExpiryUnixSeconds: expiry,
			TermVersion: 1, Scheduler: w.scheduler})
	}
	w.offer = &leasesv1.Offer{OfferId: worldOfferID, ContentId: worldContent, ContentVersion: 1, ProviderId: "provider-1",
		ManifestDigest: digest("m1"), PackageBytes: 1000, TermsDigest: digest("t1"), StartUnixSeconds: f.time.Unix(),
		DeadlineUnixSeconds: f.time.Unix() + 600, ExpiryUnixSeconds: expiry, TermVersion: 1, Scheduler: w.scheduler}
	f.offers = append(f.offers, w.offer)
	w.lease = &leasesv1.Lease{LeaseId: worldHeldLease, OfferId: "offer-0", ContentId: worldContent2, ContentVersion: 1,
		ProviderId: "provider-1", ManifestDigest: digest("m2"), PackageBytes: 500, TermsDigest: digest("t0"),
		StartUnixSeconds: f.time.Unix() - 100, DeadlineUnixSeconds: f.time.Unix() + 300,
		State: leasesv1.LeaseState_LEASE_STATE_ACTIVE, TermVersion: 1, ReservationHeld: true}
	f.leases = append(f.leases, w.lease)
	f.submitters = []*typesv1.Submitter{
		{Address: w.p1Account.PubKey().Address(), Role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER},
		{Address: w.p2Account.PubKey().Address(), Role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER},
	}
	f.accounts[string(w.p1Account.PubKey().Address())] = fakeAccount{number: p1AccountNum, sequence: p1AccountSeq}
	f.accounts[string(w.p2Account.PubKey().Address())] = fakeAccount{number: 8, sequence: 0}
	return w
}

// acceptMsg is a relay acceptance of the world's offer for leaseID signed
// by k (a BROADCASTER outer signer).
func (w *world) acceptMsg(t testing.TB, k innerKey, leaseID string) *leasesv1.MsgAcceptLease {
	t.Helper()
	sb, err := leases.AcceptSignBytes(w.offer, leaseID)
	if err != nil {
		t.Fatal(err)
	}
	return &leasesv1.MsgAcceptLease{Signer: addr("broadcaster"), BusinessId: "bcast-accept-1", OfferId: worldOfferID, LeaseId: leaseID,
		ProviderAcceptance: k.innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, sb)}
}

// renewMsg is a renewal of the held lease signed by k.
func (w *world) renewMsg(t testing.TB, k innerKey) *leasesv1.MsgRenewLease {
	t.Helper()
	m := &leasesv1.MsgRenewLease{Signer: addr("broadcaster"), BusinessId: "bcast-renew-1", LeaseId: worldHeldLease, TermVersion: 2,
		DeadlineUnixSeconds: w.lease.GetDeadlineUnixSeconds() + 3600, ManifestDigest: w.lease.GetManifestDigest(), PackageBytes: w.lease.GetPackageBytes()}
	sb, err := leases.RenewSignBytes(m.GetLeaseId(), m.GetTermVersion(), m.GetDeadlineUnixSeconds(), m.GetManifestDigest(), m.GetPackageBytes(), m.GetEvidenceId())
	if err != nil {
		t.Fatal(err)
	}
	m.ProviderSignature = k.innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, sb)
	return m
}

// endMsg is a provider end of the held lease signed by k for termVersion.
func (w *world) endMsg(t testing.TB, k innerKey, termVersion uint64) *leasesv1.MsgEndLease {
	t.Helper()
	sb, err := leases.EndSignBytes(worldHeldLease, "provider-1", termVersion)
	if err != nil {
		t.Fatal(err)
	}
	return &leasesv1.MsgEndLease{Signer: addr("broadcaster"), BusinessId: "bcast-end-1", LeaseId: worldHeldLease,
		ProviderSignature: k.innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END, sb)}
}

// relayInput is the RelayInput of msg with the world's lookup hints.
func (w *world) relayInput(msg sdk.Msg) RelayInput {
	in := RelayInput{Msg: msg, MinHeight: w.chain.height, ChainTime: w.chain.time, DeadlinePages: 10}
	switch msg.(type) {
	case *leasesv1.MsgRenewLease, *leasesv1.MsgEndLease:
		in.ProviderID, in.ContentID = "provider-1", worldContent2
	}
	return in
}
