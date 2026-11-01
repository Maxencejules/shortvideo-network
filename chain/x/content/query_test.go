package content

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
)

// TestQueryContent covers svn.content.v1.Query/Content on the keeper: the
// request checks, NOT_FOUND, a PENDING record without a tombstone, and a
// WITHDRAWN record with its tombstone.
func TestQueryContent(t *testing.T) {
	f := newFixture(t)
	f.mustRegister("reg-c1", "c1")
	f.mustRegister("reg-c2", "c2")
	f.mustWithdraw("wd-c2", "c2")
	for _, c := range []struct {
		name string
		req  *contentv1.QueryContentRequest
		want msgkit.Code
	}{
		{"nil-request", nil, msgkit.Malformed},
		{"empty-id", &contentv1.QueryContentRequest{ContentVersion: 1}, msgkit.Malformed},
		{"id-pattern", &contentv1.QueryContentRequest{ContentId: "C1", ContentVersion: 1}, msgkit.Bounds},
		{"id-65", &contentv1.QueryContentRequest{ContentId: strings.Repeat("c", 65), ContentVersion: 1}, msgkit.Bounds},
		{"version-0", &contentv1.QueryContentRequest{ContentId: "c1"}, msgkit.Malformed},
		{"absent-version", &contentv1.QueryContentRequest{ContentId: "c1", ContentVersion: 2}, msgkit.NotFound},
		{"absent-id", &contentv1.QueryContentRequest{ContentId: "c9", ContentVersion: 1}, msgkit.NotFound},
	} {
		_, err := f.keeper.Content(f.ctx, c.req)
		if got := codeFromErr(t, err); got != c.want {
			t.Fatalf("%s: code %s, want %s", c.name, got, c.want)
		}
	}
	pending, err := f.keeper.Content(f.ctx, &contentv1.QueryContentRequest{ContentId: "c1", ContentVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(pending.GetContent(), f.record("c1")) || pending.GetTombstone() != nil {
		t.Fatalf("pending response = %v", pending)
	}
	withdrawn, err := f.keeper.Content(f.ctx, &contentv1.QueryContentRequest{ContentId: "c2", ContentVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	tomb, _ := f.keeper.tombstone(f.ctx, "c2", 1)
	if withdrawn.GetContent().GetState() != contentv1.ContentState_CONTENT_STATE_WITHDRAWN || !proto.Equal(withdrawn.GetTombstone(), tomb) {
		t.Fatalf("withdrawn response = %v", withdrawn)
	}
}
