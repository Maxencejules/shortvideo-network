package agent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/disk"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/fetch"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/manifest"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/sign"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/store"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Result is the acceptance response. Committed is false unless a chain
// observation already says the lease is active. Broadcast is not that observation.
type Result struct {
	Readiness    string `json:"readiness"`
	LeaseID      string `json:"lease_id"`
	SignatureHex string `json:"signature_hex"`
	PublicHex    string `json:"public_key_hex"`
	KeyID        string `json:"key_id"`
	Canonical    string `json:"canonical"`
	ServeToken   string `json:"serve_token"`
	Broadcast    bool   `json:"broadcast"`
	Committed    bool   `json:"committed"`
}

type offerBody struct {
	OfferID        string    `json:"offer_id"`
	LeaseID        string    `json:"lease_id"`
	BusinessID     string    `json:"business_id"`
	ContentID      string    `json:"content_id"`
	ContentVersion string    `json:"content_version"`
	ProviderID     string    `json:"provider_id"`
	ManifestHex    string    `json:"manifest_digest_hex"`
	PackageBytes   string    `json:"package_bytes"`
	TermsHex       string    `json:"terms_digest_hex"`
	Start          string    `json:"start_unix_seconds"`
	Deadline       string    `json:"deadline_unix_seconds"`
	Expiry         string    `json:"expiry_unix_seconds"`
	TermVersion    string    `json:"term_version"`
	SchedulerHex   string    `json:"scheduler_hex"`
	SourceURL      string    `json:"source_url"`
	Segments       []segment `json:"segments"`
}

type segment struct {
	ID        string `json:"segment_id"`
	SHA256    string `json:"sha256"`
	ByteCount string `json:"byte_count"`
}

// Accept checks policy, disk, and quota, fetches the package, and signs the
// exact terms. A repeated identical offer returns the same signature and
// does not reserve twice. A different body for the same offer is a conflict.
func (a *Agent) Accept(offerID string, body []byte) (Result, error) {
	var in offerBody
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return Result{}, fail(httpStatus(400), "malformed")
	}
	if in.OfferID != offerID {
		return Result{}, fail(400, "malformed")
	}
	terms, segs, err := a.parseOffer(in)
	if err != nil {
		return Result{}, err
	}
	a.mu.Lock()
	if a.st.Draining {
		a.mu.Unlock()
		return Result{}, fail(409, "draining")
	}
	if existing, ok := a.st.OfferLease[in.OfferID]; ok {
		lease := a.st.Leases[existing]
		a.mu.Unlock()
		if lease == nil {
			return Result{}, fail(409, "conflict")
		}
		return a.repeat(lease, terms, in.LeaseID)
	}
	if !a.approved(in.OfferID) {
		a.mu.Unlock()
		return Result{}, fail(403, "unapproved offer")
	}
	if err := a.reserve(terms.PackageBytes); err != nil {
		a.mu.Unlock()
		return Result{}, err
	}
	token, err := newToken()
	if err != nil {
		a.release(terms.PackageBytes)
		a.mu.Unlock()
		return Result{}, fail(500, "token")
	}
	lease := &store.Lease{
		OfferID: in.OfferID, LeaseID: in.LeaseID, BusinessID: in.BusinessID,
		ContentID: in.ContentID, ContentVersion: terms.ContentVersion, PackageBytes: terms.PackageBytes,
		ManifestSHA: hex.EncodeToString(terms.Manifest[:]), TermsSHA: hex.EncodeToString(terms.TermsDigest[:]),
		Start: terms.Start, Deadline: terms.Deadline, Expiry: terms.Expiry, TermVersion: terms.TermVersion,
		SchedulerHex: in.SchedulerHex, Readiness: store.Transferring, ServeToken: token,
	}
	for _, seg := range segs {
		lease.Segments = append(lease.Segments, seg.ID)
	}
	a.st.Leases[in.LeaseID] = lease
	a.st.OfferLease[in.OfferID] = in.LeaseID
	stage := filepath.Join(a.cfg.DataDir, "staging", in.LeaseID)
	if err := os.MkdirAll(stage, 0o700); err != nil {
		a.release(terms.PackageBytes)
		delete(a.st.Leases, in.LeaseID)
		delete(a.st.OfferLease, in.OfferID)
		a.mu.Unlock()
		return Result{}, err
	}
	if err := os.WriteFile(filepath.Join(stage, "partial"), []byte("started"), 0o600); err != nil {
		a.release(terms.PackageBytes)
		delete(a.st.Leases, in.LeaseID)
		delete(a.st.OfferLease, in.OfferID)
		a.mu.Unlock()
		return Result{}, err
	}
	if err := a.save(); err != nil {
		a.dropTransfer(lease)
		_ = a.save()
		a.mu.Unlock()
		return Result{}, err
	}
	a.mu.Unlock()

	if err := a.transfer(lease, in.SourceURL, segs, terms); err != nil {
		a.mu.Lock()
		a.dropTransfer(lease)
		_ = a.save()
		a.mu.Unlock()
		return Result{}, err
	}

	a.mu.Lock()
	sig, canonical, err := sign.Accept(a.key.Private(), a.st.KeyID, in.LeaseID, terms)
	if err != nil {
		a.dropTransfer(lease)
		_ = a.save()
		a.mu.Unlock()
		return Result{}, err
	}
	lease.Readiness = store.AcceptancePending
	lease.KeyID = a.st.KeyID
	lease.PublicHex = hex.EncodeToString(sig.GetPublicKey())
	lease.SignatureHex = hex.EncodeToString(sig.GetSignature())
	lease.Canonical = canonical
	if err := a.save(); err != nil {
		a.mu.Unlock()
		return Result{}, err
	}
	a.mu.Unlock()
	view, verr := a.poll()
	var raw []byte
	if verr == nil {
		a.mu.Lock()
		a.apply(view, nil)
		uncertain := a.st.Uncertain
		a.mu.Unlock()
		if !uncertain {
			raw, err = a.buildTx(lease, sig, view)
			if err != nil {
				return Result{}, err
			}
		}
	} else {
		a.mu.Lock()
		a.apply(view, verr)
		_ = a.save()
		a.mu.Unlock()
	}
	broadcast := false
	if raw != nil {
		if a.broadcast(raw) == nil {
			broadcast = true
		}
	}
	a.finish(lease)
	a.mu.Lock()
	if broadcast {
		lease.Broadcast = true
	}
	res := a.result(lease, broadcast)
	_ = a.save()
	a.mu.Unlock()
	return res, nil
}

func (a *Agent) repeat(lease *store.Lease, terms sign.Terms, leaseID string) (Result, error) {
	canonical, err := sign.Canonical(lease.LeaseID, terms)
	if err != nil {
		return Result{}, err
	}
	if leaseID != lease.LeaseID || canonical != lease.Canonical {
		return Result{}, fail(409, "conflict")
	}
	a.finish(lease)
	a.mu.Lock()
	res := a.result(lease, lease.Broadcast)
	a.mu.Unlock()
	return res, nil
}

func (a *Agent) result(lease *store.Lease, broadcast bool) Result {
	return Result{
		Readiness: lease.Readiness, LeaseID: lease.LeaseID, SignatureHex: lease.SignatureHex,
		PublicHex: lease.PublicHex, KeyID: lease.KeyID, Canonical: lease.Canonical,
		ServeToken: lease.ServeToken, Broadcast: broadcast,
		Committed: lease.Readiness == store.Committed || lease.Readiness == store.Serving,
	}
}

func (a *Agent) parseOffer(in offerBody) (sign.Terms, []manifest.Segment, error) {
	var zero sign.Terms
	if in.ProviderID != a.cfg.ProviderID || !msgkit.ValidID(in.OfferID) || !msgkit.ValidID(in.LeaseID) || !msgkit.ValidID(in.BusinessID) || !msgkit.ValidID(in.ContentID) {
		return zero, nil, fail(400, "malformed")
	}
	version, err1 := parseU(in.ContentVersion)
	pkg, err2 := parseU(in.PackageBytes)
	term, err3 := parseU(in.TermVersion)
	start, err4 := parseI(in.Start)
	deadline, err5 := parseI(in.Deadline)
	expiry, err6 := parseI(in.Expiry)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil || version == 0 || term == 0 || pkg == 0 {
		return zero, nil, fail(400, "malformed")
	}
	if start < genesis.UnixSecondsMin || deadline > genesis.UnixSecondsMax || deadline <= start || expiry <= start {
		return zero, nil, fail(400, "malformed")
	}
	if deadline-start > a.cfg.MaxTermSeconds {
		return zero, nil, fail(400, "term")
	}
	if deadline <= time.Now().Unix() {
		return zero, nil, fail(409, "expired")
	}
	if pkg > a.cfg.MaxPackageBytes || pkg > genesis.MaxPackageBytes {
		return zero, nil, fail(400, "bounds")
	}
	manifestSum, err := parseSum(in.ManifestHex)
	if err != nil {
		return zero, nil, fail(400, "malformed")
	}
	termsSum, err := parseSum(in.TermsHex)
	if err != nil {
		return zero, nil, fail(400, "malformed")
	}
	sched, err := hex.DecodeString(in.SchedulerHex)
	if err != nil || len(sched) != genesis.AccountBytes {
		return zero, nil, fail(400, "malformed")
	}
	if len(in.Segments) == 0 || len(in.Segments) > 8 {
		return zero, nil, fail(400, "malformed")
	}
	var total uint64
	segs := make([]manifest.Segment, 0, len(in.Segments))
	seen := map[string]bool{}
	for _, seg := range in.Segments {
		if !msgkit.ValidID(seg.ID) || seen[seg.ID] || len(seg.SHA256) != 64 {
			return zero, nil, fail(400, "malformed")
		}
		if _, err := hex.DecodeString(seg.SHA256); err != nil {
			return zero, nil, fail(400, "malformed")
		}
		n, err := parseU(seg.ByteCount)
		if err != nil || n == 0 || total > ^uint64(0)-n {
			return zero, nil, fail(400, "malformed")
		}
		total += n
		seen[seg.ID] = true
		segs = append(segs, manifest.Segment{ID: seg.ID, SHA256: seg.SHA256, ByteCount: n})
	}
	if total != pkg {
		return zero, nil, fail(400, "bounds")
	}
	text, err := manifest.Canonical("m"+in.ContentID, in.ContentID, version, pkg, segs)
	if err != nil {
		return zero, nil, fail(400, "malformed")
	}
	if sha256.Sum256([]byte(text)) != manifestSum {
		return zero, nil, fail(422, "integrity")
	}
	return sign.Terms{
		OfferID: in.OfferID, ContentID: in.ContentID, ContentVersion: version, ProviderID: in.ProviderID,
		Manifest: manifestSum, PackageBytes: pkg, TermsDigest: termsSum, Start: start, Deadline: deadline,
		Expiry: expiry, TermVersion: term, Scheduler: sched,
	}, segs, nil
}

func (a *Agent) approved(offerID string) bool {
	raw, err := os.ReadFile(filepath.Join(a.cfg.DataDir, "approvals.json"))
	if err != nil {
		return false
	}
	var file struct {
		OfferIDs []string `json:"offer_ids"`
	}
	if json.Unmarshal(raw, &file) != nil {
		return false
	}
	for _, id := range file.OfferIDs {
		if id == offerID {
			return true
		}
	}
	return false
}

func (a *Agent) reserve(n uint64) error {
	if a.st.UsedBytes > ^uint64(0)-n {
		return fail(409, "insufficient quota")
	}
	if a.st.UsedBytes+n > a.cfg.QuotaBytes || a.st.UsedBytes+n > a.cfg.DeclaredCapacityBytes {
		return fail(409, "insufficient quota")
	}
	if a.cfg.BandwidthBytesPerSecond > ^uint64(0)/a.cfg.TransferBudgetSeconds {
		return fail(409, "bandwidth limit")
	}
	if n > a.cfg.BandwidthBytesPerSecond*a.cfg.TransferBudgetSeconds {
		return fail(409, "bandwidth limit")
	}
	avail, err := disk.Available(a.cfg.DataDir)
	if err != nil {
		return fail(409, "insufficient disk")
	}
	if n > ^uint64(0)-a.cfg.MinFreeBytes || avail < n+a.cfg.MinFreeBytes {
		return fail(409, "insufficient disk")
	}
	a.st.UsedBytes += n
	return nil
}

func (a *Agent) release(n uint64) {
	if a.st.UsedBytes >= n {
		a.st.UsedBytes -= n
	}
}

func (a *Agent) dropTransfer(lease *store.Lease) {
	if lease.Readiness == store.Transferring || lease.Readiness == store.Staged || lease.Readiness == store.AcceptancePending {
		os.RemoveAll(filepath.Join(a.cfg.DataDir, "staging", lease.LeaseID))
		if lease.Readiness == store.Transferring {
			os.RemoveAll(packageDir(a.cfg.DataDir, lease))
		}
	}
	if lease.Readiness == store.Transferring || lease.SignatureHex == "" {
		a.release(lease.PackageBytes)
		delete(a.st.Leases, lease.LeaseID)
		delete(a.st.OfferLease, lease.OfferID)
		os.RemoveAll(packageDir(a.cfg.DataDir, lease))
	}
}

func (a *Agent) transfer(lease *store.Lease, source string, segs []manifest.Segment, terms sign.Terms) error {
	lim := fetch.Limit{
		Timeout: time.Duration(a.cfg.TransferBudgetSeconds) * time.Second,
		Token:   a.source, CA: a.sourceC, Allow: a.cfg.Destinations,
	}
	lim.MaxBytes = 1 << 20
	manifestURL, err := joinURL(source, "manifest")
	if err != nil {
		return fail(400, "destination refused")
	}
	body, err := fetch.Bytes(a.ctx, manifestURL, lim)
	if err != nil {
		return fetchError(err)
	}
	text, err := manifest.Canonical("m"+lease.ContentID, lease.ContentID, lease.ContentVersion, lease.PackageBytes, segs)
	if err != nil || string(body) != text || sha256.Sum256(body) != terms.Manifest {
		return fail(422, "integrity")
	}
	dir := packageDir(a.cfg.DataDir, lease)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	for _, seg := range segs {
		segURL, err := joinURL(source, "segments", seg.ID)
		if err != nil {
			return fail(400, "destination refused")
		}
		lim.MaxBytes = seg.ByteCount
		buf, err := fetch.Bytes(a.ctx, segURL, lim)
		if err != nil {
			return fetchError(err)
		}
		sum := sha256.Sum256(buf)
		if uint64(len(buf)) != seg.ByteCount || hex.EncodeToString(sum[:]) != seg.SHA256 {
			return fail(422, "integrity")
		}
		path := filepath.Join(dir, seg.ID)
		if err := os.WriteFile(path, buf, 0o600); err != nil {
			return err
		}
	}
	os.RemoveAll(filepath.Join(a.cfg.DataDir, "staging", lease.LeaseID))
	a.mu.Lock()
	lease.Readiness = store.Staged
	err = a.save()
	a.mu.Unlock()
	return err
}

func (a *Agent) buildTx(lease *store.Lease, sig *typesv1.InnerSignature, view chainView) ([]byte, error) {
	signer := a.key.SDK()
	addr := a.key.Address()
	if a.outer != nil {
		// The outer account is a devnet submitter. The ed25519 key still
		// signs the acceptance. A service key is not accepted here.
		signer = a.outer
		addr = a.outer.PubKey().Address()
	}
	msg := &leasesv1.MsgAcceptLease{
		Signer: addr, BusinessId: lease.BusinessID, OfferId: lease.OfferID,
		LeaseId: lease.LeaseID, ProviderAcceptance: sig,
	}
	timeout := view.Height + 20
	if view.Height > ^uint64(0)-20 {
		return nil, fail(409, "chain")
	}
	return sign.BuildTx(msg, signer, sign.Input{
		ChainID: a.cfg.ChainID, AccountNumber: view.Account, Sequence: view.Sequence,
		TimeoutHeight: timeout, Gas: 200000,
	})
}

func fetchError(err error) error {
	if err == nil {
		return nil
	}
	text := err.Error()
	switch {
	case strings.Contains(text, fetch.ErrRedirect.Error()):
		return fail(400, "redirect refused")
	case strings.Contains(text, fetch.ErrUnbounded.Error()):
		return fail(400, "unbounded response")
	case strings.Contains(text, fetch.ErrRefused.Error()):
		return fail(400, "destination refused")
	default:
		return fail(502, "transfer")
	}
}

func joinURL(base string, parts ...string) (string, error) {
	if strings.Contains(base, "..") || strings.HasSuffix(base, "/") {
		return "", errJoin
	}
	out := base
	for _, p := range parts {
		if !msgkit.ValidID(p) && p != "manifest" && p != "segments" {
			return "", errJoin
		}
		out += "/" + p
	}
	return out, nil
}

var errJoin = fail(400, "destination refused")

func parseU(s string) (uint64, error) { return strconv.ParseUint(s, 10, 64) }
func parseI(s string) (int64, error)  { return strconv.ParseInt(s, 10, 64) }

func parseSum(s string) ([32]byte, error) {
	var out [32]byte
	buf, err := hex.DecodeString(s)
	if err != nil || len(buf) != 32 {
		return out, errSum
	}
	copy(out[:], buf)
	return out, nil
}

var errSum = fail(400, "malformed")

func newToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func httpStatus(n int) int { return n }
