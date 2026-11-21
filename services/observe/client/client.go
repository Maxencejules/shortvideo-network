// Package client calls svn-observe. An empty address or a failed request is
// unreachable. Callers must refuse the intake in that case.
package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/observe/internal/obs"
)

const (
	KindUploads     = obs.KindUploads
	KindProcessing  = obs.KindProcessing
	KindOutputBytes = obs.KindOutputBytes
	KindStorage     = obs.KindStorage
	KindAudit       = obs.KindAudit
	KindRetries     = obs.KindRetries
)

// ErrUnreachable means svn-observe did not answer. The intake must stop.
var ErrUnreachable = errors.New("admission unreachable")

// Decision is the admission answer. Granted false with a nil error is a
// cap refusal, not a transport failure.
type Decision struct {
	Granted  bool   `json:"granted"`
	Queued   bool   `json:"queued"`
	Replayed bool   `json:"replayed"`
	Reason   string `json:"reason"`
	GrantID  string `json:"grant_id"`
}

// Client is one loopback caller. Base is the origin, without a path.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

// New returns a client. An empty base still constructs it; Admit then
// returns ErrUnreachable.
func New(base, token string) *Client {
	return &Client{
		Base: strings.TrimRight(base, "/"), Token: token,
		HTTP: &http.Client{Timeout: 2 * time.Second},
	}
}

// Admit asks svn-observe for one intake. A replayed key does not consume
// another unit.
func (c *Client) Admit(ctx context.Context, kind string, units int64, key string) (Decision, error) {
	if c == nil || c.Base == "" || c.Token == "" {
		return Decision{}, ErrUnreachable
	}
	body, err := json.Marshal(map[string]any{"kind": kind, "units": units, "idempotency_key": fitKey(key)})
	if err != nil {
		return Decision{}, ErrUnreachable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/v1/admit", bytes.NewReader(body))
	if err != nil {
		return Decision{}, ErrUnreachable
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http().Do(req)
	if err != nil {
		return Decision{}, ErrUnreachable
	}
	defer res.Body.Close()
	var dec Decision
	if err := json.NewDecoder(res.Body).Decode(&dec); err != nil {
		return Decision{}, ErrUnreachable
	}
	if res.StatusCode == http.StatusOK || res.StatusCode == http.StatusAccepted || res.StatusCode == http.StatusTooManyRequests {
		return dec, nil
	}
	return Decision{}, ErrUnreachable
}

// Release returns a capacity grant. A transport failure is ErrUnreachable.
func (c *Client) Release(ctx context.Context, grantID string) error {
	if c == nil || c.Base == "" || grantID == "" {
		return ErrUnreachable
	}
	body, err := json.Marshal(map[string]string{"grant_id": grantID})
	if err != nil {
		return ErrUnreachable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/v1/release", bytes.NewReader(body))
	if err != nil {
		return ErrUnreachable
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http().Do(req)
	if err != nil {
		return ErrUnreachable
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return ErrUnreachable
	}
	return nil
}

func fitKey(key string) string {
	if key == "" || (len(key) <= 80 && keyChars(key)) {
		return key
	}
	sum := sha256.Sum256([]byte(key))
	return "h" + hex.EncodeToString(sum[:16])
}

func keyChars(key string) bool {
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == ':' || c == '-':
		default:
			return false
		}
	}
	return true
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// Listen starts a loopback svn-observe for tests. limits and queues replace
// the placeholder caps. The token is not a credential for any other system.
func Listen(limits, queues map[string]int64) (base, token string, stop func(), err error) {
	token = "synthetic-observe-25"
	svc, err := obs.New(obs.Options{Token: token, Limits: limits, Queues: queues})
	if err != nil {
		return "", "", nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", nil, err
	}
	server := &http.Server{Handler: svc.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = server.Serve(ln) }()
	return "http://" + ln.Addr().String(), token, func() { _ = server.Close() }, nil
}

// Permissive is a cap large enough for the development suites. It is still
// an admission check, not a bypass.
func Permissive() map[string]int64 {
	return map[string]int64{
		KindUploads: 1_000_000, KindProcessing: 1_000_000, KindOutputBytes: 1_000_000_000,
		KindStorage: 1_000_000_000, KindAudit: 1_000_000, KindRetries: 1_000_000,
		obs.KindLogin: 1_000_000, obs.KindSearch: 1_000_000, obs.KindReport: 1_000_000, obs.KindDiscovery: 1_000_000,
	}
}
