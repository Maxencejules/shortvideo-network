package store

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/pgtest"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/project"
)

const testChain = "shortvideo-dev-indexer"

func TestApplyIsAtomicAndIdempotent(t *testing.T) {
	_, owner, runtime := newDB(t)
	ctx := context.Background()
	st := New(runtime, testChain)
	block := oneContent(1)
	visible := false
	st.afterEffects = func() {
		n, err := New(owner, testChain).ContentCount(ctx)
		if err != nil {
			t.Errorf("count: %v", err)
		}
		if n != 0 {
			t.Errorf("content visible before checkpoint: %d", n)
		}
		visible = true
	}
	if err := st.Apply(ctx, block, contentEffect(block)); err != nil {
		t.Fatal(err)
	}
	if !visible {
		t.Fatal("hook did not run")
	}
	n, err := st.ContentCount(ctx)
	if err != nil || n != 1 {
		t.Fatalf("count %d %v", n, err)
	}
	if err := st.Apply(ctx, block, contentEffect(block)); err != nil {
		t.Fatal(err)
	}
	if n, err = st.ContentCount(ctx); err != nil || n != 1 {
		t.Fatalf("repeat count %d %v", n, err)
	}
	gap := oneContent(3)
	if err := st.Apply(ctx, gap, contentEffect(gap)); !errors.Is(err, ErrGap) {
		t.Fatalf("gap: %v", err)
	}
	if n, err = st.ContentCount(ctx); err != nil || n != 1 {
		t.Fatalf("gap wrote rows %d %v", n, err)
	}
	// A failed second effect rolls the first one back.
	bad := oneContent(2)
	badEff := contentEffect(bad)
	badEff = append(badEff, project.Effect{Source: badEff[0].Source, Lease: &project.Lease{ID: "missing", RenewOnly: true, TermVersion: 2}})
	badEff[1].Source.EventIndex = 1
	if err := st.Apply(ctx, bad, badEff); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("partial: %v", err)
	}
	cp, err := st.Checkpoint(ctx)
	if err != nil || cp.Height != 1 {
		t.Fatalf("checkpoint %+v %v", cp, err)
	}
}

func TestRenewUnknownLeaseDoesNotInventOne(t *testing.T) {
	_, _, runtime := newDB(t)
	st := New(runtime, testChain)
	block := oneContent(1)
	block.Txs = nil
	eff := []project.Effect{{
		Source: project.Source{Height: 1, TxHash: block.Hash, MsgIndex: 0, EventIndex: 0, Type: "svn.events.v1.LeaseRenewed"},
		Lease:  &project.Lease{ID: "l1", TermVersion: 2, Deadline: 9, State: "RENEWED", Active: true, RenewOnly: true},
	}}
	if err := st.Apply(context.Background(), block, eff); !errors.Is(err, ErrInconsistent) {
		t.Fatalf("renew: %v", err)
	}
	term, found, err := st.LeaseTerm(context.Background(), "l1")
	if err != nil || found || term != 0 {
		t.Fatalf("invented lease term=%d found=%v err=%v", term, found, err)
	}
}

func TestWithdrawKeepsTheRow(t *testing.T) {
	_, _, runtime := newDB(t)
	ctx := context.Background()
	st := New(runtime, testChain)
	first := oneContent(1)
	if err := st.Apply(ctx, first, contentEffect(first)); err != nil {
		t.Fatal(err)
	}
	second := oneContent(2)
	eff := []project.Effect{{
		Source:  project.Source{Height: 2, TxHash: second.Hash, MsgIndex: 0, EventIndex: 0, Type: "svn.events.v1.ContentWithdrawn"},
		Content: &project.Content{ID: "c1", Version: 1, Withdrawn: true, HistoryErased: true, WithdrawOnly: true},
	}}
	if err := st.Apply(ctx, second, eff); err != nil {
		t.Fatal(err)
	}
	var erased bool
	var manifest string
	if err := runtime.QueryRowContext(ctx, `SELECT history_erased, manifest_sha256 FROM indexer.content WHERE content_id = 'c1'`).Scan(&erased, &manifest); err != nil {
		t.Fatal(err)
	}
	if !erased || manifest == "" {
		t.Fatalf("tombstone erased the row or the manifest: erased=%v manifest=%q", erased, manifest)
	}
}

func TestRebuildLeavesOtherSchemas(t *testing.T) {
	_, owner, runtime := newDB(t)
	ctx := context.Background()
	st := New(runtime, testChain)
	block := oneContent(1)
	if err := st.Apply(ctx, block, contentEffect(block)); err != nil {
		t.Fatal(err)
	}
	before, err := st.Canonical(ctx)
	if err != nil || !strings.Contains(before, "content c1") {
		t.Fatalf("canonical: %v\n%s", err, before)
	}
	if _, err := owner.ExecContext(ctx, `CREATE SCHEMA app_private; CREATE TABLE app_private.social_records (id int); INSERT INTO app_private.social_records VALUES (7)`); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ExecContext(ctx, `CREATE SCHEMA broadcaster; CREATE TABLE broadcaster.intents (id int); INSERT INTO broadcaster.intents VALUES (3)`); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ExecContext(ctx, `CREATE TABLE indexer.social_records (id int); INSERT INTO indexer.social_records VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	ownerStore := New(owner, testChain)
	if err := ownerStore.Rebuild(ctx, testChain); !errors.Is(err, ErrAuthoritative) {
		t.Fatalf("rebuild with a foreign table: %v", err)
	}
	var social, intents int
	if err := owner.QueryRowContext(ctx, `SELECT id FROM app_private.social_records`).Scan(&social); err != nil || social != 7 {
		t.Fatalf("social %d %v", social, err)
	}
	if err := owner.QueryRowContext(ctx, `SELECT id FROM broadcaster.intents`).Scan(&intents); err != nil || intents != 3 {
		t.Fatalf("intents %d %v", intents, err)
	}
	if _, err := owner.ExecContext(ctx, `DROP TABLE indexer.social_records`); err != nil {
		t.Fatal(err)
	}
	if err := ownerStore.Rebuild(ctx, testChain); err != nil {
		t.Fatal(err)
	}
	after, err := New(runtime, testChain).Canonical(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(after, "content c1") {
		t.Fatalf("rebuild kept projection rows:\n%s", after)
	}
	if err := owner.QueryRowContext(ctx, `SELECT id FROM app_private.social_records`).Scan(&social); err != nil || social != 7 {
		t.Fatalf("social after rebuild %d %v", social, err)
	}
	approved, err := New(runtime, testChain).HasPublicationApproval(ctx)
	if err != nil || approved {
		t.Fatalf("publication approval column: %v %v", approved, err)
	}
	if _, err := runtime.ExecContext(ctx, `DELETE FROM indexer.chain_binding`); err == nil {
		t.Fatal("runtime role deleted a row")
	}
}

func TestAcceptClosesTheSingleOpenOffer(t *testing.T) {
	_, runtime, _ := newDB(t)
	ctx := context.Background()
	st := New(runtime, testChain)
	block := oneContent(1)
	offer := func(block project.Block, id string, index int32) project.Effect {
		return project.Effect{
			Source: project.Source{Height: block.Height, TxHash: block.Hash, MsgIndex: 0, EventIndex: index, Type: "svn.events.v1.LeaseOffered"},
			Offer:  &project.Offer{ID: id, ProviderID: "p1", ContentID: "c1", State: "OPEN"},
		}
	}
	lease := project.Effect{
		Source: project.Source{Height: 1, TxHash: block.Hash, MsgIndex: 0, EventIndex: 2, Type: "svn.events.v1.LeaseAccepted"},
		Lease: &project.Lease{
			ID: "l1", ProviderID: "p1", ContentID: "c1", Version: 1, PackageBytes: "4",
			State: "ACTIVE", TermVersion: 1, Deadline: 100, Active: true,
		},
	}
	if err := st.Apply(ctx, block, append(contentEffect(block), offer(block, "o1", 1), lease)); err != nil {
		t.Fatal(err)
	}
	var state string
	if err := runtime.QueryRowContext(ctx, `SELECT state FROM indexer.offers WHERE offer_id = 'o1'`).Scan(&state); err != nil || state != "CLOSED" {
		t.Fatalf("single offer %q %v", state, err)
	}
	block2 := oneContent(2)
	two := append(contentEffect(block2), offer(block2, "oa", 1), offer(block2, "ob", 2), lease)
	two[3].Lease.ID = "l2"
	two[3].Source.Height = 2
	two[3].Source.TxHash = block2.Hash
	two[3].Source.EventIndex = 3
	if err := st.Apply(ctx, block2, two); err != nil {
		t.Fatal(err)
	}
	rows, err := runtime.QueryContext(ctx, `SELECT offer_id, state FROM indexer.offers WHERE offer_id IN ('oa', 'ob') ORDER BY offer_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var id, st string
		if err := rows.Scan(&id, &st); err != nil {
			t.Fatal(err)
		}
		got[id] = st
	}
	if got["oa"] != "OPEN" || got["ob"] != "OPEN" {
		t.Fatalf("ambiguous offers %+v", got)
	}
}

func TestRecordRunDoesNotChangeALease(t *testing.T) {
	_, _, runtime := newDB(t)
	ctx := context.Background()
	st := New(runtime, testChain)
	block := oneContent(1)
	lease := project.Effect{
		Source: project.Source{Height: 1, TxHash: block.Hash, MsgIndex: 0, EventIndex: 1, Type: "svn.events.v1.LeaseAccepted"},
		Lease: &project.Lease{
			ID: "l1", ProviderID: "p1", ContentID: "c1", Version: 1, PackageBytes: "4",
			State: "ACTIVE", TermVersion: 1, Deadline: 100, Active: true,
		},
	}
	effects := append(contentEffect(block), lease)
	if err := st.Apply(ctx, block, effects); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordRun(ctx, 1, "divergent", []Finding{{Kind: "lease_mismatch", ObjectID: "l1"}}); err != nil {
		t.Fatal(err)
	}
	term, found, err := st.LeaseTerm(ctx, "l1")
	if err != nil || !found || term != 1 {
		t.Fatalf("term %d found %v err %v", term, found, err)
	}
	run, err := st.LatestRun(ctx)
	if err != nil || run.Status != "divergent" || len(run.Findings) != 1 {
		t.Fatalf("run %+v %v", run, err)
	}
}

func newDB(t *testing.T) (*pgtest.Server, *sql.DB, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	cluster, err := pgtest.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cluster.Close() })
	ownerPass, err := cluster.Cluster.CreateLoginRole(ctx, OwnerRole)
	if err != nil {
		t.Fatal(err)
	}
	runtimePass, err := cluster.Cluster.CreateLoginRole(ctx, RuntimeRole)
	if err != nil {
		t.Fatal(err)
	}
	if err := cluster.NewDatabase(ctx, Database, OwnerRole, RuntimeRole); err != nil {
		t.Fatal(err)
	}
	owner := openDB(t, cluster, OwnerRole, ownerPass)
	runtime := openDB(t, cluster, RuntimeRole, runtimePass)
	if err := Migrate(ctx, owner); err != nil {
		t.Fatal(err)
	}
	if err := Bind(ctx, owner, testChain); err != nil {
		t.Fatal(err)
	}
	return cluster, owner, runtime
}

func openDB(t *testing.T, cluster *pgtest.Server, user, pass string) *sql.DB {
	t.Helper()
	db, err := Open(Conn{
		SocketDir: cluster.Cluster.SocketDir(), Port: pgtestPort(), Database: Database,
		User: user, Password: pass, ApplicationName: "svn-indexer-test", ConnectTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func pgtestPort() uint16 { return 5432 }

func oneContent(height int64) project.Block {
	hash := bytes.Repeat([]byte{byte(height)}, 32)
	app := bytes.Repeat([]byte{byte(100 + height)}, 32)
	return project.Block{ChainID: testChain, Height: height, Hash: hash, AppHash: app, TimeUnix: 1_700_000_000 + height}
}

func contentEffect(block project.Block) []project.Effect {
	return []project.Effect{{
		Source: project.Source{Height: block.Height, TxHash: block.Hash, MsgIndex: 0, EventIndex: 0, Type: "svn.events.v1.ContentRegistered"},
		Content: &project.Content{
			ID: "c1", Version: 1, Manifest: strings.Repeat("ab", 32), PackageBytes: "4000000",
		},
	}}
}
