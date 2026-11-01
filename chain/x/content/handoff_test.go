package content

import (
	"bytes"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
)

// TestPackageIdentity: the lease handoff returns the immutable identity
// and copies; an absent (id, version) is NOT_FOUND.
func TestPackageIdentity(t *testing.T) {
	f := newFixture(t)
	f.mustRegister("reg-c1", "c1")
	id, code := f.keeper.Package(f.ctx, "c1", 1)
	if code != msgkit.OK {
		t.Fatalf("Package = %s", code)
	}
	if id.ContentID != "c1" || id.Version != 1 || id.PackageBytes != testPackageBytes ||
		!bytes.Equal(id.ManifestDigest, manifestDigest("c1")) || !bytes.Equal(id.SubmittingHost, serviceAddr) ||
		id.State != contentv1.ContentState_CONTENT_STATE_PENDING {
		t.Fatalf("identity = %+v", id)
	}
	id.ManifestDigest[0] ^= 0xff
	id.SubmittingHost[0] ^= 0xff
	again, _ := f.keeper.Package(f.ctx, "c1", 1)
	if !bytes.Equal(again.ManifestDigest, manifestDigest("c1")) || !bytes.Equal(again.SubmittingHost, serviceAddr) {
		t.Fatal("Package returned aliased slices")
	}
	for _, c := range []struct {
		id      string
		version uint64
	}{{"c1", 2}, {"c1", 0}, {"c-missing", 1}, {"C1", 1}, {"c1\x00", 1}, {"", 1}} {
		if _, code := f.keeper.Package(f.ctx, c.id, c.version); code != msgkit.NotFound {
			t.Fatalf("Package(%q, %d) = %s, want NOT_FOUND", c.id, c.version, code)
		}
	}
	f.mustWithdraw("wd-c1", "c1")
	id, code = f.keeper.Package(f.ctx, "c1", 1)
	if code != msgkit.OK || id.State != contentv1.ContentState_CONTENT_STATE_WITHDRAWN {
		t.Fatalf("withdrawn identity = %+v %s", id, code)
	}
}

// TestCheckOfferableOrder is the content part of op_offer in spec order:
// NOT_FOUND, WITHDRAWN, CONFLICT, INTEGRITY.
func TestCheckOfferableOrder(t *testing.T) {
	f := newFixture(t)
	f.mustRegister("reg-c1", "c1")
	f.mustRegister("reg-c2", "c2")
	f.mustWithdraw("wd-c2", "c2")
	good := manifestDigest("c1")
	other := manifestDigest("other")
	cases := []struct {
		name    string
		id      string
		version uint64
		digest  []byte
		bytes   uint64
		want    msgkit.Code
	}{
		{"offerable", "c1", 1, good, testPackageBytes, msgkit.OK},
		{"not-found", "c-missing", 1, good, testPackageBytes, msgkit.NotFound},
		{"not-found-id-pattern", "C1", 1, good, testPackageBytes, msgkit.NotFound},
		{"withdrawn", "c2", 1, manifestDigest("c2"), testPackageBytes, msgkit.Withdrawn},
		{"withdrawn-before-conflict", "c2", 2, other, 1, msgkit.Withdrawn},
		{"version-conflict", "c1", 2, good, testPackageBytes, msgkit.Conflict},
		{"conflict-before-integrity", "c1", 2, other, 1, msgkit.Conflict},
		{"digest-integrity", "c1", 1, other, testPackageBytes, msgkit.Integrity},
		{"short-digest-integrity", "c1", 1, good[:31], testPackageBytes, msgkit.Integrity},
		{"bytes-integrity", "c1", 1, good, testPackageBytes + 1, msgkit.Integrity},
	}
	before := f.snapshot()
	for _, c := range cases {
		if got := f.keeper.CheckOfferable(f.ctx, c.id, c.version, c.digest, c.bytes); got != c.want {
			t.Fatalf("%s: CheckOfferable = %s, want %s", c.name, got, c.want)
		}
	}
	requireSame(t, before, f.snapshot(), "CheckOfferable is read-only")
}

// TestWithdrawalBlocksOfferAndRevival is REQ-INV-006 at the API level:
// after a withdrawal the lease handoff refuses a new offer, accept, or
// renewal (WITHDRAWN), the hook ran once with (id, version) so goal 08 can
// void open offers, and the id cannot be registered again.
func TestWithdrawalBlocksOfferAndRevival(t *testing.T) {
	f := newFixture(t)
	f.mustRegister("reg-c1", "c1")
	if code := f.keeper.CheckOfferable(f.ctx, "c1", 1, manifestDigest("c1"), testPackageBytes); code != msgkit.OK {
		t.Fatalf("before withdrawal: %s", code)
	}
	if code := f.keeper.CheckNotWithdrawn(f.ctx, "c1"); code != msgkit.OK {
		t.Fatalf("before withdrawal: %s", code)
	}
	f.mustWithdraw("wd-c1", "c1")
	if code := f.keeper.CheckOfferable(f.ctx, "c1", 1, manifestDigest("c1"), testPackageBytes); code != msgkit.Withdrawn {
		t.Fatalf("offer after withdrawal = %s", code)
	}
	if code := f.keeper.CheckNotWithdrawn(f.ctx, "c1"); code != msgkit.Withdrawn {
		t.Fatalf("accept/renew after withdrawal = %s", code)
	}
	if len(f.hook.calls) != 1 || f.hook.calls[0] != (hookCall{id: "c1", version: 1}) {
		t.Fatalf("hook calls = %v", f.hook.calls)
	}
	_, err := f.register(f.registerMsg("reg-c1-revival", "c1"))
	requireCode(t, err, msgkit.DuplicateID)
	if code := f.keeper.CheckNotWithdrawn(f.ctx, "c-missing"); code != msgkit.NotFound {
		t.Fatalf("CheckNotWithdrawn on an absent id = %s", code)
	}
}
