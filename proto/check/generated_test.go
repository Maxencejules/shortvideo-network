package check_test

import (
	"testing"

	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func TestGeneratedDigestAlgorithm(t *testing.T) {
	if typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 != 1 {
		t.Fatalf("SHA-256 enum = %d", typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256)
	}
	if typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED != 0 {
		t.Fatalf("unspecified enum = %d", typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED)
	}
}
