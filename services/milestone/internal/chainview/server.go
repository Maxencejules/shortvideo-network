// Package chainview is the loopback HTTPS chain view the provider agent
// already speaks. It reads one CometBFT node. It does not invent a height,
// a lease, or a delivery code. A broadcast acknowledgement is returned only
// when both CheckTx and DeliverTx report code 0.
package chainview

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/gogoproto/proto"
	gproto "google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/address"
	"github.com/Maxencejules/shortvideo-network/chain/app"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Server is one provider's view of one node.
// This is the milestone chain gateway. Its /account route is a chain account
// query, not an application login. It does not accept an application session.
type Server struct {
	RPC       string
	Account   string
	Providers []string
	client    *rpchttp.HTTP
	enc       app.Encoding
}

// Listen serves TLS on addr until ctx is cancelled. caPath receives the
// certificate the provider is told to trust. The returned URL has no path.
func Listen(ctx context.Context, addr string, view Server) (url, caPath string, err error) {
	address.Configure()
	if view.client == nil {
		client, err := rpchttp.New(view.RPC, "/websocket")
		if err != nil {
			return "", "", err
		}
		view.client = client
	}
	if view.enc.Codec == nil {
		enc, err := app.MakeEncoding()
		if err != nil {
			return "", "", err
		}
		view.enc = enc
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", "", err
	}
	cert, der, err := certificate()
	if err != nil {
		_ = ln.Close()
		return "", "", err
	}
	ca, err := os.CreateTemp("", "svn-chainview-*.crt")
	if err != nil {
		_ = ln.Close()
		return "", "", err
	}
	if err := pem.Encode(ca, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		_ = ca.Close()
		_ = ln.Close()
		return "", "", err
	}
	if err := ca.Close(); err != nil {
		_ = ln.Close()
		return "", "", err
	}
	srv := &http.Server{
		Handler:           view.handler(),
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		_ = srv.Serve(tls.NewListener(ln, srv.TLSConfig))
	}()
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
		_ = os.Remove(ca.Name())
	}()
	return "https://" + ln.Addr().String(), ca.Name(), nil
}

func (s Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", s.status)
	mux.HandleFunc("GET /account", s.account)
	mux.HandleFunc("GET /lease/{id}", s.lease)
	mux.HandleFunc("POST /broadcast", s.broadcast)
	return mux
}

func (s Server) status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.RPC, "/")+"/status", nil)
	if err != nil {
		http.Error(w, "status", http.StatusBadGateway)
		return
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "status", http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil || res.StatusCode != http.StatusOK {
		http.Error(w, "status", http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func (s Server) account(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	data, err := proto.Marshal(&authtypes.QueryAccountRequest{Address: s.Account})
	if err != nil {
		http.Error(w, "account", http.StatusBadGateway)
		return
	}
	res, err := s.client.ABCIQuery(ctx, "/cosmos.auth.v1beta1.Query/Account", data)
	if err != nil || res.Response.Code != 0 {
		http.Error(w, "account", http.StatusBadGateway)
		return
	}
	var resp authtypes.QueryAccountResponse
	if err := s.enc.Codec.Unmarshal(res.Response.Value, &resp); err != nil {
		http.Error(w, "account", http.StatusBadGateway)
		return
	}
	var acc sdk.AccountI
	if err := s.enc.InterfaceRegistry.UnpackAny(resp.Account, &acc); err != nil || acc == nil {
		http.Error(w, "account", http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{
		"account_number": fmt.Sprintf("%d", acc.GetAccountNumber()),
		"sequence":       fmt.Sprintf("%d", acc.GetSequence()),
	})
}

func (s Server) lease(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	for _, providerID := range s.Providers {
		req := &leasesv1.QueryActiveLeasesRequest{
			ProviderId: providerID, Page: &typesv1.PageRequest{Limit: 100},
		}
		raw, err := gproto.Marshal(req)
		if err != nil {
			http.Error(w, "lease", http.StatusBadGateway)
			return
		}
		res, err := s.client.ABCIQuery(ctx, "/svn.leases.v1.Query/ActiveLeases", raw)
		if err != nil || res.Response.Code != 0 {
			http.Error(w, "lease", http.StatusBadGateway)
			return
		}
		var resp leasesv1.QueryActiveLeasesResponse
		if err := gproto.Unmarshal(res.Response.Value, &resp); err != nil {
			http.Error(w, "lease", http.StatusBadGateway)
			return
		}
		for _, lease := range resp.GetLeases() {
			if lease.GetLeaseId() != id {
				continue
			}
			name := strings.TrimPrefix(lease.GetState().String(), "LEASE_STATE_")
			writeJSON(w, map[string]string{"state": name})
			return
		}
	}
	http.NotFound(w, r)
}

func (s Server) broadcast(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || len(raw) == 0 {
		http.Error(w, "broadcast", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	res, err := s.client.BroadcastTxCommit(ctx, raw)
	if err != nil || res == nil {
		http.Error(w, "broadcast", http.StatusBadGateway)
		return
	}
	body := map[string]any{
		"check_code": res.CheckTx.Code, "deliver_code": res.TxResult.Code,
		"height": res.Height, "hash": strings.ToUpper(hex.EncodeToString(res.Hash)),
		"codespace": res.TxResult.Codespace,
	}
	if res.CheckTx.Code != 0 || res.TxResult.Code != 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(body)
		return
	}
	writeJSON(w, body)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func certificate() (tls.Certificate, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "svn-chainview"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, der, nil
}
