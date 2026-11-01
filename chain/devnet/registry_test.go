package devnet_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"

	"cosmossdk.io/log/v2"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers/capability"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// TestDevnetRegistryDemo is the Goal 07 real-transaction demo. It runs four
// svnd validator processes on one machine (loopback peers). It is not an
// independent-operator network and not a production test. Every success is
// a committed transaction (broadcast_tx_commit, DeliverTx code 0); every
// committed rejection is a DeliverTx failure code; CheckTx rejections are
// printed separately and are never included in a block. Queries read
// committed state, and an in-process replay of the fetched blocks must
// reproduce the nodes' app hashes.
func TestDevnetRegistryDemo(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip()
	}
	fmt.Println("label=single-machine four-process devnet (not independent operators)")
	home := t.TempDir()
	layout, err := devnet.Init(devnet.Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	stops := make([]func() error, 4)
	for i := range stops {
		stop, err := launch(layout.Topology.Validators[i].Home)
		if err != nil {
			t.Fatal(err)
		}
		stops[i] = stop
	}
	t.Cleanup(func() {
		for _, stop := range stops {
			if stop != nil {
				_ = stop()
			}
		}
	})
	rpcs := rpcList(layout)
	if _, _, err := devnet.WaitEqual(rpcs, 3, 45*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	d := newDemo(t, layout, home)
	fmt.Printf("phase=start height=%d chain_id=%s accounts=%d\n", d.height(), layout.Topology.ChainID, len(layout.Topology.Accounts))

	// Phase 3: committed successes.
	d.commit("register-p1", d.provider1, d.registerProvider(d.provider1, "reg-p1", "p1", capacityOne))
	d.commit("register-p2", d.provider2, d.registerProvider(d.provider2, "reg-p2", "p2", capacityOne))
	d.council("admit-p1", "prop-admit-p1", "admit-p1", admitChange("p1"))
	d.council("admit-p2", "prop-admit-p2", "admit-p2", admitChange("p2"))
	d.commit("update-p1", d.provider1, d.updateProvider(d.provider1, "upd-p1-1", "p1", capacityTwo, "u1"))
	next := newKey("p1-key-2")
	rotation := d.commit("rotate-p1", d.provider1, d.rotate(d.provider1, "rot-p1-1", "p1", 1, next))
	d.keys["p1"] = next
	d.commit("drain-p2", d.provider2, d.drain(d.provider2, "drain-p2", "p2", 1, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN))
	d.council("disable-p2", "prop-disable-p2", "disable-p2", disableChange("p2"))
	d.commit("register-c1", d.service, d.registerContent(d.service, "reg-c1", "c1", 1, sizeOne))
	d.commit("register-c2", d.service, d.registerContent(d.service, "reg-c2", "c2", 1, sizeTwo))
	beforeWithdraw := d.height()
	d.commit("withdraw-c1", d.service, d.withdrawContent(d.service, "wd-c1", "c1", 1, reason("c1")))
	d.council("force-withdraw-c2", "prop-withdraw-c2", "withdraw-c2", withdrawChange("c2", 1, reason("c2")))
	c1Before := d.abciQuery(d.clients[0], contentPath, &contentv1.QueryContentRequest{ContentId: "c1", ContentVersion: 1}, 0)
	d.commit("idempotent-replay-c1", d.service, d.registerContent(d.service, "reg-c1", "c1", 1, sizeOne))
	if !bytes.Equal(c1Before, d.abciQuery(d.clients[0], contentPath, &contentv1.QueryContentRequest{ContentId: "c1", ContentVersion: 1}, 0)) {
		t.Fatal("the idempotent replay changed the c1 record")
	}

	// Phase 4: committed rejections (DeliverTx failure codes).
	d.reject("cross-owner-update", d.provider2, d.updateProvider(d.provider2, "x-upd-1", "p1", capacityTwo, "x"), msgkit.Forbidden)
	d.reject("service-updates-provider", d.service, d.updateProvider(d.service, "x-upd-2", "p1", capacityTwo, "x"), msgkit.Forbidden)
	d.reject("duplicate-version-c1", d.service, d.registerContent(d.service, "x-c1", "c1", 1, sizeOne), msgkit.DuplicateID)
	short := d.registerProvider(d.provider1, "x-reg-1", "p3", capacityOne)
	short.EndpointManifestDigest.Value = short.EndpointManifestDigest.Value[:31]
	d.reject("digest-31-bytes", d.provider1, short, msgkit.Bounds)
	noAlg := d.registerProvider(d.provider1, "x-reg-2", "p3", capacityOne)
	noAlg.EndpointManifestDigest.Algorithm = typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED
	d.reject("digest-algorithm-0", d.provider1, noAlg, msgkit.Malformed)
	d.reject("provider-id-65-bytes", d.provider1, d.registerProvider(d.provider1, "x-reg-3", strings.Repeat("p", 65), capacityOne), msgkit.Bounds)
	d.reject("package-bytes-cap-plus-1", d.service, d.registerContent(d.service, "x-c3", "c3", 1, genesis.MaxPackageBytes+1), msgkit.Bounds)
	d.reject("replayed-business-id", d.service, d.registerContent(d.service, "reg-c1", "c3", 1, sizeOne), msgkit.Replay)
	d.reject("cross-owner-withdraw", d.service2, d.withdrawContent(d.service2, "x-wd-1", "c1", 1, reason("x")), msgkit.Forbidden)
	d.reject("second-withdraw", d.service, d.withdrawContent(d.service, "x-wd-2", "c1", 1, reason("x")), msgkit.Withdrawn)
	d.reject("stale-rotation", d.provider1, d.rotate(d.provider1, "x-rot-1", "p1", 1, newKey("p1-key-3")), msgkit.StaleVersion)
	d.reject("wrong-domain-drain", d.provider1, d.drain(d.provider1, "x-drain-1", "p1", 2, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE), msgkit.WrongDomain)

	// CheckTx rejections: never included in a block.
	outsider := loaded{name: "outsider", key: sdksecp.GenPrivKey()}
	outsider.addr = hex.EncodeToString(outsider.key.PubKey().Address())
	d.notIncluded("outsider", signDevnetWith(t, d.enc, d.chainID, outsider.key, 0, 0, 2_000_000, nil, d.registerProvider(outsider, "x-out", "p9", capacityOne)))
	d.notIncluded("fee", signDevnetWith(t, d.enc, d.chainID, d.provider1.key, d.numbers[d.provider1.addr], d.seq[d.provider1.addr], 2_000_000,
		sdk.NewCoins(sdk.NewInt64Coin("reject", 1)), d.registerProvider(d.provider1, "x-fee", "p9", capacityOne)))
	big := d.registerProvider(d.provider1, "x-big", "p9", capacityOne)
	big.OperatorGroupId = strings.Repeat("g", 70*1024)
	d.notIncluded("tx-70-KiB", signDevnetWith(t, d.enc, d.chainID, d.provider1.key, d.numbers[d.provider1.addr], d.seq[d.provider1.addr], 2_000_000, nil, big))
	d.rebroadcast("same-signed-bytes")

	// Phase 5: queries from committed state at one fixed height.
	fixed := d.height()
	if _, _, err := devnet.WaitEqual(rpcs, fixed+1, 30*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	queries := d.registryQueries(fixed)
	for i, client := range d.clients {
		for name, req := range queries {
			raw := d.abciQuery(client, req.path, req.msg, fixed)
			if i == 0 {
				req.want = raw
				queries[name] = req
				continue
			}
			if !bytes.Equal(req.want, raw) {
				t.Fatalf("query %s at %d differs on node %d", name, fixed, i+1)
			}
		}
	}
	for _, name := range sortedKeys(queries) {
		fmt.Printf("phase=query height=%d name=%s nodes=4 equal=true bytes=%d sha256=%x\n", fixed, name, len(queries[name].want), sha256.Sum256(queries[name].want))
	}
	d.checkQueryValues(queries, beforeWithdraw)
	grpcBytes := d.grpcProvider(layout.Topology.Validators[0].GRPC, "p1", fixed)
	if !bytes.Equal(grpcBytes, queries["provider-p1"].want) {
		t.Fatal("gRPC Provider bytes differ from node v1 ABCI query bytes")
	}
	fmt.Printf("phase=grpc node=v1 height=%d path=/svn.providers.v1.Query/Provider equal_to_abci=true\n", fixed)
	for h := rotation - 1; h <= rotation+cometDelay(); h++ {
		set := validatorSet(t, rpcs[0], h)
		if len(set) != 4 || !sameValidators(set, validatorSet(t, rpcs[0], 1)) {
			t.Fatalf("validator set changed at %d around the provider rotation", h)
		}
	}
	fmt.Printf("phase=validators rotation_height=%d from=%d to=%d count=4 unchanged=true\n", rotation, rotation-1, rotation+cometDelay())

	// Phase 6: replay the committed blocks into an in-process application.
	replayed := d.replay(layout.Genesis, fixed)
	for name, req := range queries {
		got := replayQuery(t, replayed, req.path, req.msg, fixed)
		if !bytes.Equal(got, req.want) {
			t.Fatalf("replayed query %s differs from the node", name)
		}
	}
	fmt.Printf("phase=replay-queries height=%d count=%d equal=true\n", fixed, len(queries))

	// Phase 7: events from every committed block.
	d.scanEvents(fixed)
}

const (
	capacityOne = uint64(1 << 30)
	capacityTwo = uint64(3 << 29)
	sizeOne     = uint64(4_000_000)
	sizeTwo     = uint64(9_000_000)
	contentPath = "/svn.content.v1.Query/Content"
)

// Synthetic off-chain inputs with canaries. Only digests reach a tx.
var (
	demoEndpoint = "https://canary-endpoint.invalid/segments/canary-7f3a"
	demoEmail    = "creator-canary@example.invalid"
	demoUser     = "canary_user_7f3a"
	demoWatch    = "watch_event:canary-viewer-7f3a:segment-0003"
	demoReport   = "complaint:canary-narrative-7f3a"
	demoMedia    = bytes.Repeat([]byte("CANARY-MEDIA-PAYLOAD-7f3a-"), 4096/26+1)[:4096]
)

func cometDelay() int64 { return authority.CometEffectiveDelay }

type ed25519Key struct {
	id   string
	priv ed25519.PrivateKey
}

func (k ed25519Key) pub() []byte { return []byte(k.priv.Public().(ed25519.PublicKey)) }

func newKey(id string) ed25519Key {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}
	return ed25519Key{id: id, priv: priv}
}

type demo struct {
	t         *testing.T
	enc       app.Encoding
	chainID   string
	clients   []*rpchttp.HTTP
	numbers   map[string]uint64
	seq       map[string]uint64
	authority loaded
	service   loaded
	service2  loaded
	provider1 loaded
	provider2 loaded
	seats     []loaded
	keys      map[string]ed25519Key
	intent    ed25519Key
	policy    uint64
	included  []string
	rejected  map[int64]string
}

func newDemo(t *testing.T, layout devnet.Layout, home string) *demo {
	t.Helper()
	enc := mustEncoding(t)
	d := &demo{
		t:         t,
		enc:       enc,
		chainID:   layout.Topology.ChainID,
		numbers:   genesisAccountNumbers(t, enc, layout.Genesis),
		seq:       map[string]uint64{},
		authority: loadSecp(t, home, "authority"),
		service:   loadSecp(t, home, "service"),
		service2:  loadSecp(t, home, "service-2"),
		provider1: loadSecp(t, home, "provider-1"),
		provider2: loadSecp(t, home, "provider-2"),
		keys:      map[string]ed25519Key{},
		intent:    newKey("intent-1"),
		policy:    1,
		rejected:  map[int64]string{},
	}
	for i := 1; i <= 4; i++ {
		d.seats = append(d.seats, loadEd(t, home, fmt.Sprintf("seat-%d", i)))
	}
	for _, node := range layout.Topology.Validators {
		client, err := rpchttp.New(node.RPC, "/websocket")
		if err != nil {
			t.Fatal(err)
		}
		d.clients = append(d.clients, client)
	}
	roles := map[string]string{}
	for _, account := range layout.Topology.Accounts {
		roles[account.Name] = account.Role
	}
	if roles["provider-1"] != genesis.RoleAdmittedProvider || roles["provider-2"] != genesis.RoleAdmittedProvider || roles["service-2"] != genesis.RoleAdmittedService {
		t.Fatalf("topology accounts %v", layout.Topology.Accounts)
	}
	return d
}

func (d *demo) height() int64 {
	d.t.Helper()
	status, err := d.clients[0].Status(context.Background())
	if err != nil {
		d.t.Fatal(err)
	}
	return status.SyncInfo.LatestBlockHeight
}

// broadcast sends raw with broadcast_tx_commit to node v1.
func (d *demo) broadcast(raw []byte) (*cmtResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := d.clients[0].BroadcastTxCommit(ctx, cmttypes.Tx(raw))
	if err != nil {
		return nil, err
	}
	return &cmtResult{check: res.CheckTx, deliver: res.TxResult, hash: res.Hash, height: res.Height}, nil
}

type cmtResult struct {
	check   abci.ResponseCheckTx
	deliver abci.ExecTxResult
	hash    []byte
	height  int64
}

func (d *demo) send(who loaded, msg sdk.Msg) *cmtResult {
	d.t.Helper()
	raw := signDevnet(d.t, d.enc, d.chainID, who.key, d.numbers[who.addr], d.seq[who.addr], msg)
	res, err := d.broadcast(raw)
	if err != nil {
		d.t.Fatalf("%s: %v", who.name, err)
	}
	if res.check.Code != 0 {
		d.t.Fatalf("%s: check tx %s/%d %s", who.name, res.check.Codespace, res.check.Code, res.check.Log)
	}
	d.seq[who.addr]++
	d.included = append(d.included, hex.EncodeToString(res.hash))
	return res
}

// commit asserts a committed success and returns its height.
func (d *demo) commit(phase string, who loaded, msg sdk.Msg) int64 {
	d.t.Helper()
	res := d.send(who, msg)
	if res.deliver.Code != 0 {
		d.t.Fatalf("phase %s: deliver %s/%d %s", phase, res.deliver.Codespace, res.deliver.Code, res.deliver.Log)
	}
	fmt.Printf("phase=%s height=%d code=0 txhash=%X\n", phase, res.height, res.hash)
	return res.height
}

// reject asserts a committed DeliverTx failure with the protocol code.
func (d *demo) reject(phase string, who loaded, msg sdk.Msg, want msgkit.Code) {
	d.t.Helper()
	res := d.send(who, msg)
	number := uint32(typesv1.ErrorCode_value[string(want)])
	if res.deliver.Codespace != "svn" || res.deliver.Code != number {
		d.t.Fatalf("phase %s: deliver %s/%d want svn/%d (%s) log %s", phase, res.deliver.Codespace, res.deliver.Code, number, want, res.deliver.Log)
	}
	d.rejected[res.height] = phase
	fmt.Printf("phase=reject name=%s height=%d codespace=svn code=%d (%s) included=true txhash=%X\n", phase, res.height, number, want, res.hash)
}

// notIncluded asserts a CheckTx rejection and that no block holds the tx.
func (d *demo) notIncluded(phase string, raw []byte) {
	d.t.Helper()
	res, err := d.broadcast(raw)
	if err != nil {
		d.t.Fatalf("phase %s: %v", phase, err)
	}
	if res.check.Code == 0 {
		d.t.Fatalf("phase %s passed CheckTx", phase)
	}
	sum := sha256.Sum256(raw)
	if _, err := d.clients[0].Tx(context.Background(), sum[:], false); err == nil {
		d.t.Fatalf("phase %s: the rejected tx is in a block", phase)
	}
	fmt.Printf("phase=checktx-reject name=%s codespace=%s code=%d included=false (not included)\n", phase, res.check.Codespace, res.check.Code)
}

// rebroadcast sends the signed bytes of the first included tx again. The
// mempool cache or the sequence check refuses them, and the original stays
// the only inclusion.
func (d *demo) rebroadcast(phase string) {
	d.t.Helper()
	first, err := hex.DecodeString(d.included[0])
	if err != nil {
		d.t.Fatal(err)
	}
	original, err := d.clients[0].Tx(context.Background(), first, false)
	if err != nil {
		d.t.Fatal(err)
	}
	res, err := d.broadcast(original.Tx)
	outcome := ""
	switch {
	case err != nil:
		outcome = "rpc-error"
	case res.check.Code != 0:
		outcome = fmt.Sprintf("checktx %s/%d", res.check.Codespace, res.check.Code)
	default:
		d.t.Fatalf("phase %s: the same signed bytes passed CheckTx", phase)
	}
	again, err := d.clients[0].Tx(context.Background(), first, false)
	if err != nil || again.Height != original.Height {
		d.t.Fatalf("phase %s: inclusion moved from %d", phase, original.Height)
	}
	fmt.Printf("phase=checktx-reject name=%s result=%q original_height=%d included_again=false (not included)\n", phase, outcome, original.Height)
}

func (d *demo) registerProvider(who loaded, bid, providerID string, capacity uint64) *providersv1.MsgRegisterProvider {
	d.t.Helper()
	manifestID := providerID
	if !msgkit.ValidID(providerID) {
		manifestID = "invalid-id"
	}
	key, ok := d.keys[providerID]
	if !ok {
		key = newKey(manifestID + "-key-1")
		d.keys[providerID] = key
	}
	return &providersv1.MsgRegisterProvider{
		Signer:                 who.key.PubKey().Address(),
		BusinessId:             bid,
		ProviderId:             providerID,
		SigningPubkey:          key.pub(),
		SigningKeyId:           key.id,
		EndpointManifestDigest: d.capability(manifestID, key.id, capacity, "r"),
		DeclaredCapacityBytes:  capacity,
		OperatorGroupId:        providers.DevnetOperatorGroup,
	}
}

func (d *demo) updateProvider(who loaded, bid, providerID string, capacity uint64, salt string) *providersv1.MsgUpdateProvider {
	d.t.Helper()
	return &providersv1.MsgUpdateProvider{
		Signer:                 who.key.PubKey().Address(),
		BusinessId:             bid,
		ProviderId:             providerID,
		EndpointManifestDigest: d.capability(providerID, d.keys[providerID].id, capacity, salt),
		DeclaredCapacityBytes:  capacity,
	}
}

// capability builds the off-chain capability manifest and returns only its
// digest. The endpoint URL never leaves this function.
func (d *demo) capability(providerID, keyID string, capacity uint64, salt string) *typesv1.Digest {
	d.t.Helper()
	result, err := capability.Encode(capability.Manifest{
		ProviderID:            providerID,
		SigningKeyID:          keyID,
		DeclaredCapacityBytes: capacity,
		Endpoints:             []capability.Endpoint{{Name: "segments-" + salt, URL: demoEndpoint + "/" + salt}},
	})
	if err != nil {
		d.t.Fatal(err)
	}
	return result.Digest()
}

func signature(domain typesv1.SignatureDomain, key ed25519Key, signBytes []byte) *typesv1.InnerSignature {
	return &typesv1.InnerSignature{
		Domain:                  domain,
		PublicKey:               key.pub(),
		Signature:               ed25519.Sign(key.priv, signBytes),
		KeyId:                   key.id,
		CanonicalProfileVersion: 1,
	}
}

func (d *demo) rotate(who loaded, bid, providerID string, expected uint64, next ed25519Key) *providersv1.MsgRotateProviderKey {
	d.t.Helper()
	signBytes, err := providers.RotateSignBytes(providerID, next.id, next.pub(), expected)
	if err != nil {
		d.t.Fatal(err)
	}
	return &providersv1.MsgRotateProviderKey{
		Signer:              who.key.PubKey().Address(),
		BusinessId:          bid,
		ProviderId:          providerID,
		NewSigningPubkey:    next.pub(),
		NewSigningKeyId:     next.id,
		ExpectedGeneration:  expected,
		CurrentKeySignature: signature(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE, d.keys[providerID], signBytes),
	}
}

// drain signs the drain payload. Any domain other than PROVIDER_DRAIN
// signs the same canonical payload under that domain's string.
func (d *demo) drain(who loaded, bid, providerID string, generation uint64, domain typesv1.SignatureDomain) *providersv1.MsgDrainProvider {
	d.t.Helper()
	signBytes, err := providers.DrainSignBytes(providerID, generation)
	if err != nil {
		d.t.Fatal(err)
	}
	if domain != typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN {
		name, _ := msgkit.DomainString(domain)
		signBytes = append([]byte(name), signBytes[bytes.IndexByte(signBytes, 0):]...)
	}
	return &providersv1.MsgDrainProvider{
		Signer:            who.key.PubKey().Address(),
		BusinessId:        bid,
		ProviderId:        providerID,
		ProviderSignature: signature(domain, d.keys[providerID], signBytes),
	}
}

// packageManifest is a synthetic off-chain package manifest holding media
// bytes, an email, a username, and a watch event. Only its hash is used.
func packageManifest(contentID string) []byte {
	var buf bytes.Buffer
	buf.WriteString(`{"content":"` + contentID + `","creator":"` + demoEmail + `","user":"` + demoUser + `","watch":"` + demoWatch + `","media":"`)
	buf.Write(demoMedia)
	buf.WriteString(`"}`)
	return buf.Bytes()
}

func reason(label string) []byte {
	sum := sha256.Sum256([]byte(demoReport + ":" + label))
	return sum[:]
}

func sha256Digest(value []byte) *typesv1.Digest {
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: append([]byte(nil), value...)}
}

func (d *demo) registerContent(who loaded, bid, contentID string, version, size uint64) *contentv1.MsgRegisterContent {
	d.t.Helper()
	sum := sha256.Sum256(packageManifest(contentID))
	signer := who.key.PubKey().Address()
	signBytes, err := content.RegisterIntentSignBytes(signer, contentID, version, sum[:], size, content.DevnetRoleName)
	if err != nil {
		d.t.Fatal(err)
	}
	return &contentv1.MsgRegisterContent{
		Signer:         signer,
		BusinessId:     bid,
		ContentId:      contentID,
		ContentVersion: version,
		ManifestDigest: sha256Digest(sum[:]),
		PackageBytes:   size,
		HostingIntent:  signature(typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT, d.intent, signBytes),
	}
}

func (d *demo) withdrawContent(who loaded, bid, contentID string, version uint64, why []byte) *contentv1.MsgWithdrawContent {
	d.t.Helper()
	signer := who.key.PubKey().Address()
	signBytes, err := content.WithdrawIntentSignBytes(signer, contentID, version, why, content.DevnetRoleName)
	if err != nil {
		d.t.Fatal(err)
	}
	return &contentv1.MsgWithdrawContent{
		Signer:         signer,
		BusinessId:     bid,
		ContentId:      contentID,
		ContentVersion: version,
		ReasonDigest:   sha256Digest(why),
		HostingIntent:  signature(typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT, d.intent, signBytes),
	}
}

func admitChange(id string) *authorityv1.ProposalPayload {
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_AdmitProvider{AdmitProvider: &authorityv1.AdmitProviderChange{ProviderId: id}}}
}

func disableChange(id string) *authorityv1.ProposalPayload {
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_DisableProvider{DisableProvider: &authorityv1.DisableProviderChange{ProviderId: id}}}
}

func withdrawChange(id string, version uint64, why []byte) *authorityv1.ProposalPayload {
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ForceWithdraw{ForceWithdraw: &authorityv1.ForceWithdrawChange{
		ContentId: id, ContentVersion: version, ReasonDigest: sha256Digest(why),
	}}}
}

// council proposes (seat-1), approves (seats 1-3), and executes (authority)
// one payload. Each executed proposal advances base_policy_version by one.
func (d *demo) council(phase, id, parameterKey string, payload *authorityv1.ProposalPayload) {
	d.t.Helper()
	digest, out := authority.PayloadDigest(payload)
	if !out.Success() {
		d.t.Fatal(out.Code)
	}
	d.commit(phase+"-propose", d.seats[0], &authorityv1.MsgProposeChange{
		Signer: d.seats[0].key.PubKey().Address(), BusinessId: "pb-" + id, ProposalId: id,
		ParameterKey: parameterKey, BasePolicyVersion: d.policy, Payload: payload,
		PayloadDigest:     sha256Digest(digest),
		ExpiryUnixSeconds: time.Now().Add(24 * time.Hour).Unix(),
	})
	for i := 0; i < 3; i++ {
		seat := d.seats[i]
		d.commit(fmt.Sprintf("%s-approve-%d", phase, i+1), seat, &authorityv1.MsgApproveChange{
			Signer: seat.key.PubKey().Address(), BusinessId: fmt.Sprintf("%s-ap-%d", id, i+1), ProposalId: id,
		})
	}
	res := d.send(d.authority, &authorityv1.MsgExecuteChange{Signer: d.authority.key.PubKey().Address(), BusinessId: "ex-" + id, ProposalId: id})
	if res.deliver.Code != 0 {
		d.t.Fatalf("phase %s execute: %s/%d %s", phase, res.deliver.Codespace, res.deliver.Code, res.deliver.Log)
	}
	for _, ev := range res.deliver.Events {
		if ev.Type == "authority_result" {
			d.t.Fatalf("phase %s execute was kept: %v", phase, ev.Attributes)
		}
	}
	d.policy++
	fmt.Printf("phase=%s height=%d code=0 txhash=%X policy_version=%d\n", phase, res.height, res.hash, d.policy)
}

type namedQuery struct {
	path string
	msg  proto.Message
	want []byte
}

func (d *demo) registryQueries(height int64) map[string]namedQuery {
	d.t.Helper()
	out := map[string]namedQuery{
		"provider-p1": {path: "/svn.providers.v1.Query/Provider", msg: &providersv1.QueryProviderRequest{ProviderId: "p1"}},
		"provider-p2": {path: "/svn.providers.v1.Query/Provider", msg: &providersv1.QueryProviderRequest{ProviderId: "p2"}},
		"content-c1":  {path: contentPath, msg: &contentv1.QueryContentRequest{ContentId: "c1", ContentVersion: 1}},
		"content-c2":  {path: contentPath, msg: &contentv1.QueryContentRequest{ContentId: "c2", ContentVersion: 1}},
	}
	// Providers with limit 1: two pages, the last with an empty next key.
	var key []byte
	for page := 1; ; page++ {
		req := &providersv1.QueryProvidersRequest{Page: &typesv1.PageRequest{Limit: 1, PageKey: key}}
		raw := d.abciQuery(d.clients[0], "/svn.providers.v1.Query/Providers", req, height)
		var res providersv1.QueryProvidersResponse
		if err := proto.Unmarshal(raw, &res); err != nil {
			d.t.Fatal(err)
		}
		if len(res.GetProviders()) != 1 {
			d.t.Fatalf("Providers page %d has %d providers", page, len(res.GetProviders()))
		}
		out[fmt.Sprintf("providers-page-%d", page)] = namedQuery{path: "/svn.providers.v1.Query/Providers", msg: req}
		key = res.GetPage().GetNextPageKey()
		fmt.Printf("phase=page height=%d page=%d provider=%s next_page_key=%q\n", height, page, res.GetProviders()[0].GetProviderId(), key)
		if len(key) == 0 {
			if page != 2 {
				d.t.Fatalf("Providers with limit 1 returned %d pages, want 2", page)
			}
			return out
		}
		if page > 2 {
			d.t.Fatal("Providers did not end after two pages")
		}
	}
}

func (d *demo) checkQueryValues(queries map[string]namedQuery, beforeWithdraw int64) {
	d.t.Helper()
	var p1, p2 providersv1.QueryProviderResponse
	if err := proto.Unmarshal(queries["provider-p1"].want, &p1); err != nil {
		d.t.Fatal(err)
	}
	if err := proto.Unmarshal(queries["provider-p2"].want, &p2); err != nil {
		d.t.Fatal(err)
	}
	if p1.GetProvider().GetState() != providersv1.ProviderState_PROVIDER_STATE_ADMITTED || p1.GetProvider().GetKeyGeneration() != 2 ||
		p1.GetProvider().GetAdmittedCapacityBytes() != capacityTwo || p2.GetProvider().GetState() != providersv1.ProviderState_PROVIDER_STATE_DISABLED {
		d.t.Fatalf("provider records p1 %v p2 %v", p1.GetProvider(), p2.GetProvider())
	}
	for _, id := range []string{"c1", "c2"} {
		var c contentv1.QueryContentResponse
		if err := proto.Unmarshal(queries["content-"+id].want, &c); err != nil {
			d.t.Fatal(err)
		}
		if c.GetContent().GetState() != contentv1.ContentState_CONTENT_STATE_WITHDRAWN || c.GetTombstone() == nil || c.GetTombstone().GetHistoryErased() {
			d.t.Fatalf("content %s: %v", id, &c)
		}
	}
	var earlier contentv1.QueryContentResponse
	if err := proto.Unmarshal(d.abciQuery(d.clients[0], contentPath, &contentv1.QueryContentRequest{ContentId: "c1", ContentVersion: 1}, beforeWithdraw), &earlier); err != nil {
		d.t.Fatal(err)
	}
	if earlier.GetContent().GetState() != contentv1.ContentState_CONTENT_STATE_PENDING || earlier.GetTombstone() != nil {
		d.t.Fatalf("c1 at height %d: %v", beforeWithdraw, &earlier)
	}
	fmt.Printf("phase=history height=%d c1=PENDING tombstone=false; height=latest c1=WITHDRAWN tombstone=true\n", beforeWithdraw)
}

func (d *demo) abciQuery(client *rpchttp.HTTP, path string, req proto.Message, height int64) []byte {
	d.t.Helper()
	data, err := proto.Marshal(req)
	if err != nil {
		d.t.Fatal(err)
	}
	res, err := client.ABCIQueryWithOptions(context.Background(), path, data, rpcclient.ABCIQueryOptions{Height: height})
	if err != nil {
		d.t.Fatal(err)
	}
	if res.Response.Code != 0 {
		d.t.Fatalf("%s at %d: %s/%d %s", path, height, res.Response.Codespace, res.Response.Code, res.Response.Log)
	}
	if height != 0 && res.Response.Height != height {
		d.t.Fatalf("%s answered at height %d, asked %d", path, res.Response.Height, height)
	}
	return res.Response.Value
}

// grpcProvider calls svn.providers.v1.Query/Provider on the node's gRPC
// server at height and returns the response in deterministic protobuf.
func (d *demo) grpcProvider(address, id string, height int64) []byte {
	d.t.Helper()
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		d.t.Fatal(err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(ctx, grpctypes.GRPCBlockHeightHeader, strconv.FormatInt(height, 10))
	var header metadata.MD
	res := &providersv1.QueryProviderResponse{}
	if err := conn.Invoke(ctx, "/svn.providers.v1.Query/Provider", &providersv1.QueryProviderRequest{ProviderId: id}, res, grpc.Header(&header)); err != nil {
		d.t.Fatal(err)
	}
	if got := header.Get(grpctypes.GRPCBlockHeightHeader); len(got) != 1 || got[0] != strconv.FormatInt(height, 10) {
		d.t.Fatalf("gRPC answered at height %v, asked %d", got, height)
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(res)
	if err != nil {
		d.t.Fatal(err)
	}
	return raw
}

func sameValidators(a, b []voted) bool {
	if len(a) != len(b) {
		return false
	}
	for _, row := range a {
		if !hasPower(b, row.pub, row.power) {
			return false
		}
	}
	return true
}

// replay rebuilds the chain in process: InitChain from the on-disk genesis
// as CometBFT sends it, then FinalizeBlock and Commit for blocks 1..to as
// fetched from node v1. After each block the replayed app hash must equal
// the app hash the nodes committed for it (header app_hash of the next
// block). A block holding one committed rejection must leave the full
// non-account state unchanged.
func (d *demo) replay(genesisPath string, to int64) *app.App {
	d.t.Helper()
	genDoc, err := cmttypes.GenesisDocFromFile(genesisPath)
	if err != nil {
		d.t.Fatal(err)
	}
	replayed, err := app.New(app.Config{Logger: log.NewNopLogger(), DB: dbm.NewMemDB(), ChainID: genDoc.ChainID})
	if err != nil {
		d.t.Fatal(err)
	}
	validators := make([]*cmttypes.Validator, len(genDoc.Validators))
	for i, val := range genDoc.Validators {
		validators[i] = cmttypes.NewValidator(val.PubKey, val.Power)
	}
	params := genDoc.ConsensusParams.ToProto()
	if _, err := replayed.InitChain(&abci.RequestInitChain{
		Time:            genDoc.GenesisTime,
		ChainId:         genDoc.ChainID,
		InitialHeight:   genDoc.InitialHeight,
		ConsensusParams: &params,
		Validators:      cmttypes.TM2PB.ValidatorUpdates(cmttypes.NewValidatorSet(validators)),
		AppStateBytes:   genDoc.AppState,
	}); err != nil {
		d.t.Fatal(err)
	}
	previous := replayed.FullSnapshot()
	ctx := context.Background()
	for h := int64(1); h <= to; h++ {
		height := h
		block, err := d.clients[0].Block(ctx, &height)
		if err != nil {
			d.t.Fatal(err)
		}
		b := block.Block
		if _, err := replayed.FinalizeBlock(&abci.RequestFinalizeBlock{
			Hash:               b.Hash(),
			NextValidatorsHash: b.NextValidatorsHash,
			ProposerAddress:    b.ProposerAddress,
			Height:             b.Height,
			Time:               b.Time,
			Misbehavior:        b.Evidence.Evidence.ToABCI(),
			Txs:                b.Txs.ToSliceOfBytes(),
		}); err != nil {
			d.t.Fatalf("replay height %d: %v", h, err)
		}
		if _, err := replayed.Commit(); err != nil {
			d.t.Fatal(err)
		}
		nextHeight := h + 1
		next, err := d.clients[0].Block(ctx, &nextHeight)
		if err != nil {
			d.t.Fatal(err)
		}
		if got := replayed.LastCommitID().Hash; !bytes.Equal(got, next.Block.AppHash) {
			d.t.Fatalf("replay height %d: app hash %X, node %X", h, got, []byte(next.Block.AppHash))
		}
		snapshot := replayed.FullSnapshot()
		if name, ok := d.rejected[h]; ok {
			if len(b.Txs) != 1 {
				d.t.Fatalf("rejection %s shares block %d with %d txs", name, h, len(b.Txs))
			}
			if !bytes.Equal(previous, snapshot) {
				d.t.Fatalf("rejection %s at %d changed the full state", name, h)
			}
			fmt.Printf("phase=replay-reject name=%s height=%d full_state_unchanged=true\n", name, h)
		}
		previous = snapshot
	}
	final := replayed.LastCommitID().Hash
	nextHeight := to + 1
	for i, client := range d.clients {
		next, err := client.Block(ctx, &nextHeight)
		if err != nil {
			d.t.Fatal(err)
		}
		if !bytes.Equal(final, next.Block.AppHash) {
			d.t.Fatalf("node %d header app_hash at %d %X, replay %X", i+1, nextHeight, []byte(next.Block.AppHash), final)
		}
	}
	fmt.Printf("phase=replay height=%d app_hash=%X nodes=4 equal=true\n", to, final)
	return replayed
}

func replayQuery(t *testing.T, replayed *app.App, path string, req proto.Message, height int64) []byte {
	t.Helper()
	data, err := proto.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	res, err := replayed.Query(context.Background(), &abci.RequestQuery{Path: path, Data: data, Height: height})
	if err != nil {
		t.Fatal(err)
	}
	if res.Code != 0 {
		t.Fatalf("replayed %s: %s/%d %s", path, res.Codespace, res.Code, res.Log)
	}
	return res.Value
}

// sdkEventNames are SDK ante event keys that contain a forbidden name
// pattern. They are framework attributes; no fee is ever charged.
var sdkEventNames = map[string]bool{"fee": true, "fee_payer": true}

// scanEvents reads every committed block's results and checks that no
// canary, no URL, and no forbidden field name appears.
func (d *demo) scanEvents(to int64) {
	d.t.Helper()
	patterns, exemptions := msgkittest.ForbiddenNamePatterns(d.t)
	canaries := [][]byte{
		[]byte(demoEndpoint), []byte("canary-endpoint.invalid"), []byte(demoEmail), []byte(demoUser),
		[]byte(demoWatch), []byte(demoReport), demoMedia, demoMedia[:64],
	}
	var events, typed int
	for h := int64(1); h <= to; h++ {
		height := h
		res, err := d.clients[0].BlockResults(context.Background(), &height)
		if err != nil {
			d.t.Fatal(err)
		}
		var all []abci.Event
		for _, tx := range res.TxsResults {
			all = append(all, tx.Events...)
		}
		all = append(all, res.FinalizeBlockEvents...)
		for _, ev := range all {
			events++
			if strings.HasPrefix(ev.Type, "svn.events.v1.") {
				typed++
			}
			var blob []byte
			blob = append(blob, ev.Type...)
			if hits := msgkittest.NameHits(ev.Type, patterns, exemptions); len(hits) != 0 {
				d.t.Fatalf("height %d event type %s matches %v", h, ev.Type, hits)
			}
			for _, attr := range ev.Attributes {
				blob = append(blob, attr.Key...)
				blob = append(blob, 0)
				blob = append(blob, attr.Value...)
				blob = append(blob, 0)
				hits := msgkittest.NameHits(attr.Key, patterns, exemptions)
				if len(hits) == 0 {
					continue
				}
				if ev.Type == sdk.EventTypeTx && sdkEventNames[attr.Key] {
					if attr.Key == "fee" && attr.Value != "" {
						d.t.Fatalf("height %d fee %q", h, attr.Value)
					}
					continue
				}
				d.t.Fatalf("height %d event %s key %s matches %v", h, ev.Type, attr.Key, hits)
			}
			if hit, found := msgkittest.FindCanary(blob, canaries...); found {
				d.t.Fatalf("height %d event %s holds %s", h, ev.Type, hit)
			}
			if bytes.Contains(blob, []byte("https://")) || bytes.Contains(blob, []byte("http://")) {
				d.t.Fatalf("height %d event %s holds a URL", h, ev.Type)
			}
		}
	}
	if typed == 0 {
		d.t.Fatal("no typed registry event was committed")
	}
	fmt.Printf("phase=events heights=1..%d events=%d typed=%d canaries=0 forbidden_names=0\n", to, events, typed)
}

func sortedKeys(m map[string]namedQuery) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// signDevnetWith signs one message with an explicit gas limit and fee.
func signDevnetWith(t *testing.T, enc app.Encoding, chainID string, key cryptotypes.PrivKey, number, sequence, gas uint64, fee sdk.Coins, msg sdk.Msg) []byte {
	t.Helper()
	builder := enc.TxConfig.NewTxBuilder()
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
	sig, err := tx.SignWithPrivKey(context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT, signer, builder, key, enc.TxConfig, sequence)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.SetSignatures(sig); err != nil {
		t.Fatal(err)
	}
	raw, err := enc.TxConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
