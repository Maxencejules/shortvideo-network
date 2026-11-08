package api

// Unit-test fakes: an in-memory outbox and a chain reader. They model the
// behaviour the API relies on (caller-scoped reads, the idempotency rule,
// the content owner uniqueness) and are never end-to-end evidence; the
// real store is tested against PostgreSQL in internal/store and the real
// chain client in internal/chain.

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// fakeStore is an in-memory stand-in for *store.Store.
type fakeStore struct {
	mu        sync.Mutex
	seq       int
	intents   map[string]store.Intent
	owners    map[string]store.ContentOwner // content id -> non-VOID owner row
	scheduled map[string]bool               // caller + "/" + offer id
	health    *store.ChainHealth
	healthErr error
	episode   *store.StallEpisode
	// enqueueErr, when set, is returned by EnqueueHTTP with nothing stored.
	enqueueErr error
	enqueued   []store.EnqueueRequest
	// missReplays makes the next IntentByIdempotencyKey calls answer
	// ErrNotFound, as a lookup that ran before a concurrent create with the
	// same key committed.
	missReplays int
}

func newFakeStore() *fakeStore {
	return &fakeStore{intents: map[string]store.Intent{}, owners: map[string]store.ContentOwner{}, scheduled: map[string]bool{}}
}

func (f *fakeStore) EnqueueHTTP(_ context.Context, r store.EnqueueRequest) (store.EnqueueResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.enqueueErr != nil {
		return store.EnqueueResult{}, f.enqueueErr
	}
	for _, it := range f.intents {
		if it.CallerPrincipal == r.CallerPrincipal && it.IdempotencyKey == r.IdempotencyKey {
			if it.Entry != "HTTP" || string(it.RequestDigest) != string(r.RequestDigest[:]) {
				return store.EnqueueResult{}, store.ErrIdempotencyConflict
			}
			return store.EnqueueResult{IntentID: it.ID, BusinessID: it.BusinessID}, nil
		}
	}
	if r.Kind == "REGISTER_CONTENT" {
		if _, held := f.owners[r.Object.ID]; held {
			return store.EnqueueResult{}, store.ErrContentUnavailable
		}
	}
	f.seq++
	id := fmt.Sprintf("00000000-0000-4000-8000-%012d", f.seq)
	authDigest := sha256.Sum256(r.AuthContext)
	obj := r.Object
	now := time.Date(2026, 11, 8, 12, 0, f.seq, 0, time.UTC)
	f.intents[id] = store.Intent{
		ID: id, CallerPrincipal: r.CallerPrincipal, IdempotencyKey: r.IdempotencyKey, Kind: r.Kind, Entry: "HTTP",
		State: store.StateRequested, Phase: store.PhaseQueued, BusinessID: fmt.Sprintf("bcastfake%d", f.seq),
		CreatedAt: now, ExpiresAt: now.Add(r.ExpiresIn), RequestDigest: append([]byte(nil), r.RequestDigest[:]...),
		AuthContextDigest: authDigest[:], Object: &obj, SignerAddress: r.SignerAddress,
	}
	if r.Kind == "REGISTER_CONTENT" {
		f.owners[r.Object.ID] = store.ContentOwner{
			ContentID: r.Object.ID, ContentVersion: r.Object.Version, CallerPrincipal: r.CallerPrincipal,
			SignerAddress: r.SignerAddress, IntentID: id, Status: store.OwnerPending,
		}
	}
	f.enqueued = append(f.enqueued, r)
	return store.EnqueueResult{IntentID: id, Created: true, BusinessID: f.intents[id].BusinessID}, nil
}

func (f *fakeStore) GetIntent(_ context.Context, caller, id string) (store.Intent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.intents[id]
	if !ok || it.CallerPrincipal != caller {
		return store.Intent{}, store.ErrNotFound
	}
	return it, nil
}

func (f *fakeStore) ListIntents(_ context.Context, caller string, flt store.IntentFilter) ([]store.Intent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []store.Intent
	for _, it := range f.intents {
		if it.CallerPrincipal != caller {
			continue
		}
		if len(flt.States) > 0 {
			match := false
			for _, s := range flt.States {
				match = match || s == it.State
			}
			if !match {
				continue
			}
		}
		all = append(all, it)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	if flt.After != "" {
		if a, ok := f.intents[flt.After]; !ok || a.CallerPrincipal != caller {
			return nil, store.ErrNotFound
		}
		for len(all) > 0 && all[0].ID <= flt.After {
			all = all[1:]
		}
	}
	if len(all) > flt.Limit {
		all = all[:flt.Limit]
	}
	return all, nil
}

func (f *fakeStore) Cancel(_ context.Context, caller, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	it, ok := f.intents[id]
	if !ok || it.CallerPrincipal != caller {
		return store.ErrNotFound
	}
	if it.State != store.StateRequested || it.AttemptCount != 0 {
		return store.ErrStateChanged
	}
	it.State, it.Phase, it.Reason = store.StateFailed, store.PhaseNone, "CANCELLED"
	f.intents[id] = it
	return nil
}

func (f *fakeStore) ContentOwnerFor(_ context.Context, caller, contentID string) (store.ContentOwner, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.owners[contentID]
	if !ok || o.CallerPrincipal != caller {
		return store.ContentOwner{}, store.ErrNotFound
	}
	return o, nil
}

func (f *fakeStore) CallerScheduledOffer(_ context.Context, caller, offerID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scheduled[caller+"/"+offerID], nil
}

func (f *fakeStore) ReadChainHealth(context.Context) (store.ChainHealth, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.healthErr != nil {
		return store.ChainHealth{}, f.healthErr
	}
	if f.health == nil {
		return store.ChainHealth{}, store.ErrNotFound
	}
	return *f.health, nil
}

func (f *fakeStore) OpenStallEpisode(context.Context) (store.StallEpisode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.episode == nil {
		return store.StallEpisode{}, store.ErrNotFound
	}
	return *f.episode, nil
}

func (f *fakeStore) IntentByIdempotencyKey(_ context.Context, caller, key string) (store.Intent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.missReplays > 0 {
		f.missReplays--
		return store.Intent{}, store.ErrNotFound
	}
	for _, it := range f.intents {
		if it.CallerPrincipal == caller && it.IdempotencyKey == key {
			return it, nil
		}
	}
	return store.Intent{}, store.ErrNotFound
}

// put stores an intent built by a test (a state the worker would reach).
func (f *fakeStore) put(it store.Intent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.intents[it.ID] = it
}

func (f *fakeStore) enqueueCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.enqueued)
}

// fakeChain is an in-memory stand-in for *chain.Client.
type fakeChain struct {
	mu        sync.Mutex
	status    chain.Status
	statusErr error
	content   map[string]*contentv1.QueryContentResponse // id/version
	providers map[string]*providersv1.Provider
	leases    map[string]*leasesv1.Lease // provider/content
	offers    []*leasesv1.Offer
	// submitters is the Query/Governance submitter list; account is the
	// committed account every Account read returns.
	submitters []*typesv1.Submitter
	account    chain.Account
	reads      int
}

func newFakeChain(chainID string) *fakeChain {
	return &fakeChain{
		status:    chain.Status{ChainID: chainID, Height: 100, Time: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)},
		content:   map[string]*contentv1.QueryContentResponse{},
		providers: map[string]*providersv1.Provider{},
		leases:    map[string]*leasesv1.Lease{},
	}
}

func (c *fakeChain) Status(context.Context) (chain.Status, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	return c.status, c.statusErr
}

func (c *fakeChain) Content(_ context.Context, id string, version uint64, _ int64) (*contentv1.QueryContentResponse, bool, int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	r, ok := c.content[fmt.Sprintf("%s/%d", id, version)]
	return r, ok, c.status.Height, nil
}

func (c *fakeChain) Provider(_ context.Context, id string, _ int64) (*providersv1.Provider, bool, int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	p, ok := c.providers[id]
	return p, ok, c.status.Height, nil
}

func (c *fakeChain) HeldLease(_ context.Context, providerID, contentID string, _ int64) (*leasesv1.Lease, bool, int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	l, ok := c.leases[providerID+"/"+contentID]
	return l, ok, c.status.Height, nil
}

func (c *fakeChain) Deadlines(context.Context, int64, []byte, uint32, int64) (*leasesv1.QueryDeadlinesResponse, int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	return &leasesv1.QueryDeadlinesResponse{Offers: c.offers}, c.status.Height, nil
}

func (c *fakeChain) Account(context.Context, []byte, int64) (chain.Account, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	if c.account.Height == 0 {
		return chain.Account{}, errors.New("fake chain: no account")
	}
	return c.account, nil
}

// Governance answers one page with the submitters.
func (c *fakeChain) Governance(context.Context, []byte, uint32, int64) (*authorityv1.QueryGovernanceResponse, int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reads++
	return &authorityv1.QueryGovernanceResponse{State: &authorityv1.AuthorityState{Submitters: c.submitters}}, c.status.Height, nil
}

func (c *fakeChain) readCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads
}
