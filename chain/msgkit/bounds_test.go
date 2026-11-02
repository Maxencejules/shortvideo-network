package msgkit

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

type boundsFile struct {
	CanonicalProfileVersion int               `json:"canonical_profile_version"`
	DigestBytes             int               `json:"digest_bytes"`
	Ed25519PublicBytes      int               `json:"ed25519_public_bytes"`
	Ed25519SignatureBytes   int               `json:"ed25519_signature_bytes"`
	AccountAddressBytes     int               `json:"account_address_bytes"`
	IDMaxBytes              int               `json:"id_max_bytes"`
	IDPattern               string            `json:"id_pattern"`
	SafeIntegerMax          int64             `json:"safe_integer_max"`
	SafetyMaxPageLimit      uint64            `json:"safety_max_page_limit"`
	PageKeyMaxBytes         int               `json:"page_key_max_bytes"`
	SafetyMaxTxBytes        uint64            `json:"safety_max_tx_bytes"`
	SafetyMaxGasPerTx       uint64            `json:"safety_max_gas_per_tx"`
	SafetyMinGasPerTx       uint64            `json:"safety_min_gas_per_tx"`
	SafetyMaxPackageBytes   uint64            `json:"safety_max_package_bytes"`
	Domains                 map[string]string `json:"domains"`
	GovernedLimitCaps       map[string]uint64 `json:"governed_limit_caps"`
}

func loadBounds(t *testing.T) boundsFile {
	t.Helper()
	raw, err := os.ReadFile(msgkittest.BoundsFile(t))
	if err != nil {
		t.Fatal(err)
	}
	var b boundsFile
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	return b
}

// TestBoundsDrift fails when a constant msgkit pins differs from
// docs/protocol/bounds.json.
func TestBoundsDrift(t *testing.T) {
	b := loadBounds(t)

	if b.CanonicalProfileVersion != CanonicalProfileVersion {
		t.Errorf("canonical_profile_version %d, msgkit %d", b.CanonicalProfileVersion, CanonicalProfileVersion)
	}
	if b.DigestBytes != DigestBytes || b.Ed25519PublicBytes != PublicKeyBytes || b.Ed25519SignatureBytes != SignatureBytes {
		t.Errorf("sizes digest %d pub %d sig %d", b.DigestBytes, b.Ed25519PublicBytes, b.Ed25519SignatureBytes)
	}
	if b.AccountAddressBytes != AccountBytes {
		t.Errorf("account_address_bytes %d, msgkit %d", b.AccountAddressBytes, AccountBytes)
	}
	if b.IDMaxBytes != IDMaxBytes || b.IDPattern != IDPattern {
		t.Errorf("id %d %q, msgkit %d %q", b.IDMaxBytes, b.IDPattern, IDMaxBytes, IDPattern)
	}
	if b.SafeIntegerMax != safeIntegerMax {
		t.Errorf("safe_integer_max %d, msgkit %d", b.SafeIntegerMax, safeIntegerMax)
	}
	if b.PageKeyMaxBytes != PageKeyMaxBytes {
		t.Errorf("page_key_max_bytes %d, msgkit %d", b.PageKeyMaxBytes, PageKeyMaxBytes)
	}
	if b.SafetyMaxPageLimit != uint64(genesis.MaxPageLimit) {
		t.Errorf("safety_max_page_limit %d, genesis %d", b.SafetyMaxPageLimit, genesis.MaxPageLimit)
	}
	if b.SafetyMaxTxBytes != genesis.MaxTxBytes || b.SafetyMaxGasPerTx != genesis.MaxGasPerTx || b.SafetyMinGasPerTx != genesis.MinGasPerTx {
		t.Errorf("tx bounds %d %d %d", b.SafetyMaxTxBytes, b.SafetyMaxGasPerTx, b.SafetyMinGasPerTx)
	}

	// Domains: every enum value but UNSPECIFIED has exactly the bounds.json
	// string, and bounds.json names no other domain.
	values := typesv1.SignatureDomain(0).Descriptor().Values()
	if len(b.Domains) != values.Len()-1 {
		t.Errorf("bounds.json has %d domains, enum has %d", len(b.Domains), values.Len()-1)
	}
	for i := 0; i < values.Len(); i++ {
		value := values.Get(i)
		d := typesv1.SignatureDomain(value.Number())
		got, ok := DomainString(d)
		if value.Number() == 0 {
			if ok {
				t.Errorf("UNSPECIFIED has a domain string %q", got)
			}
			continue
		}
		want, inFile := b.Domains[string(value.Name())]
		if !inFile || !ok || got != want {
			t.Errorf("%s: msgkit %q (%v), bounds.json %q (%v)", value.Name(), got, ok, want, inFile)
		}
	}
	if s, ok := DomainString(typesv1.SignatureDomain(values.Len())); ok {
		t.Errorf("unknown domain number has string %q", s)
	}

	// Governed limits: same key set and ceilings.
	ours := map[string]uint64{}
	for _, key := range LimitKeys() {
		ceiling, ok := Ceiling(key)
		if !ok {
			t.Fatalf("LimitKeys lists %q without a ceiling", key)
		}
		ours[key] = ceiling
	}
	if !reflect.DeepEqual(ours, b.GovernedLimitCaps) {
		t.Errorf("governed caps %v, bounds.json %v", ours, b.GovernedLimitCaps)
	}
	if b.GovernedLimitCaps[LimitMaxPackageBytes] != b.SafetyMaxPackageBytes {
		t.Errorf("max_package_bytes cap %d, safety_max_package_bytes %d", b.GovernedLimitCaps[LimitMaxPackageBytes], b.SafetyMaxPackageBytes)
	}
}

// ValidID is a hand-written matcher. It must agree with the bounds.json
// pattern compiled as a regular expression.
func TestValidIDAgreesWithBoundsPattern(t *testing.T) {
	re := regexp.MustCompile(loadBounds(t).IDPattern)
	alphabet := []byte("az09._-A\x00/ \n\xff")
	var check func(prefix []byte, depth int)
	count := 0
	check = func(prefix []byte, depth int) {
		s := string(prefix)
		if ValidID(s) != re.MatchString(s) {
			t.Fatalf("ValidID(%q)=%v, pattern %v", s, ValidID(s), re.MatchString(s))
		}
		count++
		if depth == 3 {
			return
		}
		for _, c := range alphabet {
			check(append(append([]byte{}, prefix...), c), depth+1)
		}
	}
	check(nil, 0)
	if count < 2000 {
		t.Fatalf("only %d strings checked", count)
	}
	long := []string{
		string(make([]byte, 0)),
		repeat('a', 63), repeat('a', 64), repeat('a', 65),
		"a" + repeat('-', 63), "a" + repeat('-', 64),
		"abc\n", "\nabc", "ABC", "a_b.c-d", "-ab", ".ab", "_ab", "é",
	}
	for _, s := range long {
		if ValidID(s) != re.MatchString(s) {
			t.Fatalf("ValidID(%q)=%v, pattern %v", s, ValidID(s), re.MatchString(s))
		}
	}
}

func repeat(c byte, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = c
	}
	return string(out)
}
