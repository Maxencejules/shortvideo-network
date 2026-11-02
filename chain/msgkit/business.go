package msgkit

import (
	"bytes"
	"fmt"
	"sort"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BusinessSource is one module's committed business-id ledger.
type BusinessSource interface {
	BusinessDigest(ctx sdk.Context, id string) ([]byte, bool)
}

// Peers joins the business-id ledgers of several modules into the single
// protocol namespace. Names are kept sorted, so lookups visit modules in
// the same order on every node.
//
// Peers is filled while the app is wired, before any block runs. It is not
// safe to call Add concurrently with Lookup.
type Peers struct {
	names   []string
	sources map[string]BusinessSource
}

// NewPeers returns an empty set.
func NewPeers() *Peers {
	return &Peers{sources: map[string]BusinessSource{}}
}

// Add registers one module ledger. An empty name, a nil source, or a name
// added twice is a wiring error and panics before the chain starts.
func (p *Peers) Add(name string, s BusinessSource) {
	if name == "" {
		panic("msgkit: peer name is empty")
	}
	if s == nil {
		panic(fmt.Sprintf("msgkit: peer %q has a nil source", name))
	}
	if _, exists := p.sources[name]; exists {
		panic(fmt.Sprintf("msgkit: peer %q added twice", name))
	}
	p.sources[name] = s
	p.names = append(p.names, name)
	sort.Strings(p.names)
}

// Names returns the registered module names in lookup order.
func (p *Peers) Names() []string {
	return append([]string(nil), p.names...)
}

// Lookup returns the digest committed for id by the first module, in name
// order, other than self.
func (p *Peers) Lookup(ctx sdk.Context, id, self string) ([]byte, bool) {
	if p == nil {
		return nil, false
	}
	for _, name := range p.names {
		if name == self {
			continue
		}
		if digest, ok := p.sources[name].BusinessDigest(ctx, id); ok {
			return digest, true
		}
	}
	return nil, false
}

// Ledger is one module's business-id store: key Prefix || id, value the
// 32-byte business digest. Self is this module's name in Peers.
type Ledger struct {
	Key    *storetypes.KVStoreKey
	Prefix byte
	Self   string
	Peers  *Peers
}

func (l Ledger) storeKey(id string) []byte {
	out := make([]byte, 0, 1+len(id))
	out = append(out, l.Prefix)
	return append(out, id...)
}

// Gate is the protocol business gate. It runs at the position the spec
// gives it in each message's check order:
//
//   - id empty: MALFORMED; outside the id pattern: BOUNDS
//   - digest not 32 bytes: MALFORMED
//   - own ledger first, then the peers in name order: absent proceeds;
//     the same digest is an idempotent success; any other digest is REPLAY.
//
// Gate never writes. Commit stores the id only after every other check of
// the message passed.
func (l Ledger) Gate(ctx sdk.Context, id string, digest []byte) Outcome {
	if code := CheckID(id); code != OK {
		return Outcome{Code: code}
	}
	if len(digest) != DigestBytes {
		return Outcome{Code: Malformed}
	}
	previous, found := l.BusinessDigest(ctx, id)
	if !found {
		previous, found = l.Peers.Lookup(ctx, id, l.Self)
	}
	if !found {
		return Outcome{}
	}
	if bytes.Equal(previous, digest) {
		return Outcome{Idempotent: true}
	}
	return Outcome{Code: Replay}
}

// Commit stores id -> digest in this module's ledger. The caller has
// already passed Gate with the same arguments; an invalid id or digest here
// is a programming error and panics (the SDK turns a handler panic into a
// failed transaction on every node alike).
func (l Ledger) Commit(ctx sdk.Context, id string, digest []byte) {
	if !ValidID(id) || len(digest) != DigestBytes {
		panic("msgkit: Ledger.Commit called without a passing Gate")
	}
	ctx.KVStore(l.Key).Set(l.storeKey(id), append([]byte(nil), digest...))
}

// BusinessDigest returns the digest this module committed for id. It does
// not consult peers.
func (l Ledger) BusinessDigest(ctx sdk.Context, id string) ([]byte, bool) {
	if !ValidID(id) {
		return nil, false
	}
	value := ctx.KVStore(l.Key).Get(l.storeKey(id))
	if value == nil {
		return nil, false
	}
	return append([]byte(nil), value...), true
}
