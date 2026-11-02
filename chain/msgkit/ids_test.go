package msgkit

import (
	"bytes"
	"testing"

	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func TestCheckID(t *testing.T) {
	cases := []struct {
		id   string
		want Code
	}{
		{"", Malformed},
		{"a", OK},
		{"0", OK},
		{"provider-1", OK},
		{"p.1_x-y", OK},
		{repeat('a', 64), OK},
		{repeat('a', 65), Bounds},
		{"A", Bounds},
		{"-a", Bounds},
		{".a", Bounds},
		{"_a", Bounds},
		{"a b", Bounds},
		{"a/b", Bounds},
		{"a\x00", Bounds},
		{"a\n", Bounds},
		{"ü", Bounds},
	}
	for _, tc := range cases {
		if got := CheckID(tc.id); got != tc.want {
			t.Errorf("CheckID(%q) = %q, want %q", tc.id, got, tc.want)
		}
		if ValidID(tc.id) != (tc.want == OK) {
			t.Errorf("ValidID(%q) = %v", tc.id, ValidID(tc.id))
		}
	}
}

func TestCheckDigest(t *testing.T) {
	sha := typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256
	value := bytes.Repeat([]byte{0xab}, 32)
	cases := []struct {
		name string
		d    *typesv1.Digest
		want Code
	}{
		{"nil", nil, Malformed},
		{"algorithm 0", &typesv1.Digest{Value: value}, Malformed},
		{"algorithm 2 (unknown)", &typesv1.Digest{Algorithm: 2, Value: value}, Malformed},
		{"algorithm -1 (unknown)", &typesv1.Digest{Algorithm: -1, Value: value}, Malformed},
		{"algorithm 0 with a bad length is still MALFORMED", &typesv1.Digest{Value: value[:31]}, Malformed},
		{"empty value", &typesv1.Digest{Algorithm: sha}, Bounds},
		{"31 bytes", &typesv1.Digest{Algorithm: sha, Value: value[:31]}, Bounds},
		{"33 bytes", &typesv1.Digest{Algorithm: sha, Value: append(append([]byte{}, value...), 1)}, Bounds},
		{"1 KiB", &typesv1.Digest{Algorithm: sha, Value: make([]byte, 1024)}, Bounds},
		{"32 bytes", &typesv1.Digest{Algorithm: sha, Value: value}, OK},
	}
	for _, tc := range cases {
		got, code := CheckDigest(tc.d)
		if code != tc.want {
			t.Errorf("%s: code %q, want %q", tc.name, code, tc.want)
		}
		if code == OK {
			if !bytes.Equal(got, value) {
				t.Errorf("%s: value %x", tc.name, got)
			}
			got[0] ^= 0xff
			if tc.d.Value[0] != 0xab {
				t.Errorf("%s: returned value aliases the message", tc.name)
			}
		} else if got != nil {
			t.Errorf("%s: value returned with code %q", tc.name, code)
		}
	}
}
