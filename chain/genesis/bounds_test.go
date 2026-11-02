package genesis

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	cmttypes "github.com/cometbft/cometbft/types"
)

// protocolBounds is the part of docs/protocol/bounds.json that bounds.go
// mirrors. Pointer fields tell a missing key from a zero value.
// uint64_max is a string in the file and has no constant here.
type protocolBounds struct {
	AccountAddressBytes            *int64   `json:"account_address_bytes"`
	ConsensusPubkeyBytes           *int64   `json:"consensus_pubkey_bytes"`
	DigestBytes                    *int64   `json:"digest_bytes"`
	IDMaxBytes                     *int     `json:"id_max_bytes"`
	IDPattern                      *string  `json:"id_pattern"`
	UnixSecondsMin                 *int64   `json:"unix_seconds_min"`
	UnixSecondsMax                 *int64   `json:"unix_seconds_max"`
	SafetyMaxTxBytes               *uint64  `json:"safety_max_tx_bytes"`
	SafetyMaxGasPerTx              *uint64  `json:"safety_max_gas_per_tx"`
	SafetyMinGasPerTx              *uint64  `json:"safety_min_gas_per_tx"`
	SafetyMaxPackageBytes          *uint64  `json:"safety_max_package_bytes"`
	SafetyMaxEvidenceBatch         *uint32  `json:"safety_max_evidence_batch"`
	SafetyMaxExpiryStepsPerBlock   *uint32  `json:"safety_max_expiry_steps_per_block"`
	SafetyMaxPageLimit             *uint32  `json:"safety_max_page_limit"`
	SafetyMaxOpenOffersPerProvider *uint32  `json:"safety_max_open_offers_per_provider"`
	SafeIntegerMax                 *int64   `json:"safe_integer_max"`
	DevnetChainIDPrefix            *string  `json:"devnet_chain_id_prefix"`
	DevnetTestLabel                *string  `json:"devnet_test_label"`
	Decisions                      []string `json:"decisions"`
}

// readBoundsFile reads docs/protocol/bounds.json relative to this package.
func readBoundsFile(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "protocol", "bounds.json"))
	if err != nil {
		t.Fatalf("read bounds.json: %v", err)
	}
	return raw
}

func requireBound[T comparable](t *testing.T, key string, file *T, code T) {
	t.Helper()
	if file == nil {
		t.Errorf("bounds.json has no %s", key)
		return
	}
	if *file != code {
		t.Errorf("%s: bounds.json %v, bounds.go %v", key, *file, code)
	}
}

// TestBoundsMatchProtocolFile fails when a constant in bounds.go drifts from
// docs/protocol/bounds.json (review extra finding X1).
func TestBoundsMatchProtocolFile(t *testing.T) {
	raw := readBoundsFile(t)
	var file protocolBounds
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("decode bounds.json: %v", err)
	}

	requireBound(t, "account_address_bytes", file.AccountAddressBytes, int64(AccountBytes))
	requireBound(t, "consensus_pubkey_bytes", file.ConsensusPubkeyBytes, int64(ConsensusKeyBytes))
	requireBound(t, "digest_bytes", file.DigestBytes, int64(DigestBytes))
	requireBound(t, "id_pattern", file.IDPattern, IDPattern)
	requireBound(t, "unix_seconds_min", file.UnixSecondsMin, int64(UnixSecondsMin))
	requireBound(t, "unix_seconds_max", file.UnixSecondsMax, int64(UnixSecondsMax))
	requireBound(t, "safety_max_tx_bytes", file.SafetyMaxTxBytes, MaxTxBytes)
	requireBound(t, "safety_max_gas_per_tx", file.SafetyMaxGasPerTx, MaxGasPerTx)
	requireBound(t, "safety_min_gas_per_tx", file.SafetyMinGasPerTx, MinGasPerTx)
	requireBound(t, "safety_max_package_bytes", file.SafetyMaxPackageBytes, MaxPackageBytes)
	requireBound(t, "safety_max_evidence_batch", file.SafetyMaxEvidenceBatch, MaxEvidenceBatch)
	requireBound(t, "safety_max_expiry_steps_per_block", file.SafetyMaxExpiryStepsPerBlock, MaxExpiryStepsPerBlock)
	requireBound(t, "safety_max_page_limit", file.SafetyMaxPageLimit, MaxPageLimit)
	requireBound(t, "safety_max_open_offers_per_provider", file.SafetyMaxOpenOffersPerProvider, MaxOpenOffersPerProvider)
	requireBound(t, "safe_integer_max", file.SafeIntegerMax, SafeIntegerMax)
	requireBound(t, "devnet_chain_id_prefix", file.DevnetChainIDPrefix, DevnetChainIDPrefix)
	requireBound(t, "devnet_test_label", file.DevnetTestLabel, DevnetTestLabel)
	if !slices.Equal(file.Decisions, DecisionIDs) {
		t.Errorf("decisions: bounds.json %v, bounds.go %v", file.Decisions, DecisionIDs)
	}

	// DefaultSafetyCaps is what validateCaps requires of every document. Its
	// JSON names are the bounds.json keys, so decode the file into it too.
	var caps SafetyCaps
	if err := json.Unmarshal(raw, &caps); err != nil {
		t.Fatalf("decode safety caps: %v", err)
	}
	if caps != DefaultSafetyCaps() {
		t.Errorf("safety caps: bounds.json %+v, DefaultSafetyCaps %+v", caps, DefaultSafetyCaps())
	}

	// id_max_bytes is not a constant here; IDPattern carries it.
	if file.IDMaxBytes == nil {
		t.Fatal("bounds.json has no id_max_bytes")
	}
	longest := "a" + strings.Repeat("b", *file.IDMaxBytes-1)
	if !idPattern.MatchString(longest) || idPattern.MatchString(longest+"b") {
		t.Errorf("IDPattern does not cap ids at id_max_bytes %d", *file.IDMaxBytes)
	}

	// MaxChainIDLen has no bounds.json key. It is the CometBFT limit.
	if MaxChainIDLen != cmttypes.MaxChainIDLen {
		t.Errorf("MaxChainIDLen %d, CometBFT %d", MaxChainIDLen, cmttypes.MaxChainIDLen)
	}
}
