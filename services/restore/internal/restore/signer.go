package restore

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

const (
	pubFile   = "consensus_pub"
	stateFile = "signing_state.json"
	stopFile  = "stop.json"
	holding   = "holding"
)

var (
	ErrDuplicateSigner = errors.New("restore: duplicate live signer")
	ErrNotVerified     = errors.New("restore: old signer is not stopped and verified")
	ErrRolledBack      = errors.New("restore: signer safety would move backwards")
	ErrStaleSnapshot   = errors.New("restore: stale snapshot is not permission to sign")
	ErrCopiedLive      = errors.New("restore: copied live validator key")
)

// SignState is the height, round, and step of the selected signer. CometBFT
// stores the same three values. Height is written as a decimal string.
type SignState struct {
	Height int64 `json:"-"`
	Round  int32 `json:"round"`
	Step   int32 `json:"step"`
}

// Behind reports whether s is strictly older than other.
func (s SignState) Behind(other SignState) bool {
	if s.Height != other.Height {
		return s.Height < other.Height
	}
	if s.Round != other.Round {
		return s.Round < other.Round
	}
	return s.Step < other.Step
}

type stopRecord struct {
	Stopped bool   `json:"stopped"`
	PID     int    `json:"pid"`
	Address string `json:"address"`
	Height  string `json:"height"`
	Round   int32  `json:"round"`
	Step    int32  `json:"step"`
}

func (s stopRecord) state() (SignState, error) {
	return parseHeight(s.Height, s.Round, s.Step)
}

// Address is the hex of the first 20 bytes of SHA-256 of the consensus public key.
func Address(pub []byte) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:20])
}

// Hold locks the consensus address and blocks until release reaches EOF.
// A second process that holds the same address is refused. On a clean
// return the stop attestation is written before the lock is released.
func Hold(home, lockDir string, release io.Reader) error {
	pub, err := os.ReadFile(filepath.Join(home, pubFile))
	if err != nil {
		return err
	}
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("restore: consensus public key is not 32 bytes")
	}
	state, err := ReadSignState(filepath.Join(home, stateFile))
	if err != nil {
		return err
	}
	lock, err := lockAddress(lockDir, Address(pub))
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := writePrivate(filepath.Join(home, holding), []byte(strconv.Itoa(os.Getpid())+"\n")); err != nil {
		return err
	}
	if _, err := io.Copy(io.Discard, release); err != nil {
		return err
	}
	record := stopRecord{
		Stopped: true,
		PID:     os.Getpid(),
		Address: Address(pub),
		Height:  strconv.FormatInt(state.Height, 10),
		Round:   state.Round,
		Step:    state.Step,
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	path := filepath.Join(home, stopFile)
	if err := writePrivate(path, raw); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

// Replace installs next into newHome only after the old process is stopped
// and its attestation matches the same consensus address. A live lock, a
// missing attestation, a live pid, an older state, or a snapshot behind
// the chain head is refused. This function does not leave the lock held
// and it does not start a second signer.
func Replace(oldHome, newHome, lockDir string, next SignState, chainHead int64, sign bool) error {
	if next.Height < 0 || next.Round < 0 || next.Step < 0 || chainHead < 0 {
		return ErrRolledBack
	}
	pub, err := os.ReadFile(filepath.Join(newHome, pubFile))
	if err != nil {
		return err
	}
	old, err := os.ReadFile(filepath.Join(oldHome, pubFile))
	if err != nil {
		return err
	}
	if Address(pub) != Address(old) {
		return ErrNotVerified
	}
	lock, err := lockAddress(lockDir, Address(pub))
	if err != nil {
		return err
	}
	defer lock.Close()
	raw, err := os.ReadFile(filepath.Join(oldHome, stopFile))
	if err != nil {
		return ErrNotVerified
	}
	var record stopRecord
	if err := json.Unmarshal(raw, &record); err != nil || !record.Stopped || record.Address != Address(pub) {
		return ErrNotVerified
	}
	if processLive(record.PID) {
		return ErrNotVerified
	}
	stopped, err := record.state()
	if err != nil {
		return err
	}
	if next.Behind(stopped) {
		return ErrRolledBack
	}
	currentPath := filepath.Join(newHome, stateFile)
	if current, err := ReadSignState(currentPath); err == nil && next.Behind(current) {
		return ErrRolledBack
	}
	if next.Height < chainHead {
		return ErrStaleSnapshot
	}
	if err := WriteSignState(currentPath, next); err != nil {
		return err
	}
	if !sign {
		return nil
	}
	return writePrivate(filepath.Join(newHome, "signing_allowed"), []byte("drill\n"))
}

// Inspect writes the metadata beside the live state file and does not
// mark the home as allowed to sign.
func Inspect(home string, meta SignerMeta) error {
	if meta.PrivateKeyIncluded {
		return ErrSecret
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return writePrivate(filepath.Join(home, "signer_safety_inspection.json"), raw)
}

// RefuseCopiedLive fails when a process already holds this consensus address.
func RefuseCopiedLive(lockDir string, pub []byte) error {
	lock, err := lockAddress(lockDir, Address(pub))
	if errors.Is(err, ErrDuplicateSigner) {
		return ErrCopiedLive
	}
	if err != nil {
		return err
	}
	return lock.Close()
}

// DistinctKeyFiles refuses two paths that are the same file or the same bytes.
// The three paths are the peer identity, the validator signing key, and the
// governance key.
func DistinctKeyFiles(peer, validator, governance string) error {
	paths := []string{peer, validator, governance}
	seenPath := map[string]struct{}{}
	seenBody := map[string]struct{}{}
	for _, path := range paths {
		abs, err := filepath.EvalSymlinks(path)
		if err != nil {
			return err
		}
		if _, ok := seenPath[abs]; ok {
			return ErrSameKeyFile
		}
		seenPath[abs] = struct{}{}
		body, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		key := hex.EncodeToString(sum[:])
		if _, ok := seenBody[key]; ok {
			return ErrSameKeyFile
		}
		seenBody[key] = struct{}{}
	}
	return nil
}

// RotateService generates a new service signing key. The retired public key
// cannot verify a signature of the new key. The private key is returned to
// the caller and is not written here. Production rotation needs an operator
// token. No operator is named.
func RotateService(approval Approval) (nextPub, retired ed25519.PublicKey, next ed25519.PrivateKey, err error) {
	if err = approval.Allow(); err != nil {
		return nil, nil, nil, err
	}
	_, retiredPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	_, next, err = ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	message := []byte("svn-service-rotate-drill")
	oldSig := ed25519.Sign(retiredPriv, message)
	newSig := ed25519.Sign(next, message)
	nextPub = next.Public().(ed25519.PublicKey)
	retired = retiredPriv.Public().(ed25519.PublicKey)
	if ed25519.Verify(nextPub, message, oldSig) {
		return nil, nil, nil, errors.New("restore: retired service key was accepted")
	}
	if !ed25519.Verify(nextPub, message, newSig) {
		return nil, nil, nil, errors.New("restore: new service key was rejected")
	}
	return nextPub, retired, next, nil
}

// ReadSignState reads height, round, and step. Height may be a JSON string
// or a JSON number.
func ReadSignState(path string) (SignState, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return SignState{}, err
	}
	var wire struct {
		Height json.RawMessage `json:"height"`
		Round  int32           `json:"round"`
		Step   int32           `json:"step"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return SignState{}, err
	}
	var text string
	if err := json.Unmarshal(wire.Height, &text); err != nil {
		var number int64
		if err := json.Unmarshal(wire.Height, &number); err != nil {
			return SignState{}, err
		}
		text = strconv.FormatInt(number, 10)
	}
	return parseHeight(text, wire.Round, wire.Step)
}

// WriteSignState writes height as a decimal string.
func WriteSignState(path string, state SignState) error {
	if state.Height < 0 || state.Round < 0 || state.Step < 0 {
		return ErrRolledBack
	}
	body, err := json.Marshal(struct {
		Height string `json:"height"`
		Round  int32  `json:"round"`
		Step   int32  `json:"step"`
	}{Height: strconv.FormatInt(state.Height, 10), Round: state.Round, Step: state.Step})
	if err != nil {
		return err
	}
	return writePrivate(path, body)
}

func parseHeight(height string, round, step int32) (SignState, error) {
	value, err := strconv.ParseInt(height, 10, 64)
	if err != nil || value < 0 || round < 0 || step < 0 {
		return SignState{}, ErrRolledBack
	}
	return SignState{Height: value, Round: round, Step: step}, nil
}

func lockAddress(lockDir, address string) (*os.File, error) {
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(lockDir, 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(lockDir, address+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		file.Close()
		return nil, ErrDuplicateSigner
	}
	if err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func processLive(pid int) bool {
	if pid <= 0 {
		return true
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// WriteHome writes a consensus public key and a signing state. It does not
// write a private key.
func WriteHome(dir string, pub []byte, state SignState) error {
	if len(pub) != ed25519.PublicKeySize {
		return errors.New("restore: consensus public key is not 32 bytes")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	if err := writePrivate(filepath.Join(dir, pubFile), pub); err != nil {
		return err
	}
	return WriteSignState(filepath.Join(dir, stateFile), state)
}
