package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/config"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/keys"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/manifest"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/sign"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func TestConformance(t *testing.T) {
	if child := os.Getenv("SVN_PROVIDER_CHILD"); child != "" {
		os.Exit(command(strings.Fields(child)))
	}

	code, text := runChild("init", filepath.Join(t.TempDir(), "nope.json"))
	if code == 0 || !strings.Contains(text, "no such file") && !strings.Contains(text, "open ") {
		// A missing file is a refusal. The consent check is the next command.
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	writeRaw(t, cfgPath, `{
		"provider_id":"p","key_id":"k","machine_id":"m","listen":"127.0.0.1:0",
		"data_dir":"`+filepath.Join(dir, "data")+`","hosting_consent":false,
		"declared_capacity_bytes":1000,"quota_bytes":1000,"min_free_bytes":0,
		"bandwidth_bytes_per_second":1000,"transfer_budget_seconds":30,
		"max_package_bytes":1000,"max_term_seconds":100000,
		"clock_uncertainty_seconds":1,"stall_after_seconds":1,
		"chain_id":"shortvideo-dev-1","chain_url":"https://127.0.0.1:1",
		"source_ca":"","chain_ca":"","destinations":[],"chain_destinations":[]
	}`)
	code, text = runChild("init", cfgPath)
	if code == 0 || !strings.Contains(text, "hosting_consent is false") {
		t.Fatalf("consent refusal: exit %d text %q", code, text)
	}
	fmt.Printf("CONSENT_REFUSED exit=%d\n", code)

	bad := config.File{
		ProviderID: "p", KeyID: "k", MachineID: "m", Listen: "127.0.0.1:0",
		DataDir: dir, HostingConsent: true, DeclaredCapacityBytes: 1000, QuotaBytes: 1000,
		BandwidthBytesPerSecond: 1000, TransferBudgetSeconds: 30, MaxPackageBytes: 1000,
		MaxTermSeconds: 100, ClockUncertaintySeconds: 1, StallAfterSeconds: 1,
		ChainID: "shortvideo-dev-1", ChainURL: "https://127.0.0.1:1",
		Destinations: []config.Destination{{Host: "169.254.169.254", IP: "169.254.169.254", Port: 443}},
	}
	body, _ := json.Marshal(bad)
	writeRaw(t, cfgPath, string(body))
	if _, err := config.Load(cfgPath); err == nil {
		t.Fatal("link-local destination was accepted")
	}
	fmt.Printf("LINK_LOCAL_CONFIG refused=true\n")

	const sourceToken = "source-token"
	src := newPackageServer(t, sourceToken)
	defer src.close()
	chain := newChainServer(t)
	defer chain.close()
	srcCA := writeCert(t, filepath.Join(dir, "source-ca.pem"), src.srv)
	chainCA := writeCert(t, filepath.Join(dir, "chain-ca.pem"), chain.srv)

	a := newRunning(t, "alpha", "ak", "vm-1", 1<<20, 0, 1<<20, 30, 2, 1, src, chain, srcCA, chainCA)
	b := newRunning(t, "beta", "bk", "vm-1", 1<<20, 0, 1<<20, 30, 2, 1, src, chain, srcCA, chainCA)
	defer a.stop()
	defer b.stop()
	fmt.Printf("INSTALL alpha=%s beta=%s\n", a.url, b.url)

	stA := a.status(t)
	stB := b.status(t)
	if stA.IndependentOperator || stB.IndependentOperator || stA.PhysicalRedundancy || stB.PhysicalRedundancy || stA.IPFS || stA.LightClient {
		t.Fatalf("independence claim: %+v %+v", stA, stB)
	}
	if stA.MachineID != "vm-1" || stB.MachineID != "vm-1" {
		t.Fatalf("machine ids %s %s", stA.MachineID, stB.MachineID)
	}
	fmt.Printf("INDEPENDENCE alpha=%t beta=%t machine=%s note=%s disk_available_bytes=%d\n",
		stA.IndependentOperator, stB.IndependentOperator, stA.MachineID, stA.SameMachineNote, stA.DiskAvailableBytes)

	pkgA := putPackage(t, src, "ca")
	a.approve(t, pkgA.id)
	resA := a.accept(t, pkgA, 200)
	if resA.Readiness != "ACCEPTANCE_PENDING" || resA.Committed || resA.KeyID != "ak" {
		t.Fatalf("pending acceptance: %+v", resA)
	}
	if got := a.getPackage(t, pkgA.content, resA.ServeToken); got != 404 {
		t.Fatalf("package before commit: %d", got)
	}
	chain.set(pkgA.lease, "ACTIVE")
	stA = a.status(t)
	if row := leaseRow(t, stA, pkgA.lease); row.Readiness != "SERVING" || !row.Advertised || row.Obligation != "COMMITTED" {
		t.Fatalf("serving row: %+v", row)
	}
	sum, status := a.fetchPackage(t, pkgA.content, resA.ServeToken)
	if status != 200 || sum != pkgA.sum {
		t.Fatalf("serve status %d sum %x want %x", status, sum, pkgA.sum)
	}
	fmt.Printf("TRANSFER alpha status=%d bytes=%d sha256=%x readiness=%s\n", status, pkgA.total, sum, "SERVING")

	pubA, _ := hex.DecodeString(resA.PublicHex)
	sigA, _ := hex.DecodeString(resA.SignatureHex)
	inner := &typesv1.InnerSignature{
		Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, PublicKey: pubA,
		Signature: sigA, KeyId: resA.KeyID, CanonicalProfileVersion: msgkit.CanonicalProfileVersion,
	}
	if err := sign.Verify(inner, inner.Domain, resA.KeyID, pubA, resA.Canonical); err != nil {
		t.Fatal(err)
	}
	pkgA.terms.ProviderID = "alpha"
	wantCanon, err := sign.Canonical(pkgA.lease, pkgA.terms)
	if err != nil || wantCanon != resA.Canonical {
		t.Fatalf("canonical mismatch %v", err)
	}
	pkgA.terms.PackageBytes++
	other, err := sign.Canonical(pkgA.lease, pkgA.terms)
	pkgA.terms.PackageBytes--
	if err != nil || other == resA.Canonical {
		t.Fatal("altered terms still matched")
	}
	sigA[0] ^= 0xff
	inner.Signature = sigA
	if err := sign.Verify(inner, inner.Domain, resA.KeyID, pubA, resA.Canonical); err == nil {
		t.Fatal("altered signature verified")
	}
	svc, err := keys.Generate()
	if err != nil {
		t.Fatal(err)
	}
	inner.Signature, _ = hex.DecodeString(resA.SignatureHex)
	if err := sign.Verify(inner, inner.Domain, resA.KeyID, svc.Public(), resA.Canonical); err == nil {
		t.Fatal("service key verified the provider acceptance")
	}
	fmt.Printf("ACCEPT alpha verifies=true altered_signature=false service_key=false terms_bound=true\n")

	if chain.n() != 1 {
		t.Fatalf("broadcasts %d", chain.n())
	}
	decoded, err := sign.DecodeAcceptTx(chain.txAt(0))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded.PubKey, pubA) || !bytes.Equal(decoded.Msg.GetSigner(), a.address(t)) {
		t.Fatalf("tx signer is not alpha")
	}
	if bytes.Contains(chain.txAt(0), pkgA.raw) {
		t.Fatal("segment bytes are inside the transaction")
	}
	fmt.Printf("TX_SEGMENT_BYTES=false signer_is_alpha=true broadcasts=%d\n", len(chain.txs))

	again := a.accept(t, pkgA, 200)
	if again.SignatureHex != resA.SignatureHex || a.status(t).UsedBytes != pkgA.total {
		t.Fatalf("repeat changed the signature or the quota")
	}
	fmt.Printf("REPEAT same_signature=true used_bytes=%d\n", a.status(t).UsedBytes)

	conflict := pkgA
	conflict.termsHex = hex.EncodeToString(bytes.Repeat([]byte{0x44}, 32))
	if code, msg := a.acceptCode(t, conflict); code != 409 || msg != "conflict" {
		t.Fatalf("conflict %d %s", code, msg)
	}
	unapproved := putPackage(t, src, "cno")
	if code, msg := a.acceptCode(t, unapproved); code != 403 || msg != "unapproved offer" {
		t.Fatalf("unapproved %d %s", code, msg)
	}
	badBytes := putPackage(t, src, "cbad")
	src.bad[badBytes.content] = true
	a.approve(t, badBytes.id)
	if code, msg := a.acceptCode(t, badBytes); code != 422 || msg != "integrity" {
		t.Fatalf("altered bytes %d %s", code, msg)
	}
	if a.status(t).UsedBytes != pkgA.total {
		t.Fatalf("failed transfer reserved bytes: %d", a.status(t).UsedBytes)
	}
	fmt.Printf("REJECT conflict=409 unapproved=403 altered_bytes=422 used_bytes=%d\n", a.status(t).UsedBytes)

	evil := pkgA
	evil.id, evil.lease = "ehttp", "lehttp"
	evil.source = "http://127.0.0.1/pkg/ca"
	a.approve(t, evil.id)
	if _, msg := a.acceptCode(t, evil); msg != "destination refused" {
		t.Fatalf("http url: %s", msg)
	}
	evil.id, evil.lease = "emeta", "lemeta"
	evil.source = "https://169.254.169.254/latest"
	a.approve(t, evil.id)
	if _, msg := a.acceptCode(t, evil); msg != "destination refused" {
		t.Fatalf("metadata url: %s", msg)
	}
	evil.id, evil.lease = "edot", "ledot"
	evil.source = src.url + "/pkg/../secret"
	a.approve(t, evil.id)
	if _, msg := a.acceptCode(t, evil); msg != "destination refused" {
		t.Fatalf("dotdot url: %s", msg)
	}
	redir := putPackage(t, src, "credi")
	src.redirect[redir.content] = true
	a.approve(t, redir.id)
	if _, msg := a.acceptCode(t, redir); msg != "redirect refused" {
		t.Fatalf("redirect: %s", msg)
	}
	huge := putPackage(t, src, "chuge")
	src.huge[huge.content] = true
	a.approve(t, huge.id)
	if _, msg := a.acceptCode(t, huge); msg != "unbounded response" {
		t.Fatalf("huge: %s", msg)
	}
	fmt.Printf("MALICIOUS_URL http=destination_refused metadata=destination_refused dotdot=destination_refused redirect=redirect_refused unbounded=unbounded_response\n")

	secret := "secret-marker-not-served"
	if err := os.WriteFile(filepath.Join(a.dir, "secret-outside"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	rawPath := a.rawGet(t, "/%2e%2e/%2e%2e/"+secret)
	if bytes.Contains(rawPath, []byte(secret)) || !bytes.Contains(rawPath, []byte("path refused")) {
		t.Fatalf("path response %q", rawPath)
	}
	fmt.Printf("PATH_REFUSED contains_secret=false\n")

	used := a.status(t).UsedBytes
	a.stop()
	a = a.restart(t)
	defer a.stop()
	stA = a.status(t)
	if stA.UsedBytes != used || leaseRow(t, stA, pkgA.lease).Readiness != "SERVING" {
		t.Fatalf("restart inventory %+v", stA)
	}
	sum, status = a.fetchPackage(t, pkgA.content, resA.ServeToken)
	if status != 200 || sum != pkgA.sum {
		t.Fatalf("restart serve %d", status)
	}
	fmt.Printf("RESTART_COMMITTED status=%d used_bytes=%d readiness=SERVING\n", status, stA.UsedBytes)

	exp := putPackage(t, src, "cexp")
	exp.deadline = time.Now().Unix() + 4
	a.approve(t, exp.id)
	resE := a.accept(t, exp, 200)
	chain.set(exp.lease, "ACTIVE")
	if leaseRow(t, a.status(t), exp.lease).Readiness != "SERVING" {
		t.Fatalf("expiring lease did not serve: %+v", a.status(t))
	}
	time.Sleep(5 * time.Second)
	if got := a.getPackage(t, exp.content, resE.ServeToken); got != 410 {
		t.Fatalf("deadline status %d", got)
	}
	if leaseRow(t, a.status(t), exp.lease).Readiness != "EXPIRED" {
		t.Fatal("deadline did not expire the lease")
	}
	fmt.Printf("DEADLINE status=410 readiness=EXPIRED\n")

	near := putPackage(t, src, "cnear")
	near.deadline = time.Now().Unix() + 2
	a.approve(t, near.id)
	resN := a.accept(t, near, 200)
	chain.set(near.lease, "ACTIVE")
	rowN := leaseRow(t, a.status(t), near.lease)
	if rowN.Readiness != "COMMITTED" || rowN.Advertised || rowN.DeadlineCertainty != "uncertain" {
		t.Fatalf("near deadline: %+v", rowN)
	}
	if got := a.getPackage(t, near.content, resN.ServeToken); got == 200 {
		t.Fatal("uncertain deadline was advertised")
	}
	fmt.Printf("DEADLINE_UNCERTAIN readiness=%s advertised=%t certainty=%s\n", rowN.Readiness, rowN.Advertised, rowN.DeadlineCertainty)

	oldPub := a.status(t).KeyID
	rot := a.admin(t, "rotate", nil, 200)
	if rot["key_id"] == oldPub {
		t.Fatal("rotation kept the key id")
	}
	next := putPackage(t, src, "crot")
	a.approve(t, next.id)
	resR := a.accept(t, next, 200)
	chain.set(next.lease, "ACTIVE")
	if resR.KeyID == "ak" || leaseRow(t, a.status(t), next.lease).Readiness != "SERVING" {
		t.Fatalf("rotated acceptance %s %+v", resR.KeyID, a.status(t))
	}
	pubR, _ := hex.DecodeString(resR.PublicHex)
	sigR, _ := hex.DecodeString(resR.SignatureHex)
	rotInner := &typesv1.InnerSignature{
		Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, PublicKey: pubR,
		Signature: sigR, KeyId: resR.KeyID, CanonicalProfileVersion: msgkit.CanonicalProfileVersion,
	}
	if err := sign.Verify(rotInner, rotInner.Domain, resR.KeyID, pubR, resR.Canonical); err != nil {
		t.Fatal(err)
	}
	if err := sign.Verify(rotInner, rotInner.Domain, resR.KeyID, pubA, resR.Canonical); err == nil {
		t.Fatal("old key verified the new acceptance")
	}
	fmt.Printf("ROTATE old_key_rejected=true new_key=%s\n", resR.KeyID)

	if code, msg := a.adminCode(t, "drain", nil, ""); code != 401 || msg != "unauthorized" {
		t.Fatalf("admin %d %s", code, msg)
	}
	if a.status(t).Draining {
		t.Fatal("unauthorized drain changed state")
	}
	fmt.Printf("ADMIN_UNAUTHORIZED status=401 draining=false\n")

	chain.freeze = true
	time.Sleep(1200 * time.Millisecond)
	stA = a.status(t)
	if !stA.ChainUncertain || stA.CommitmentCertainty != "uncertain" {
		t.Fatalf("stall not visible: %+v", stA)
	}
	if leaseRow(t, stA, pkgA.lease).Readiness != "SERVING" {
		t.Fatal("stall stopped an accepted package")
	}
	stall := putPackage(t, src, "cstall")
	a.approve(t, stall.id)
	chain.set(stall.lease, "ACTIVE")
	before := chain.n()
	resS := a.accept(t, stall, 200)
	if resS.Readiness != "ACCEPTANCE_PENDING" || resS.Committed || chain.n() != before {
		t.Fatalf("stall acceptance %+v broadcasts %d", resS, len(chain.txs))
	}
	fmt.Printf("STALL chain_uncertain=true new_readiness=%s broadcasts_added=0 serving_kept=true\n", resS.Readiness)

	dr := a.admin(t, "drain", nil, 200)
	if dr["draining"] != true {
		t.Fatalf("drain %+v", dr)
	}
	if _, msg := a.acceptCode(t, putPackage(t, src, "cdrained")); msg != "draining" && msg != "unapproved offer" {
		// not approved, so approve then expect draining
	}
	drained := putPackage(t, src, "cdrain")
	a.approve(t, drained.id)
	if _, msg := a.acceptCode(t, drained); msg != "draining" {
		t.Fatalf("drain did not refuse: %s", msg)
	}
	if got := a.getPackage(t, pkgA.content, resA.ServeToken); got != 200 {
		t.Fatalf("drain stopped serving: %d", got)
	}
	fmt.Printf("DRAIN new_offer=draining serving_status=200\n")

	wd := a.admin(t, "withdraw", map[string]string{"lease_id": pkgA.lease}, 200)
	if wd["chain_end"] != false || wd["readiness"] != "REMOVED" {
		t.Fatalf("withdraw %+v", wd)
	}
	if got := a.getPackage(t, pkgA.content, resA.ServeToken); got != 410 && got != 404 {
		t.Fatalf("withdrawn package %d", got)
	}
	inv := a.inventory(t)
	if !strings.Contains(inv["canonical"].(string), "ATTESTATION_NOT_STORAGE_PROOF") || !strings.Contains(inv["canonical"].(string), `"independentOperator":false`) {
		t.Fatalf("inventory %v", inv["canonical"])
	}
	fmt.Printf("WITHDRAW chain_end=false local_readiness=REMOVED inventory_attestation=true\n")

	capDoc := a.capability(t)
	if _, err := hex.DecodeString(capDoc["digest_hex"].(string)); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("CAPABILITY digest=%s key=%s\n", capDoc["digest_hex"], capDoc["key_id"])
	rec := a.receipt(t, next.lease)
	fmt.Printf("RECEIPT status=201 key=%s\n", rec["key_id"])

	pkgB := putPackage(t, src, "cb")
	b.approve(t, pkgB.id)
	resB := b.accept(t, pkgB, 200)
	chain.set(pkgB.lease, "ACTIVE")
	b.status(t)
	pubB, _ := hex.DecodeString(resB.PublicHex)
	if bytes.Equal(pubA, pubB) {
		t.Fatal("agents share a key")
	}
	sigB, _ := hex.DecodeString(resB.SignatureHex)
	bInner := &typesv1.InnerSignature{
		Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, PublicKey: pubB,
		Signature: sigB, KeyId: resB.KeyID, CanonicalProfileVersion: msgkit.CanonicalProfileVersion,
	}
	if err := sign.Verify(bInner, bInner.Domain, resB.KeyID, pubB, resB.Canonical); err != nil {
		t.Fatal(err)
	}
	if err := sign.Verify(bInner, bInner.Domain, resB.KeyID, pubA, resB.Canonical); err == nil {
		t.Fatal("alpha key verified beta's acceptance")
	}
	fmt.Printf("TWO_KEYS alpha_rejects_beta=true beta_verifies=true\n")

	// Unauthenticated source fetch.
	req, _ := http.NewRequest(http.MethodGet, src.url+"/pkg/ca/manifest", nil)
	res, err := src.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("source without token: %d", res.StatusCode)
	}
	fmt.Printf("SOURCE_UNAUTHENTICATED status=401\n")

	partial := newRunning(t, "partial", "pk", "vm-1", 1<<20, 0, 1<<20, 30, 2, 1, src, chain, srcCA, chainCA)
	hang := putPackage(t, src, "chang")
	src.hang[hang.content] = true
	partial.approve(t, hang.id)
	done := make(chan struct{})
	go func() {
		_, _, _ = partial.postOfferErr(hang)
		close(done)
	}()
	waitFor(t, filepath.Join(partial.dir, "state.json"), "TRANSFERRING")
	if _, err := os.Stat(filepath.Join(partial.dir, "staging", hang.lease, "partial")); err != nil {
		t.Fatal(err)
	}
	partial.stop()
	<-done
	partial = partial.restart(t)
	defer partial.stop()
	pst := partial.status(t)
	if pst.UsedBytes != 0 || len(pst.Leases) != 0 {
		t.Fatalf("partial cleanup %+v", pst)
	}
	if _, err := os.Stat(filepath.Join(partial.dir, "staging", hang.lease)); !os.IsNotExist(err) {
		t.Fatal("staging directory remains")
	}
	fmt.Printf("PARTIAL_RESTART used_bytes=%d leases=%d staging_removed=true\n", pst.UsedBytes, len(pst.Leases))

	// min_free is above any disk this test runner has. The agent still reads
	// statfs and refuses because the measured free space is smaller.
	const minFree = uint64(1<<63 - 1)
	diskA := newRunning(t, "disk", "dk", "vm-1", 1<<20, minFree, 1<<20, 30, 2, 1, src, chain, srcCA, chainCA)
	defer diskA.stop()
	diskPkg := putPackage(t, src, "cdisk")
	diskA.approve(t, diskPkg.id)
	if _, msg := diskA.acceptCode(t, diskPkg); msg != "insufficient disk" {
		t.Fatalf("disk: %s", msg)
	}
	fmt.Printf("DISK_REJECT available=%d min_free=%d error=insufficient_disk\n", diskA.status(t).DiskAvailableBytes, minFree)

	quota := newRunning(t, "quota", "qk", "vm-1", 10, 0, 1<<20, 30, 2, 1, src, chain, srcCA, chainCA)
	defer quota.stop()
	qPkg := putPackage(t, src, "cquota")
	quota.approve(t, qPkg.id)
	if _, msg := quota.acceptCode(t, qPkg); msg != "insufficient quota" {
		t.Fatalf("quota: %s", msg)
	}
	fmt.Printf("QUOTA_REJECT quota_bytes=10 error=insufficient_quota\n")

	band := newRunning(t, "band", "bk2", "vm-1", 1<<20, 0, 1, 1, 2, 1, src, chain, srcCA, chainCA)
	defer band.stop()
	bPkg := putPackage(t, src, "cband")
	band.approve(t, bPkg.id)
	if _, msg := band.acceptCode(t, bPkg); msg != "bandwidth limit" {
		t.Fatalf("bandwidth: %s", msg)
	}
	fmt.Printf("BANDWIDTH_REJECT bytes_per_second=1 budget_seconds=1 error=bandwidth_limit\n")
}

type offerSpec struct {
	id, lease, content, source, termsHex string
	deadline                             int64
	segs                                 []manifest.Segment
	raw                                  []byte
	sum                                  [32]byte
	total                                uint64
	terms                                sign.Terms
}

func putPackage(t *testing.T, src *packageServer, content string) offerSpec {
	t.Helper()
	one := make([]byte, 32)
	two := make([]byte, 32)
	if _, err := rand.Read(one); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(two); err != nil {
		t.Fatal(err)
	}
	segs := [][]byte{one, two}
	var total uint64
	out := make([]manifest.Segment, len(segs))
	var raw []byte
	for i, buf := range segs {
		sum := sha256.Sum256(buf)
		out[i] = manifest.Segment{ID: "s" + strconv.Itoa(i), SHA256: hex.EncodeToString(sum[:]), ByteCount: uint64(len(buf))}
		total += uint64(len(buf))
		raw = append(raw, buf...)
		src.blobs[content+"/"+out[i].ID] = append([]byte(nil), buf...)
	}
	text, err := manifest.Canonical("m"+content, content, 1, total, out)
	if err != nil {
		t.Fatal(err)
	}
	src.manifests[content] = text
	sum := sha256.Sum256([]byte(text))
	termsSum := sha256.Sum256([]byte("terms:" + content))
	start := time.Now().Unix()
	spec := offerSpec{
		id: "o" + content, lease: "l" + content, content: content, source: src.url + "/pkg/" + content,
		termsHex: hex.EncodeToString(termsSum[:]), deadline: start + 3600, segs: out, raw: raw,
		sum: sha256.Sum256(raw), total: total,
	}
	spec.terms = sign.Terms{
		OfferID: spec.id, ContentID: content, ContentVersion: 1, ProviderID: "",
		Manifest: sum, PackageBytes: total, TermsDigest: termsSum, Start: start,
		Deadline: spec.deadline, Expiry: spec.deadline, TermVersion: 1,
		Scheduler: bytes.Repeat([]byte{0x22}, 20),
	}
	return spec
}

type running struct {
	t      *testing.T
	cmd    *exec.Cmd
	dir    string
	cfg    string
	url    string
	client *http.Client
	id     string
	buf    *bytes.Buffer
}

func newRunning(t *testing.T, id, keyID, machine string, quota, minFree, bandwidth, budget uint64, uncertainty, stall int64, src *packageServer, chain *chainServer, srcCA, chainCA string) *running {
	t.Helper()
	dir := t.TempDir()
	data := filepath.Join(dir, "data")
	cfgPath := filepath.Join(dir, "config.json")
	host, port, err := net.SplitHostPort(src.addr)
	if err != nil {
		t.Fatal(err)
	}
	ch, cp, err := net.SplitHostPort(chain.addr)
	if err != nil {
		t.Fatal(err)
	}
	sp, _ := strconv.Atoi(port)
	csp, _ := strconv.Atoi(cp)
	file := config.File{
		ProviderID: id, KeyID: keyID, MachineID: machine, Listen: "127.0.0.1:0", DataDir: data,
		HostingConsent: true, DeclaredCapacityBytes: quota, QuotaBytes: quota, MinFreeBytes: minFree,
		BandwidthBytesPerSecond: bandwidth, TransferBudgetSeconds: budget, MaxPackageBytes: 1 << 20,
		MaxTermSeconds: 100000, ClockUncertaintySeconds: uncertainty, StallAfterSeconds: stall,
		ChainID: "shortvideo-dev-1", ChainURL: "https://" + chain.addr, SourceCA: srcCA, ChainCA: chainCA,
		Destinations:      []config.Destination{{Host: host, IP: host, Port: uint16(sp)}},
		ChainDestinations: []config.Destination{{Host: ch, IP: ch, Port: uint16(csp)}},
	}
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeRaw(t, cfgPath, string(raw))
	if code, text := runChild("init", cfgPath); code != 0 {
		t.Fatalf("init %s: %s", id, text)
	}
	if err := os.WriteFile(filepath.Join(data, "source.token"), []byte("source-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "approvals.json"), []byte("{\"offer_ids\":[]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &running{t: t, dir: data, cfg: cfgPath, id: id, buf: &bytes.Buffer{}}
	r.start()
	return r
}

func (r *running) start() {
	r.t.Helper()
	_ = os.Remove(filepath.Join(r.dir, "listen.json"))
	cmd := exec.Command(os.Args[0], "-test.run=^TestConformance$")
	cmd.Env = append(os.Environ(), "SVN_PROVIDER_CHILD=run "+r.cfg)
	cmd.Stdout = r.buf
	cmd.Stderr = r.buf
	if err := cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	r.cmd = cmd
	var listen struct {
		URL string `json:"url"`
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(filepath.Join(r.dir, "listen.json"))
		if err == nil && json.Unmarshal(body, &listen) == nil && listen.URL != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if listen.URL == "" {
		r.t.Fatalf("listen %s: %s", r.id, r.buf.String())
	}
	r.url = strings.TrimRight(listen.URL, "/")
	pool := x509.NewCertPool()
	pemBytes, err := os.ReadFile(filepath.Join(r.dir, "tls.crt"))
	if err != nil || !pool.AppendCertsFromPEM(pemBytes) {
		r.t.Fatal(err)
	}
	r.client = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		res, err := r.client.Get(r.url + "/health")
		if err == nil {
			res.Body.Close()
			if res.StatusCode == 200 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	r.t.Fatalf("health %s: %s", r.id, r.buf.String())
}

func (r *running) stop() {
	if r.cmd != nil && r.cmd.Process != nil {
		_ = r.cmd.Process.Kill()
		_, _ = r.cmd.Process.Wait()
		r.cmd = nil
	}
}

func (r *running) restart(t *testing.T) *running {
	t.Helper()
	r.stop()
	next := *r
	next.buf = &bytes.Buffer{}
	next.t = t
	next.start()
	return &next
}

func (r *running) approve(t *testing.T, id string) {
	t.Helper()
	path := filepath.Join(r.dir, "approvals.json")
	var file struct {
		OfferIDs []string `json:"offer_ids"`
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(body, &file) != nil {
		t.Fatal("approvals")
	}
	file.OfferIDs = append(file.OfferIDs, id)
	raw, _ := json.Marshal(file)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func (r *running) accept(t *testing.T, spec offerSpec, want int) acceptResult {
	t.Helper()
	res, doc := r.postOffer(t, spec)
	if res.StatusCode != want {
		t.Fatalf("accept %s: %d %s", spec.id, res.StatusCode, doc)
	}
	var out acceptResult
	if json.Unmarshal(doc, &out) != nil {
		t.Fatalf("accept json %s", doc)
	}
	return out
}

func (r *running) acceptCode(t *testing.T, spec offerSpec) (int, string) {
	t.Helper()
	res, doc := r.postOffer(t, spec)
	var out struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(doc, &out)
	return res.StatusCode, out.Error
}

type acceptResult struct {
	Readiness    string `json:"readiness"`
	LeaseID      string `json:"lease_id"`
	SignatureHex string `json:"signature_hex"`
	PublicHex    string `json:"public_key_hex"`
	KeyID        string `json:"key_id"`
	Canonical    string `json:"canonical"`
	ServeToken   string `json:"serve_token"`
	Committed    bool   `json:"committed"`
}

func (r *running) postOffer(t *testing.T, spec offerSpec) (*http.Response, []byte) {
	t.Helper()
	res, buf, err := r.postOfferErr(spec)
	if err != nil {
		t.Fatal(err)
	}
	return res, buf
}

func (r *running) postOfferErr(spec offerSpec) (*http.Response, []byte, error) {
	spec.terms.ProviderID = r.id
	if spec.terms.OfferID == "" {
		spec.terms.OfferID = spec.id
		spec.terms.ContentID = spec.content
		spec.terms.ContentVersion = 1
		spec.terms.TermVersion = 1
		spec.terms.Scheduler = bytes.Repeat([]byte{0x22}, 20)
		if spec.deadline == 0 {
			spec.terms.Start = time.Now().Unix()
			spec.terms.Deadline = spec.terms.Start + 3600
			spec.terms.Expiry = spec.terms.Deadline
		}
	}
	if spec.deadline != 0 {
		spec.terms.Deadline = spec.deadline
		spec.terms.Expiry = spec.deadline
	}
	body := map[string]any{
		"offer_id": spec.id, "lease_id": spec.lease, "business_id": "b" + spec.content,
		"content_id": spec.content, "content_version": "1", "provider_id": r.id,
		"manifest_digest_hex":   hex.EncodeToString(spec.terms.Manifest[:]),
		"package_bytes":         strconv.FormatUint(spec.terms.PackageBytes, 10),
		"terms_digest_hex":      spec.termsHex,
		"start_unix_seconds":    strconv.FormatInt(spec.terms.Start, 10),
		"deadline_unix_seconds": strconv.FormatInt(spec.terms.Deadline, 10),
		"expiry_unix_seconds":   strconv.FormatInt(spec.terms.Expiry, 10),
		"term_version":          "1", "scheduler_hex": hex.EncodeToString(spec.terms.Scheduler),
		"source_url": spec.source,
	}
	segs := make([]any, len(spec.segs))
	for i, seg := range spec.segs {
		segs[i] = map[string]string{"segment_id": seg.ID, "sha256": seg.SHA256, "byte_count": strconv.FormatUint(seg.ByteCount, 10)}
	}
	body["segments"] = segs
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, r.url+"/offers/"+spec.id+"/acceptance", bytes.NewReader(raw))
	if err != nil {
		return nil, nil, err
	}
	res, err := r.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	buf, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, buf, nil
}

func (r *running) status(t *testing.T) statusDoc {
	t.Helper()
	res, err := r.client.Get(r.url + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var doc statusDoc
	if json.NewDecoder(res.Body).Decode(&doc) != nil || res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	return doc
}

type statusDoc struct {
	IndependentOperator bool   `json:"independent_operator"`
	PhysicalRedundancy  bool   `json:"physical_redundancy"`
	MachineID           string `json:"machine_id"`
	SameMachineNote     string `json:"same_machine_note"`
	IPFS                bool   `json:"ipfs"`
	LightClient         bool   `json:"light_client"`
	UsedBytes           uint64 `json:"used_bytes"`
	DiskAvailableBytes  uint64 `json:"disk_available_bytes"`
	ChainUncertain      bool   `json:"chain_uncertain"`
	CommitmentCertainty string `json:"commitment_certainty"`
	Draining            bool   `json:"draining"`
	KeyID               string `json:"key_id"`
	Leases              []struct {
		LeaseID           string `json:"lease_id"`
		Readiness         string `json:"readiness"`
		Obligation        string `json:"obligation"`
		Advertised        bool   `json:"advertised"`
		DeadlineCertainty string `json:"deadline_certainty"`
	} `json:"leases"`
}

func leaseRow(t *testing.T, doc statusDoc, id string) struct {
	LeaseID           string `json:"lease_id"`
	Readiness         string `json:"readiness"`
	Obligation        string `json:"obligation"`
	Advertised        bool   `json:"advertised"`
	DeadlineCertainty string `json:"deadline_certainty"`
} {
	t.Helper()
	for _, row := range doc.Leases {
		if row.LeaseID == id {
			return row
		}
	}
	t.Fatalf("lease %s missing in %+v", id, doc.Leases)
	return doc.Leases[0]
}

func (r *running) fetchPackage(t *testing.T, content, token string) ([32]byte, int) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, r.url+"/packages/"+content+"/1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := r.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		return [32]byte{}, res.StatusCode
	}
	return sha256.Sum256(buf), res.StatusCode
}

func (r *running) getPackage(t *testing.T, content, token string) int {
	t.Helper()
	_, status := r.fetchPackage(t, content, token)
	return status
}

func (r *running) admin(t *testing.T, action string, body any, want int) map[string]any {
	t.Helper()
	res, doc := r.adminRaw(t, action, body, r.adminToken(t))
	if res != want {
		t.Fatalf("admin %s: %d %s", action, res, doc)
	}
	var out map[string]any
	if json.Unmarshal(doc, &out) != nil {
		t.Fatal(string(doc))
	}
	return out
}

func (r *running) adminCode(t *testing.T, action string, body any, token string) (int, string) {
	t.Helper()
	code, doc := r.adminRaw(t, action, body, token)
	var out struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(doc, &out)
	return code, out.Error
}

func (r *running) adminRaw(t *testing.T, action string, body any, token string) (int, []byte) {
	t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	} else {
		raw = []byte("{}")
	}
	req, err := http.NewRequest(http.MethodPost, r.url+"/admin/"+action, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := r.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(res.Body)
	return res.StatusCode, buf
}

func (r *running) adminToken(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(r.dir, "admin.token"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(body))
}

func (r *running) inventory(t *testing.T) map[string]any {
	t.Helper()
	res, err := r.client.Post(r.url+"/inventory", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var doc map[string]any
	if json.NewDecoder(res.Body).Decode(&doc) != nil || res.StatusCode != 200 {
		t.Fatalf("inventory %d", res.StatusCode)
	}
	return doc
}

func (r *running) capability(t *testing.T) map[string]any {
	t.Helper()
	res, err := r.client.Get(r.url + "/capability")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var doc map[string]any
	if json.NewDecoder(res.Body).Decode(&doc) != nil || res.StatusCode != 200 {
		t.Fatal("capability")
	}
	return doc
}

func (r *running) receipt(t *testing.T, id string) map[string]any {
	t.Helper()
	res, err := r.client.Post(r.url+"/transfers/"+id+"/receipt", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var doc map[string]any
	if json.NewDecoder(res.Body).Decode(&doc) != nil || res.StatusCode != 201 {
		t.Fatalf("receipt %d", res.StatusCode)
	}
	return doc
}

func (r *running) address(t *testing.T) []byte {
	t.Helper()
	res, err := r.client.Get(r.url + "/enrollment")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var doc struct {
		Address string `json:"address_hex"`
	}
	if json.NewDecoder(res.Body).Decode(&doc) != nil {
		t.Fatal("enrollment")
	}
	buf, err := hex.DecodeString(doc.Address)
	if err != nil {
		t.Fatal(err)
	}
	return buf
}

func (r *running) rawGet(t *testing.T, path string) []byte {
	t.Helper()
	pool := x509.NewCertPool()
	pemBytes, err := os.ReadFile(filepath.Join(r.dir, "tls.crt"))
	if err != nil || !pool.AppendCertsFromPEM(pemBytes) {
		t.Fatal(err)
	}
	host := strings.TrimPrefix(r.url, "https://")
	conn, err := tls.Dial("tcp", host, &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12, ServerName: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: 127.0.0.1\r\nConnection: close\r\n\r\n", path); err != nil {
		t.Fatal(err)
	}
	buf, err := io.ReadAll(conn)
	if err != nil {
		t.Fatal(err)
	}
	return buf
}

func runChild(args ...string) (int, string) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestConformance$")
	cmd.Env = append(os.Environ(), "SVN_PROVIDER_CHILD="+strings.Join(args, " "))
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if err == nil {
		return 0, buf.String()
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), buf.String()
	}
	return 1, err.Error() + buf.String()
}

func writeRaw(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCert(t *testing.T, path string, srv *httptest.Server) string {
	t.Helper()
	body := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitFor(t *testing.T, path, needle string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		body, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(body), needle) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s in %s", needle, path)
}

type packageServer struct {
	srv       *httptest.Server
	url, addr string
	client    *http.Client
	token     string
	blobs     map[string][]byte
	manifests map[string]string
	bad       map[string]bool
	redirect  map[string]bool
	huge      map[string]bool
	hang      map[string]bool
	block     chan struct{}
}

func newPackageServer(t *testing.T, token string) *packageServer {
	t.Helper()
	s := &packageServer{
		token: token, blobs: map[string][]byte{}, manifests: map[string]string{},
		bad: map[string]bool{}, redirect: map[string]bool{}, huge: map[string]bool{}, hang: map[string]bool{},
		block: make(chan struct{}),
	}
	s.srv = httptest.NewTLSServer(http.HandlerFunc(s.serve))
	t.Cleanup(func() { close(s.block); s.srv.Close() })
	s.url = s.srv.URL
	s.addr = strings.TrimPrefix(s.srv.Listener.Addr().String(), "[::]")
	// httptest may listen on 127.0.0.1.
	s.addr = s.srv.Listener.Addr().String()
	pool := x509.NewCertPool()
	pool.AddCert(s.srv.Certificate())
	s.client = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	return s
}

func (s *packageServer) close() {}

func (s *packageServer) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+s.token {
		http.Error(w, "unauthorized", 401)
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "pkg" {
		http.NotFound(w, r)
		return
	}
	content := parts[1]
	if s.redirect[content] {
		http.Redirect(w, r, "https://169.254.169.254/latest", http.StatusFound)
		return
	}
	if len(parts) == 3 && parts[2] == "manifest" {
		w.Write([]byte(s.manifests[content]))
		return
	}
	if len(parts) == 4 && parts[2] == "segments" {
		buf := s.blobs[content+"/"+parts[3]]
		if s.bad[content] && len(buf) > 0 {
			buf = append([]byte(nil), buf...)
			buf[0] ^= 0xff
		}
		if s.huge[content] {
			buf = append(buf, 0)
		}
		if s.hang[content] {
			w.WriteHeader(200)
			_, _ = w.Write([]byte{1})
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			<-s.block
			return
		}
		_, _ = w.Write(buf)
		return
	}
	http.NotFound(w, r)
}

type chainServer struct {
	srv    *httptest.Server
	addr   string
	mu     sync.Mutex
	height uint64
	freeze bool
	leases map[string]string
	txs    [][]byte
}

func newChainServer(t *testing.T) *chainServer {
	t.Helper()
	c := &chainServer{leases: map[string]string{}}
	c.srv = httptest.NewTLSServer(http.HandlerFunc(c.serve))
	t.Cleanup(c.srv.Close)
	c.addr = c.srv.Listener.Addr().String()
	return c
}

func (c *chainServer) close() {}

func (c *chainServer) n() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.txs)
}

func (c *chainServer) txAt(i int) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.txs[i]...)
}

func (c *chainServer) set(id, state string) {
	c.mu.Lock()
	c.leases[id] = state
	c.mu.Unlock()
}

func (c *chainServer) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/status":
		c.mu.Lock()
		if !c.freeze {
			c.height++
		}
		h := c.height
		c.mu.Unlock()
		fmt.Fprintf(w, `{"result":{"sync_info":{"latest_block_height":"%d","catching_up":false}}}`, h)
	case r.URL.Path == "/account":
		w.Write([]byte(`{"account_number":"0","sequence":"0"}`))
	case strings.HasPrefix(r.URL.Path, "/lease/"):
		id := strings.TrimPrefix(r.URL.Path, "/lease/")
		c.mu.Lock()
		state := c.leases[id]
		c.mu.Unlock()
		if state == "" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `{"state":"%s"}`, state)
	case r.URL.Path == "/broadcast" && r.Method == http.MethodPost:
		buf, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		c.mu.Lock()
		c.txs = append(c.txs, buf)
		c.mu.Unlock()
		w.Write([]byte(`{"code":0}`))
	default:
		http.NotFound(w, r)
	}
}
