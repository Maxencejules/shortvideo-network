// Package fetch downloads one HTTPS response with the SSRF limits from the
// blueprint: https only, no userinfo, no redirects, a bounded body, and a
// private address only when the operator named that host, IP, and port.
package fetch

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/policy"
	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/config"
)

// MaxRedirects is zero. A redirect is refused, not followed.
const maxBody = 1 << 20

var (
	ErrRefused   = errors.New("fetch: destination refused")
	ErrRedirect  = errors.New("fetch: redirect refused")
	ErrUnbounded = errors.New("fetch: response larger than the bound")
	ErrStatus    = errors.New("fetch: response status")
)

// Limit is the caller's byte cap for one response.
type Limit struct {
	MaxBytes uint64
	Timeout  time.Duration
	Token    string
	CA       *tls.Config
	Allow    []config.Destination
}

// Bytes fetches url and returns at most lim.MaxBytes. An extra byte is
// ErrUnbounded. Redirects are not followed. The dial uses the vetted IP,
// not a later name lookup.
func Bytes(ctx context.Context, raw string, lim Limit) ([]byte, error) {
	target, addr, err := vet(ctx, raw, lim.Allow)
	if err != nil {
		return nil, err
	}
	if lim.MaxBytes == 0 || lim.Timeout <= 0 {
		return nil, ErrRefused
	}
	ctx, cancel := context.WithTimeout(ctx, lim.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, ErrRefused
	}
	if lim.Token != "" {
		req.Header.Set("Authorization", "Bearer "+lim.Token)
	}
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrRedirect },
		Timeout:       lim.Timeout,
		Transport: &http.Transport{
			Proxy:             nil,
			DialContext:       dial(addr),
			TLSClientConfig:   lim.CA,
			ForceAttemptHTTP2: false,
			// One connection. A pooled connection could be to another host.
			DisableKeepAlives: true,
		},
	}
	res, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrRedirect) {
			return nil, ErrRedirect
		}
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("%w: %d", ErrStatus, res.StatusCode)
	}
	if res.ContentLength > int64(lim.MaxBytes) {
		return nil, ErrUnbounded
	}
	buf, err := io.ReadAll(io.LimitReader(res.Body, int64(lim.MaxBytes)+1))
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	if uint64(len(buf)) > lim.MaxBytes {
		return nil, ErrUnbounded
	}
	return buf, nil
}

// Post sends body to raw with the same destination checks.
func Post(ctx context.Context, raw string, body []byte, lim Limit) ([]byte, error) {
	target, addr, err := vet(ctx, raw, lim.Allow)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, lim.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), strings.NewReader(string(body)))
	if err != nil {
		return nil, ErrRefused
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return ErrRedirect },
		Timeout:       lim.Timeout,
		Transport: &http.Transport{
			Proxy:             nil,
			DialContext:       dial(addr),
			TLSClientConfig:   lim.CA,
			DisableKeepAlives: true,
		},
	}
	res, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrRedirect) {
			return nil, ErrRedirect
		}
		return nil, fmt.Errorf("fetch: %w", err)
	}
	defer res.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(buf) > maxBody {
		return nil, ErrUnbounded
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("%w: %d", ErrStatus, res.StatusCode)
	}
	return buf, nil
}

func vet(ctx context.Context, raw string, allow []config.Destination) (*url.URL, netip.AddrPort, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return nil, netip.AddrPort{}, ErrRefused
	}
	if u.Hostname() == "" || len(raw) > 2048 {
		return nil, netip.AddrPort{}, ErrRefused
	}
	for _, seg := range strings.Split(u.EscapedPath(), "/") {
		if seg == "" {
			continue
		}
		if seg == "." || seg == ".." || strings.Contains(seg, "\\") || strings.Contains(strings.ToLower(seg), "%2e") || strings.Contains(seg, "%") {
			return nil, netip.AddrPort{}, ErrRefused
		}
	}
	port := u.Port()
	if port == "" {
		port = "443"
		u.Host = net.JoinHostPort(u.Hostname(), port)
	}
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil {
		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", u.Hostname())
		if err != nil || len(ips) == 0 || len(ips) > 8 {
			return nil, netip.AddrPort{}, ErrRefused
		}
		ip = ips[0]
		for _, one := range ips {
			if !permitted(u.Hostname(), one, port, allow) {
				return nil, netip.AddrPort{}, ErrRefused
			}
		}
	}
	if !permitted(u.Hostname(), ip, port, allow) {
		return nil, netip.AddrPort{}, ErrRefused
	}
	ap, err := netip.ParseAddrPort(net.JoinHostPort(ip.String(), port))
	if err != nil {
		return nil, netip.AddrPort{}, ErrRefused
	}
	return u, ap, nil
}

func permitted(host string, ip netip.Addr, port string, allow []config.Destination) bool {
	v := policy.Classify(ip.Unmap())
	for _, d := range allow {
		want, err := netip.ParseAddr(d.IP)
		if err != nil {
			continue
		}
		if host == d.Host && ip.Unmap() == want.Unmap() && port == fmt.Sprintf("%d", d.Port) {
			if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
				return false
			}
			return true
		}
	}
	return v.Global && port == "443"
}

func dial(target netip.AddrPort) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		network := "tcp4"
		if target.Addr().Is6() {
			network = "tcp6"
		}
		d := net.Dialer{Timeout: 5 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
			got, err := netip.ParseAddrPort(address)
			if err != nil || got.Addr().Unmap() != target.Addr().Unmap() || got.Port() != target.Port() {
				return ErrRefused
			}
			return nil
		}}
		return d.DialContext(ctx, network, target.String())
	}
}
