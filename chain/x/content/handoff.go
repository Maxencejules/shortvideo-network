package content

import (
	"bytes"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
)

// Handoff API for the lease module (goal 08). These reads never write.

// PackageIdentity is the immutable identity of one registered package:
// everything a lease commits to. ManifestDigest is the 32-byte SHA-256
// value of the off-chain manifest; SubmittingHost is the 20-byte account
// that registered it. Only State changes after registration, and only
// from PENDING to WITHDRAWN.
type PackageIdentity struct {
	ContentID      string
	Version        uint64
	ManifestDigest []byte
	PackageBytes   uint64
	SubmittingHost []byte
	State          contentv1.ContentState
}

func identityOf(c *contentv1.Content) PackageIdentity {
	return PackageIdentity{
		ContentID:      c.GetContentId(),
		Version:        c.GetContentVersion(),
		ManifestDigest: append([]byte(nil), c.GetManifestDigest().GetValue()...),
		PackageBytes:   c.GetPackageBytes(),
		SubmittingHost: append([]byte(nil), c.GetSubmittingHost()...),
		State:          c.GetState(),
	}
}

// Package returns the identity of (contentID, version). An absent pair,
// including any id outside the id pattern, is NOT_FOUND. The returned
// slices are copies.
func (k Keeper) Package(ctx sdk.Context, contentID string, version uint64) (PackageIdentity, msgkit.Code) {
	record, found := k.content(ctx, contentID, version)
	if !found {
		return PackageIdentity{}, msgkit.NotFound
	}
	return identityOf(record), msgkit.OK
}

// CheckOfferable is the content part of op_offer, in spec order:
//
//  1. no record of contentID: NOT_FOUND
//  2. the record is WITHDRAWN: WITHDRAWN (a tombstone blocks new offers)
//  3. the record is not PENDING or version is not its version: CONFLICT
//  4. manifestSHA256 or packageBytes differs from the record: INTEGRITY
//
// The offer handler validates the offer's digest shape before it calls
// this; a digest of the wrong size simply does not match (INTEGRITY).
func (k Keeper) CheckOfferable(ctx sdk.Context, contentID string, version uint64, manifestSHA256 []byte, packageBytes uint64) msgkit.Code {
	record, found := k.contentByID(ctx, contentID)
	if !found {
		return msgkit.NotFound
	}
	if record.GetState() == contentv1.ContentState_CONTENT_STATE_WITHDRAWN {
		return msgkit.Withdrawn
	}
	if record.GetState() != contentv1.ContentState_CONTENT_STATE_PENDING || record.GetContentVersion() != version {
		return msgkit.Conflict
	}
	if !bytes.Equal(record.GetManifestDigest().GetValue(), manifestSHA256) || record.GetPackageBytes() != packageBytes {
		return msgkit.Integrity
	}
	return msgkit.OK
}

// CheckNotWithdrawn is the content check of op_accept and op_renew: an
// absent id is NOT_FOUND and a WITHDRAWN record is WITHDRAWN.
func (k Keeper) CheckNotWithdrawn(ctx sdk.Context, contentID string) msgkit.Code {
	record, found := k.contentByID(ctx, contentID)
	if !found {
		return msgkit.NotFound
	}
	if record.GetState() == contentv1.ContentState_CONTENT_STATE_WITHDRAWN {
		return msgkit.Withdrawn
	}
	return msgkit.OK
}
