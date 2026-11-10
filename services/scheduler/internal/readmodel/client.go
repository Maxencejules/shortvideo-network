// Package readmodel reads the indexer JSON from Goal 12.
// The field names are the Go names of the untagged projection structs.
// A stale status is not a lease renewal and not a publication.
package readmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Lease is one committed lease row.
type Lease struct {
	ID         string
	ProviderID string
	ContentID  string
	Version    uint64
	State      string
	Term       int64
	Deadline   int64
	Active     bool
}

// Content is one content row.
type Content struct {
	ID        string
	Version   uint64
	Withdrawn bool
}

// Provider is one provider row.
type Provider struct {
	ID    string
	State string
}

// Snapshot is one read. Stale means the indexer said the read is stale.
type Snapshot struct {
	Stale        bool
	StaleReasons []string
	ChainID      string
	Leases       []Lease
	Content      []Content
	Providers    []Provider
}

// Client reads one indexer base URL. It does not broadcast.
type Client struct {
	Base string
	HTTP *http.Client
}

// Load reads status, leases, content, and providers.
func (c Client) Load(ctx context.Context) (Snapshot, error) {
	var snap Snapshot
	var status struct {
		Stale        bool     `json:"stale"`
		StaleReasons []string `json:"stale_reasons"`
		ChainID      string   `json:"chain_id"`
	}
	if err := c.get(ctx, "/v1/status", &status); err != nil {
		return snap, err
	}
	snap.Stale = status.Stale
	snap.StaleReasons = status.StaleReasons
	snap.ChainID = status.ChainID
	var leases struct {
		Leases []Lease `json:"leases"`
	}
	if err := c.get(ctx, "/v1/leases", &leases); err != nil {
		return snap, err
	}
	snap.Leases = leases.Leases
	var content struct {
		Content []Content `json:"content"`
	}
	if err := c.get(ctx, "/v1/content", &content); err != nil {
		return snap, err
	}
	snap.Content = content.Content
	var providers struct {
		Providers []Provider `json:"providers"`
	}
	if err := c.get(ctx, "/v1/providers", &providers); err != nil {
		return snap, err
	}
	snap.Providers = providers.Providers
	return snap, nil
}

func (c Client) get(ctx context.Context, path string, dest any) error {
	if !strings.HasPrefix(c.Base, "http://127.0.0.1:") && !strings.HasPrefix(c.Base, "http://[::1]:") {
		return fmt.Errorf("readmodel: indexer must be loopback http")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.Base, "/")+path, nil)
	if err != nil {
		return err
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != 200 {
		return fmt.Errorf("readmodel: %s status %d", path, res.StatusCode)
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return err
	}
	return nil
}
