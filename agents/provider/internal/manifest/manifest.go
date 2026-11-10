// Package manifest builds the off-chain media manifest whose digest an
// offer binds. The schema is docs/protocol/schemas/media-manifest.schema.json.
package manifest

import (
	"crypto/sha256"
	"fmt"

	"github.com/Maxencejules/shortvideo-network/chain/jcs"
)

// Segment is one file in the package.
type Segment struct {
	ID        string
	SHA256    string
	ByteCount uint64
}

// Canonical returns the SVN-JCS-1 manifest text. packageBytes is the sum of
// the segment lengths the caller claims; this function does not add them.
func Canonical(manifestID, contentID string, version, packageBytes uint64, segs []Segment) (string, error) {
	if manifestID == "" || contentID == "" || version == 0 || version > 9007199254740991 || packageBytes > 9007199254740991 || len(segs) == 0 {
		return "", fmt.Errorf("manifest: missing field")
	}
	items := make([]any, 0, len(segs))
	for _, seg := range segs {
		if seg.ID == "" || len(seg.SHA256) != 64 {
			return "", fmt.Errorf("manifest: bad segment")
		}
		items = append(items, map[string]any{
			"segment_id": seg.ID,
			"sha256":     seg.SHA256,
			"byte_count": fmt.Sprintf("%d", seg.ByteCount),
		})
	}
	text, err := jcs.Canonicalize(map[string]any{
		"schema_version":  int64(1),
		"profile":         "SVN-JCS-1",
		"manifest_id":     manifestID,
		"content_id":      contentID,
		"content_version": int64(version),
		"package_bytes":   fmt.Sprintf("%d", packageBytes),
		"renditions": []any{map[string]any{
			"rendition_id": "r0",
			"codec":        "raw",
			"segments":     items,
		}},
	})
	if err != nil {
		return "", err
	}
	return text, nil
}

// Sum is SHA-256 over the canonical text.
func Sum(text string) [32]byte {
	return sha256.Sum256([]byte(text))
}
