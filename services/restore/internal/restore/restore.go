package restore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The encryption key stays outside the backup directory.

const (
	Format             = "SVN-BACKUP-1"
	FormatVersion      = 1
	SchemaVersion      = 1
	IndexSchema        = "SVN-INDEX-CHECKPOINT-1"
	SourceCheckpoint   = "trusted_checkpoint"
	SourceCanonical    = "canonical_state"
	SourceReplica      = "live_replica"
	RetentionPolicy    = "PLACEHOLDER"
	RetentionPeriod    = "UNSET"
	CustodyPolicy      = "PLACEHOLDER"
	RecoveryPolicy     = "PLACEHOLDER"
	partConfig         = "config"
	partDatabase       = "database"
	partSigned         = "signed_records"
	partMedia          = "media"
	partIndex          = "index"
	partSigner         = "signer_safety"
	fileManifest       = "manifest.json"
	fileConfig         = "config.json"
	fileDatabase       = "database.blob"
	fileSigned         = "signed-records.blob"
	fileMedia          = "media.blob"
	fileIndex          = "index.blob"
	fileSigner         = "signer-safety.json"
	admissionsFile     = "admissions-stopped"
	historyErasedField = false
)

// RestoreOrder is the only order a restore applies. A manifest that names
// another order is refused.
var RestoreOrder = []string{partConfig, partDatabase, partSigned, partMedia, partIndex, partSigner}

var (
	ErrVersion           = errors.New("restore: wrong version")
	ErrCorrupted         = errors.New("restore: corrupted backup")
	ErrIncomplete        = errors.New("restore: incomplete backup")
	ErrOriginAvailable   = errors.New("restore: origin is still available")
	ErrReplicaNotBackup  = errors.New("restore: a live replica is not a backup")
	ErrAccess            = errors.New("restore: backup access is too wide")
	ErrSecret            = errors.New("restore: live secret refused")
	ErrApproval          = errors.New("restore: operator approval required")
	ErrRecoveryBlocked   = errors.New("restore: account recovery is blocked")
	ErrMissingMedia      = errors.New("restore: media missing")
	ErrDigest            = errors.New("restore: digest mismatch")
	ErrRemoved           = errors.New("restore: removed")
	ErrAdmissionsStopped = errors.New("restore: admissions are stopped")
	ErrPostgresAbsent    = errors.New("restore: postgres is absent")
	ErrSameKeyFile       = errors.New("restore: key files are not distinct")
)

// Approval is a drill flag or an operator token. The token does not name a
// person. A drill and a token together are refused.
type Approval struct {
	Drill         bool
	OperatorToken string
}

func (a Approval) Allow() error {
	if a.Drill && strings.TrimSpace(a.OperatorToken) != "" {
		return errors.New("restore: drill approval is not an operator approval")
	}
	if a.Drill {
		return nil
	}
	if strings.TrimSpace(a.OperatorToken) == "" {
		return ErrApproval
	}
	return nil
}

// World is the application state a backup reads. The database is a logical
// dump, not a copy of a live data directory.
type World struct {
	ChainID       string
	SchemaVersion int
	Config        map[string]string
	DatabaseSQL   []byte
	SignedRecords []SignedRecord
	Media         []MediaFile
	Index         IndexCheckpoint
	Signer        SignerMeta
}

// SignedRecord is an off-chain signed export. The signing key is not part
// of the record.
type SignedRecord struct {
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Document  string `json:"document"`
	Public    string `json:"public"`
	Signature string `json:"signature"`
}

// MediaFile is one critical media object, named by the SHA-256 of its bytes.
type MediaFile struct {
	Digest string
	Body   []byte
}

// IndexCheckpoint is a trusted checkpoint or a canonical-state export.
// A live replica is not either of those.
type IndexCheckpoint struct {
	Schema  string     `json:"schema"`
	ChainID string     `json:"chain_id"`
	Height  int64      `json:"height"`
	AppHash string     `json:"app_hash"`
	Source  string     `json:"source"`
	Rows    []IndexRow `json:"rows"`
}

// IndexRow is one projection row. Authorization is "published" or "removed".
type IndexRow struct {
	ContentID     string `json:"content_id"`
	MediaDigest   string `json:"media_digest"`
	Authorization string `json:"authorization"`
}

// SignerMeta is the public signer-safety record. PrivateKeyIncluded must
// stay false. The signing key is not a field of this struct.
type SignerMeta struct {
	ConsensusAddress   string `json:"consensus_address"`
	PublicKey          string `json:"public_key"`
	Height             int64  `json:"height"`
	Round              int32  `json:"round"`
	Step               int32  `json:"step"`
	ChainHead          int64  `json:"chain_head"`
	PrivateKeyIncluded bool   `json:"private_key_included"`
}

// Manifest is the plaintext description of a backup. It holds digests, not
// the encryption key.
type Manifest struct {
	Format            string            `json:"format"`
	FormatVersion     int               `json:"format_version"`
	AppSchemaVersion  int               `json:"app_schema_version"`
	ChainID           string            `json:"chain_id"`
	CreatedUnixNano   int64             `json:"created_unix_nano"`
	NewestUnixNano    int64             `json:"newest_unix_nano"`
	RetentionPolicy   string            `json:"retention_policy"`
	RetentionPeriod   string            `json:"retention_period"`
	RetentionDecision string            `json:"retention_decision"`
	CustodyPolicy     string            `json:"custody_policy"`
	CustodyDecision   string            `json:"custody_decision"`
	RecoveryPolicy    string            `json:"recovery_policy"`
	ServiceRTO        string            `json:"service_rto"`
	ServiceRPO        string            `json:"service_rpo"`
	RestoreOrder      []string          `json:"restore_order"`
	Parts             map[string]Part   `json:"parts"`
	NotABackup        []string          `json:"not_a_backup"`
	SecretsIncluded   bool              `json:"secrets_included"`
	CompatibleWith    []string          `json:"compatible_with"`
	Requests          map[string]string `json:"requests"`
}

// Part is one file in the backup directory.
type Part struct {
	File      string `json:"file"`
	SHA256    string `json:"sha256"`
	PlainSHA  string `json:"plain_sha256"`
	Encrypted bool   `json:"encrypted"`
}

// Options selects the key and the age of the newest application record.
type Options struct {
	Key    []byte
	KeyOut string
	Newest time.Time
}

// Result is one restore. The observed times are measurements of this call.
// They are not service guarantees, and the two guarantee fields stay false.
type Result struct {
	Status                        string   `json:"status"`
	OriginUnavailable             bool     `json:"origin_unavailable"`
	DigestsMatch                  bool     `json:"digests_match"`
	ObservedRTOSeconds            float64  `json:"observed_rto_seconds"`
	ObservedRPOSeconds            float64  `json:"observed_rpo_seconds"`
	RTOAndRPOAreServiceGuarantees bool     `json:"rto_rpo_are_service_guarantees"`
	Missing                       []string `json:"missing,omitempty"`
	Restored                      []string `json:"restored"`
	HistoryErased                 bool     `json:"history_erased"`
	RetentionPeriod               string   `json:"retention_period"`
}

// Backup writes a new backup directory. dir must not already exist. The key
// is written only to opt.KeyOut, which must be outside dir.
func Backup(dir string, world World, opt Options) (Manifest, error) {
	var zero Manifest
	if err := validateWorld(world); err != nil {
		return zero, err
	}
	if err := dirMustBeNew(dir); err != nil {
		return zero, err
	}
	key := opt.Key
	if key == nil {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return zero, err
		}
	}
	if len(key) != 32 {
		return zero, errors.New("restore: encryption key must be 32 bytes")
	}
	if opt.KeyOut != "" {
		rel, err := filepath.Rel(dir, opt.KeyOut)
		if err != nil {
			return zero, err
		}
		if rel == "." || !strings.HasPrefix(rel, "..") {
			return zero, ErrSecret
		}
	}
	config, err := stripConfig(world.Config)
	if err != nil {
		return zero, err
	}
	configBytes, err := json.Marshal(config)
	if err != nil {
		return zero, err
	}
	signedBytes, err := json.Marshal(world.SignedRecords)
	if err != nil {
		return zero, err
	}
	mediaBytes, err := marshalMedia(world.Media)
	if err != nil {
		return zero, err
	}
	indexBytes, err := json.Marshal(world.Index)
	if err != nil {
		return zero, err
	}
	signerBytes, err := json.Marshal(world.Signer)
	if err != nil {
		return zero, err
	}
	sealed := map[string][]byte{}
	for name, plain := range map[string][]byte{
		partDatabase: world.DatabaseSQL,
		partSigned:   signedBytes,
		partMedia:    mediaBytes,
		partIndex:    indexBytes,
	} {
		out, err := seal(name, key, plain)
		if err != nil {
			return zero, err
		}
		sealed[name] = out
	}
	newest := opt.Newest
	if newest.IsZero() {
		newest = time.Now()
	}
	created := time.Now()
	if created.Before(newest) {
		created = newest
	}
	manifest := Manifest{
		Format:            Format,
		FormatVersion:     FormatVersion,
		AppSchemaVersion:  world.SchemaVersion,
		ChainID:           world.ChainID,
		CreatedUnixNano:   created.UnixNano(),
		NewestUnixNano:    newest.UnixNano(),
		RetentionPolicy:   RetentionPolicy,
		RetentionPeriod:   RetentionPeriod,
		RetentionDecision: "DEC-004",
		CustodyPolicy:     CustodyPolicy,
		CustodyDecision:   "DEC-001",
		RecoveryPolicy:    RecoveryPolicy,
		ServiceRTO:        RetentionPeriod,
		ServiceRPO:        RetentionPeriod,
		RestoreOrder:      append([]string(nil), RestoreOrder...),
		NotABackup:        []string{SourceReplica},
		SecretsIncluded:   false,
		CompatibleWith:    []string{Format},
		Requests: map[string]string{
			"DR-26-01": "retention period unset",
			"DR-26-02": "key custody procedure unset",
			"DR-26-03": "recovery policy unset",
		},
		Parts: map[string]Part{},
	}
	files := map[string][]byte{
		fileConfig:   configBytes,
		fileDatabase: sealed[partDatabase],
		fileSigned:   sealed[partSigned],
		fileMedia:    sealed[partMedia],
		fileIndex:    sealed[partIndex],
		fileSigner:   signerBytes,
	}
	plain := map[string][]byte{
		fileConfig:   configBytes,
		fileDatabase: world.DatabaseSQL,
		fileSigned:   signedBytes,
		fileMedia:    mediaBytes,
		fileIndex:    indexBytes,
		fileSigner:   signerBytes,
	}
	names := map[string]string{
		partConfig:   fileConfig,
		partDatabase: fileDatabase,
		partSigned:   fileSigned,
		partMedia:    fileMedia,
		partIndex:    fileIndex,
		partSigner:   fileSigner,
	}
	encrypted := map[string]bool{
		partDatabase: true, partSigned: true, partMedia: true, partIndex: true,
	}
	for _, part := range RestoreOrder {
		name := names[part]
		manifest.Parts[part] = Part{
			File:      name,
			SHA256:    digest(files[name]),
			PlainSHA:  digest(plain[name]),
			Encrypted: encrypted[part],
		}
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		return zero, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return zero, err
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return zero, err
	}
	files[fileManifest] = append(manifestBytes, '\n')
	for name, body := range files {
		if err := writePrivate(filepath.Join(dir, name), body); err != nil {
			return zero, err
		}
	}
	if opt.KeyOut != "" {
		if err := writePrivate(opt.KeyOut, key); err != nil {
			return zero, err
		}
	}
	return manifest, nil
}

// Restore reads a backup into dest. origin must be a path that does not
// exist: the original application and media are unavailable. A partial
// restore is not reported as restored.
func Restore(backupDir, keyPath, dest, origin string, partial bool) (Result, error) {
	started := time.Now()
	var result Result
	result.HistoryErased = historyErasedField
	result.RetentionPeriod = RetentionPeriod
	result.RTOAndRPOAreServiceGuarantees = false
	if _, err := os.Stat(origin); err == nil {
		return result, ErrOriginAvailable
	} else if !os.IsNotExist(err) {
		return result, err
	}
	result.OriginUnavailable = true
	if err := accessOK(backupDir); err != nil {
		return result, err
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return result, err
	}
	info, err := os.Stat(keyPath)
	if err != nil {
		return result, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return result, ErrAccess
	}
	manifest, err := readManifest(filepath.Join(backupDir, fileManifest))
	if err != nil {
		return result, err
	}
	if err := checkManifest(manifest); err != nil {
		return result, err
	}
	if _, err := os.Stat(dest); err == nil {
		return result, errors.New("restore: destination already exists")
	}
	missing := missingParts(backupDir, manifest)
	if len(missing) > 0 && !partial {
		return result, ErrIncomplete
	}
	plain := map[string][]byte{}
	var restored []string
	for _, part := range RestoreOrder {
		item, ok := manifest.Parts[part]
		if !ok {
			missing = append(missing, part)
			continue
		}
		path := filepath.Join(backupDir, item.File)
		body, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			missing = append(missing, part)
			continue
		}
		if err != nil {
			return result, err
		}
		if digest(body) != item.SHA256 {
			return result, ErrCorrupted
		}
		opened := body
		if item.Encrypted {
			opened, err = open(part, key, body)
			if err != nil {
				return result, ErrCorrupted
			}
		}
		if digest(opened) != item.PlainSHA {
			return result, ErrCorrupted
		}
		plain[part] = opened
		restored = append(restored, part)
	}
	if err := os.Mkdir(dest, 0o700); err != nil {
		return result, err
	}
	if err := writeRestored(dest, plain); err != nil {
		_ = os.RemoveAll(dest)
		return result, err
	}
	result.Restored = restored
	result.Missing = unique(missing)
	result.DigestsMatch = true
	result.ObservedRTOSeconds = time.Since(started).Seconds()
	result.ObservedRPOSeconds = float64(manifest.CreatedUnixNano-manifest.NewestUnixNano) / float64(time.Second)
	if len(result.Missing) > 0 {
		result.Status = "partial"
		return result, nil
	}
	result.Status = "restored"
	return result, nil
}

func writeRestored(dest string, plain map[string][]byte) error {
	writes := map[string][]byte{}
	if body, ok := plain[partConfig]; ok {
		writes["config.json"] = body
	}
	if body, ok := plain[partDatabase]; ok {
		writes["database.sql"] = body
	}
	if body, ok := plain[partSigned]; ok {
		writes["signed-records.json"] = body
	}
	if body, ok := plain[partIndex]; ok {
		writes["index.json"] = body
	}
	if body, ok := plain[partSigner]; ok {
		writes["signer-safety.json"] = body
	}
	for name, body := range writes {
		if err := writePrivate(filepath.Join(dest, name), body); err != nil {
			return err
		}
	}
	body, ok := plain[partMedia]
	if !ok {
		return nil
	}
	var bundle struct {
		Files []struct {
			Digest string `json:"digest"`
			Body   string `json:"body"`
		} `json:"files"`
	}
	if err := json.Unmarshal(body, &bundle); err != nil {
		return ErrCorrupted
	}
	mediaDir := filepath.Join(dest, "media")
	if err := os.Mkdir(mediaDir, 0o700); err != nil {
		return err
	}
	for _, file := range bundle.Files {
		raw, err := base64.StdEncoding.DecodeString(file.Body)
		if err != nil {
			return ErrCorrupted
		}
		if digest(raw) != file.Digest {
			return ErrDigest
		}
		if err := writePrivate(filepath.Join(mediaDir, file.Digest), raw); err != nil {
			return err
		}
	}
	return nil
}

// ExportCI writes the manifest and nothing else. A forbidden file name or a
// PEM header in the backup refuses the export and writes nothing.
func ExportCI(backupDir, out string) error {
	if err := scanSecrets(backupDir); err != nil {
		return err
	}
	manifest, err := readManifest(filepath.Join(backupDir, fileManifest))
	if err != nil {
		return err
	}
	if manifest.SecretsIncluded {
		return ErrSecret
	}
	raw, err := os.ReadFile(filepath.Join(backupDir, fileManifest))
	if err != nil {
		return err
	}
	if strings.Contains(string(raw), "-----BEGIN ") {
		return ErrSecret
	}
	if err := os.Mkdir(out, 0o700); err != nil {
		return err
	}
	return writePrivate(filepath.Join(out, fileManifest), raw)
}

// Playback returns the media bytes for a published row. A removed row, a
// stopped admission, a missing file, or a mismatched digest is refused.
func Playback(indexPath, mediaDir, contentID string, admissionsStopped bool) ([]byte, error) {
	if admissionsStopped {
		return nil, ErrAdmissionsStopped
	}
	rows, err := loadIndex(indexPath)
	if err != nil {
		return nil, err
	}
	var row *IndexRow
	for i := range rows.Rows {
		if rows.Rows[i].ContentID == contentID {
			row = &rows.Rows[i]
			break
		}
	}
	if row == nil {
		return nil, ErrMissingMedia
	}
	if row.Authorization != "published" {
		return nil, ErrRemoved
	}
	body, err := os.ReadFile(filepath.Join(mediaDir, row.MediaDigest))
	if err != nil {
		return nil, ErrMissingMedia
	}
	if digest(body) != row.MediaDigest {
		return nil, ErrDigest
	}
	return body, nil
}

// Remove marks one row removed. It does not delete the media bytes and it
// does not claim that history was erased.
func Remove(indexPath, contentID string) error {
	rows, err := loadIndex(indexPath)
	if err != nil {
		return err
	}
	found := false
	for i := range rows.Rows {
		if rows.Rows[i].ContentID == contentID {
			rows.Rows[i].Authorization = "removed"
			found = true
		}
	}
	if !found {
		return ErrMissingMedia
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	return writePrivate(indexPath, raw)
}

// Authorizations returns content id to authorization.
func Authorizations(indexPath string) (map[string]string, error) {
	rows, err := loadIndex(indexPath)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, row := range rows.Rows {
		out[row.ContentID] = row.Authorization
	}
	return out, nil
}

// Digests returns content id to media digest.
func Digests(indexPath string) (map[string]string, error) {
	rows, err := loadIndex(indexPath)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, row := range rows.Rows {
		out[row.ContentID] = row.MediaDigest
	}
	return out, nil
}

// RebuildProjection copies the checkpoint rows and nothing else. Media files
// that the checkpoint does not name are not added.
func RebuildProjection(indexPath, outPath string) (IndexCheckpoint, error) {
	cp, err := loadIndex(indexPath)
	if err != nil {
		return IndexCheckpoint{}, err
	}
	if cp.Source != SourceCheckpoint && cp.Source != SourceCanonical {
		return IndexCheckpoint{}, ErrReplicaNotBackup
	}
	if cp.Schema != IndexSchema {
		return IndexCheckpoint{}, ErrVersion
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		return IndexCheckpoint{}, err
	}
	if err := writePrivate(outPath, raw); err != nil {
		return IndexCheckpoint{}, err
	}
	return cp, nil
}

// StopAdmissions writes the marker that Playback honours.
func StopAdmissions(dir string) error {
	return writePrivate(filepath.Join(dir, admissionsFile), []byte("stopped\n"))
}

// AdmissionsStopped reports the marker.
func AdmissionsStopped(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, admissionsFile))
	return err == nil
}

// Handoff is a lease handoff record. ChainSubmitted stays false: this
// package does not submit a transaction.
type Handoff struct {
	From           string `json:"from"`
	To             string `json:"to"`
	LeaseID        string `json:"lease_id"`
	ChainSubmitted bool   `json:"chain_submitted"`
}

// LeaseHandoff writes the record. It does not dial a node.
func LeaseHandoff(dir, from, to, leaseID string) (Handoff, error) {
	item := Handoff{From: from, To: to, LeaseID: leaseID, ChainSubmitted: false}
	raw, err := json.Marshal(item)
	if err != nil {
		return Handoff{}, err
	}
	if err := writePrivate(filepath.Join(dir, "lease-handoff.json"), raw); err != nil {
		return Handoff{}, err
	}
	return item, nil
}

// DeleteResult records that deletion did not erase history and that the
// retention period is unset.
type DeleteResult struct {
	HistoryErased   bool   `json:"history_erased"`
	RetentionPeriod string `json:"retention_period"`
}

// ControlledDelete removes one backup directory after confirmation. It
// refuses a directory that is not a backup, a symlink, or a missing approval.
func ControlledDelete(dir, confirm string, approval Approval) (DeleteResult, error) {
	result := DeleteResult{HistoryErased: false, RetentionPeriod: RetentionPeriod}
	if confirm != "delete" {
		return result, errors.New("restore: confirm delete")
	}
	if err := approval.Allow(); err != nil {
		return result, err
	}
	manifest, err := readManifest(filepath.Join(dir, fileManifest))
	if err != nil {
		return result, err
	}
	if manifest.Format != Format {
		return result, ErrVersion
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return result, err
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return result, ErrSecret
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return result, err
	}
	return result, nil
}

// RecoveryResult is the account-recovery rehearsal. Completion stays blocked.
type RecoveryResult struct {
	ExportRestored bool   `json:"export_restored"`
	Completion     string `json:"completion"`
	Request        string `json:"request"`
}

// RehearseAccountExport encrypts and decrypts an account export. Completing
// recovery is refused: the custody decision does not name a procedure.
func RehearseAccountExport(doc, key []byte) ([]byte, RecoveryResult, error) {
	result := RecoveryResult{Completion: "BLOCKED", Request: "DR-26-05"}
	sealed, err := seal("account_export", key, doc)
	if err != nil {
		return nil, result, err
	}
	plain, err := open("account_export", key, sealed)
	if err != nil {
		return nil, result, err
	}
	result.ExportRestored = string(plain) == string(doc)
	return plain, result, nil
}

// CompleteAccountRecovery is refused until a recovery procedure is approved.
func CompleteAccountRecovery() error {
	return ErrRecoveryBlocked
}

// ManualApprovals is the list a live recovery still needs. No person is named.
func ManualApprovals() []string {
	return []string{
		"DR-26-01 retention period is unset",
		"DR-26-02 key custody procedure is unset",
		"DR-26-03 recovery policy and service RTO and RPO targets are unset",
		"DR-26-04 no operator is named to approve a production restore or a production key rotation",
		"DR-26-05 account recovery completion waits on the custody decision",
		"DR-26-06 signer replacement on a live node needs an independent review",
	}
}

// UnreconstructableFromReplicas lists state a current media replica does not hold.
func UnreconstructableFromReplicas() []string {
	return []string{
		"validator signing state",
		"backup encryption key",
		"account sessions",
		"uncommitted quarantine uploads",
		"moderation complaint text",
		"pruned chain blocks",
		"package versions absent from the current replica set",
	}
}

// Compatible refuses every version other than the one this package writes.
// No migration is applied.
func Compatible(formatVersion, schemaVersion int) error {
	if formatVersion != FormatVersion || schemaVersion != SchemaVersion {
		return ErrVersion
	}
	return nil
}

// VerifySigned checks one record with the public key it names.
func VerifySigned(record SignedRecord) error {
	pub, err := hex.DecodeString(record.Public)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return ErrDigest
	}
	sig, err := hex.DecodeString(record.Signature)
	if err != nil {
		return ErrDigest
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(record.Document), sig) {
		return ErrDigest
	}
	return nil
}

func validateWorld(world World) error {
	if world.SchemaVersion != SchemaVersion {
		return ErrVersion
	}
	if world.ChainID == "" {
		return errors.New("restore: chain id is empty")
	}
	if world.Index.Source == SourceReplica {
		return ErrReplicaNotBackup
	}
	if world.Index.Source != SourceCheckpoint && world.Index.Source != SourceCanonical {
		return ErrReplicaNotBackup
	}
	if world.Index.Schema != IndexSchema || world.Index.ChainID != world.ChainID {
		return ErrVersion
	}
	if world.Signer.PrivateKeyIncluded {
		return ErrSecret
	}
	media := map[string]struct{}{}
	for _, file := range world.Media {
		if digest(file.Body) != file.Digest {
			return ErrDigest
		}
		media[file.Digest] = struct{}{}
	}
	for _, row := range world.Index.Rows {
		if row.Authorization != "published" && row.Authorization != "removed" {
			return errors.New("restore: authorization is unset")
		}
		if _, ok := media[row.MediaDigest]; !ok {
			return ErrMissingMedia
		}
	}
	for _, record := range world.SignedRecords {
		if err := VerifySigned(record); err != nil {
			return err
		}
	}
	return nil
}

func stripConfig(in map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for key, value := range in {
		lower := strings.ToLower(key)
		switch {
		case strings.Contains(lower, "secret"), strings.Contains(lower, "password"),
			strings.Contains(lower, "token"), strings.Contains(lower, "private"),
			strings.HasSuffix(lower, "_key"), lower == "key":
			return nil, ErrSecret
		}
		out[key] = value
	}
	return out, nil
}

func marshalMedia(files []MediaFile) ([]byte, error) {
	type item struct {
		Digest string `json:"digest"`
		Body   string `json:"body"`
	}
	bundle := struct {
		Files []item `json:"files"`
	}{}
	for _, file := range files {
		bundle.Files = append(bundle.Files, item{Digest: file.Digest, Body: base64.StdEncoding.EncodeToString(file.Body)})
	}
	return json.Marshal(bundle)
}

func seal(part string, key, plain []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nil, nonce, plain, []byte(part))
	out := make([]byte, 0, 5+len(nonce)+len(sealed))
	out = append(out, 'S', 'V', 'N', 'B', 1)
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

func open(part string, key, blob []byte) ([]byte, error) {
	if len(blob) < 5+12 || string(blob[:4]) != "SVNB" || blob[4] != 1 {
		return nil, ErrCorrupted
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrCorrupted
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrCorrupted
	}
	nonceSize := gcm.NonceSize()
	if len(blob) < 5+nonceSize {
		return nil, ErrCorrupted
	}
	nonce := blob[5 : 5+nonceSize]
	plain, err := gcm.Open(nil, nonce, blob[5+nonceSize:], []byte(part))
	if err != nil {
		return nil, ErrCorrupted
	}
	return plain, nil
}

func digest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func writePrivate(path string, body []byte) error {
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func readManifest(path string) (Manifest, error) {
	var manifest Manifest
	raw, err := os.ReadFile(path)
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, ErrCorrupted
	}
	return manifest, nil
}

func checkManifest(manifest Manifest) error {
	if manifest.Format != Format || manifest.FormatVersion != FormatVersion || manifest.AppSchemaVersion != SchemaVersion {
		return ErrVersion
	}
	if len(manifest.RestoreOrder) != len(RestoreOrder) {
		return ErrVersion
	}
	for i, part := range RestoreOrder {
		if manifest.RestoreOrder[i] != part {
			return ErrVersion
		}
	}
	if manifest.SecretsIncluded || manifest.RetentionPeriod != RetentionPeriod || manifest.ServiceRTO != RetentionPeriod || manifest.ServiceRPO != RetentionPeriod {
		return ErrSecret
	}
	for _, part := range RestoreOrder {
		if _, ok := manifest.Parts[part]; !ok {
			return ErrIncomplete
		}
	}
	return nil
}

func missingParts(dir string, manifest Manifest) []string {
	var missing []string
	for _, part := range RestoreOrder {
		item := manifest.Parts[part]
		if _, err := os.Stat(filepath.Join(dir, item.File)); err != nil {
			missing = append(missing, part)
		}
	}
	return missing
}

func accessOK(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return ErrAccess
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode().Perm()&0o077 != 0 {
			return ErrAccess
		}
	}
	return nil
}

func scanSecrets(dir string) error {
	forbidden := map[string]bool{
		"priv_validator_key.json": true,
		"node_key.json":           true,
		"id_ed25519":              true,
		"id_rsa":                  true,
	}
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name := info.Name()
		if forbidden[name] || strings.HasSuffix(name, ".key") || strings.HasSuffix(name, ".pem") {
			return ErrSecret
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(name, ".blob") || info.Size() > 1<<20 {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), "-----BEGIN ") {
			return ErrSecret
		}
		return nil
	})
}

// RefuseLiveSecrets fails when dir holds a live secret file name or a PEM
// header. Encrypted blobs are not scanned: ciphertext is not a key file.
func RefuseLiveSecrets(dir string) error {
	return scanSecrets(dir)
}

func dirMustBeNew(dir string) error {
	if dir == "" {
		return nil
	}
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("restore: backup directory already exists")
	}
	return nil
}

func loadIndex(path string) (IndexCheckpoint, error) {
	var cp IndexCheckpoint
	raw, err := os.ReadFile(path)
	if err != nil {
		return cp, err
	}
	if err := json.Unmarshal(raw, &cp); err != nil {
		return cp, ErrCorrupted
	}
	return cp, nil
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range in {
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// LoadWorld reads the directory layout the command line accepts.
func LoadWorld(dir string) (World, error) {
	var world World
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		return world, err
	}
	if err := json.Unmarshal(raw, &world.Config); err != nil {
		return world, err
	}
	world.ChainID = world.Config["chain_id"]
	world.SchemaVersion = SchemaVersion
	world.DatabaseSQL, err = os.ReadFile(filepath.Join(dir, "database.sql"))
	if err != nil {
		return world, err
	}
	raw, err = os.ReadFile(filepath.Join(dir, "signed-records.json"))
	if err != nil {
		return world, err
	}
	if err := json.Unmarshal(raw, &world.SignedRecords); err != nil {
		return world, err
	}
	raw, err = os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		return world, err
	}
	if err := json.Unmarshal(raw, &world.Index); err != nil {
		return world, err
	}
	raw, err = os.ReadFile(filepath.Join(dir, "signer.json"))
	if err != nil {
		return world, err
	}
	if err := json.Unmarshal(raw, &world.Signer); err != nil {
		return world, err
	}
	mediaDir := filepath.Join(dir, "media")
	entries, err := os.ReadDir(mediaDir)
	if err != nil {
		return world, err
	}
	for _, entry := range entries {
		body, err := os.ReadFile(filepath.Join(mediaDir, entry.Name()))
		if err != nil {
			return world, err
		}
		world.Media = append(world.Media, MediaFile{Digest: digest(body), Body: body})
	}
	return world, nil
}

// Digest is the SHA-256 hex of body.
func Digest(body []byte) string {
	return digest(body)
}

// ReadKey reads a 32-byte key file.
func ReadKey(path string) ([]byte, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(body) != 32 {
		return nil, errors.New("restore: encryption key must be 32 bytes")
	}
	return body, nil
}
