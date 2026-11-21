package restore

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("SVN_RESTORE_CHILD") == "hold" {
		err := Hold(os.Getenv("SVN_HOME"), os.Getenv("SVN_LOCKDIR"), os.Stdin)
		if errors.Is(err, ErrDuplicateSigner) {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(2)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestDrillRestoresDatabaseMediaAndIndex(t *testing.T) {
	if _, err := os.Stat(filepath.Join(postgresBin, "initdb")); err != nil {
		t.Skip("PostgreSQL 18 is not installed")
	}
	work := t.TempDir()
	result, err := Drill(work)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "restored" || !result.DigestsMatch || !result.AuthorizationMatch || !result.PlaybackAfter || !result.RemovalAfter || !result.Postgres || !result.OriginUnavailable || !result.RowsMatch || !result.SignedMatch || result.Guarantees || result.HistoryErased {
		t.Fatalf("%+v", result)
	}
	if result.Corrupt != "refused" || result.WrongVersion != "refused" || result.Incomplete != "refused" || result.Partial != "partial" || result.MissingMedia != "refused" || !result.ReplicaRejected || result.SecretsInCI {
		t.Fatalf("fixtures %+v", result)
	}
	if _, err := os.Stat(filepath.Join(work, "corrupt-dest")); !os.IsNotExist(err) {
		t.Fatal("corrupt restore wrote a destination")
	}
	if _, err := os.Stat(filepath.Join(work, "wrong-dest")); !os.IsNotExist(err) {
		t.Fatal("wrong-version restore wrote a destination")
	}
	fmt.Printf("DRILL status=%s digests_match=%t authorization_match=%t playback_after=%t removal_after=%t postgres=%t origin_unavailable=%t rows_match=%t signed_match=%t\n",
		result.Status, result.DigestsMatch, result.AuthorizationMatch, result.PlaybackAfter, result.RemovalAfter, result.Postgres, result.OriginUnavailable, result.RowsMatch, result.SignedMatch)
	fmt.Printf("DRILL observed_rto_seconds=%g observed_rpo_seconds=%g rto_rpo_are_service_guarantees=%t\n",
		result.ObservedRTOSeconds, result.ObservedRPOSeconds, result.Guarantees)
	fmt.Printf("DRILL corrupt=%s wrong_version=%s incomplete=%s partial=%s missing_media=%s replica_rejected=%t secrets_in_ci=%t\n",
		result.Corrupt, result.WrongVersion, result.Incomplete, result.Partial, result.MissingMedia, result.ReplicaRejected, result.SecretsInCI)
}

func TestSignerDoesNotRunTwiceOrRollBack(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	lockDir := filepath.Join(root, "locks")
	oldHome := filepath.Join(root, "old")
	state := SignState{Height: 4, Round: 1, Step: 6}
	if err := WriteHome(oldHome, pub, state); err != nil {
		t.Fatal(err)
	}
	first := startHold(t, oldHome, lockDir)
	secondOut := runHold(t, filepath.Join(root, "copy"), lockDir, pub, state)
	if secondOut.code != 2 || !bytes.Contains(secondOut.stderr, []byte("duplicate live signer")) {
		t.Fatalf("second signer code %d %s", secondOut.code, secondOut.stderr)
	}
	fmt.Printf("SIGNER duplicate=refused\n")
	if err := RefuseCopiedLive(lockDir, pub); !errors.Is(err, ErrCopiedLive) {
		t.Fatal(err)
	}
	fmt.Printf("SIGNER copied_live_key=refused\n")
	if err := first.release(); err != nil {
		t.Fatal(err)
	}
	if err := Replace(oldHome, oldHome, lockDir, SignState{Height: 3, Round: 0, Step: 0}, 4, false); !errors.Is(err, ErrRolledBack) {
		t.Fatal(err)
	}
	fmt.Printf("SIGNER rollback=refused\n")
	if _, err := os.Stat(filepath.Join(oldHome, "signing_allowed")); !os.IsNotExist(err) {
		t.Fatal("stale snapshot was allowed to sign before the check")
	}
	if err := Replace(oldHome, oldHome, lockDir, state, 9, true); !errors.Is(err, ErrStaleSnapshot) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(oldHome, "signing_allowed")); !os.IsNotExist(err) {
		t.Fatal("stale snapshot wrote a signing permission")
	}
	fmt.Printf("SIGNER stale=refused\n")
	killedHome := filepath.Join(root, "killed")
	if err := WriteHome(killedHome, pub, state); err != nil {
		t.Fatal(err)
	}
	killed := startHold(t, killedHome, lockDir)
	_ = killed.cmd.Process.Kill()
	if err := killed.wait(); err == nil {
		t.Fatal("killed signer exited cleanly")
	}
	if err := Replace(killedHome, killedHome, lockDir, SignState{Height: 5, Round: 0, Step: 0}, 4, false); !errors.Is(err, ErrNotVerified) {
		t.Fatal(err)
	}
	fmt.Printf("SIGNER unverified=refused\n")
	if err := Replace(oldHome, oldHome, lockDir, SignState{Height: 5, Round: 0, Step: 0}, 4, true); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(oldHome, "signing_allowed"))
	if err != nil || string(raw) != "drill\n" {
		t.Fatalf("replace did not allow the newer state: %v %q", err, raw)
	}
	fmt.Printf("SIGNER replace=accepted\n")
	peer := filepath.Join(root, "peer")
	validator := filepath.Join(root, "validator")
	governance := filepath.Join(root, "governance")
	for _, path := range []string{peer, validator, governance} {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := DistinctKeyFiles(peer, validator, governance); err != nil {
		t.Fatal(err)
	}
	copied := filepath.Join(root, "validator-copy")
	body, err := os.ReadFile(validator)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copied, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := DistinctKeyFiles(copied, validator, governance); !errors.Is(err, ErrSameKeyFile) {
		t.Fatal(err)
	}
	fmt.Printf("SIGNER distinct_keys=true\n")
}

func TestServiceRotationAndAccountRecovery(t *testing.T) {
	if _, _, _, err := RotateService(Approval{}); !errors.Is(err, ErrApproval) {
		t.Fatal(err)
	}
	next, retired, priv, err := RotateService(Approval{Drill: true})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(next, retired) || len(priv) != ed25519.PrivateKeySize {
		t.Fatal("rotation did not retire the old public key")
	}
	if _, _, _, err := RotateService(Approval{OperatorToken: "test-approval"}); err != nil {
		t.Fatal(err)
	}
	fmt.Printf("ROTATE old_rejected=true new_accepted=true operator_named=false approval_required=true\n")
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	doc := []byte(`{"account_id":"acct-drill"}`)
	plain, recovery, err := RehearseAccountExport(doc, key)
	if err != nil || !bytes.Equal(plain, doc) || recovery.Completion != "BLOCKED" || recovery.Request != "DR-26-05" {
		t.Fatalf("%v %+v", err, recovery)
	}
	if err := CompleteAccountRecovery(); !errors.Is(err, ErrRecoveryBlocked) {
		t.Fatal(err)
	}
	fmt.Printf("ACCOUNT export_restored=true completion=%s request=%s\n", recovery.Completion, recovery.Request)
}

func TestEndOfService(t *testing.T) {
	world := sampleWorld(t)
	root := t.TempDir()
	backup := filepath.Join(root, "backup")
	key := filepath.Join(root, "backup.key")
	if _, err := Backup(backup, world, Options{KeyOut: key, Newest: time.Now()}); err != nil {
		t.Fatal(err)
	}
	origin := filepath.Join(root, "origin")
	dest := filepath.Join(root, "dest")
	if _, err := Restore(backup, key, dest, origin, false); err != nil {
		t.Fatal(err)
	}
	if err := StopAdmissions(dest); err != nil {
		t.Fatal(err)
	}
	if _, err := Playback(filepath.Join(dest, "index.json"), filepath.Join(dest, "media"), "c1", AdmissionsStopped(dest)); !errors.Is(err, ErrAdmissionsStopped) {
		t.Fatal(err)
	}
	handoff, err := LeaseHandoff(dest, "provider-a", "provider-b", "lease-drill")
	if err != nil || handoff.ChainSubmitted {
		t.Fatal(err)
	}
	if _, err := ControlledDelete(backup, "no", Approval{Drill: true}); err == nil {
		t.Fatal("delete without confirmation")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatal(err)
	}
	if _, err := ControlledDelete(backup, "delete", Approval{}); !errors.Is(err, ErrApproval) {
		t.Fatal(err)
	}
	deleted, err := ControlledDelete(backup, "delete", Approval{Drill: true})
	if err != nil || deleted.HistoryErased || deleted.RetentionPeriod != RetentionPeriod {
		t.Fatalf("%v %+v", err, deleted)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatal("backup directory still exists")
	}
	fmt.Printf("END admissions_stopped=true chain_submitted=%t history_erased=%t retention_period=%s\n", handoff.ChainSubmitted, deleted.HistoryErased, deleted.RetentionPeriod)
}

func TestCanonicalRebuildAndCompatibility(t *testing.T) {
	if err := Compatible(FormatVersion, SchemaVersion); err != nil {
		t.Fatal(err)
	}
	if err := Compatible(2, SchemaVersion); !errors.Is(err, ErrVersion) {
		t.Fatal(err)
	}
	dir := t.TempDir()
	world := sampleWorld(t)
	world.Index.Source = SourceCanonical
	backup := filepath.Join(dir, "backup")
	key := filepath.Join(dir, "key")
	if _, err := Backup(backup, world, Options{KeyOut: key}); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "dest")
	if _, err := Restore(backup, key, dest, filepath.Join(dir, "missing-origin"), false); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dest, "projection.json")
	if _, err := RebuildProjection(filepath.Join(dest, "index.json"), out); err != nil {
		t.Fatal(err)
	}
	world.Index.Source = SourceReplica
	raw := []byte(`{"schema":"SVN-INDEX-CHECKPOINT-1","chain_id":"shortvideo-dev-drill","height":4,"app_hash":"aa","source":"live_replica","rows":[]}`)
	replica := filepath.Join(dir, "replica.json")
	if err := os.WriteFile(replica, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := RebuildProjection(replica, filepath.Join(dir, "nope.json")); !errors.Is(err, ErrReplicaNotBackup) {
		t.Fatal(err)
	}
	if len(ManualApprovals()) != 6 || len(UnreconstructableFromReplicas()) != 7 {
		t.Fatal("approval or reconstruction list changed")
	}
	fmt.Printf("COMPAT format=%s schema=%d migration=refused\n", Format, SchemaVersion)
	for _, item := range UnreconstructableFromReplicas() {
		fmt.Printf("UNRECONSTRUCTABLE %s\n", item)
	}
	for _, item := range ManualApprovals() {
		fmt.Printf("APPROVAL %s\n", item)
	}
}

func TestOriginAndAccess(t *testing.T) {
	world := sampleWorld(t)
	root := t.TempDir()
	backup := filepath.Join(root, "backup")
	key := filepath.Join(root, "key")
	if _, err := Backup(backup, world, Options{KeyOut: key}); err != nil {
		t.Fatal(err)
	}
	origin := filepath.Join(root, "origin")
	if err := os.Mkdir(origin, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(backup, key, filepath.Join(root, "dest"), origin, false); !errors.Is(err, ErrOriginAvailable) {
		t.Fatal(err)
	}
	if err := os.Chmod(backup, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(origin); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(backup, key, filepath.Join(root, "dest2"), origin, false); !errors.Is(err, ErrAccess) {
		t.Fatal(err)
	}
	info, err := os.Stat(key)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode %v %v", info.Mode(), err)
	}
	entries, err := os.ReadDir(backup)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(backup, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		keyBody, err := os.ReadFile(key)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, keyBody) {
			t.Fatal("backup contains the encryption key")
		}
	}
	fmt.Printf("ACCESS origin_present=refused mode=refused key_in_backup=false\n")
}

type holdResult struct {
	code   int
	stderr []byte
}

type proc struct {
	cmd      *exec.Cmd
	stop     chan struct{}
	done     chan error
	finished chan struct{}
	once     sync.Once
	err      bytes.Buffer
}

func (p *proc) release() error {
	p.once.Do(func() { close(p.stop) })
	return p.wait()
}

func (p *proc) wait() error {
	select {
	case err := <-p.done:
		return err
	case <-time.After(5 * time.Second):
		_ = p.cmd.Process.Kill()
		return errors.New("holder timed out")
	}
}

func startHold(t *testing.T, home, lockDir string) *proc {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "SVN_RESTORE_CHILD=hold", "SVN_HOME="+home, "SVN_LOCKDIR="+lockDir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	holder := &proc{}
	holder.cmd = cmd
	holder.stop = make(chan struct{})
	holder.done = make(chan error, 1)
	holder.finished = make(chan struct{})
	cmd.Stderr = &holder.err
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		holder.done <- cmd.Wait()
		close(holder.finished)
	}()
	go func() {
		<-holder.stop
		_ = stdin.Close()
	}()
	t.Cleanup(func() {
		holder.once.Do(func() { close(holder.stop) })
		select {
		case <-holder.finished:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
			<-holder.finished
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Join(home, holding)); err == nil {
			return holder
		}
		select {
		case err := <-holder.done:
			t.Fatalf("holder exited early: %v %s", err, holder.err.Bytes())
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	_ = cmd.Process.Kill()
	t.Fatal("holder did not lock")
	return nil
}

func runHold(t *testing.T, home, lockDir string, pub []byte, state SignState) holdResult {
	t.Helper()
	if err := WriteHome(home, pub, state); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "SVN_RESTORE_CHILD=hold", "SVN_HOME="+home, "SVN_LOCKDIR="+lockDir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return holdResult{code: code, stderr: stderr.Bytes()}
}

func sampleWorld(t *testing.T) World {
	t.Helper()
	body := []byte("synthetic-media")
	sum := Digest(body)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	document := `{"authorization":"published","content_id":"c1"}`
	return World{
		ChainID:       "shortvideo-dev-drill",
		SchemaVersion: SchemaVersion,
		Config:        map[string]string{"chain_id": "shortvideo-dev-drill"},
		DatabaseSQL:   []byte("SELECT 1;\n"),
		SignedRecords: []SignedRecord{{
			Kind: "publication", ID: "c1", Document: document,
			Public: hex.EncodeToString(pub), Signature: hex.EncodeToString(ed25519.Sign(priv, []byte(document))),
		}},
		Media: []MediaFile{{Digest: sum, Body: body}},
		Index: IndexCheckpoint{
			Schema: IndexSchema, ChainID: "shortvideo-dev-drill", Height: 4,
			AppHash: "abcd", Source: SourceCheckpoint,
			Rows: []IndexRow{{ContentID: "c1", MediaDigest: sum, Authorization: "published"}},
		},
		Signer: SignerMeta{ConsensusAddress: Address(pub), PublicKey: hex.EncodeToString(pub), Height: 4, ChainHead: 4},
	}
}
