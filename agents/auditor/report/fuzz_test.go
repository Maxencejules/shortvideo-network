package report

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"testing"
)

// reportSeeds is the seed corpus of FuzzReportParse: the two golden reports
// (sampled FAIL, whole-package PASS), each encoded report, and edits that
// Parse refuses at each stage (size, SVN-JCS-1, canonical form, object
// shape, field rules, re-encoding).
func reportSeeds(t testing.TB) [][]byte {
	t.Helper()
	var out [][]byte
	for _, file := range []string{"../testdata/report/sampled-fail.json", "../testdata/report/whole-pass.json"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, raw, bytes.TrimSuffix(raw, []byte("\n")))
	}
	good, err := Encode(sampledReport())
	if err != nil {
		t.Fatal(err)
	}
	whole, err := Encode(wholeReport())
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, good, whole)
	for _, edit := range [][2]string{
		{`"bytes_read":"65536"`, `"bytes_read":65536`},
		{`"claim_limit":"ATTESTATION_NOT_STORAGE_PROOF",`, ``},
		{`"auditor_key_id"`, `"auditor_extra":"x","auditor_key_id"`},
		{`"mode":"SAMPLED_SEGMENTS"`, `"mode":null`},
		{`"result":"FAIL"`, `"result":"PASS"`},
	} {
		if !bytes.Contains(good, []byte(edit[0])) {
			t.Fatalf("encoded report lacks %q", edit[0])
		}
		out = append(out, bytes.Replace(good, []byte(edit[0]), []byte(edit[1]), 1))
	}
	return append(out, append([]byte(" "), good...), bytes.ToUpper(good), []byte(""), []byte("[]"), []byte("{}"),
		bytes.Repeat([]byte(" "), MaxBytes+1))
}

// TestReportSeeds: the seed corpus reaches accepted reports of both modes
// and refusals; every refusal wraps ErrInvalid.
func TestReportSeeds(t *testing.T) {
	modes := map[string]bool{}
	refused := 0
	for _, raw := range reportSeeds(t) {
		r, err := Parse(raw)
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("%.80q: %v", raw, err)
			}
			refused++
			continue
		}
		modes[r.Mode] = true
	}
	if len(modes) != 2 || refused < 10 {
		t.Fatalf("seeds reach modes %v and %d refusals", modes, refused)
	}
}

// FuzzReportParse: Parse never panics and refuses only with ErrInvalid; an
// accepted report encodes back to exactly the input bytes, and parsing those
// bytes gives the same report.
func FuzzReportParse(f *testing.F) {
	for _, raw := range reportSeeds(f) {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		r, err := Parse(raw)
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("%q: %v", raw, err)
			}
			return
		}
		encoded, err := Encode(r)
		if err != nil || !bytes.Equal(encoded, raw) {
			t.Fatalf("%q: encodes to %q, %v", raw, encoded, err)
		}
		again, err := Parse(encoded)
		if err != nil || !reflect.DeepEqual(again, r) {
			t.Fatalf("%q: parses again to %+v, %v", raw, again, err)
		}
	})
}
