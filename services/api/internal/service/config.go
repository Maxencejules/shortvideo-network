package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// Decimal megabytes, as the blueprint's planning unit. 100 MiB is a different number.
const (
	LimitsLabel                      = "PROPOSED"
	ProposedMaxInputBytes      int64 = 100_000_000
	ProposedMaxDurationSeconds int64 = 60
	ProposedMaxDimension             = 7680
)

// Limits are proposed beta caps. Validate refuses a value above the proposed ceiling
// and refuses a label other than PROPOSED, so a config file cannot mark the numbers approved.
type Limits struct {
	Label                      string `json:"label"`
	MaxInputBytes              int64  `json:"max_input_bytes"`
	MaxDurationSeconds         int64  `json:"max_duration_seconds"`
	MaxDimension               int    `json:"max_dimension"`
	AccountInputAllowanceBytes int64  `json:"account_input_allowance_bytes"`
	AccountOutputBudgetBytes   int64  `json:"account_output_budget_bytes"`
	OutputReservationBytes     int64  `json:"output_reservation_bytes"`
	TransferTTLSeconds         int64  `json:"transfer_ttl_seconds"`
	IntentRatePerMinute        int    `json:"intent_rate_per_minute"`
	ProbeTimeoutSeconds        int64  `json:"probe_timeout_seconds"`
}

// Fixture is a development principal. The token is not a credential for production.
type Fixture struct {
	Token     string `json:"token"`
	AccountID string `json:"account_id"`
	Role      string `json:"role"`
}

// WebAuthnConfig is the relying party. Origins are exact. http://localhost is
// the development relying-party origin. Any other origin must be https.
type WebAuthnConfig struct {
	RPID        string   `json:"rp_id"`
	DisplayName string   `json:"rp_display_name"`
	Origins     []string `json:"origins"`
}

// File is the on-disk configuration. Database credentials, the bootstrap
// invite, and the application export key are not in this file.
type File struct {
	Environment       string         `json:"environment"`
	EnableDevFixtures bool           `json:"enable_dev_fixtures"`
	PublicLaunch      bool           `json:"public_launch"`
	Listen            string         `json:"listen"`
	QuarantineDir     string         `json:"quarantine_dir"`
	Fixtures          []Fixture      `json:"fixtures"`
	Limits            Limits         `json:"limits"`
	WebAuthn          WebAuthnConfig `json:"webauthn"`
	EnrollPerMinute   int            `json:"enroll_per_minute"`
	LoginPerMinute    int            `json:"login_per_minute"`
	SessionSeconds    int            `json:"session_seconds"`
}

// LoadFile reads a configuration file. It does not open a database or a socket.
func LoadFile(path string) (File, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var file File
	if err := dec.Decode(&file); err != nil {
		return File{}, errors.New("config file is not valid")
	}
	if dec.More() {
		return File{}, errors.New("config file is not valid")
	}
	if err := Validate(file.Limits); err != nil {
		return File{}, err
	}
	return file, nil
}

// Validate checks proposed ceilings. It does not approve the numbers.
func Validate(l Limits) error {
	if l.Label != LimitsLabel {
		return errors.New("limits label must be PROPOSED")
	}
	if l.MaxInputBytes < 1 || l.MaxInputBytes > ProposedMaxInputBytes {
		return fmt.Errorf("max input bytes must be 1..%d", ProposedMaxInputBytes)
	}
	if l.MaxDurationSeconds < 1 || l.MaxDurationSeconds > ProposedMaxDurationSeconds {
		return fmt.Errorf("max duration seconds must be 1..%d", ProposedMaxDurationSeconds)
	}
	if l.MaxDimension < 1 || l.MaxDimension > ProposedMaxDimension {
		return fmt.Errorf("max dimension must be 1..%d", ProposedMaxDimension)
	}
	if l.AccountInputAllowanceBytes < 0 || l.AccountOutputBudgetBytes < 0 {
		return errors.New("account allowances must be zero or positive")
	}
	if l.OutputReservationBytes < 1 || l.OutputReservationBytes > l.AccountOutputBudgetBytes {
		return errors.New("output reservation must fit in the account output budget")
	}
	if l.TransferTTLSeconds < 1 || l.TransferTTLSeconds > int64((24*time.Hour).Seconds()) {
		return errors.New("transfer ttl must be 1 second through 24 hours")
	}
	if l.IntentRatePerMinute < 1 || l.IntentRatePerMinute > 1000 {
		return errors.New("intent rate must be 1..1000 per minute")
	}
	if l.ProbeTimeoutSeconds < 1 || l.ProbeTimeoutSeconds > 30 {
		return errors.New("probe timeout must be 1..30 seconds")
	}
	return nil
}
