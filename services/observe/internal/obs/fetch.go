package obs

import (
	"net"
	"net/url"
	"strings"
)

// Lookup resolves a hostname. A nil lookup refuses names. IP literals are
// checked without it. The function does not dial.
type Lookup func(host string) ([]net.IP, error)

// FetchError is a refusal. The reason is a code. The URL is not included.
type FetchError struct {
	Reason string
}

func (e FetchError) Error() string {
	return "fetch refused: " + e.Reason
}

// CheckFetch applies the default policy: HTTPS only, no userinfo, no
// non-global address, and no unresolved hostname. Non-global includes the
// ranges Go's IsPrivate does not cover: this-network, shared CGNAT,
// benchmarking, documentation, and the reserved block.
func CheckFetch(raw, redirect string, lookup Lookup) error {
	if err := checkOne(raw, lookup); err != nil {
		return err
	}
	if redirect == "" {
		return nil
	}
	if err := checkOne(redirect, lookup); err != nil {
		var fe FetchError
		if asFetch(err, &fe) {
			return FetchError{Reason: "redirect_" + fe.Reason}
		}
		return err
	}
	return nil
}

func asFetch(err error, dest *FetchError) bool {
	fe, ok := err.(FetchError)
	if !ok {
		return false
	}
	*dest = fe
	return true
}

func checkOne(raw string, lookup Lookup) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Opaque != "" {
		return FetchError{Reason: "malformed"}
	}
	if parsed.Scheme != "https" {
		return FetchError{Reason: "scheme"}
	}
	if parsed.User != nil {
		return FetchError{Reason: "userinfo"}
	}
	return checkHost(parsed.Hostname(), lookup)
}

func checkHost(host string, lookup Lookup) error {
	host = strings.TrimSuffix(host, ".")
	if host == "" || strings.Contains(host, "%") {
		return FetchError{Reason: "malformed"}
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") || strings.HasSuffix(lower, ".local") || lower == "metadata.google.internal" {
		return FetchError{Reason: "name_refused"}
	}
	if ip := net.ParseIP(host); ip != nil {
		if blockedIP(ip) {
			return FetchError{Reason: "private_address"}
		}
		return nil
	}
	labels := strings.Split(lower, ".")
	last := labels[len(labels)-1]
	if last == "" || last[0] >= '0' && last[0] <= '9' {
		// "127.1", "2130706433", and "0x7f000001" are not global names.
		return FetchError{Reason: "private_address"}
	}
	if lookup == nil {
		return FetchError{Reason: "name_unresolved"}
	}
	ips, err := lookup(host)
	if err != nil || len(ips) == 0 {
		return FetchError{Reason: "name_unresolved"}
	}
	for _, ip := range ips {
		if blockedIP(ip) {
			return FetchError{Reason: "private_address"}
		}
	}
	return nil
}

func blockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		return blockedV4(v4)
	}
	if len(ip) == net.IPv6len && ip[0] == 0xfe && ip[1]&0xc0 == 0xc0 {
		return true // fec0::/10, deprecated site-local
	}
	if len(ip) == net.IPv6len && ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x0d && ip[3] == 0xb8 {
		return true // 2001:db8::/32 documentation
	}
	return false
}

// blockedV4 reports addresses that are not globally reachable and that
// IsPrivate does not already cover. 192.0.0.9 and 192.0.0.10 stay allowed:
// the IANA special-purpose registry marks those two anycast addresses
// globally reachable.
func blockedV4(v4 net.IP) bool {
	switch {
	case v4[0] == 0: // 0.0.0.0/8, this network
		return true
	case v4[0] == 100 && v4[1]&0xc0 == 64: // 100.64.0.0/10, shared
		return true
	case v4[0] == 192 && v4[1] == 0 && v4[2] == 0:
		return v4[3] != 9 && v4[3] != 10
	case v4[0] == 192 && v4[1] == 0 && v4[2] == 2: // 192.0.2.0/24
		return true
	case v4[0] == 192 && v4[1] == 88 && v4[2] == 99: // 192.88.99.0/24
		return true
	case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19): // 198.18.0.0/15
		return true
	case v4[0] == 198 && v4[1] == 51 && v4[2] == 100: // 198.51.100.0/24
		return true
	case v4[0] == 203 && v4[1] == 0 && v4[2] == 113: // 203.0.113.0/24
		return true
	case v4[0] >= 240: // 240.0.0.0/4
		return true
	default:
		return false
	}
}
