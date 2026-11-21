package obs

import (
	"net"
	"net/url"
	"strconv"
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

// CheckFetch applies the default policy: HTTPS only, no userinfo, no private
// or loopback or link-local address, and no unresolved hostname.
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
	if host == "" {
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
	if _, err := strconv.Atoi(host); err == nil {
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
	if v4 := ip.To4(); v4 != nil && v4.Equal(net.IPv4(169, 254, 169, 254)) {
		return true
	}
	return false
}
