package worker

// Helper processes. TestMain runs one of these instead of the tests when
// the test binary is re-executed by pgtest.HelperCommand, so a test can
// SIGKILL a real process at a known point (a goroutine is not a process).

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// helperSpecEnv names the environment variable that holds the path of a
// helper's JSON spec.
const helperSpecEnv = "SVN_WORKER_HELPER_SPEC"

// helperSpec tells a helper where the database is and what to do. The
// password is in a file of mode 0600, never in the environment.
type helperSpec struct {
	SocketDir     string `json:"socket_dir"`
	Port          uint16 `json:"port"`
	Database      string `json:"database"`
	User          string `json:"user"`
	PasswordFile  string `json:"password_file"`
	SignerHex     string `json:"signer_hex"`
	IntentID      string `json:"intent_id"`
	AccountNumber uint64 `json:"account_number"`
	Sequence      uint64 `json:"sequence"`
	TimeoutHeight uint64 `json:"timeout_height"`
	Gas           uint64 `json:"gas"`
	TxHex         string `json:"tx_hex"`
	Marker        string `json:"marker"`
}

// helperPersistAndCount is the helper "persist-and-count-send": it takes
// the signer's lock, persists the attempt of the spec (plan 8.3 step 1),
// counts one send as started (step 2), writes the marker file, and waits to
// be killed, as a worker that dies before it broadcasts or records.
const helperPersistAndCount = "persist-and-count-send"

func runHelper(name string) int {
	if name != helperPersistAndCount {
		fmt.Fprintln(os.Stderr, "worker helper: unknown helper", name)
		return 2
	}
	if err := persistAndCount(); err != nil {
		fmt.Fprintln(os.Stderr, "worker helper:", err)
		return 1
	}
	time.Sleep(10 * time.Minute)
	return 0
}

func persistAndCount() error {
	raw, err := os.ReadFile(os.Getenv(helperSpecEnv))
	if err != nil {
		return err
	}
	var spec helperSpec
	if err := json.Unmarshal(raw, &spec); err != nil {
		return err
	}
	password, err := os.ReadFile(spec.PasswordFile)
	if err != nil {
		return err
	}
	db, err := store.OpenDB(store.ConnConfig{SocketDir: spec.SocketDir, Port: spec.Port, Database: spec.Database, User: spec.User,
		Password: string(password), ApplicationName: "worker-helper", ConnectTimeout: 10 * time.Second})
	if err != nil {
		return err
	}
	st, err := store.New(db, storeLimits(), "worker-helper")
	if err != nil {
		return err
	}
	signerAddr, err := hex.DecodeString(spec.SignerHex)
	if err != nil {
		return err
	}
	tx, err := hex.DecodeString(spec.TxHex)
	if err != nil {
		return err
	}
	ctx := context.Background()
	ss, err := st.LockSigner(ctx, signerAddr)
	if err != nil {
		return err
	}
	draft := store.AttemptDraft{IntentID: spec.IntentID, AccountNumber: spec.AccountNumber, Sequence: spec.Sequence,
		TimeoutHeight: spec.TimeoutHeight, Gas: spec.Gas, TxBytes: tx}
	draft.TxHash = sumOf(tx)
	res, err := ss.PersistAttempt(ctx, draft)
	if err != nil {
		return err
	}
	if res.Refusal != store.RefusedNone {
		return fmt.Errorf("persist refused: %s", res.Refusal)
	}
	ok, err := ss.BeginSend(ctx, res.AttemptID)
	if err != nil || !ok {
		return fmt.Errorf("begin send: %v %v", ok, err)
	}
	return os.WriteFile(spec.Marker, []byte(res.AttemptID), 0o600)
}
