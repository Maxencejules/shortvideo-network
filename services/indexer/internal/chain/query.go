package chain

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const (
	pathProviders  = "/svn.providers.v1.Query/Providers"
	pathContent    = "/svn.content.v1.Query/Content"
	pathActive     = "/svn.leases.v1.Query/ActiveLeases"
	pathDeadlines  = "/svn.leases.v1.Query/Deadlines"
	pathPolicy     = "/svn.policy.v1.Query/Policy"
	maxPages       = 64
	queryPageLimit = 100
)

// ProviderView is the reconciliation view of one provider.
type ProviderView struct {
	ID            string
	State         string
	KeyGeneration uint64
}

// ContentView is the reconciliation view of one content version.
type ContentView struct {
	ID            string
	Version       uint64
	Manifest      string
	PackageBytes  uint64
	Withdrawn     bool
	HistoryErased bool
}

// LeaseView is one held lease from ActiveLeases.
type LeaseView struct {
	ID         string
	ProviderID string
	ContentID  string
	Version    uint64
	State      string
	Term       uint64
	Deadline   int64
}

// DeadlineView is one row from Deadlines.
type DeadlineView struct {
	Kind string
	ID   string
	At   int64
}

// Providers reads every provider page at height. An error is not an empty list.
func (c *Client) Providers(ctx context.Context, height int64) ([]ProviderView, int64, error) {
	var out []ProviderView
	var key []byte
	var read int64
	for page := 0; page < maxPages; page++ {
		resp := &providersv1.QueryProvidersResponse{}
		got, err := c.query(ctx, pathProviders, &providersv1.QueryProvidersRequest{
			Page: &typesv1.PageRequest{Limit: queryPageLimit, PageKey: key},
		}, resp, height)
		if err != nil {
			return nil, 0, err
		}
		read = got
		for _, p := range resp.GetProviders() {
			out = append(out, ProviderView{
				ID: p.GetProviderId(), State: trimPrefix(p.GetState().String(), "PROVIDER_STATE_"),
				KeyGeneration: p.GetKeyGeneration(),
			})
		}
		key = resp.GetPage().GetNextPageKey()
		if len(key) == 0 {
			return out, read, nil
		}
	}
	return nil, 0, fmt.Errorf("%w: provider pages exceeded %d", ErrQuery, maxPages)
}

// Content reads one content version at height. found is false only for a
// not-found answer. Any other failure is an error, not an absent record.
func (c *Client) Content(ctx context.Context, id string, version uint64, height int64) (ContentView, bool, int64, error) {
	resp := &contentv1.QueryContentResponse{}
	got, found, err := c.queryFound(ctx, pathContent, &contentv1.QueryContentRequest{ContentId: id, ContentVersion: version}, resp, height)
	if err != nil || !found {
		return ContentView{}, false, got, err
	}
	if tomb := resp.GetTombstone(); tomb != nil {
		return ContentView{
			ID: id, Version: version, Withdrawn: true, HistoryErased: tomb.GetHistoryErased(),
		}, true, got, nil
	}
	rec := resp.GetContent()
	if rec == nil {
		return ContentView{}, false, got, fmt.Errorf("%w: content answer without a record", ErrQuery)
	}
	manifest := ""
	if d := rec.GetManifestDigest(); d != nil {
		manifest = hex.EncodeToString(d.GetValue())
	}
	return ContentView{
		ID: rec.GetContentId(), Version: rec.GetContentVersion(),
		Manifest: manifest, PackageBytes: rec.GetPackageBytes(),
	}, true, got, nil
}

// ActiveLeases reads the held leases of one provider at height.
func (c *Client) ActiveLeases(ctx context.Context, providerID string, height int64) ([]LeaseView, int64, error) {
	if !msgkit.ValidID(providerID) {
		return nil, 0, fmt.Errorf("%w: provider id", ErrQuery)
	}
	var out []LeaseView
	var key []byte
	var read int64
	for page := 0; page < maxPages; page++ {
		resp := &leasesv1.QueryActiveLeasesResponse{}
		got, err := c.query(ctx, pathActive, &leasesv1.QueryActiveLeasesRequest{
			ProviderId: providerID, Page: &typesv1.PageRequest{Limit: queryPageLimit, PageKey: key},
		}, resp, height)
		if err != nil {
			return nil, 0, err
		}
		read = got
		for _, lease := range resp.GetLeases() {
			out = append(out, LeaseView{
				ID: lease.GetLeaseId(), ProviderID: lease.GetProviderId(), ContentID: lease.GetContentId(),
				Version: lease.GetContentVersion(), State: trimPrefix(lease.GetState().String(), "LEASE_STATE_"),
				Term: lease.GetTermVersion(), Deadline: lease.GetDeadlineUnixSeconds(),
			})
		}
		key = resp.GetPage().GetNextPageKey()
		if len(key) == 0 {
			return out, read, nil
		}
	}
	return nil, 0, fmt.Errorf("%w: lease pages exceeded %d", ErrQuery, maxPages)
}

// Deadlines reads every deadline page at height whose time is at or before
// the protocol maximum. An error is not an empty deadline list.
func (c *Client) Deadlines(ctx context.Context, height int64) ([]DeadlineView, int64, error) {
	var out []DeadlineView
	var key []byte
	var read int64
	for page := 0; page < maxPages; page++ {
		resp := &leasesv1.QueryDeadlinesResponse{}
		got, err := c.query(ctx, pathDeadlines, &leasesv1.QueryDeadlinesRequest{
			NotAfterUnixSeconds: genesis.UnixSecondsMax,
			Page:                &typesv1.PageRequest{Limit: queryPageLimit, PageKey: key},
		}, resp, height)
		if err != nil {
			return nil, 0, err
		}
		read = got
		for _, lease := range resp.GetLeases() {
			out = append(out, DeadlineView{Kind: "lease", ID: lease.GetLeaseId(), At: lease.GetDeadlineUnixSeconds()})
		}
		for _, offer := range resp.GetOffers() {
			out = append(out, DeadlineView{Kind: "offer", ID: offer.GetOfferId(), At: offer.GetExpiryUnixSeconds()})
		}
		key = resp.GetPage().GetNextPageKey()
		if len(key) == 0 {
			return out, read, nil
		}
	}
	return nil, 0, fmt.Errorf("%w: deadline pages exceeded %d", ErrQuery, maxPages)
}

// PolicyVersion reads the council policy version at height.
func (c *Client) PolicyVersion(ctx context.Context, height int64) (uint64, int64, error) {
	resp := &policyv1.QueryPolicyResponse{}
	got, err := c.query(ctx, pathPolicy, &policyv1.QueryPolicyRequest{}, resp, height)
	if err != nil {
		return 0, 0, err
	}
	if resp.GetState() == nil {
		return 0, got, fmt.Errorf("%w: policy answer without a state", ErrQuery)
	}
	return resp.GetState().GetPolicyVersion(), got, nil
}

func (c *Client) query(ctx context.Context, path string, req, resp proto.Message, height int64) (int64, error) {
	got, found, err := c.queryFound(ctx, path, req, resp, height)
	if err != nil {
		return got, err
	}
	if !found {
		return got, fmt.Errorf("%w: %s answered not found", ErrQuery, path)
	}
	return got, nil
}

func (c *Client) queryFound(ctx context.Context, path string, req, resp proto.Message, height int64) (int64, bool, error) {
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(req)
	if err != nil {
		return 0, false, err
	}
	res, err := c.raw.Query(ctx, path, data, height)
	if err != nil {
		return 0, false, err
	}
	if res.Height < height {
		return res.Height, false, fmt.Errorf("%w: %s height %d below %d", ErrQuery, path, res.Height, height)
	}
	if res.Code != 0 {
		if res.Codespace == sdkerrors.ErrKeyNotFound.Codespace() && res.Code == sdkerrors.ErrKeyNotFound.ABCICode() {
			return res.Height, false, nil
		}
		return res.Height, false, fmt.Errorf("%w: %s %s/%d", ErrQuery, path, res.Codespace, res.Code)
	}
	if err := proto.Unmarshal(res.Value, resp); err != nil {
		return res.Height, false, fmt.Errorf("%w: decode %s: %v", ErrQuery, path, err)
	}
	return res.Height, true, nil
}

func trimPrefix(s, prefix string) string {
	return strings.TrimPrefix(s, prefix)
}
