package obs

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type manualClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

func testService(t *testing.T) (*Service, *manualClock) {
	t.Helper()
	clk := &manualClock{at: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	svc, err := New(Options{Token: "synthetic-observe-25", Now: clk.Now})
	if err != nil {
		t.Fatal(err)
	}
	return svc, clk
}

func readyDeps(t *testing.T, svc *Service) {
	t.Helper()
	if err := svc.ReportDep("indexer", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.ReportDep("database", true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Observe(Observation{Component: CompChain, Kind: ObsCommitAge, Value: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Observe(Observation{Component: CompChain, Kind: ObsQuorumLost, Value: 0}); err != nil {
		t.Fatal(err)
	}
}

func TestCorrelationAcrossComponents(t *testing.T) {
	svc, _ := testService(t)
	id, err := NewCorrelationID()
	if err != nil {
		t.Fatal(err)
	}
	for _, component := range RequiredComponents {
		got, err := svc.Observe(Observation{
			Component: component, CorrelationID: id, Kind: ObsQueueRetries, Value: 1,
		})
		if err != nil {
			t.Fatal(component, err)
		}
		if got != id {
			t.Fatalf("id changed for %s", component)
		}
	}
	if _, err := svc.Observe(Observation{
		Component: CompChain, Kind: ObsDelivered, Class: ClassUserPayload, Value: 1,
	}); err != ErrOffChain {
		t.Fatalf("chain media err %v", err)
	}
	trace := svc.Trace(id)
	if len(trace) != len(RequiredComponents) || svc.ChainSubmissions() != 0 {
		t.Fatalf("trace %v submissions %d", trace, svc.ChainSubmissions())
	}
	emailID, err := svc.Observe(Observation{
		Component: CompAPI, CorrelationID: "person@example.com", Kind: ObsQueueRetries, Value: 1,
	})
	if err != nil || !correlationOK(emailID) || strings.Contains(emailID, "@") {
		t.Fatalf("identity id %q %v", emailID, err)
	}
	t.Logf("TRACE components=%d chain_submissions=%d", len(trace), svc.ChainSubmissions())
}

func TestReadinessFailsClosed(t *testing.T) {
	svc, _ := testService(t)
	ok, bad := svc.Ready()
	if ok || len(bad) != 3 {
		t.Fatalf("fresh ready=%v unusable=%v", ok, bad)
	}
	readyDeps(t, svc)
	ok, bad = svc.Ready()
	if !ok || len(bad) != 0 {
		t.Fatalf("recovered ready=%v unusable=%v", ok, bad)
	}
	if err := svc.ReportDep("database", false); err != nil {
		t.Fatal(err)
	}
	ok, bad = svc.Ready()
	if ok || len(bad) != 1 || bad[0] != "database" {
		t.Fatalf("database ready=%v unusable=%v", ok, bad)
	}
	if err := svc.ReportDep("chain", true); err == nil {
		t.Fatal("chain dependency was marked usable without a reading")
	}
	t.Logf("READY fresh=false database_down=true diagnosis=%s", svc.Diagnosis())
}

func TestFaultInjectionAndRecovery(t *testing.T) {
	svc, _ := testService(t)
	readyDeps(t, svc)

	if _, err := svc.Observe(Observation{Component: CompJobs, Kind: ObsProcessingAge, Value: 31}); err != nil {
		t.Fatal(err)
	}
	slow, err := svc.Admit(KindProcessing, 1, "")
	if err != nil || slow.Granted || slow.Reason != "intake_stopped" {
		t.Fatalf("slow %#v %v", slow, err)
	}
	if _, err := svc.Observe(Observation{Component: CompJobs, Kind: ObsProcessingAge, Value: 1}); err != nil {
		t.Fatal(err)
	}
	recovered, err := svc.Admit(KindProcessing, 1, "proc-1")
	if err != nil || !recovered.Granted {
		t.Fatalf("processing recovery %#v %v", recovered, err)
	}

	if _, err := svc.Observe(Observation{Component: CompProviders, Kind: ObsDiskFree, Value: 100}); err != nil {
		t.Fatal(err)
	}
	low, err := svc.Admit(KindStorage, 100, "")
	if err != nil || low.Granted || low.Reason != "low_disk" {
		t.Fatalf("disk %#v %v", low, err)
	}
	if _, err := svc.Observe(Observation{Component: CompProviders, Kind: ObsDiskFree, Value: 100000}); err != nil {
		t.Fatal(err)
	}
	diskOK, err := svc.Admit(KindStorage, 100, "disk-1")
	if err != nil || !diskOK.Granted {
		t.Fatalf("disk recovery %#v %v", diskOK, err)
	}

	if _, err := svc.Observe(Observation{Component: CompChain, Kind: ObsCommitAge, Value: 21}); err != nil {
		t.Fatal(err)
	}
	ready, _ := svc.Ready()
	if ready || svc.Diagnosis() != "stale_commit" {
		t.Fatalf("stale ready=%v diagnosis=%s", ready, svc.Diagnosis())
	}
	audit, err := svc.Admit(KindAudit, 1, "")
	if err != nil || audit.Granted || audit.Reason != "chain_unusable" {
		t.Fatalf("audit %#v %v", audit, err)
	}
	if _, err := svc.Observe(Observation{Component: CompIndexer, Kind: ObsIndexLag, Value: 4}); err != nil {
		t.Fatal(err)
	}
	if svc.Diagnosis() != "stale_commit+index_lag" {
		t.Fatalf("combined diagnosis %s", svc.Diagnosis())
	}
	if _, err := svc.Observe(Observation{Component: CompChain, Kind: ObsCommitAge, Value: 1}); err != nil {
		t.Fatal(err)
	}
	ready, _ = svc.Ready()
	if !ready || svc.Diagnosis() != "index_lag" {
		t.Fatalf("lag ready=%v diagnosis=%s", ready, svc.Diagnosis())
	}

	quorum, _ := testService(t)
	readyDeps(t, quorum)
	if _, err := quorum.Observe(Observation{Component: CompChain, Kind: ObsQuorumLost, Value: 1}); err != nil {
		t.Fatal(err)
	}
	qReady, _ := quorum.Ready()
	if qReady || quorum.Diagnosis() != "quorum_loss" {
		t.Fatalf("quorum ready=%v diagnosis=%s", qReady, quorum.Diagnosis())
	}
	if _, err := quorum.Observe(Observation{Component: CompIndexer, Kind: ObsIndexLag, Value: 2}); err != nil {
		t.Fatal(err)
	}
	both := quorum.Diagnosis()
	if both == "index_lag" || !strings.Contains(both, "quorum_loss") || !strings.Contains(both, "index_lag") {
		t.Fatalf("quorum and lag were not distinct: %s", both)
	}

	repair, _ := testService(t)
	if _, err := repair.NoteRepairFailure(CompProviders, ""); err != nil {
		t.Fatal(err)
	}
	denied, err := repair.Admit(KindStorage, 100, "")
	if err != nil || denied.Reason != "repair_backlog" {
		t.Fatalf("repair %#v %v", denied, err)
	}
	before := repair.Snapshot()
	if before.DeliveredBytes[ClassRepair] != 0 || before.Counters["repair_failures"] != 1 {
		t.Fatalf("repair bytes %#v", before)
	}
	if _, err := repair.Observe(Observation{Component: CompProviders, Kind: ObsRepairBacklog, Value: 0}); err != nil {
		t.Fatal(err)
	}
	allowed, err := repair.Admit(KindStorage, 100, "repair-ok")
	if err != nil || !allowed.Granted {
		t.Fatalf("repair recovery %#v %v", allowed, err)
	}
	t.Logf("SLOW intake_stopped=true DISK low_disk=true STALE diagnosis=stale_commit QUORUM distinct=true REPAIR delivered_repair=0 RECOVERY storage_granted=true")
}

func TestQuotaRace(t *testing.T) {
	svc, _ := testService(t)
	const n = 40
	var wg sync.WaitGroup
	var mu sync.Mutex
	granted, denied := 0, 0
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			d, err := svc.Admit(KindUploads, 1, "")
			if err != nil {
				t.Errorf("admit %v", err)
				return
			}
			mu.Lock()
			if d.Granted {
				granted++
			} else {
				denied++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if granted != int(DefaultUploadLimit) || denied != n-int(DefaultUploadLimit) {
		t.Fatalf("granted=%d denied=%d", granted, denied)
	}
	used, _, err := svc.Levels(KindUploads)
	if err != nil || used != DefaultUploadLimit {
		t.Fatalf("used=%d err=%v", used, err)
	}

	bytesSvc, _ := testService(t)
	var bg sync.WaitGroup
	got, refused := 0, 0
	bg.Add(8)
	for i := 0; i < 8; i++ {
		go func() {
			defer bg.Done()
			d, err := bytesSvc.Admit(KindOutputBytes, 600, "")
			if err != nil {
				t.Errorf("bytes %v", err)
				return
			}
			mu.Lock()
			if d.Granted {
				got++
			} else {
				refused++
			}
			mu.Unlock()
		}()
	}
	bg.Wait()
	usedBytes, _, err := bytesSvc.Levels(KindOutputBytes)
	if err != nil || got != 1 || refused != 7 || usedBytes != 600 {
		t.Fatalf("STORAGE granted=%d denied=%d used=%d err=%v", got, refused, usedBytes, err)
	}
	t.Logf("RACE granted=%d denied=%d limit=%d", granted, denied, DefaultUploadLimit)
	t.Logf("BYTES granted=%d denied=%d used=%d", got, refused, usedBytes)
}

func TestIdempotentReplayDoesNotConsume(t *testing.T) {
	svc, _ := testService(t)
	const n = 20
	var wg sync.WaitGroup
	ids := make([]string, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			d, err := svc.Admit(KindOutputBytes, 100, "same-key")
			if err != nil || !d.Granted {
				t.Errorf("replay admit %#v %v", d, err)
				return
			}
			ids[i] = d.GrantID
		}(i)
	}
	wg.Wait()
	used, _, err := svc.Levels(KindOutputBytes)
	if err != nil || used != 100 {
		t.Fatalf("same key used=%d", used)
	}
	for _, id := range ids {
		if id != ids[0] || id == "" {
			t.Fatalf("grant ids diverged %q %q", id, ids[0])
		}
	}
	if _, _, err := svc.Release(ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, replayed, err := svc.Release(ids[0]); err != nil || !replayed {
		t.Fatalf("second release replayed=%v err=%v", replayed, err)
	}
	used, _, _ = svc.Levels(KindOutputBytes)
	again, err := svc.Admit(KindOutputBytes, 100, "same-key")
	if err != nil || again.Granted || again.Reason != "already_released" {
		t.Fatalf("released replay %#v %v", again, err)
	}
	usedAfter, _, _ := svc.Levels(KindOutputBytes)
	if used != 0 || usedAfter != 0 {
		t.Fatalf("used after replay %d %d", used, usedAfter)
	}
	fresh, err := svc.Admit(KindOutputBytes, 100, "other-key")
	if err != nil || !fresh.Granted {
		t.Fatalf("new key %#v %v", fresh, err)
	}

	login, _ := testService(t)
	for i := 0; i < int(DefaultLoginLimit); i++ {
		d, err := login.Admit(KindLogin, 1, "")
		if err != nil || !d.Granted {
			t.Fatalf("login %#v %v", d, err)
		}
	}
	blocked, err := login.Admit(KindLogin, 1, "")
	if err != nil || blocked.Granted {
		t.Fatalf("login cap %#v %v", blocked, err)
	}
	loginClock := &manualClock{at: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	login, err = New(Options{Token: "synthetic-observe-25", Now: loginClock.Now})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < int(DefaultLoginLimit); i++ {
		if _, err := login.Admit(KindLogin, 1, ""); err != nil {
			t.Fatal(err)
		}
	}
	if d, err := login.Admit(KindLogin, 1, ""); err != nil || d.Granted {
		t.Fatalf("still open %#v", d)
	}
	hold, err := login.Admit(KindStorage, 100, "held")
	if err != nil || !hold.Granted {
		t.Fatal(err)
	}
	loginClock.Advance(time.Minute)
	if d, err := login.Admit(KindLogin, 1, ""); err != nil || !d.Granted {
		t.Fatalf("window %#v %v", d, err)
	}
	usedStorage, _, _ := login.Levels(KindStorage)
	if usedStorage != 100 {
		t.Fatalf("storage reset with the clock: %d", usedStorage)
	}
	t.Logf("REPLAY used=0 LOGIN after_window=true STORAGE clock_reset=false")
}

func TestProcessingQueue(t *testing.T) {
	svc, _ := testService(t)
	var running, queued, denied int
	for i := 0; i < 5; i++ {
		d, err := svc.Admit(KindProcessing, 1, "")
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case d.Granted:
			running++
		case d.Queued:
			queued++
		default:
			denied++
		}
	}
	if running != 2 || queued != 2 || denied != 1 {
		t.Fatalf("running=%d queued=%d denied=%d", running, queued, denied)
	}
	used, q, err := svc.Levels(KindProcessing)
	if err != nil || used != 2 || q != 2 {
		t.Fatalf("before release used=%d queued=%d", used, q)
	}
	// Release is by grant id. Admit without keys still returns ids on the decisions
	// we did not keep. Drain one by admitting was already full. Take a fresh
	// service and keep the ids.
	svc, _ = testService(t)
	var ids []string
	for i := 0; i < 4; i++ {
		d, err := svc.Admit(KindProcessing, 1, "")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, d.GrantID)
	}
	if _, _, err := svc.Release(ids[0]); err != nil {
		t.Fatal(err)
	}
	used, q, _ = svc.Levels(KindProcessing)
	if used != 2 || q != 1 {
		t.Fatalf("after release used=%d queued=%d", used, q)
	}
	t.Logf("QUEUE running=2 queued=1 denied=1")
}

func TestAlertsAreNotASpendCap(t *testing.T) {
	svc, _ := testService(t)
	if err := svc.RaiseAlert("playback_errors"); err != nil {
		t.Fatal(err)
	}
	d, err := svc.Admit(KindUploads, 1, "")
	if err != nil || !d.Granted || d.SpendCap || d.BudgetApproved || d.ThresholdLabel != LabelPlaceholder {
		t.Fatalf("alert changed admission %#v %v", d, err)
	}
	t.Logf("ALERT spend_cap=%t admitted=%t", d.SpendCap, d.Granted)
}

func TestRedactionAndLogRetention(t *testing.T) {
	clk := &manualClock{at: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}
	svc, err := New(Options{
		Token: "synthetic-observe-25", Now: clk.Now, LogEntries: 3, LogBytes: 4096,
	})
	if err != nil {
		t.Fatal(err)
	}
	canary := "SVN-SYNTHETIC-CANARY-" + "25"
	pem := "-----BEGIN " + "PRIVATE KEY-----\n" + canary + "\n-----END " + "PRIVATE KEY-----"
	message := "bearer " + canary + " person@example.com acc_example1 password=hunter " + pem + " synthetic-observe-25"
	id, err := svc.Observe(Observation{
		Component: CompAPI, Kind: ObsQueueRetries, Value: 1, Message: message,
	})
	if err != nil {
		t.Fatal(err)
	}
	logs := svc.Logs()
	if len(logs) != 1 {
		t.Fatalf("logs %d", len(logs))
	}
	text := logs[0].Text
	for _, secret := range []string{canary, "person@example.com", "acc_example1", "hunter", "synthetic-observe-25", "PRIVATE KEY"} {
		if strings.Contains(text, secret) {
			t.Fatalf("log kept %q in %q", secret, text)
		}
	}
	if logs[0].CorrelationID != id || !strings.Contains(text, "[redacted]") {
		t.Fatalf("redaction lost the id or the marker: %#v", logs[0])
	}
	for i := 0; i < 4; i++ {
		if _, err := svc.Observe(Observation{
			Component: CompAPI, Kind: ObsQueueRetries, Value: 1, Message: "line",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(svc.Logs()) != 3 {
		t.Fatalf("retention %d", len(svc.Logs()))
	}
	t.Logf("REDACT canary=false email=false")
}

func TestFixtureBytes(t *testing.T) {
	svc, _ := testService(t)
	if err := svc.ApplyKnownFixture(); err != nil {
		t.Fatal(err)
	}
	snap := svc.Snapshot()
	total := int64(0)
	for _, n := range snap.DeliveredBytes {
		total += n
	}
	if snap.DeliveredBytes[ClassUserPayload] != FixtureUserPayload || total != 9700 {
		t.Fatalf("delivered %#v total %d", snap.DeliveredBytes, total)
	}
	if snap.DeliveredBytes[ClassUserPayload] == total {
		t.Fatal("user payload absorbed the other classes")
	}
	if snap.StorageBytes[ClassUserPayload] != FixtureStorageUser {
		t.Fatalf("storage %#v", snap.StorageBytes)
	}
	sheet := svc.Worksheet()
	if sheet.NetworkBytes != total || sheet.NetworkBytes == total+deliveredTotal(snap.StorageBytes) {
		t.Fatalf("network bytes %d", sheet.NetworkBytes)
	}
	for _, name := range Unmeasured {
		if _, ok := snap.DeliveredBytes[name]; ok {
			t.Fatalf("unmeasured class was counted: %s", name)
		}
	}
	if snap.ChainSubmissions != 0 || snap.WatchEventsOnChain != 0 || snap.PublicLaunch {
		t.Fatalf("snapshot claims %#v", snap)
	}
	t.Logf("FIXTURE user_payload=%d delivered_total=%d storage_user=%d double_count=false", snap.DeliveredBytes[ClassUserPayload], total, snap.StorageBytes[ClassUserPayload])
	t.Logf("UNMEASURED %s", strings.Join(snap.Unmeasured, ","))
}

func TestWorksheetHasNoMonthlyBill(t *testing.T) {
	svc, _ := testService(t)
	if err := svc.ApplyKnownFixture(); err != nil {
		t.Fatal(err)
	}
	sheet := svc.Worksheet()
	if sheet.MonthlyBill != nil || sheet.PeriodProduct != nil || sheet.CurrencyApproved || sheet.ForecastBlockedReason != "rates_not_supplied" {
		t.Fatalf("empty rates %#v", sheet)
	}
	if sheet.CurrencyPreference != "CAD" {
		t.Fatalf("preference %s", sheet.CurrencyPreference)
	}
	transfer := int64(2)
	if err := svc.SetRates(Rates{Currency: "CAD", TransferMinorPerByte: &transfer}); err != nil {
		t.Fatal(err)
	}
	partial := svc.Worksheet()
	if partial.PeriodProduct != nil || partial.ForecastBlockedReason != "rates_incomplete" || partial.MonthlyBill != nil {
		t.Fatalf("partial %#v", partial)
	}
	storage := int64(3)
	if err := svc.SetRates(Rates{
		Currency: "CAD", TransferMinorPerByte: &transfer, StorageMinorPerByte: &storage,
	}); err != nil {
		t.Fatal(err)
	}
	full := svc.Worksheet()
	if full.MonthlyBill != nil || full.PeriodProduct == nil || *full.PeriodProduct != 46400 || full.PeriodProductLabel != "not_a_monthly_forecast" {
		t.Fatalf("product %#v", full)
	}
	if err := svc.SetFounderCash(50); err != nil {
		t.Fatal(err)
	}
	svc.SetVolunteerNote("volunteer time, not cash")
	after := svc.Worksheet()
	if after.NetworkBytes != full.NetworkBytes || after.FounderCashMinor == nil || *after.FounderCashMinor != 50 {
		t.Fatalf("founder changed network bytes %#v", after)
	}
	if *after.PeriodProduct != *full.PeriodProduct {
		t.Fatal("founder cash was added to the period product")
	}
	t.Logf("WORKSHEET monthly_bill=absent period_product=%d currency_approved=false", *after.PeriodProduct)
}

func TestFetcherRejectsPrivateTargets(t *testing.T) {
	refused := []string{
		"https://127.0.0.1/a",
		"https://10.1.2.3/a",
		"https://192.168.0.1/a",
		"https://172.16.0.1/a",
		"https://169.254.169.254/",
		"https://[::1]/",
		"https://[fd00::1]/",
		"https://0.0.0.0/",
		"https://0.0.0.1/",
		"https://[::ffff:0.0.0.1]/",
		"https://[::ffff:10.1.2.3]/",
		"https://100.64.0.1/",
		"https://192.0.2.1/",
		"https://192.88.99.1/",
		"https://198.18.0.1/",
		"https://198.51.100.1/",
		"https://203.0.113.10/a",
		"https://240.0.0.1/",
		"https://[2001:db8::1]/",
		"https://[fec0::1]/",
		"https://127.1/",
		"https://0x7f000001/",
		"https://localhost/a",
		"https://metadata.google.internal/",
		"http://1.2.3.4/",
		"https://user:pass@1.2.3.4/",
		"file:///tmp/x",
		"https://2130706433/",
	}
	for _, raw := range refused {
		if err := CheckFetch(raw, "", nil); err == nil {
			t.Fatalf("allowed %s", raw)
		}
	}
	if err := CheckFetch("https://1.2.3.4/a", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := CheckFetch("https://192.0.0.9/", "", nil); err != nil {
		t.Fatal(err)
	}
	if err := CheckFetch("https://example.com/a", "", nil); err == nil {
		t.Fatal("unresolved name was allowed")
	}
	lookup := func(host string) ([]net.IP, error) {
		if host == "public.test" {
			return []net.IP{net.ParseIP("1.2.3.4")}, nil
		}
		return []net.IP{net.ParseIP("10.0.0.1")}, nil
	}
	if err := CheckFetch("https://public.test/a", "", lookup); err != nil {
		t.Fatal(err)
	}
	if err := CheckFetch("https://private.test/a", "", lookup); err == nil {
		t.Fatal("private DNS target was allowed")
	}
	err := CheckFetch("https://1.2.3.4/a", "https://127.0.0.1/b", nil)
	fe, ok := err.(FetchError)
	if !ok || fe.Reason != "redirect_private_address" {
		t.Fatalf("redirect %#v", err)
	}
	t.Logf("FETCH private=refused public=allowed redirect_private=refused")
}

func TestPlaybackDropsIdentity(t *testing.T) {
	svc, _ := testService(t)
	canary := "SVN-SYNTHETIC-CANARY-" + "25"
	id, err := svc.Observe(Observation{
		Component: CompPlayback, Kind: ObsDelivered, Class: ClassUserPayload, Value: 10,
		Message: canary + " person@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Observe(Observation{
		Component: CompPlayback, CorrelationID: id, Kind: ObsPlaybackFailure, Value: 1,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(svc.Playback())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("account")) || bytes.Contains(raw, []byte(canary)) || bytes.Contains(raw, []byte("@")) {
		t.Fatalf("playback kept identity %s", raw)
	}
	for _, line := range svc.Logs() {
		if strings.Contains(line.Text, canary) || strings.Contains(line.Text, "@") {
			t.Fatalf("log %s", line.Text)
		}
	}
	t.Logf("PLAYBACK identity_dropped=true")
}

func TestMetricsRequireAuthorization(t *testing.T) {
	svc, _ := testService(t)
	canary := "SVN-SYNTHETIC-CANARY-" + "25"
	if _, err := svc.Observe(Observation{Component: CompAPI, Kind: ObsQueueRetries, Value: 1, Message: canary}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(svc.Handler())
	defer srv.Close()
	check := func(method, path, token string, want int) string {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(`{"kind":"uploads","units":1}`))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != want {
			t.Fatalf("%s %s %d %s", method, path, res.StatusCode, body)
		}
		if bytes.Contains(body, []byte(canary)) || bytes.Contains(body, []byte("synthetic-observe-25")) {
			t.Fatalf("response leaked a secret %s", body)
		}
		return string(body)
	}
	check(http.MethodGet, "/v1/metrics", "", http.StatusUnauthorized)
	check(http.MethodGet, "/v1/logs", "", http.StatusUnauthorized)
	check(http.MethodGet, "/v1/metrics?token=synthetic-observe-25", "synthetic-observe-25", http.StatusUnauthorized)
	body := check(http.MethodGet, "/healthz", "", http.StatusOK)
	if !strings.Contains(body, `"public_launch":false`) {
		t.Fatalf("health %s", body)
	}
	ready := check(http.MethodGet, "/readyz", "", http.StatusServiceUnavailable)
	if !strings.Contains(ready, `"ready":false`) {
		t.Fatalf("ready %s", ready)
	}
	check(http.MethodGet, "/v1/metrics", "synthetic-observe-25", http.StatusOK)
	check(http.MethodGet, "/v1/logs", "synthetic-observe-25", http.StatusOK)
	usage := check(http.MethodGet, "/v1/usage", "synthetic-observe-25", http.StatusOK)
	if !strings.Contains(usage, `"tradable_asset":false`) || !strings.Contains(usage, `"budget_owner":"UNASSIGNED"`) {
		t.Fatalf("usage %s", usage)
	}
	fetchReq, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/fetch-check", strings.NewReader(`{"url":"https://127.0.0.1/secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	fetchReq.Header.Set("Authorization", "Bearer synthetic-observe-25")
	res, err := srv.Client().Do(fetchReq)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusForbidden || !bytes.Contains(raw, []byte("private_address")) {
		t.Fatalf("fetch %d %s", res.StatusCode, raw)
	}
	t.Logf("ACCESS metrics=401 logs=401 health=200")
}

func TestDashboardRolesAreUnassigned(t *testing.T) {
	svc, _ := testService(t)
	for _, panel := range svc.Dashboard() {
		if panel.Assigned || !strings.HasPrefix(panel.RolePlaceholder, "UNASSIGNED ") || panel.Runbook == "" {
			t.Fatalf("panel %#v", panel)
		}
	}
	if len(svc.UsageReport().Decisions) != len(OpenDecisions()) {
		t.Fatal("decision list drifted")
	}
}
