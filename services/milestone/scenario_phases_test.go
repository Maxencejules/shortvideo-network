package milestone_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cosmos/cosmos-sdk/client/tx"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/gogoproto/proto"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func (m *mile) upload() {
	m.t.Helper()
	clip := filepath.Join(m.work, "clip.mp4")
	m.run("ffmpeg", "ffmpeg", "-y", "-f", "lavfi", "-i", "testsrc=size=64x48:rate=10", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-t", "1", "-shortest", "-map", "0:v:0", "-map", "1:a:0", "-pix_fmt", "yuv420p", "-c:a", "aac", clip)
	body, err := os.ReadFile(clip)
	if err != nil {
		m.t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	m.clipSHA = hex.EncodeToString(sum[:])
	fmt.Printf("phase=clip bytes=%d sha256=%s synthetic=true\n", len(body), m.clipSHA)
	auth := m.creatorHeader()
	first := m.sendJSON(http.MethodPost, m.api+"/upload-intents", auth, map[string]any{"idempotency_key": "clip-1", "declared_bytes": len(body)})
	again := m.sendJSON(http.MethodPost, m.api+"/upload-intents", auth, map[string]any{"idempotency_key": "clip-1", "declared_bytes": len(body)})
	if first.status != 202 || again.status != 202 || first.json()["upload_id"] != again.json()["upload_id"] {
		m.t.Fatalf("intent %d %s / %d %s", first.status, first.body, again.status, again.body)
	}
	uploadID := str(first.json()["upload_id"])
	cmdID := exec.Command("psql", "-h", m.pg.socket, "-p", m.pg.port, "-U", "svn_api", "-d", "svn_api", "-tAc", "SELECT content_id FROM api.uploads WHERE upload_id = '"+uploadID+"'")
	cmdID.Env = append(m.env(), "PGPASSWORD="+m.pg.password)
	m.content = strings.TrimSpace(m.runCmd("content-id", cmdID))
	fmt.Printf("phase=UPLOAD_RESERVED upload=%s content=%s status=%s repeat_upload=%s\n", uploadID, m.content, first.json()["status"], again.json()["upload_id"])
	transfer, _ := first.json()["transfer"].(map[string]any)
	token := str(transfer["token"])
	put := m.sendRaw(http.MethodPut, m.api+"/uploads/"+uploadID+"/content", map[string]string{"Authorization": "Transfer " + token}, body)
	if put.status < 200 || put.status >= 300 {
		m.t.Fatalf("put %d %s", put.status, put.body)
	}
	fmt.Printf("phase=UPLOAD_RECEIVED status=%d\n", put.status)
	done := m.sendJSON(http.MethodPost, m.api+"/uploads/"+uploadID+"/complete", auth, map[string]string{"digest": m.clipSHA})
	if done.status != 202 {
		m.t.Fatalf("complete %d %s", done.status, done.body)
	}
	fmt.Printf("phase=PROCESSING_QUEUED status=%s\n", done.json()["status"])
	againDone := m.sendJSON(http.MethodPost, m.api+"/uploads/"+uploadID+"/complete", auth, map[string]string{"digest": m.clipSHA})
	if againDone.status != 202 || againDone.json()["status"] != done.json()["status"] {
		m.t.Fatalf("complete retry %d %s", againDone.status, againDone.body)
	}
	blocked := m.sendJSON(http.MethodPost, m.api+"/publish", auth, map[string]any{})
	fmt.Printf("phase=API_PUBLISH_BLOCKED status=%d\n", blocked.status)
	if blocked.status != 403 {
		m.t.Fatalf("publish %d %s", blocked.status, blocked.body)
	}
	cmd := exec.Command("python3", "-m", "svn_media", "claim-run", "--root", m.worker, "--pg-socket", m.pg.socket, "--pg-user", "svn_api", "--pg-database", "svn_api", "--pg-port", m.pg.port)
	cmd.Env = append(m.env(), "PGPASSWORD="+m.pg.password, "PYTHONPATH="+filepath.Join(m.root, "workers/media")+string(os.PathListSeparator)+filepath.Join(m.root, "packages/protocol"))
	out := m.runCmd("media", cmd)
	fmt.Printf("phase=READY_FOR_REVIEW worker=%s\n", strings.TrimSpace(out))
	m.loadPackage()
	m.bootDelivery()
}

func (m *mile) loadPackage() {
	m.t.Helper()
	dir := filepath.Join(m.worker, "packages", m.content, "v1")
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		m.t.Fatal(err)
	}
	var manifest struct {
		PackageBytes string `json:"package_bytes"`
		Renditions   []struct {
			Codec    string `json:"codec"`
			Segments []struct {
				ID    string `json:"segment_id"`
				SHA   string `json:"sha256"`
				Count string `json:"byte_count"`
			} `json:"segments"`
		} `json:"renditions"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		m.t.Fatal(err)
	}
	n, err := strconv.ParseUint(manifest.PackageBytes, 10, 64)
	if err != nil {
		m.t.Fatal(err)
	}
	m.pkgBytes = n
	suffix := map[string]string{"avc1": ".mp4", "jpeg": ".jpg", "webvtt": ".vtt"}
	for _, rendition := range manifest.Renditions {
		ext := suffix[rendition.Codec]
		for _, seg := range rendition.Segments {
			count, err := strconv.ParseUint(seg.Count, 10, 64)
			if err != nil {
				m.t.Fatal(err)
			}
			m.segments = append(m.segments, segInfo(seg.ID, seg.SHA, filepath.Join(dir, "segments", seg.ID+ext), count))
		}
	}
	text := packageText(m.content, 1, m.pkgBytes, m.segments)
	sum := sha256.Sum256([]byte(text))
	m.digest = hex.EncodeToString(sum[:])
	fmt.Printf("phase=PACKAGE content=%s package_text_sha256=%s package_bytes=%d segments=%d\n", m.content, m.digest, m.pkgBytes, len(m.segments))
}

func segInfo(id, sha, path string, n uint64) seg { return seg{ID: id, SHA: sha, Path: path, N: n} }

// packageText is the scheduler package document. It matches
// services/scheduler/internal/policy.ManifestText.
func packageText(contentID string, version, packageBytes uint64, segs []seg) string {
	var b strings.Builder
	b.WriteString(`{"content_id":"`)
	b.WriteString(contentID)
	b.WriteString(`","content_version":`)
	b.WriteString(strconv.FormatUint(version, 10))
	b.WriteString(`,"manifest_id":"m`)
	b.WriteString(contentID)
	b.WriteString(`","package_bytes":"`)
	b.WriteString(strconv.FormatUint(packageBytes, 10))
	b.WriteString(`","profile":"SVN-JCS-1","renditions":[{"codec":"raw","rendition_id":"r0","segments":[`)
	for i, seg := range segs {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"byte_count":"%d","segment_id":"%s","sha256":"%s"}`, seg.N, seg.ID, seg.SHA)
	}
	b.WriteString(`]}],"schema_version":1}`)
	return b.String()
}

func (m *mile) bootDelivery() {
	m.t.Helper()
	py := strings.Join([]string{
		filepath.Join(m.root, "services/delivery"),
		filepath.Join(m.root, "packages/protocol"),
		filepath.Join(m.root, "proto/gen/python"),
	}, string(os.PathListSeparator))
	env := append(m.env(), "PYTHONPATH="+py)
	note := exec.Command("python3", "-m", "svn_delivery", "note-chain", "--root", m.worker, "--note", "HELD")
	note.Env = env
	m.runCmd("note-chain", note)
	cmd := exec.Command("python3", "-m", "svn_delivery", "serve", "--root", m.worker, "--bind", "127.0.0.1", "--port", "0")
	cmd.Env = env
	c := m.startCmd("delivery", cmd)
	line := m.waitLine(c, `"origin"`, 20*time.Second)
	var doc struct {
		Origin string `json:"origin"`
		CA     string `json:"ca"`
	}
	if err := json.Unmarshal([]byte(line), &doc); err != nil {
		m.t.Fatalf("delivery: %v %s", err, c.out.String())
	}
	m.deliver, m.deliverCA = doc.Origin, doc.CA
	authz := exec.Command("python3", "-m", "svn_delivery", "authorize", "--root", m.worker, "--content", m.content, "--seconds", "3600")
	authz.Env = env
	digest := strings.TrimSpace(m.runCmd("authorize", authz))
	fmt.Printf("phase=PLAYBACK_GRANT delivery=%s manifest=%s\n", m.deliver, digest)
}

func (m *mile) registerAndPublish() {
	m.t.Helper()
	for _, id := range []string{"p1", "p2", "p3", "p4"} {
		writeJSONMode(m.t, filepath.Join(m.prov[id].dir, "approvals.json"), map[string]any{"offer_ids": []string{"o" + m.content + id}}, 0o600)
	}
	m.intentRegister(m.content, m.digest, m.pkgBytes, "reg-"+m.content)
	m.waitIndexer("ADMITTED")
	// A read that has not reconciled yet is stale. Wait until it is fresh,
	// then ask. A stale read is a stall, not a moderation decision.
	m.run("indexer-reconcile-before", m.bins["svn-indexer"], "reconcile-once", "--config", filepath.Join(m.work, "indexer", "indexer.json"))
	m.waitFresh()
	withheld := m.publish(false, "")
	fmt.Printf("phase=MODERATION_WITHHELD publication=%s reason=%s\n", str(withheld["publication"]), str(withheld["publication_reason"]))
	if str(withheld["publication"]) != "WITHHELD" {
		m.t.Fatalf("withheld %#v", withheld)
	}
	m.run("indexer-reconcile", m.bins["svn-indexer"], "reconcile-once", "--config", filepath.Join(m.work, "indexer", "indexer.json"))
	fmt.Printf("phase=INDEXER_STATUS %s\n", strings.TrimSpace(m.sendRaw(http.MethodGet, m.indexer+"/v1/status", nil, nil).body))
	approved := m.publish(true, "approval-dev-1")
	deadline := time.Now().Add(20 * time.Second)
	for str(approved["publication"]) != "PUBLISHED" && time.Now().Before(deadline) && approved["verified_transfers"] == float64(3) {
		time.Sleep(time.Second)
		approved = m.getJSON(m.sched + "/v1/availability?content=" + m.content)
	}
	fmt.Printf("phase=INDEXER_AFTER %s\n", strings.TrimSpace(m.sendRaw(http.MethodGet, m.indexer+"/v1/status", nil, nil).body))
	fmt.Printf("phase=PUBLISHED publication=%s leases=%v distinct=%v verified=%v physical=%v simulated=%v independent=%v at=%s\n",
		str(approved["publication"]), approved["active_commitments"], approved["distinct_operators"], approved["verified_transfers"],
		approved["physical_failure_domain_count"], approved["simulated_labels"], approved["independent_operator_verification"], time.Now().UTC().Format(time.RFC3339Nano))
	if str(approved["publication"]) != "PUBLISHED" {
		if m.schedProc != nil {
			fmt.Printf("scheduler-log\n%s\n", m.schedProc.out.String())
		}
		for _, id := range []string{"p1", "p2", "p3"} {
			fmt.Printf("provider-log %s\n%s\n", id, m.prov[id].child.out.String())
		}
		m.t.Fatalf("publish %#v", approved)
	}
	reconciled := strings.TrimSpace(m.run("indexer-reconcile-after", m.bins["svn-indexer"], "reconcile-once", "--config", filepath.Join(m.work, "indexer", "indexer.json")))
	fmt.Printf("phase=INDEXER_RECONCILE %s\n", reconciled)
	fmt.Printf("phase=INDEXER_FRESH %s\n", strings.TrimSpace(m.sendRaw(http.MethodGet, m.indexer+"/v1/status", nil, nil).body))
	if reconciled != "ok" {
		m.t.Fatalf("reconciliation after the accepted leases: %s", reconciled)
	}
}

func (m *mile) publish(approved bool, approval string) map[string]any {
	m.t.Helper()
	segs := []any{}
	for _, seg := range m.segments {
		segs = append(segs, map[string]any{"segment_id": seg.ID, "sha256": seg.SHA, "byte_count": seg.N, "path": seg.Path})
	}
	res := m.sendJSON(http.MethodPost, m.sched+"/v1/publications", m.operatorHeader(), map[string]any{
		"content_id": m.content, "version": 1, "manifest_hex": m.digest, "terms_hex": hex.EncodeToString(bytes.Repeat([]byte{0x22}, 32)),
		"term_seconds": 3600, "scheduler_hex": hex.EncodeToString(m.svc.key.PubKey().Address()),
		"approved": approved, "approval_id": approval, "segments": segs,
	})
	if res.status != 200 {
		m.t.Fatalf("publication %d %s", res.status, res.body)
	}
	return res.json()
}

func (m *mile) play() {
	m.t.Helper()
	cmd := exec.Command("python3", filepath.Join(m.root, "scripts/milestone/client.py"),
		"--indexer", m.indexer, "--content", m.content, "--delivery", m.deliver, "--ca", m.deliverCA)
	cmd.Env = m.env()
	out := m.runCmd("client", cmd)
	fmt.Print(out)
}

func (m *mile) retry() {
	m.t.Helper()
	first := m.intentRegister(m.content, m.digest, m.pkgBytes, "reg-"+m.content)
	if first["idempotent"] != true && first["status"] != "OPERATION_STATUS_COMMITTED" {
		// A repeated idempotency key returns the committed intent.
	}
	fmt.Printf("phase=RETRY status=%s intent=%s\n", str(first["status"]), str(first["intent_id"]))
	if str(first["status"]) != "OPERATION_STATUS_COMMITTED" {
		m.t.Fatalf("retry %#v", first)
	}
	rows := m.indexerContent()
	n := 0
	for _, row := range rows {
		if str(row["ID"]) == m.content {
			n++
		}
	}
	fmt.Printf("phase=ONE_EFFECT content_rows=%d\n", n)
	if n != 1 {
		m.t.Fatalf("content rows %d", n)
	}
}

func (m *mile) corrupt() {
	m.t.Helper()
	path := m.segments[0].Path
	// Corrupt the provider copy, not the origin file the scheduler hashed.
	lease := "l" + m.content + "p1"
	stored := filepath.Join(m.prov["p1"].dir, "packages", lease, m.segments[0].ID)
	buf, err := os.ReadFile(stored)
	if err != nil {
		m.t.Fatal(err)
	}
	buf[0] ^= 0xff
	if err := os.WriteFile(stored, buf, 0o600); err != nil {
		m.t.Fatal(err)
	}
	_ = path
	view := m.getJSON(m.sched + "/v1/availability?content=" + m.content)
	dist := m.sendRaw(http.MethodGet, m.sched+"/v1/distribution/"+m.content+"/1", nil, nil)
	sum := sha256.Sum256(dist.raw)
	fmt.Printf("phase=CORRUPT availability_verified=%v distribution=%d sha256=%s origin_sha=%s\n", view["verified_transfers"], dist.status, hex.EncodeToString(sum[:]), m.segments[0].SHA)
	if dist.status != 200 {
		m.t.Fatalf("distribution after corruption %d", dist.status)
	}
}

func (m *mile) unauthorized() {
	m.t.Helper()
	// p1 was stopped for the repair. p3 is still serving, so the refusal is
	// the provider's decision and not a dead socket.
	bad := m.providerSend(http.MethodPost, m.prov["p3"].url+"/offers/onotapproved/acceptance", m.prov["p3"].ca, map[string]any{"offer_id": "onotapproved"})
	fmt.Printf("phase=UNAPPROVED_OFFER status=%d\n", bad.status)
	if bad.status != 403 && bad.status != 400 {
		m.t.Fatalf("unapproved %d %s", bad.status, bad.body)
	}
	manifest := bytes.Repeat([]byte{0xcd}, 32)
	m.intentRegister("cbad", hex.EncodeToString(manifest), 64, "reg-cbad")
	start := time.Now().Unix()
	m.intentOffer("ocbadp1", "cbad", "p1", hex.EncodeToString(manifest), 64, start, start+3600)
	_, wrong, err := ed25519.GenerateKey(nil)
	if err != nil {
		m.t.Fatal(err)
	}
	offer := &leasesv1.Offer{
		OfferId: "ocbadp1", ContentId: "cbad", ContentVersion: 1, ProviderId: "p1",
		ManifestDigest: shaDigest(manifest), PackageBytes: 64, TermsDigest: shaDigest(bytes.Repeat([]byte{0x22}, 32)),
		StartUnixSeconds: start, DeadlineUnixSeconds: start + 3600, ExpiryUnixSeconds: start + 3600, TermVersion: 1,
		Scheduler: append([]byte(nil), m.svc.key.PubKey().Address()...),
	}
	signed, err := leases.AcceptSignBytes(offer, "lcbadp1")
	if err != nil {
		m.t.Fatal(err)
	}
	i := bytes.IndexByte(signed, 0)
	canonical := string(signed[i+1:])
	msg := &leasesv1.MsgAcceptLease{
		Signer: m.p1.key.PubKey().Address(), BusinessId: "bad-accept-1", OfferId: "ocbadp1", LeaseId: "lcbadp1",
		ProviderAcceptance: &typesv1.InnerSignature{
			Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, PublicKey: wrong.Public().(ed25519.PublicKey),
			Signature: ed25519.Sign(wrong, signed), KeyId: "kbad", CanonicalProfileVersion: 1,
		},
	}
	_ = canonical
	code, height, hash := m.commitAllow("unauthorized-accept", m.p1, msg)
	want := uint32(typesv1.ErrorCode_value[string(msgkit.Unauthorized)])
	fmt.Printf("phase=UNAUTHORIZED_ACCEPT height=%d code=%d want=%d txhash=%s\n", height, code, want, hash)
	if code != want {
		m.t.Fatalf("unauthorized code %d", code)
	}
}

func (m *mile) faults() {
	m.t.Helper()
	m.postJSON(m.sched+"/v1/origin", map[string]any{"content_id": m.content, "up": false})
	dist := m.sendRaw(http.MethodGet, m.sched+"/v1/distribution/"+m.content+"/1", nil, nil)
	fmt.Printf("phase=ORIGIN_DOWN distribution=%d bytes=%d\n", dist.status, len(dist.raw))
	if dist.status != 200 || len(dist.raw) == 0 {
		m.t.Fatalf("origin down distribution %d", dist.status)
	}
	m.prov["p1"].child.stop()
	view := m.getJSON(m.sched + "/v1/availability?content=" + m.content)
	fmt.Printf("phase=PROVIDER_DOWN reachability=%s degraded=%v\n", str(view["reachability"]), view["degraded_providers"])
	m.waitFresh()
	repair := m.sendJSON(http.MethodPost, m.sched+"/v1/repairs", m.operatorHeader(), map[string]any{"content_id": m.content, "version": 1})
	fmt.Printf("phase=REPAIR status=%d body=%s\n", repair.status, repair.body)
	if repair.status != 200 || repair.json()["state"] != "CONFIRMED" {
		m.t.Fatalf("repair %d %s", repair.status, repair.body)
	}
	m.prov["p2"].child.stop()
	_ = os.Remove(filepath.Join(m.prov["p2"].dir, "listen.json"))
	path := filepath.Join(m.work, "p2.json")
	c := m.start("p2-restart", m.bins["svn-provider"], "run", path)
	m.prov["p2"].child = c
	listen := m.waitFile(filepath.Join(m.prov["p2"].dir, "listen.json"), 20*time.Second)
	var again struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(listen, &again); err != nil {
		m.t.Fatal(err)
	}
	m.prov["p2"].url = strings.TrimRight(again.URL, "/")
	m.postJSON(m.sched+"/v1/hosts", map[string]string{
		"provider_id": "p2", "endpoint": m.prov["p2"].url, "ca_file": m.prov["p2"].ca,
	})
	health := m.providerGet(m.prov["p2"].url+"/health", m.prov["p2"].ca)
	fmt.Printf("phase=PROVIDER_RESTART health=%d\n", health)
	if health != 200 {
		m.t.Fatalf("restart health %d", health)
	}
}

func (m *mile) quorum() {
	m.t.Helper()
	before := m.chainStatus()
	m.vals[2].stop()
	m.vals[3].stop()
	time.Sleep(16 * time.Second)
	during := m.chainStatus()
	play := m.deliveryGet("/v1/playback/" + m.content)
	fmt.Printf("phase=QUORUM_LOST height_before=%s height_during=%s playback=%d\n", before["height"], during["height"], play)
	if before["height"] != during["height"] {
		m.t.Fatalf("height moved during quorum loss %s -> %s", before["height"], during["height"])
	}
	if play != 200 {
		m.t.Fatalf("playback during stall %d", play)
	}
	stalled := m.bcSend(m.bcURL+"/v1/intents", m.registerBody("cstall", hex.EncodeToString(bytes.Repeat([]byte{0x11}, 32)), 32, "reg-cstall"))
	fmt.Printf("phase=NEW_COMMITMENT_PENDING status=%s intent=%s\n", str(stalled["status"]), str(stalled["intent_id"]))
	if str(stalled["status"]) == "OPERATION_STATUS_COMMITTED" {
		m.t.Fatal("a new commitment committed while two validators were down")
	}
	m.vals[2] = m.start("v3", m.bins["svnd"], "start", "--home", m.layout.Topology.Validators[2].Home)
	m.vals[3] = m.start("v4", m.bins["svnd"], "start", "--home", m.layout.Topology.Validators[3].Home)
	var rpcs []string
	for _, node := range m.layout.Topology.Validators {
		rpcs = append(rpcs, node.RPC)
	}
	stallHeight, _ := strconv.ParseInt(during["height"], 10, 64)
	height, appHash, err := devnet.WaitEqual(rpcs, stallHeight+1, 90*time.Second)
	if err != nil {
		m.t.Fatal(err)
	}
	if height <= stallHeight {
		m.t.Fatalf("resume height %d stall %d", height, stallHeight)
	}
	fmt.Printf("phase=QUORUM_RESUMED height=%d app_hash=%s stall_height=%d\n", height, appHash, stallHeight)
}

func (m *mile) withdraw() {
	m.t.Helper()
	why := sha256.Sum256([]byte("milestone withdrawal"))
	m.intentWithdraw(m.content, hex.EncodeToString(why[:]))
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		rows := m.indexerContent()
		for _, row := range rows {
			if str(row["ID"]) == m.content && row["Withdrawn"] == true {
				fmt.Printf("phase=WITHDRAWN indexer=true\n")
				dist := m.sendRaw(http.MethodGet, m.sched+"/v1/distribution/"+m.content+"/1", nil, nil)
				fmt.Printf("phase=WITHDRAWN_DISTRIBUTION status=%d\n", dist.status)
				py := strings.Join([]string{filepath.Join(m.root, "services/delivery"), filepath.Join(m.root, "packages/protocol"), filepath.Join(m.root, "proto/gen/python")}, string(os.PathListSeparator))
				cmd := exec.Command("python3", "-m", "svn_delivery", "remove", "--root", m.worker, "--content", m.content)
				cmd.Env = append(m.env(), "PYTHONPATH="+py)
				fmt.Printf("phase=DELIVERY_REMOVE %s\n", strings.TrimSpace(m.runCmd("remove", cmd)))
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	m.t.Fatal("indexer did not show the withdrawal")
}

func (m *mile) intentRegister(contentID, digest string, size uint64, key string) map[string]any {
	m.t.Helper()
	return m.waitIntent(m.bcSend(m.bcURL+"/v1/intents", m.registerBody(contentID, digest, size, key)))
}

func (m *mile) registerBody(contentID, digest string, size uint64, key string) []byte {
	m.t.Helper()
	rawDigest, err := hex.DecodeString(digest)
	if err != nil {
		m.t.Fatal(err)
	}
	signBytes, err := content.RegisterIntentSignBytes(m.svc.key.PubKey().Address(), contentID, 1, rawDigest, size, content.DevnetRoleName)
	if err != nil {
		m.t.Fatal(err)
	}
	body := map[string]any{
		"kind": "REGISTER_CONTENT", "idempotency_key": key, "expires_in_seconds": 3600,
		"auth_context": m.authContext(),
		"request": map[string]any{
			"content_id": contentID, "content_version": 1, "manifest_digest_hex": digest, "package_bytes": size,
			"hosting_intent": inner(m.intent, signBytes),
		},
	}
	out, err := json.Marshal(body)
	if err != nil {
		m.t.Fatal(err)
	}
	return out
}

func (m *mile) intentOffer(offerID, contentID, providerID, digest string, size uint64, start, deadline int64) {
	m.t.Helper()
	body := map[string]any{
		"kind": "OFFER_LEASE", "idempotency_key": offerID, "expires_in_seconds": 3600,
		"auth_context": m.authContext(),
		"request": map[string]any{
			"offer_id": offerID, "content_id": contentID, "content_version": 1, "provider_id": providerID,
			"manifest_digest_hex": digest, "package_bytes": size, "terms_digest_hex": hex.EncodeToString(bytes.Repeat([]byte{0x22}, 32)),
			"start_unix_seconds": start, "deadline_unix_seconds": deadline, "expiry_unix_seconds": deadline,
		},
	}
	raw, _ := json.Marshal(body)
	m.waitIntent(m.bcSend(m.bcURL+"/v1/intents", raw))
}

func (m *mile) intentWithdraw(contentID, reason string) {
	m.t.Helper()
	rawReason, _ := hex.DecodeString(reason)
	signBytes, err := content.WithdrawIntentSignBytes(m.svc.key.PubKey().Address(), contentID, 1, rawReason, content.DevnetRoleName)
	if err != nil {
		m.t.Fatal(err)
	}
	body := map[string]any{
		"kind": "WITHDRAW_CONTENT", "idempotency_key": "wd-" + contentID, "expires_in_seconds": 3600,
		"auth_context": m.authContext(),
		"request": map[string]any{
			"content_id": contentID, "content_version": 1, "reason_digest_hex": reason,
			"hosting_intent": inner(m.intent, signBytes),
		},
	}
	raw, _ := json.Marshal(body)
	m.waitIntent(m.bcSend(m.bcURL+"/v1/intents", raw))
}

func (m *mile) authContext() map[string]any {
	return map[string]any{
		"principal_kind": "DEV_FIXTURE", "principal_id": "creator-1", "policy_ref": "policy-1",
		"permissions": []string{"PUBLISH_CONTENT", "WITHDRAW_CONTENT", "SCHEDULE_HOSTING", "RELAY_PROVIDER_SIGNED"},
	}
}

func inner(key ed25519.PrivateKey, signBytes []byte) map[string]any {
	i := bytes.IndexByte(signBytes, 0)
	_ = i
	return map[string]any{
		"domain": "SIGNATURE_DOMAIN_HOSTING_INTENT", "key_id": "intent-key-1",
		"public_key_hex":            hex.EncodeToString(key.Public().(ed25519.PublicKey)),
		"signature_hex":             hex.EncodeToString(ed25519.Sign(key, signBytes)),
		"canonical_profile_version": 1,
	}
}

func (m *mile) commitOffer(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		http.Error(w, "malformed", 400)
		return
	}
	offerID := str(body["offer_id"])
	start, _ := strconv.ParseInt(str(body["start_unix_seconds"]), 10, 64)
	deadline, _ := strconv.ParseInt(str(body["deadline_unix_seconds"]), 10, 64)
	size, _ := strconv.ParseUint(str(body["package_bytes"]), 10, 64)
	version, _ := strconv.ParseUint(str(body["content_version"]), 10, 64)
	req := map[string]any{
		"kind": "OFFER_LEASE", "idempotency_key": offerID, "expires_in_seconds": 3600,
		"auth_context": m.authContext(),
		"request": map[string]any{
			"offer_id": offerID, "content_id": str(body["content_id"]), "content_version": version,
			"provider_id": str(body["provider_id"]), "manifest_digest_hex": str(body["manifest_digest_hex"]),
			"package_bytes": size, "terms_digest_hex": str(body["terms_digest_hex"]),
			"start_unix_seconds": start, "deadline_unix_seconds": deadline, "expiry_unix_seconds": deadline,
		},
	}
	raw, _ := json.Marshal(req)
	res := m.waitIntent(m.bcSend(m.bcURL+"/v1/intents", raw))
	if str(res["status"]) != "OPERATION_STATUS_COMMITTED" {
		http.Error(w, str(res["status"])+" "+str(res["reason"]), 502)
		return
	}
	cm, _ := res["commitment"].(map[string]any)
	fmt.Printf("phase=OFFER_COMMITTED offer=%s height=%v txhash=%v\n", offerID, cm["height"], cm["tx_hash"])
	w.WriteHeader(http.StatusNoContent)
}

func (m *mile) bcSend(url string, body []byte) map[string]any {
	m.t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		m.t.Fatal(err)
	}
	res, err := m.bc.Do(req)
	if err != nil {
		m.t.Fatal(err)
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != 200 && res.StatusCode != 201 {
		m.t.Fatalf("broadcaster %d %s", res.StatusCode, buf)
	}
	var doc map[string]any
	if err := json.Unmarshal(buf, &doc); err != nil {
		m.t.Fatal(err)
	}
	return doc
}

func (m *mile) waitIntent(doc map[string]any) map[string]any {
	m.t.Helper()
	id := str(doc["intent_id"])
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		res, err := m.bc.Get(m.bcURL + "/v1/intents/" + id)
		if err != nil {
			m.t.Fatal(err)
		}
		buf, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		var cur map[string]any
		if err := json.Unmarshal(buf, &cur); err != nil {
			m.t.Fatal(err)
		}
		switch str(cur["status"]) {
		case "OPERATION_STATUS_COMMITTED", "OPERATION_STATUS_FAILED", "OPERATION_STATUS_PENDING_STALL":
			if cm, ok := cur["commitment"].(map[string]any); ok && str(cur["status"]) == "OPERATION_STATUS_COMMITTED" {
				fmt.Printf("phase=COMMITTED kind=%s height=%v txhash=%v idempotent=%v\n", str(cur["kind"]), cm["height"], cm["tx_hash"], cm["idempotent_replay"])
			}
			return cur
		}
		time.Sleep(200 * time.Millisecond)
	}
	m.t.Fatalf("intent %s did not finish", id)
	return nil
}

func (m *mile) commit(phase string, who loaded, msg sdk.Msg) {
	m.t.Helper()
	code, height, hash := m.commitAllow(phase, who, msg)
	if code != 0 {
		m.t.Fatalf("%s code %d", phase, code)
	}
	fmt.Printf("phase=%s height=%d code=0 txhash=%s\n", phase, height, hash)
}

func (m *mile) commitAllow(phase string, who loaded, msg sdk.Msg) (uint32, int64, string) {
	m.t.Helper()
	number, sequence := m.account(who)
	raw := m.sign(who.key, number, sequence, msg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := m.rpc.BroadcastTxCommit(ctx, raw)
	if err != nil {
		m.t.Fatalf("%s broadcast: %v", phase, err)
	}
	code := res.TxResult.Code
	if res.CheckTx.Code != 0 {
		code = res.CheckTx.Code
	}
	return code, res.Height, strings.ToUpper(hex.EncodeToString(res.Hash))
}

func (m *mile) council(id string, payload *authorityv1.ProposalPayload) {
	m.t.Helper()
	digest, out := authority.PayloadDigest(payload)
	if !out.Success() {
		m.t.Fatal(out.Code)
	}
	m.commit(id+"-propose", m.seats[0], &authorityv1.MsgProposeChange{
		Signer: m.seats[0].key.PubKey().Address(), BusinessId: "pb-" + id, ProposalId: id,
		ParameterKey: id, BasePolicyVersion: m.policy, Payload: payload,
		PayloadDigest:     &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: append([]byte(nil), digest...)},
		ExpiryUnixSeconds: time.Now().Add(24 * time.Hour).Unix(),
	})
	for i := 0; i < 3; i++ {
		m.commit(fmt.Sprintf("%s-approve-%d", id, i+1), m.seats[i], &authorityv1.MsgApproveChange{
			Signer: m.seats[i].key.PubKey().Address(), BusinessId: fmt.Sprintf("%s-ap-%d", id, i+1), ProposalId: id,
		})
	}
	m.commit(id+"-execute", m.auth, &authorityv1.MsgExecuteChange{
		Signer: m.auth.key.PubKey().Address(), BusinessId: "ex-" + id, ProposalId: id,
	})
	m.policy++
}

func (m *mile) sign(key cryptotypes.PrivKey, number, sequence uint64, msg sdk.Msg) []byte {
	m.t.Helper()
	builder := m.enc.TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		m.t.Fatal(err)
	}
	builder.SetGasLimit(2000000)
	signer := authsigning.SignerData{
		Address: sdk.AccAddress(key.PubKey().Address()).String(), ChainID: m.chainID,
		AccountNumber: number, Sequence: sequence, PubKey: key.PubKey(),
	}
	placeholder := signingtypes.SignatureV2{PubKey: key.PubKey(), Sequence: sequence, Data: &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT}}
	if err := builder.SetSignatures(placeholder); err != nil {
		m.t.Fatal(err)
	}
	signature, err := tx.SignWithPrivKey(context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT, signer, builder, key, m.enc.TxConfig, sequence)
	if err != nil {
		m.t.Fatal(err)
	}
	if err := builder.SetSignatures(signature); err != nil {
		m.t.Fatal(err)
	}
	raw, err := m.enc.TxConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		m.t.Fatal(err)
	}
	return raw
}

func (m *mile) account(who loaded) (uint64, uint64) {
	m.t.Helper()
	data, err := proto.Marshal(&authtypes.QueryAccountRequest{Address: sdk.AccAddress(who.key.PubKey().Address()).String()})
	if err != nil {
		m.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := m.rpc.ABCIQuery(ctx, "/cosmos.auth.v1beta1.Query/Account", data)
	if err != nil || res.Response.Code != 0 {
		m.t.Fatalf("account %s: %v", who.name, err)
	}
	var resp authtypes.QueryAccountResponse
	if err := m.enc.Codec.Unmarshal(res.Response.Value, &resp); err != nil {
		m.t.Fatal(err)
	}
	var acc sdk.AccountI
	if err := m.enc.InterfaceRegistry.UnpackAny(resp.Account, &acc); err != nil || acc == nil {
		m.t.Fatal(err)
	}
	return acc.GetAccountNumber(), acc.GetSequence()
}

func (m *mile) chainStatus() map[string]string {
	m.t.Helper()
	res, err := http.Get(m.layout.Topology.Validators[0].RPC + "/status")
	if err != nil {
		m.t.Fatal(err)
	}
	defer res.Body.Close()
	var doc struct {
		Result struct {
			SyncInfo struct {
				Height  string `json:"latest_block_height"`
				AppHash string `json:"latest_app_hash"`
			} `json:"sync_info"`
		} `json:"result"`
	}
	if err := json.NewDecoder(res.Body).Decode(&doc); err != nil {
		m.t.Fatal(err)
	}
	return map[string]string{"height": doc.Result.SyncInfo.Height, "app_hash": doc.Result.SyncInfo.AppHash}
}

func (m *mile) waitFresh() {
	m.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		res := m.sendRaw(http.MethodGet, m.indexer+"/v1/status", nil, nil)
		last = res.body
		var doc struct {
			Stale bool `json:"stale"`
		}
		if json.Unmarshal(res.raw, &doc) == nil && !doc.Stale {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	m.t.Fatalf("indexer stayed stale %s", last)
}

func (m *mile) waitIndexer(state string) {
	m.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		doc := m.getJSON(m.indexer + "/v1/providers")
		rows, _ := doc["providers"].([]any)
		found := 0
		for _, row := range rows {
			item, _ := row.(map[string]any)
			if str(item["State"]) == state {
				found++
			}
		}
		if found >= 4 {
			fmt.Printf("phase=INDEXER_PROVIDERS admitted=%d\n", found)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	m.t.Fatal("indexer did not show four admitted providers")
}

func (m *mile) indexerContent() []map[string]any {
	m.t.Helper()
	doc := m.getJSON(m.indexer + "/v1/content")
	rows, _ := doc["content"].([]any)
	var out []map[string]any
	for _, row := range rows {
		item, _ := row.(map[string]any)
		out = append(out, item)
	}
	return out
}

func shaDigest(v []byte) *typesv1.Digest {
	sum := sha256.Sum256(v)
	if len(v) == 32 {
		sum = sha256.Sum256(nil)
		copy(sum[:], v)
		// The caller already holds the digest bytes. Do not hash them again.
		return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: append([]byte(nil), v...)}
	}
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sum[:]}
}

func (m *mile) bech(key cryptotypes.PrivKey) string {
	return sdk.AccAddress(key.PubKey().Address()).String()
}

func (m *mile) env() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = m.work
	}
	path := "/usr/bin:/bin:/usr/lib/postgresql/18/bin"
	// SVN_MILESTONE_PATH is read in this process. The child environment
	// replaces PATH, so a venv on the parent PATH would not reach python3.
	if extra := os.Getenv("SVN_MILESTONE_PATH"); extra != "" {
		path = extra + ":" + path
	}
	return []string{"PATH=" + path, "TZ=UTC", "LC_ALL=C.UTF-8", "HOME=" + home}
}

func (m *mile) start(name, bin string, args ...string) *child {
	cmd := exec.Command(bin, args...)
	cmd.Env = m.env()
	return m.startCmd(name, cmd)
}

func (m *mile) startCmd(name string, cmd *exec.Cmd) *child {
	m.t.Helper()
	c := &child{name: name, cmd: cmd, out: &syncBuf{}, done: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = c.out, c.out
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		m.t.Fatalf("start %s: %v", name, err)
	}
	go func() { _ = cmd.Wait(); close(c.done) }()
	m.t.Cleanup(func() { c.stop() })
	return c
}

func (c *child) stop() {
	c.once.Do(func() {
		if c.cmd.Process == nil {
			return
		}
		_ = c.cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-c.done:
		case <-time.After(15 * time.Second):
			_ = c.cmd.Process.Kill()
			<-c.done
		}
	})
}

func (m *mile) run(name, bin string, args ...string) string {
	cmd := exec.Command(bin, args...)
	cmd.Env = m.env()
	return m.runCmd(name, cmd)
}

func (m *mile) runCmd(name string, cmd *exec.Cmd) string {
	m.t.Helper()
	if cmd.Env == nil {
		cmd.Env = m.env()
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		m.t.Fatalf("%s: %v\n%s", name, err, buf.String())
	}
	return buf.String()
}

func (m *mile) waitLine(c *child, needle string, d time.Duration) string {
	m.t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(c.out.String(), "\n") {
			if strings.Contains(line, needle) {
				return line
			}
		}
		select {
		case <-c.done:
			m.t.Fatalf("%s exited waiting for %s\n%s", c.name, needle, c.out.String())
		case <-time.After(50 * time.Millisecond):
		}
	}
	m.t.Fatalf("%s timeout waiting for %s\n%s", c.name, needle, c.out.String())
	return ""
}

func (m *mile) waitPrefixed(c *child, prefix string, d time.Duration) string {
	return m.waitLine(c, prefix, d)
}

func (m *mile) waitFileOrExit(c *child, path string, d time.Duration) {
	m.t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-c.done:
			m.t.Fatalf("%s exited before %s\n%s", c.name, path, c.out.String())
		case <-time.After(50 * time.Millisecond):
		}
	}
	m.t.Fatalf("%s timeout before %s\n%s", c.name, path, c.out.String())
}

func (m *mile) waitFile(path string, d time.Duration) []byte {
	m.t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		buf, err := os.ReadFile(path)
		if err == nil && len(buf) > 0 {
			return buf
		}
		time.Sleep(50 * time.Millisecond)
	}
	m.t.Fatalf("missing %s", path)
	return nil
}

func (m *mile) waitHTTP(client *http.Client, url string, want int, d time.Duration, c *child) {
	m.t.Helper()
	deadline := time.Now().Add(d)
	var last error
	for time.Now().Before(deadline) {
		res, err := client.Get(url)
		if err == nil {
			res.Body.Close()
			if res.StatusCode == want || (want == 404 && res.StatusCode == 404) {
				return
			}
			last = fmt.Errorf("status %d", res.StatusCode)
		} else {
			last = err
		}
		select {
		case <-c.done:
			m.t.Fatalf("%s exited: %v\n%s", c.name, last, c.out.String())
		case <-time.After(100 * time.Millisecond):
		}
	}
	m.t.Fatalf("%s not ready: %v\n%s", c.name, last, c.out.String())
}

type httpResult struct {
	status int
	body   string
	raw    []byte
}

func (r httpResult) json() map[string]any {
	var doc map[string]any
	_ = json.Unmarshal(r.raw, &doc)
	return doc
}

func (m *mile) sendJSON(method, url string, header map[string]string, body any) httpResult {
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	return m.sendRaw(method, url, header, raw)
}

func (m *mile) sendRaw(method, url string, header map[string]string, body []byte) httpResult {
	m.t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		m.t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		m.t.Fatal(err)
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	return httpResult{status: res.StatusCode, body: string(buf), raw: buf}
}

func (m *mile) creatorHeader() map[string]string {
	return map[string]string{"Authorization": "Bearer " + m.creatorToken}
}

func (m *mile) operatorHeader() map[string]string {
	return map[string]string{"Authorization": "Bearer " + m.operatorToken}
}

func (m *mile) checkSchedulerAuth() {
	m.t.Helper()
	anonymous := m.sendJSON(http.MethodPost, m.sched+"/v1/publications", nil, map[string]any{"content_id": "x"})
	creator := m.sendJSON(http.MethodPost, m.sched+"/v1/publications", m.creatorHeader(), map[string]any{"content_id": "x"})
	fmt.Printf("phase=SCHEDULER_AUTH anonymous=%d creator=%d\n", anonymous.status, creator.status)
	if anonymous.status != http.StatusUnauthorized || creator.status != http.StatusUnauthorized {
		m.t.Fatalf("scheduler auth anonymous=%d creator=%d", anonymous.status, creator.status)
	}
}

func (m *mile) postJSON(url string, body any) {
	res := m.sendJSON(http.MethodPost, url, m.operatorHeader(), body)
	if res.status >= 300 {
		m.t.Fatalf("POST %s %d %s", url, res.status, res.body)
	}
}

func (m *mile) getJSON(url string) map[string]any {
	res := m.sendRaw(http.MethodGet, url, nil, nil)
	if res.status != 200 {
		m.t.Fatalf("GET %s %d %s", url, res.status, res.body)
	}
	return res.json()
}

func (m *mile) providerSend(method, url, caFile string, body any) httpResult {
	m.t.Helper()
	pemBytes, err := os.ReadFile(caFile)
	if err != nil {
		m.t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pemBytes)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(raw))
	if err != nil {
		m.t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		m.t.Fatal(err)
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return httpResult{status: res.StatusCode, body: string(buf), raw: buf}
}

func (m *mile) providerGet(url, caFile string) int {
	m.t.Helper()
	pemBytes, err := os.ReadFile(caFile)
	if err != nil {
		m.t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pemBytes)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}}
	res, err := client.Get(url)
	if err != nil {
		m.t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func (m *mile) deliveryGet(path string) int {
	m.t.Helper()
	pemBytes, err := os.ReadFile(m.deliverCA)
	if err != nil {
		m.t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(pemBytes)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12, ServerName: "localhost"}}}
	res, err := client.Get(m.deliver + path)
	if err != nil {
		m.t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func dest(host string, port int) map[string]any {
	return map[string]any{"host": host, "ip": host, "port": port}
}

func portOf(t interface{ Fatal(...any) }, hostport string) int {
	_, port, err := net.SplitHostPort(hostport)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func freePort(t interface{ Fatal(...any) }) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	n := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return n
}

func writeJSON(t interface{ Fatal(...any) }, path string, v any) {
	writeJSONMode(t, path, v, 0o644)
}

func writeJSONMode(t interface{ Fatal(...any) }, path string, v any, mode os.FileMode) {
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(body, '\n'), mode); err != nil {
		t.Fatal(err)
	}
}

func copyMode(t interface{ Fatal(...any) }, src, dst string) {
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(path string) {
	if err := os.MkdirAll(path, 0o700); err != nil {
		panic(err)
	}
}

func jsonLineContaining(text, needle string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, needle) && strings.HasPrefix(line, "{") {
			return line
		}
	}
	return strings.TrimSpace(text)
}

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	default:
		return ""
	}
}

func mtls(t interface{ Fatal(...any) }, caFile, certFile, keyFile string) *http.Client {
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("ca")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{cert}, RootCAs: pool, MinVersion: tls.VersionTLS12,
	}}}
}
