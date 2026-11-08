package devnet_test

// Phases S (setup), H (happy path) and L (log canary) of
// TestDevnetBroadcasterLifecycle, and the helpers phases share.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/client/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"google.golang.org/protobuf/proto"

	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// bcPI is what phase S prepares for phase PI: provider p1's renewal and
// end signatures over the PI lease, made with its first key before the
// rotation.
type bcPI struct {
	contentID, offerID, leaseID string
	deadline, renewDeadline     int64
	firstKey                    ed25519Key
	oldRenew, oldEnd            []byte
}

// phaseSetup is phase S: providers p1 and p2 (and p3, the small provider of
// phase A) register and the council admits them; p1 signs one renewal and
// one end with its first key and then rotates its key. The callers,
// caller SQL role and intent-key registry were configured before serve
// started.
func phaseSetup(n *bnet, name string) {
	d := n.d
	d.commit(name+"-register-p1", d.provider1, d.registerProvider(d.provider1, "reg-p1", "p1", capacityOne))
	d.commit(name+"-register-p2", d.provider2, d.registerProvider(d.provider2, "reg-p2", "p2", capacityOne))
	d.commit(name+"-register-p3", d.provider2, d.registerProvider(d.provider2, "reg-p3", "p3", capacitySmall))
	for _, id := range []string{"p1", "p2", "p3"} {
		d.council(name+"-admit-"+id, "prop-admit-"+id, "admit-"+id, admitChange(id))
	}
	now := d.chainTime()
	pi := &bcPI{contentID: "cpi", offerID: "opi", leaseID: "lpi", deadline: now + devnetTerm, renewDeadline: now + 2*devnetTerm,
		firstKey: d.keys["p1"]}
	pi.oldRenew = n.renewSig(pi.leaseID, pi.contentID, sizeOne, 2, pi.renewDeadline, pi.firstKey).GetSignature()
	pi.oldEnd = n.endSig(pi.leaseID, "p1", 1, pi.firstKey).GetSignature()
	next := newKey("p1-key-2")
	d.commit(name+"-rotate-p1", d.provider1, d.rotate(d.provider1, "rot-p1-1", "p1", 1, next))
	d.keys["p1"] = next
	n.pi = pi
	n.providers = []string{"p1", "p2", "p3"}
	for _, id := range []string{"p1", "p2", "p3"} {
		p := n.provider(id)
		if p.GetState() != providersv1.ProviderState_PROVIDER_STATE_ADMITTED {
			n.t.Fatalf("phase %s: provider %s is %s", name, id, p.GetState())
		}
	}
	if gen := n.provider("p1").GetKeyGeneration(); gen != 2 {
		n.t.Fatalf("phase %s: p1 key generation %d after the rotation, want 2", name, gen)
	}
	n.d.syncSequence(n.service)
	n.d.syncSequence(n.bcast)
	fmt.Printf("phase=%s providers=p1,p2,p3 state=ADMITTED p1_key_generation=2 pre_rotation_signatures=renew,end service_account_number=%d broadcaster_account_number=%d service_sequence=%d broadcaster_sequence=%d\n",
		name, d.numbers[n.service.addr], d.numbers[n.bcast.addr], d.seq[n.service.addr], d.seq[n.bcast.addr])
}

// provider reads one provider record from validator 0.
func (n *bnet) provider(id string) *providersv1.Provider {
	n.t.Helper()
	var res providersv1.QueryProviderResponse
	if err := proto.Unmarshal(n.d.abciQuery(n.d.clients[0], providerPath, &providersv1.QueryProviderRequest{ProviderId: id}, 0), &res); err != nil {
		n.t.Fatal(err)
	}
	return res.GetProvider()
}

// terms returns an offer deadline and expiry from the latest block time.
func (n *bnet) terms() (deadline, expiry int64) {
	now := n.d.chainTime()
	return now + devnetTerm, now + 3000
}

// ownContent registers contentIDs through the API as caller (principal
// creator-1) and waits until each is COMMITTED and its ownership row is
// EFFECTIVE.
func (n *bnet) ownContent(phase, caller string, size uint64, contentIDs ...string) {
	n.t.Helper()
	var ids []string
	for _, c := range contentIDs {
		ids = append(ids, n.create(caller, n.registerReq(creatorOne, c, size)).IntentID)
	}
	for i, id := range ids {
		n.committed(phase, id)
		bcWait(n.t, "ownership of "+contentIDs[i], 30*time.Second, func() bool {
			owners := n.contentOwners(contentIDs[i])
			return len(owners) == 1 && owners[0] == "EFFECTIVE:"+caller
		}, n.serve)
	}
}

// offerCommitted creates an OFFER_LEASE intent for offer as caller and
// waits for its commit.
func (n *bnet) offerCommitted(phase, caller string, offer *leasesv1.MsgOfferLease) {
	n.t.Helper()
	n.committed(phase, n.create(caller, n.offerReq(creatorOne, offer)).IntentID)
}

// leaseCommitted relays p's acceptance of offer as leaseID with its
// current key and waits for the commit.
func (n *bnet) leaseCommitted(phase string, offer *leasesv1.MsgOfferLease, leaseID string) {
	n.t.Helper()
	sig := n.acceptSig(offer, leaseID, n.d.keys[offer.GetProviderId()])
	n.committed(phase, n.create(callerApp, n.acceptReq(creatorOne, offer, leaseID, sig)).IntentID)
}

// signTimeout signs msg from who at who's next sequence with a timeout
// height, as a provider agent signs a transaction for verbatim forwarding.
func (n *bnet) signTimeout(who loaded, timeoutHeight uint64, msg sdk.Msg) []byte {
	n.t.Helper()
	d := n.d
	builder := d.enc.TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		n.t.Fatal(err)
	}
	builder.SetGasLimit(2_000_000)
	builder.SetTimeoutHeight(timeoutHeight)
	sequence := d.seq[who.addr]
	signer := authsigning.SignerData{
		Address: sdk.AccAddress(who.key.PubKey().Address()).String(), ChainID: d.chainID,
		AccountNumber: d.numbers[who.addr], Sequence: sequence, PubKey: who.key.PubKey(),
	}
	placeholder := signingtypes.SignatureV2{PubKey: who.key.PubKey(), Sequence: sequence,
		Data: &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT}}
	if err := builder.SetSignatures(placeholder); err != nil {
		n.t.Fatal(err)
	}
	sig, err := tx.SignWithPrivKey(context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT, signer, builder, who.key, d.enc.TxConfig, sequence)
	if err != nil {
		n.t.Fatal(err)
	}
	if err := builder.SetSignatures(sig); err != nil {
		n.t.Fatal(err)
	}
	raw, err := d.enc.TxConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		n.t.Fatal(err)
	}
	return raw
}

// checkEvidence requires an intent's API commitment to equal the chain's
// /tx and /commit answers (T11 evidence).
func (n *bnet) checkEvidence(phase string, v bcView) {
	n.t.Helper()
	c := v.Commitment
	hash := n.hashOf(c.FirstCommitTxHash)
	res := n.committedTx(hash, 30*time.Second)
	h := c.Height
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	commit, err := n.d.clients[0].Commit(ctx, &h)
	cancel()
	if err != nil {
		n.t.Fatal(err)
	}
	if res.Height != c.Height || res.TxResult.Code != 0 || int64(res.Index) != int64(c.TxIndex) || c.ChainID != n.layout.Topology.ChainID ||
		!strings.EqualFold(hex.EncodeToString(commit.SignedHeader.Commit.BlockID.Hash), c.BlockHash) {
		n.t.Fatalf("phase %s: commitment %+v disagrees with /tx (height %d index %d code %d) or /commit (block %X)",
			phase, c, res.Height, res.Index, res.TxResult.Code, commit.SignedHeader.Commit.BlockID.Hash)
	}
	fmt.Printf("phase=%s committed intent=%s kind=%s tx=%s height=%d block=%s own=%t evidence=/tx,/commit\n",
		phase, v.IntentID, v.Kind, c.FirstCommitTxHash, c.Height, c.BlockHash, c.OwnTransaction)
}

// phaseHappy is phase H: one intent per kind through the API, and
// REGISTER_CONTENT and OFFER_LEASE also through the SQL path, each enqueued
// in one transaction with an application row.
func phaseHappy(n *bnet, name string) {
	d := n.d
	register := n.create(callerApp, n.registerReq(creatorOne, "ch1", sizeOne))
	v := n.committed(name, register.IntentID)
	n.checkEvidence(name, v)
	bcWait(n.t, "ownership of ch1", 30*time.Second, func() bool {
		owners := n.contentOwners("ch1")
		return len(owners) == 1 && owners[0] == "EFFECTIVE:"+callerApp
	}, n.serve)
	n.ownContent(name, callerApp, sizeOne, "ch2")

	deadline, expiry := n.terms()
	offer := n.offerOf("oh1", "ch1", "p1", sizeOne, deadline, expiry)
	v = n.committed(name, n.create(callerApp, n.offerReq(creatorOne, offer)).IntentID)
	n.checkEvidence(name, v)
	v = n.committed(name, n.create(callerApp, n.acceptReq(creatorOne, offer, "lh1", n.acceptSig(offer, "lh1", d.keys["p1"]))).IntentID)
	n.checkEvidence(name, v)
	renewDeadline := deadline + devnetTerm
	v = n.committed(name, n.create(callerApp, n.renewReq(creatorOne, "lh1", "p1", "ch1", sizeOne, 2, renewDeadline,
		n.renewSig("lh1", "ch1", sizeOne, 2, renewDeadline, d.keys["p1"]))).IntentID)
	n.checkEvidence(name, v)
	v = n.committed(name, n.create(callerApp, n.endReq(creatorOne, "lh1", "p1", "ch1", n.endSig("lh1", "p1", 2, d.keys["p1"]))).IntentID)
	n.checkEvidence(name, v)
	v = n.committed(name, n.create(callerApp, n.withdrawReq(creatorOne, "ch1")).IntentID)
	n.checkEvidence(name, v)

	// FORWARD_PROVIDER_TX: p1's own account signs its acceptance with a
	// short timeout height; the broadcaster forwards the bytes unchanged.
	offer2 := n.offerOf("oh2", "ch2", "p1", sizeOne, deadline, expiry)
	n.offerCommitted(name, callerApp, offer2)
	d.syncSequence(d.provider1)
	accept := d.acceptMsg(d.provider1, "fw-oh2", offer2, "lh2", d.keys["p1"])
	raw := n.signTimeout(d.provider1, uint64(d.height()+30), accept)
	v = n.committed(name, n.create(callerApp, n.forwardReq(creatorOne, raw, "p1", "ch2", "lh2")).IntentID)
	if !bytes.Equal(n.hashOf(v.Commitment.FirstCommitTxHash), sha256Of(raw)) {
		n.t.Fatalf("phase %s: the forward committed %s, not the provider's bytes %X", name, v.Commitment.FirstCommitTxHash, sha256Of(raw))
	}
	n.checkEvidence(name, v)

	// The SQL path: the application row and the intent commit together.
	sqlRegister := n.registerReq(creatorOne, "chs1", sizeOne)
	id, created := n.sqlEnqueue("upload-chs1", sqlRegister, true)
	if !created {
		n.t.Fatalf("phase %s: the SQL enqueue of chs1 found an existing intent", name)
	}
	v = n.committed(name, id)
	n.checkEvidence(name, v)
	bcWait(n.t, "ownership of chs1", 30*time.Second, func() bool {
		owners := n.contentOwners("chs1")
		return len(owners) == 1 && owners[0] == "EFFECTIVE:"+callerApp
	}, n.serve)
	sqlOffer := n.offerOf("ohs1", "chs1", "p2", sizeOne, deadline, expiry)
	id, _ = n.sqlEnqueue("upload-ohs1", n.offerReq(creatorOne, sqlOffer), true)
	v = n.committed(name, id)
	n.checkEvidence(name, v)
	// A rolled-back transaction leaves neither row.
	rolled, _ := n.sqlEnqueue("upload-rolled", n.registerReq(creatorOne, "chs-rolled", sizeOne), false)
	ctx, cancel := bcCtx()
	defer cancel()
	var rows int
	if err := n.reader.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM broadcaster.intents WHERE intent_id = $1::uuid)
		+ (SELECT count(*) FROM app_fixture.uploads WHERE id = 'upload-rolled')`, rolled).Scan(&rows); err != nil {
		n.t.Fatal(err)
	}
	if rows != 0 {
		n.t.Fatalf("phase %s: a rolled-back SQL enqueue left %d rows", name, rows)
	}
	var apps int
	if err := n.reader.QueryRowContext(ctx, `SELECT count(*) FROM app_fixture.uploads u JOIN broadcaster.intents i ON i.intent_id = u.intent_id
		WHERE i.state = 'COMMITTED'`).Scan(&apps); err != nil {
		n.t.Fatal(err)
	}
	if apps != 2 {
		n.t.Fatalf("phase %s: %d committed SQL-path intents with their application row, want 2", name, apps)
	}
	fmt.Printf("phase=%s kinds=REGISTER_CONTENT,OFFER_LEASE,RELAY_ACCEPT_LEASE,RELAY_RENEW_LEASE,RELAY_END_LEASE,WITHDRAW_CONTENT,FORWARD_PROVIDER_TX sql_path=REGISTER_CONTENT,OFFER_LEASE rollback_rows=0\n", name)
}

// teeStdout copies everything the test prints to standard output into a
// file as well, so phase L can scan the test's own output for canaries.
func teeStdout(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "svn-bdev-out-")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "stdout.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.MultiWriter(orig, f), r)
		close(done)
	}()
	t.Cleanup(func() {
		os.Stdout = orig
		_ = w.Close()
		<-done
		_ = f.Close()
		_ = os.RemoveAll(dir)
	})
	return path
}

// bcCanary is one run-time canary and the forms a log could carry it in.
type bcCanary struct {
	name  string
	forms []string
}

func canaryForms(name string, raw []byte) bcCanary {
	h := hex.EncodeToString(raw)
	return bcCanary{name: name, forms: []string{h, strings.ToUpper(h), base64.StdEncoding.EncodeToString(raw),
		base64.RawStdEncoding.EncodeToString(raw), base64.RawURLEncoding.EncodeToString(raw)}}
}

// keyFileCanary reads a signer key file (hex) as a canary.
func (n *bnet) keyFileCanary(name, file string) bcCanary {
	n.t.Helper()
	keyHex, err := os.ReadFile(filepath.Join(n.base, "keys", file))
	if err != nil {
		n.t.Fatal(err)
	}
	keyRaw, err := hex.DecodeString(strings.TrimSpace(string(keyHex)))
	if err != nil {
		n.t.Fatal(err)
	}
	return canaryForms(name, keyRaw)
}

// passwordCanary reads a database role's password file as a canary.
func (n *bnet) passwordCanary(name, role string) bcCanary {
	n.t.Helper()
	pass, err := os.ReadFile(n.passFile[role])
	if err != nil {
		n.t.Fatal(err)
	}
	return bcCanary{name: name, forms: []string{strings.TrimSpace(string(pass))}}
}

// phaseLogCanary is phase L: canaries planted in the two key files, the
// runtime and owner database passwords, an authorization context id, an
// inner signature and a request body. Each is first shown processed; then
// every broadcaster process's log (standard error) and standard output,
// the PostgreSQL log, every node log and the test's own standard output
// (a copy of what it printed up to here, not the testing framework's own
// lines) are scanned. Zero hits is the pass condition. Node logs are
// scanned for the canaries that never enter a transaction, and a
// restarted validator's node log holds only what it wrote after its last
// start. The logs themselves are not kept (plan section 15; this departs
// from plan 13.4 L's "bounded copies"): they live in temporary
// directories, and copying them into the evidence would commit a secret
// if the redactor failed; the scan prints each log's size, sha256 and hit
// count.
func phaseLogCanary(n *bnet, name string) {
	// The key files and the database passwords are the run's own secrets:
	// the key files the signer read, the runtime role's password serve
	// used, and the owner role's password migrate used.
	canaries := []bcCanary{
		n.keyFileCanary("key-file", "service.key"), n.keyFileCanary("broadcaster-key-file", "broadcaster.key"),
		n.passwordCanary("db-password", roleRuntime), n.passwordCanary("db-owner-password", roleOwner),
	}

	// An authorization context id, an inner signature and a request body
	// value, carried by one relay that commits.
	var nonce [6]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		n.t.Fatal(err)
	}
	principal := "canary-" + hex.EncodeToString(nonce[:])
	n.ownContent(name, callerApp, sizeOne, "ccan")
	deadline, expiry := n.terms()
	offer := n.offerOf("ocan", "ccan", "p2", sizeOne, deadline, expiry)
	sum := sha256.Sum256(append([]byte("canary-terms:"), nonce[:]...))
	offer.TermsDigest = sha256Digest(sum[:])
	offerView := n.committed(name, n.create(callerApp, n.offerReq(creatorOne, offer)).IntentID)
	offerTx := n.committedTx(n.hashOf(offerView.Commitment.FirstCommitTxHash), 30*time.Second)
	if !bytes.Contains(offerTx.Tx, sum[:]) {
		n.t.Fatalf("phase %s: the committed offer does not carry the canary terms digest", name)
	}
	sig := n.acceptSig(offer, "lcan", n.d.keys["p2"])
	relay := n.acceptReq(principal, offer, "lcan", sig)
	v := n.committed(name, n.create(callerApp, relay).IntentID)
	ctx, cancel := bcCtx()
	defer cancel()
	var stored string
	if err := n.reader.QueryRowContext(ctx, `SELECT auth_context->>'principal_id' FROM broadcaster.intents WHERE intent_id = $1`, v.IntentID).Scan(&stored); err != nil {
		n.t.Fatal(err)
	}
	if stored != principal {
		n.t.Fatalf("phase %s: the stored authorization context names %q, not the canary", name, stored)
	}
	res := n.committedTx(n.hashOf(v.Commitment.FirstCommitTxHash), 30*time.Second)
	if !bytes.Contains(res.Tx, sig.GetSignature()) {
		n.t.Fatalf("phase %s: the committed relay does not carry the canary signature", name)
	}
	fmt.Printf("phase=%s canaries_processed=key-file(signer signed),broadcaster-key-file(signer signed relays),db-password(serve connected),db-owner-password(migrate connected),auth-context-id(stored),inner-signature(relayed in %s),request-body(terms digest of committed offer ocan)\n",
		name, v.Commitment.FirstCommitTxHash)
	canaries = append(canaries, bcCanary{name: "auth-context-id", forms: []string{principal}},
		canaryForms("inner-signature", sig.GetSignature()), canaryForms("request-body", sum[:]))

	// The test's own output: a marker shows the tee has caught up.
	marker := "canary-scan-marker-" + hex.EncodeToString(nonce[:2])
	fmt.Println(marker)
	bcWait(n.t, "test output flushed", 10*time.Second, func() bool {
		raw, err := os.ReadFile(n.stdout)
		return err == nil && bytes.Contains(raw, []byte(marker))
	})
	logs := map[string]string{"go-test-output": n.stdout, "postgres": filepath.Join(n.db.ClusterDir, "postgres.log")}
	stdouts := map[string]string{}
	for _, p := range n.launch.procs {
		logs["broadcaster/"+filepath.Base(p.logPath)] = p.logPath
		stdouts["broadcaster-stdout/"+strings.TrimSuffix(filepath.Base(p.logPath), ".log")] = p.stdout.String()
	}
	for _, node := range n.layout.Topology.Validators {
		logs["node/"+node.Name] = filepath.Join(node.Home, "node.log")
	}
	// A node logs the transactions it commits, so the two canaries that
	// reach the chain by design (the relayed signature and the offer's
	// terms digest are in committed transactions) are public data in a
	// node log; node logs are scanned for the three canaries that never
	// enter a transaction.
	onChain := map[string]bool{"inner-signature": true, "request-body": true}
	total := 0
	for _, label := range sortedKeysOf(logs) {
		var forms []string
		var names []string
		for _, c := range canaries {
			if strings.HasPrefix(label, "node/") && onChain[c.name] {
				continue
			}
			names = append(names, c.name)
			forms = append(forms, c.forms...)
		}
		size, sum, found := scanLog(n.t, logs[label], forms)
		if len(found) != 0 {
			n.leaked = true
		}
		for _, form := range found {
			n.t.Errorf("phase %s: a canary form %d bytes long was found in %s", name, len(form), label)
		}
		total += len(found)
		fmt.Printf("phase=%s canary_log=%s bytes=%d sha256=%x canaries=%s hits=%d\n", name, label, size, sum, strings.Join(names, ","), len(found))
	}
	var all []string
	for _, c := range canaries {
		all = append(all, c.forms...)
	}
	for _, label := range sortedKeysOf(stdouts) {
		var found []string
		for _, form := range all {
			if strings.Contains(stdouts[label], form) {
				found = append(found, form)
			}
		}
		if len(found) != 0 {
			n.leaked = true
		}
		for _, form := range found {
			n.t.Errorf("phase %s: a canary form %d bytes long was found in %s", name, len(form), label)
		}
		total += len(found)
		fmt.Printf("phase=%s canary_log=%s bytes=%d sha256=%x canaries=all hits=%d\n", name, label, len(stdouts[label]),
			sha256.Sum256([]byte(stdouts[label])), len(found))
	}
	if total != 0 {
		n.t.Fatalf("phase %s: %d canary hits", name, total)
	}
	fmt.Printf("phase=%s canaries=%d logs=%d stdouts=%d hits=0 logs_kept=no (plan section 15)\n", name, len(canaries), len(logs), len(stdouts))
	n.checkRaceReports(name)
}

// checkRaceReports fails the test if a broadcaster process log holds a data
// race report. A binary built with -race reports a race on its standard
// error and keeps running, so every process log is read, including those
// of processes a phase killed.
func (n *bnet) checkRaceReports(phase string) {
	n.t.Helper()
	races := 0
	for _, p := range n.launch.procs {
		if _, _, found := scanLog(n.t, p.logPath, []string{"WARNING: DATA RACE"}); len(found) != 0 {
			races++
			n.t.Errorf("phase %s: %s reported a data race", phase, filepath.Base(p.logPath))
		}
	}
	fmt.Printf("phase=%s broadcaster_race_build=%t process_logs=%d with_data_race=%d\n", phase, broadcasterRace, len(n.launch.procs), races)
}

// scanLog streams the file at path and returns its size, its sha256 and
// every form found in it. Reads overlap by the longest form, so a form
// split across two reads is found.
func scanLog(t *testing.T, path string, forms []string) (int64, [32]byte, []string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	defer f.Close()
	longest := 0
	for _, form := range forms {
		longest = max(longest, len(form))
	}
	digest := sha256.New()
	hit := map[string]bool{}
	buf := make([]byte, 0, (1<<20)+longest)
	chunk := make([]byte, 1<<20)
	var size int64
	for {
		k, err := f.Read(chunk)
		if k > 0 {
			digest.Write(chunk[:k])
			size += int64(k)
			buf = append(buf, chunk[:k]...)
			for _, form := range forms {
				if form != "" && bytes.Contains(buf, []byte(form)) {
					hit[form] = true
				}
			}
			if keep := longest - 1; keep > 0 && len(buf) > keep {
				buf = append(buf[:0], buf[len(buf)-keep:]...)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("scan %s: %v", path, err)
		}
	}
	var sum [32]byte
	copy(sum[:], digest.Sum(nil))
	var found []string
	for form := range hit {
		found = append(found, form)
	}
	return size, sum, found
}

func sortedKeysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
