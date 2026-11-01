package content

import (
	"encoding/binary"
	"fmt"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
)

// Store layout. Values are deterministic protobuf bytes, never JSON.
//
//	0x01 | content_id | 0x00 | content_version (8 bytes big endian) -> Content
//	0x02 | content_id | 0x00 | content_version (8 bytes big endian) -> Tombstone
//	0x03 | business_id                                             -> 32-byte business digest
//
// Ids match the protocol id pattern, so they never contain 0x00. The 0x00
// terminator stops "c1" from matching "c10" in a prefix scan. Records are
// keyed by (id, version) so a later decision that allows versions above 1
// needs no key migration; v0 writes version 1 only.
const (
	ContentPrefix   byte = 0x01
	TombstonePrefix byte = 0x02
	BusinessPrefix  byte = 0x03

	idTerminator byte = 0x00
	versionBytes      = 8
)

var marshalOptions = proto.MarshalOptions{Deterministic: true}

// idPrefix is prefix | id | 0x00. Callers pass only ids that match the id
// pattern; an id holding 0x00 could otherwise reach another id's records.
func idPrefix(prefix byte, id string) []byte {
	out := make([]byte, 0, 2+len(id)+versionBytes)
	out = append(out, prefix)
	out = append(out, id...)
	return append(out, idTerminator)
}

func recordKey(prefix byte, id string, version uint64) []byte {
	out := idPrefix(prefix, id)
	return binary.BigEndian.AppendUint64(out, version)
}

// mustDecode decodes one stored record. Stored bytes are written only by
// this package, so a decode failure means the store is corrupt. It panics:
// the SDK turns a handler panic into the same failed transaction on every
// node, and a query panic into an error.
func mustDecode(raw []byte, into proto.Message, key []byte) {
	if err := proto.Unmarshal(raw, into); err != nil {
		panic(fmt.Sprintf("content: corrupt record at key %x: %v", key, err))
	}
}

func mustEncode(m proto.Message) []byte {
	raw, err := marshalOptions.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("content: encode %s: %v", m.ProtoReflect().Descriptor().FullName(), err))
	}
	return raw
}

// content returns the record for (id, version). An id outside the pattern
// is absent without a store read.
func (k Keeper) content(ctx sdk.Context, id string, version uint64) (*contentv1.Content, bool) {
	if !msgkit.ValidID(id) {
		return nil, false
	}
	key := recordKey(ContentPrefix, id, version)
	raw := ctx.KVStore(k.storeKey).Get(key)
	if raw == nil {
		return nil, false
	}
	out := &contentv1.Content{}
	mustDecode(raw, out, key)
	return out, true
}

// contentByID returns the first record of id in version order. v0 holds at
// most one record per id: RegisterContent refuses any second record of an
// id and genesis refuses a repeated id. An id outside the pattern is absent
// without a store read. The scan stops at the first entry.
func (k Keeper) contentByID(ctx sdk.Context, id string) (*contentv1.Content, bool) {
	if !msgkit.ValidID(id) {
		return nil, false
	}
	prefix := idPrefix(ContentPrefix, id)
	it := ctx.KVStore(k.storeKey).Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	defer it.Close()
	// Iterator.Error is not consulted: inside a transaction or query the
	// store is a cachekv merge iterator, which reports an exhausted range as
	// an error ("invalid cacheMergeIterator").
	if !it.Valid() {
		return nil, false
	}
	out := &contentv1.Content{}
	mustDecode(it.Value(), out, it.Key())
	return out, true
}

func (k Keeper) tombstone(ctx sdk.Context, id string, version uint64) (*contentv1.Tombstone, bool) {
	if !msgkit.ValidID(id) {
		return nil, false
	}
	key := recordKey(TombstonePrefix, id, version)
	raw := ctx.KVStore(k.storeKey).Get(key)
	if raw == nil {
		return nil, false
	}
	out := &contentv1.Tombstone{}
	mustDecode(raw, out, key)
	return out, true
}

func (k Keeper) setContent(ctx sdk.Context, c *contentv1.Content) {
	key := recordKey(ContentPrefix, c.GetContentId(), c.GetContentVersion())
	ctx.KVStore(k.storeKey).Set(key, mustEncode(c))
}

func (k Keeper) setTombstone(ctx sdk.Context, t *contentv1.Tombstone) {
	key := recordKey(TombstonePrefix, t.GetContentId(), t.GetContentVersion())
	ctx.KVStore(k.storeKey).Set(key, mustEncode(t))
}

// allContents and allTombstones read one prefix in key order: content id
// bytes, then version. Export and invariant tests use them.
func (k Keeper) allContents(ctx sdk.Context) []*contentv1.Content {
	var out []*contentv1.Content
	k.scan(ctx, ContentPrefix, func(key, raw []byte) {
		c := &contentv1.Content{}
		mustDecode(raw, c, key)
		out = append(out, c)
	})
	return out
}

func (k Keeper) allTombstones(ctx sdk.Context) []*contentv1.Tombstone {
	var out []*contentv1.Tombstone
	k.scan(ctx, TombstonePrefix, func(key, raw []byte) {
		t := &contentv1.Tombstone{}
		mustDecode(raw, t, key)
		out = append(out, t)
	})
	return out
}

func (k Keeper) scan(ctx sdk.Context, prefix byte, visit func(key, raw []byte)) {
	start := []byte{prefix}
	it := ctx.KVStore(k.storeKey).Iterator(start, storetypes.PrefixEndBytes(start))
	defer it.Close()
	// Iterator.Error is not consulted after the loop: a cachekv merge
	// iterator reports an exhausted range as an error.
	for ; it.Valid(); it.Next() {
		visit(it.Key(), it.Value())
	}
}
