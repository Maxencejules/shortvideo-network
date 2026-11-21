package restore

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
)

const postgresBin = "/usr/lib/postgresql/18/bin"

var hexToken = regexp.MustCompile(`^[0-9a-f]+$`)

// DrillResult is one disposable rehearsal. The observed times are this run
// only. Guarantees stays false. AccountRecovery stays BLOCKED.
type DrillResult struct {
	Status             string
	DigestsMatch       bool
	AuthorizationMatch bool
	PlaybackAfter      bool
	RemovalAfter       bool
	Postgres           bool
	OriginUnavailable  bool
	ObservedRTOSeconds float64
	ObservedRPOSeconds float64
	Guarantees         bool
	DuplicateSigner    string
	StaleSnapshot      string
	Rollback           string
	Corrupt            string
	WrongVersion       string
	Incomplete         string
	Partial            string
	MissingMedia       string
	ServiceRotation    string
	AccountRecovery    string
	SecretsInCI        bool
	ReplicaRejected    bool
	RowsMatch          bool
	SignedMatch        bool
	HistoryErased      bool
}

// Drill builds a publication, backs it up, removes the origin, and restores
// it onto a clean directory and a clean database. The original files are
// gone before the restore reads anything.
func Drill(work string) (DrillResult, error) {
	var result DrillResult
	result.Guarantees = false
	result.HistoryErased = false
	result.AccountRecovery = "BLOCKED"
	if err := os.MkdirAll(work, 0o700); err != nil {
		return result, err
	}
	origin := filepath.Join(work, "origin")
	if err := os.MkdirAll(origin, 0o700); err != nil {
		return result, err
	}
	published, removed, err := twoObjects()
	if err != nil {
		return result, err
	}
	sqlText, err := publicationSQL(published, removed)
	if err != nil {
		return result, err
	}
	if _, err := os.Stat(filepath.Join(postgresBin, "initdb")); err != nil {
		return result, ErrPostgresAbsent
	}
	before, dump, err := loadAndDump(sqlText)
	if err != nil {
		return result, err
	}
	newest := time.Now()
	result.Postgres = true
	world, err := worldFrom(published, removed, dump)
	if err != nil {
		return result, err
	}
	backupDir := filepath.Join(work, "backup")
	keyPath := filepath.Join(work, "backup.key")
	if _, err := Backup(backupDir, world, Options{KeyOut: keyPath, Newest: newest}); err != nil {
		return result, err
	}
	if err := os.RemoveAll(origin); err != nil {
		return result, err
	}
	dest := filepath.Join(work, "restored")
	started := time.Now()
	restored, err := Restore(backupDir, keyPath, dest, origin, false)
	if err != nil {
		return result, err
	}
	result.Status = restored.Status
	result.OriginUnavailable = restored.OriginUnavailable
	result.ObservedRPOSeconds = restored.ObservedRPOSeconds
	afterAuth, err := Authorizations(filepath.Join(dest, "index.json"))
	if err != nil {
		return result, err
	}
	afterDigest, err := Digests(filepath.Join(dest, "index.json"))
	if err != nil {
		return result, err
	}
	beforeAuth := map[string]string{published.id: "published", removed.id: "removed"}
	beforeDigest := map[string]string{published.id: published.digest, removed.id: removed.digest}
	result.AuthorizationMatch = sameMap(beforeAuth, afterAuth)
	result.DigestsMatch = sameMap(beforeDigest, afterDigest)
	body, err := Playback(filepath.Join(dest, "index.json"), filepath.Join(dest, "media"), published.id, false)
	if err != nil {
		return result, err
	}
	result.PlaybackAfter = digest(body) == published.digest
	if _, err := Playback(filepath.Join(dest, "index.json"), filepath.Join(dest, "media"), removed.id, false); !errors.Is(err, ErrRemoved) {
		return result, fmt.Errorf("removed object played: %w", err)
	}
	result.RemovalAfter = true
	projection := filepath.Join(dest, "projection.json")
	if _, err := RebuildProjection(filepath.Join(dest, "index.json"), projection); err != nil {
		return result, err
	}
	rebuilt, err := Authorizations(projection)
	if err != nil {
		return result, err
	}
	if !sameMap(beforeAuth, rebuilt) {
		return result, errors.New("restore: rebuilt projection changed authorization")
	}
	rows, err := restoreAndQuery(filepath.Join(dest, "database.sql"))
	if err != nil {
		return result, err
	}
	result.ObservedRTOSeconds = time.Since(started).Seconds()
	result.RowsMatch = rows == before
	records, err := readRecords(filepath.Join(dest, "signed-records.json"))
	if err != nil {
		return result, err
	}
	result.SignedMatch = len(records) == 1 && VerifySigned(records[0]) == nil
	if err := classifyFixtures(work, backupDir, keyPath, origin, &result); err != nil {
		return result, err
	}
	return result, nil
}

type object struct {
	id     string
	digest string
	body   []byte
}

func twoObjects() (object, object, error) {
	first, err := oneObject("c1")
	if err != nil {
		return object{}, object{}, err
	}
	second, err := oneObject("c2")
	if err != nil {
		return object{}, object{}, err
	}
	return first, second, nil
}

func oneObject(id string) (object, error) {
	body := make([]byte, 32)
	if _, err := rand.Read(body); err != nil {
		return object{}, err
	}
	return object{id: id, digest: digest(body), body: body}, nil
}

func publicationSQL(published, removed object) (string, error) {
	for _, value := range []string{published.id, published.digest, removed.id, removed.digest} {
		if !hexToken.MatchString(value) && value != "c1" && value != "c2" {
			return "", errors.New("restore: publication value is not hex")
		}
	}
	return fmt.Sprintf(`CREATE TABLE publication (
  content_id text PRIMARY KEY,
  media_digest text NOT NULL,
  auth_state text NOT NULL,
  recorded_at timestamptz NOT NULL
);
INSERT INTO publication (content_id, media_digest, auth_state, recorded_at) VALUES
  ('%s', '%s', 'published', '2026-01-01T00:00:00Z'),
  ('%s', '%s', 'removed', '2026-01-01T00:00:00Z');
`, published.id, published.digest, removed.id, removed.digest), nil
}

func worldFrom(published, removed object, dump []byte) (World, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return World{}, err
	}
	document := fmt.Sprintf(`{"authorization":"published","content_id":"%s","digest":"%s"}`, published.id, published.digest)
	signature := ed25519.Sign(priv, []byte(document))
	world := World{
		ChainID:       "shortvideo-dev-drill",
		SchemaVersion: SchemaVersion,
		Config:        map[string]string{"chain_id": "shortvideo-dev-drill", "schema": "1"},
		DatabaseSQL:   dump,
		SignedRecords: []SignedRecord{{
			Kind:      "publication",
			ID:        published.id,
			Document:  document,
			Public:    hex.EncodeToString(pub),
			Signature: hex.EncodeToString(signature),
		}},
		Media: []MediaFile{{Digest: published.digest, Body: published.body}, {Digest: removed.digest, Body: removed.body}},
		Index: IndexCheckpoint{
			Schema:  IndexSchema,
			ChainID: "shortvideo-dev-drill",
			Height:  4,
			AppHash: digest([]byte("drill-app-hash")),
			Source:  SourceCheckpoint,
			Rows: []IndexRow{
				{ContentID: published.id, MediaDigest: published.digest, Authorization: "published"},
				{ContentID: removed.id, MediaDigest: removed.digest, Authorization: "removed"},
			},
		},
		Signer: SignerMeta{
			ConsensusAddress:   Address(pub),
			PublicKey:          hex.EncodeToString(pub),
			Height:             4,
			Round:              0,
			Step:               0,
			ChainHead:          4,
			PrivateKeyIncluded: false,
		},
	}
	return world, nil
}

func classifyFixtures(work, backupDir, keyPath, origin string, result *DrillResult) error {
	corruptDir := filepath.Join(work, "corrupt")
	if err := copyBackup(backupDir, corruptDir); err != nil {
		return err
	}
	blob := filepath.Join(corruptDir, fileDatabase)
	body, err := os.ReadFile(blob)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return ErrCorrupted
	}
	body[len(body)-1] ^= 0xff
	if err := writePrivate(blob, body); err != nil {
		return err
	}
	if err := retargetDigest(corruptDir, partDatabase, body); err != nil {
		return err
	}
	if _, err := Restore(corruptDir, keyPath, filepath.Join(work, "corrupt-dest"), origin, false); !errors.Is(err, ErrCorrupted) {
		return fmt.Errorf("corrupt restore: %w", err)
	}
	result.Corrupt = "refused"
	wrongDir := filepath.Join(work, "wrong-version")
	if err := copyBackup(backupDir, wrongDir); err != nil {
		return err
	}
	if err := setFormatVersion(wrongDir, 2); err != nil {
		return err
	}
	if _, err := Restore(wrongDir, keyPath, filepath.Join(work, "wrong-dest"), origin, false); !errors.Is(err, ErrVersion) {
		return fmt.Errorf("wrong version: %w", err)
	}
	result.WrongVersion = "refused"
	incomplete := filepath.Join(work, "incomplete")
	if err := copyBackup(backupDir, incomplete); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(incomplete, fileMedia)); err != nil {
		return err
	}
	if _, err := Restore(incomplete, keyPath, filepath.Join(work, "incomplete-dest"), origin, false); !errors.Is(err, ErrIncomplete) {
		return fmt.Errorf("incomplete: %w", err)
	}
	result.Incomplete = "refused"
	partialDest := filepath.Join(work, "partial-dest")
	partial, err := Restore(incomplete, keyPath, partialDest, origin, true)
	if err != nil {
		return err
	}
	if partial.Status != "partial" {
		return fmt.Errorf("partial status %s", partial.Status)
	}
	if _, err := os.Stat(filepath.Join(partialDest, "database.sql")); err != nil {
		return err
	}
	if _, err := Playback(filepath.Join(partialDest, "index.json"), filepath.Join(partialDest, "media"), "c1", false); !errors.Is(err, ErrMissingMedia) {
		return fmt.Errorf("partial playback: %w", err)
	}
	result.Partial = "partial"
	result.MissingMedia = "refused"
	replica := World{ChainID: "shortvideo-dev-drill", SchemaVersion: SchemaVersion, Index: IndexCheckpoint{Source: SourceReplica, Schema: IndexSchema, ChainID: "shortvideo-dev-drill"}}
	if _, err := Backup(filepath.Join(work, "replica"), replica, Options{}); !errors.Is(err, ErrReplicaNotBackup) {
		return fmt.Errorf("replica: %w", err)
	}
	result.ReplicaRejected = true
	ci := filepath.Join(work, "ci-artifact")
	if err := ExportCI(backupDir, ci); err != nil {
		return err
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	artifact, err := os.ReadFile(filepath.Join(ci, fileManifest))
	if err != nil {
		return err
	}
	result.SecretsInCI = bytes.Contains(artifact, key)
	if err := os.WriteFile(filepath.Join(backupDir, "live.key"), key, 0o600); err != nil {
		return err
	}
	if err := ExportCI(backupDir, filepath.Join(work, "ci-secret")); !errors.Is(err, ErrSecret) {
		return fmt.Errorf("ci secret: %w", err)
	}
	_ = os.Remove(filepath.Join(backupDir, "live.key"))
	return nil
}

func retargetDigest(dir, part string, body []byte) error {
	manifest, err := readManifest(filepath.Join(dir, fileManifest))
	if err != nil {
		return err
	}
	item := manifest.Parts[part]
	item.SHA256 = digest(body)
	manifest.Parts[part] = item
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(filepath.Join(dir, fileManifest), append(raw, '\n'))
}

func setFormatVersion(dir string, version int) error {
	manifest, err := readManifest(filepath.Join(dir, fileManifest))
	if err != nil {
		return err
	}
	manifest.FormatVersion = version
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(filepath.Join(dir, fileManifest), append(raw, '\n'))
}

func copyBackup(src, dst string) error {
	if err := os.Mkdir(dst, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(src, entry.Name()))
		if err != nil {
			return err
		}
		if err := writePrivate(filepath.Join(dst, entry.Name()), body); err != nil {
			return err
		}
	}
	return nil
}

func readRecords(path string) ([]SignedRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var records []SignedRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func sameMap(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

const rowQuery = `SELECT content_id || ' ' || media_digest || ' ' || auth_state FROM publication ORDER BY content_id`

func loadAndDump(sqlText string) (string, []byte, error) {
	var rows string
	var dump []byte
	err := withCluster(func(socket string) error {
		if err := psql(socket, "template1", "CREATE DATABASE drill"); err != nil {
			return err
		}
		file, err := writeTemp(sqlText)
		if err != nil {
			return err
		}
		defer os.Remove(file)
		if err := psqlFile(socket, "drill", file); err != nil {
			return err
		}
		out, err := psqlQuery(socket, "drill", rowQuery)
		if err != nil {
			return err
		}
		rows = out
		dumped, err := pgDump(socket, "drill")
		if err != nil {
			return err
		}
		dump = dumped
		return nil
	})
	return rows, dump, err
}

func restoreAndQuery(dumpPath string) (string, error) {
	var rows string
	err := withCluster(func(socket string) error {
		if err := psql(socket, "template1", "CREATE DATABASE drill"); err != nil {
			return err
		}
		if err := psqlFile(socket, "drill", dumpPath); err != nil {
			return err
		}
		out, err := psqlQuery(socket, "drill", rowQuery)
		if err != nil {
			return err
		}
		rows = out
		return nil
	})
	return rows, err
}

func withCluster(fn func(socket string) error) error {
	base, err := os.MkdirTemp("/tmp", "svnr")
	if err != nil {
		return err
	}
	defer os.RemoveAll(base)
	data := filepath.Join(base, "data")
	run := filepath.Join(base, "run")
	if err := os.MkdirAll(run, 0o700); err != nil {
		return err
	}
	init := exec.Command(filepath.Join(postgresBin, "initdb"), "-D", data, "-U", "svn_super", "--auth-local=trust", "--auth-host=reject", "--locale=C", "--encoding=UTF8", "--no-instructions")
	init.Env = append(os.Environ(), "LC_ALL=C")
	if out, err := init.CombinedOutput(); err != nil {
		return fmt.Errorf("initdb: %v: %s", err, out)
	}
	logFile, err := os.Create(filepath.Join(base, "postgres.log"))
	if err != nil {
		return err
	}
	defer logFile.Close()
	server := exec.Command(filepath.Join(postgresBin, "postgres"), "-D", data, "-k", run,
		"-c", "listen_addresses=",
		"-c", "unix_socket_permissions=0700",
		"-c", "log_statement=none",
		"-c", "log_min_messages=fatal",
	)
	server.Stdout = logFile
	server.Stderr = logFile
	if err := server.Start(); err != nil {
		return err
	}
	defer func() {
		_ = server.Process.Signal(syscall.SIGINT)
		done := make(chan struct{})
		go func() {
			_ = server.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = server.Process.Kill()
			<-done
		}
	}()
	deadline := time.Now().Add(30 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		check := exec.Command(filepath.Join(postgresBin, "pg_isready"), "-h", run, "-U", "svn_super")
		if check.Run() == nil {
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		logged, _ := os.ReadFile(logFile.Name())
		return fmt.Errorf("%w: %s", ErrPostgresAbsent, logged)
	}
	return fn(run)
}

func psql(socket, database, statement string) error {
	cmd := exec.Command(filepath.Join(postgresBin, "psql"), "-h", socket, "-U", "svn_super", "-d", database, "-w", "-v", "ON_ERROR_STOP=1", "-c", statement)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("psql: %v: %s", err, out)
	}
	return nil
}

func psqlFile(socket, database, path string) error {
	cmd := exec.Command(filepath.Join(postgresBin, "psql"), "-h", socket, "-U", "svn_super", "-d", database, "-w", "-v", "ON_ERROR_STOP=1", "-f", path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("psql file: %v: %s", err, out)
	}
	return nil
}

func psqlQuery(socket, database, statement string) (string, error) {
	cmd := exec.Command(filepath.Join(postgresBin, "psql"), "-h", socket, "-U", "svn_super", "-d", database, "-w", "-v", "ON_ERROR_STOP=1", "-At", "-c", statement)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("psql query: %v: %s", err, out)
	}
	return string(out), nil
}

func pgDump(socket, database string) ([]byte, error) {
	cmd := exec.Command(filepath.Join(postgresBin, "pg_dump"), "-h", socket, "-U", "svn_super", "-w", "--no-owner", "--no-acl", "--format=p", database)
	out, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("pg_dump: %v: %s", err, exit.Stderr)
		}
		return nil, err
	}
	return out, nil
}

func writeTemp(body string) (string, error) {
	file, err := os.CreateTemp("", "svn-sql-")
	if err != nil {
		return "", err
	}
	if _, err := file.WriteString(body); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return "", err
	}
	name := file.Name()
	return name, file.Close()
}
