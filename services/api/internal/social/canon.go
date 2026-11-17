package social

import (
	"fmt"
	"strconv"
	"strings"
)

// Canonical formats one follow or unfollow envelope. The key order matches
// the Goal 02 public-record fixture. This is not a general canonicalizer.
func Canonical(author, recordID, keyID, previous, recordType, body string, sequence int64) (string, error) {
	if !idRE.MatchString(author) || !idRE.MatchString(recordID) || !idRE.MatchString(keyID) || !idRE.MatchString(body) {
		return "", errMalformed
	}
	if recordType != "follow" && recordType != "unfollow" {
		return "", errMalformed
	}
	if len(previous) != 64 || strings.Trim(previous, "0123456789abcdef") != "" {
		return "", errMalformed
	}
	if sequence < 0 {
		return "", errMalformed
	}
	return fmt.Sprintf(
		`{"author_id":"%s","key_id":"%s","payload":{"body":"%s","record_type":"%s"},"previous_digest":"%s","privacy_class":"PUBLIC_RECORD","profile":"SVN-JCS-1","record_id":"%s","schema_version":1,"sequence":%s}`,
		author, keyID, body, recordType, previous, recordID, strconv.FormatInt(sequence, 10),
	), nil
}
