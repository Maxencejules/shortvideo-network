// Package policy is the destination policy of the audit producer: which
// provider URLs it may fetch and which addresses it may connect to.
//
// It is PROPOSED (decision request DR-09-10) and fail-closed. It applies the
// blueprint's SSRF rule ("constrain schemes, redirects and permitted
// destinations; reject private-network targets unless explicitly configured
// by the operator", docs/blueprint.md) to the producer:
//
//   - URL: scheme https only, at most 2048 bytes (bounds.json
//     safety_max_url_bytes), no userinfo, no fragment, no query, no path
//     other than "/" (the producer builds the request path itself), ASCII
//     host only, and a host name whose last label starts with a letter.
//   - Address: deny unless globally reachable, judged against the pinned
//     IANA special-purpose table after unmapping (Classify). Every resolved
//     address must pass; a name that resolves to one permitted and one
//     denied address is refused as a whole.
//   - Allowlist: a destination that is not global is permitted only by an
//     entry (provider_id, host, ip, port) that matches all four exactly.
//     There is no CIDR, no wildcard, no "allow private" switch, and no
//     global disable. Link-local, unspecified, multicast, and broadcast
//     addresses cannot be named by an entry. An IPv6 entry address must be
//     ::1, unique-local, NAT64 64:ff9b::/96, or inside 2000::/3.
//   - Port: 443, unless an entry names another port.
//
// The package makes no network calls. Resolution and connection are in
// package fetch, which connects only to the addresses Vet returns.
package policy

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
)

// Bounds.
const (
	// MaxURLBytes is bounds.json safety_max_url_bytes.
	MaxURLBytes = 2048
	// DefaultPort is the only port permitted without an allowlist entry.
	DefaultPort = 443
	// MaxEntries bounds the allowlist.
	MaxEntries = 64
	// MaxResolvedAddresses bounds the addresses one name may resolve to.
	MaxResolvedAddresses = 16
	// maxHostBytes is the DNS name length limit (RFC 1035).
	maxHostBytes = 253
)

// Rule names the policy rule that refused a URL or a destination.
type Rule string

// Refusal rules.
const (
	RuleURLLength        Rule = "URL_LENGTH"
	RuleURLSyntax        Rule = "URL_SYNTAX"
	RuleScheme           Rule = "SCHEME"
	RuleUserinfo         Rule = "USERINFO"
	RuleFragment         Rule = "FRAGMENT"
	RulePath             Rule = "PATH"
	RuleHost             Rule = "HOST"
	RulePort             Rule = "PORT"
	RuleNoAddress        Rule = "NO_ADDRESS"
	RuleTooManyAddresses Rule = "TOO_MANY_ADDRESSES"
	RuleAddress          Rule = "ADDRESS"
)

// Refusal is the error every refusal of this package returns. Its text
// names the rule and, for RuleAddress, the table row. It never contains
// the URL or a resolved address.
type Refusal struct {
	Rule Rule
	// Row is the row that denied the address (RuleAddress only).
	Row *Row
}

func (r *Refusal) Error() string {
	if r.Row == nil {
		return "policy refused: " + string(r.Rule)
	}
	if r.Row.Prefix.IsValid() {
		return fmt.Sprintf("policy refused: %s: %s (%s)", r.Rule, r.Row.Name, r.Row.Prefix)
	}
	return fmt.Sprintf("policy refused: %s: %s", r.Rule, r.Row.Name)
}

func refuse(rule Rule) error { return &Refusal{Rule: rule} }

// Endpoint is a provider base URL that passed the URL rules.
type Endpoint struct {
	// Host is the lower-case host name, or the IP literal without brackets.
	Host string
	// Literal is the address when Host is an IP literal, else invalid.
	Literal netip.Addr
	// Port is the URL port, or 443 when the URL has none.
	Port uint16
	// explicitPort records whether the URL named the port.
	explicitPort bool
}

// Authority is the host[:port] of request URLs and the Host header, as the
// base URL wrote it (IPv6 literals in brackets).
func (e Endpoint) Authority() string {
	host := e.Host
	if e.Literal.Is6() {
		host = "[" + host + "]"
	}
	if e.explicitPort {
		return host + ":" + strconv.Itoa(int(e.Port))
	}
	return host
}

// DialAuthority is the host:port the HTTP transport asks to dial.
func (e Endpoint) DialAuthority() string {
	host := e.Host
	if e.Literal.Is6() {
		host = "[" + host + "]"
	}
	return host + ":" + strconv.Itoa(int(e.Port))
}

// ParseEndpoint applies the URL rules to a provider base URL.
func ParseEndpoint(raw string) (Endpoint, error) {
	if len(raw) > MaxURLBytes {
		return Endpoint{}, refuse(RuleURLLength)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Endpoint{}, refuse(RuleURLSyntax)
	}
	switch {
	case u.Scheme != "https":
		return Endpoint{}, refuse(RuleScheme)
	case u.Opaque != "":
		return Endpoint{}, refuse(RuleURLSyntax)
	case u.User != nil:
		return Endpoint{}, refuse(RuleUserinfo)
	case strings.Contains(raw, "#"):
		return Endpoint{}, refuse(RuleFragment)
	case u.RawQuery != "" || u.ForceQuery:
		return Endpoint{}, refuse(RulePath)
	case (u.Path != "" && u.Path != "/") || u.RawPath != "":
		return Endpoint{}, refuse(RulePath)
	case strings.HasSuffix(u.Host, ":"):
		return Endpoint{}, refuse(RuleURLSyntax)
	}
	if !isASCII(u.Host) || strings.Contains(u.Host, "%") {
		return Endpoint{}, refuse(RuleHost)
	}
	host, literal, err := checkHost(strings.ToLower(u.Hostname()))
	if err != nil {
		return Endpoint{}, err
	}
	ep := Endpoint{Host: host, Literal: literal, Port: DefaultPort}
	if text := u.Port(); text != "" {
		port, err := strconv.ParseUint(text, 10, 16)
		if err != nil || strconv.FormatUint(port, 10) != text {
			return Endpoint{}, refuse(RuleURLSyntax)
		}
		if port == 0 {
			return Endpoint{}, refuse(RulePort)
		}
		ep.Port, ep.explicitPort = uint16(port), true
	}
	return ep, nil
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// checkHost accepts a lower-case DNS name or an IP literal without a zone.
// A name whose last label starts with a digit is refused. No top-level
// domain starts with a digit (RFC 1123 section 2.1: the highest-level label
// is alphabetic), and such a name may be an inet_aton-style address
// ("127.1", "2130706433", "0x7f000001", "017700000001") that some resolvers
// would accept.
func checkHost(host string) (string, netip.Addr, error) {
	if host == "" || len(host) > maxHostBytes || !isASCII(host) {
		return "", netip.Addr{}, refuse(RuleHost)
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.Zone() != "" {
			return "", netip.Addr{}, refuse(RuleHost)
		}
		return addr.String(), addr, nil
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if !dnsLabel.MatchString(label) {
			return "", netip.Addr{}, refuse(RuleHost)
		}
	}
	if c := labels[len(labels)-1][0]; c >= '0' && c <= '9' {
		return "", netip.Addr{}, refuse(RuleHost)
	}
	return host, netip.Addr{}, nil
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// Entry is one allowlist entry. All four fields must match exactly.
type Entry struct {
	ProviderID string
	Host       string
	IP         netip.Addr
	Port       uint16
}

// ErrEntry wraps every allowlist entry error.
var ErrEntry = errors.New("allowlist entry invalid")

var idPattern = regexp.MustCompile(genesis.IDPattern)

// checkEntry validates one entry. Host must already be lower case; an IP
// literal host must be the entry's IP. The IP must be canonical (not
// IPv4-mapped, no zone) and must not be link-local, unspecified, multicast,
// or the limited broadcast address, nor carry such an address inside a
// NAT64 or 6to4 address. An IPv6 IP must be in entryIPv6.
func checkEntry(e Entry) error {
	if !idPattern.MatchString(e.ProviderID) {
		return fmt.Errorf("%w: provider_id must match %s", ErrEntry, genesis.IDPattern)
	}
	host, literal, err := checkHost(e.Host)
	if err != nil || host != e.Host {
		return fmt.Errorf("%w: host must be a lower-case DNS name or an IP literal", ErrEntry)
	}
	if !e.IP.IsValid() || e.IP.Zone() != "" || e.IP.Is4In6() {
		return fmt.Errorf("%w: ip must be a plain IPv4 or IPv6 address", ErrEntry)
	}
	if literal.IsValid() && literal != e.IP {
		return fmt.Errorf("%w: an IP literal host must equal ip", ErrEntry)
	}
	for _, a := range []netip.Addr{e.IP, Classify(e.IP).Embedded} {
		if a.IsValid() && forbiddenInEntry(a) {
			return fmt.Errorf("%w: ip is link-local, unspecified, multicast, or broadcast", ErrEntry)
		}
	}
	if e.IP.Is6() && !inEntryIPv6(e.IP) {
		return fmt.Errorf("%w: an IPv6 ip must be ::1, unique-local, in 64:ff9b::/96, or in 2000::/3", ErrEntry)
	}
	if e.Port == 0 {
		return fmt.Errorf("%w: port must be 1 to 65535", ErrEntry)
	}
	return nil
}

var limitedBroadcast = netip.AddrFrom4([4]byte{255, 255, 255, 255})

// entryIPv6 is the IPv6 space an entry may name: loopback, unique-local,
// the well-known NAT64 prefix (its embedded IPv4 is checked), and global
// unicast (a 6to4 address's embedded IPv4 is checked). Every other IPv6
// form is refused, among them IPv4-compatible ::/96 and IPv4-translated
// ::ffff:0:0:0/96 addresses, whose embedded IPv4 address this package does
// not decode, local-use NAT64 64:ff9b:1::/48, site-local, and discard-only.
var entryIPv6 = []netip.Prefix{
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"),
	nat64WellKnown,
	GlobalUnicast6,
}

func inEntryIPv6(a netip.Addr) bool {
	for _, p := range entryIPv6 {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

func forbiddenInEntry(a netip.Addr) bool {
	return a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() || a.IsUnspecified() ||
		a.IsMulticast() || a == limitedBroadcast
}

// Policy holds a validated allowlist.
type Policy struct {
	entries []Entry
}

// New validates the allowlist. Duplicate entries are refused.
func New(entries []Entry) (*Policy, error) {
	if len(entries) > MaxEntries {
		return nil, fmt.Errorf("%w: %d entries, at most %d", ErrEntry, len(entries), MaxEntries)
	}
	seen := make(map[Entry]bool, len(entries))
	for i, e := range entries {
		if err := checkEntry(e); err != nil {
			return nil, fmt.Errorf("allowlist[%d]: %w", i, err)
		}
		if seen[e] {
			return nil, fmt.Errorf("allowlist[%d]: %w: duplicate", i, ErrEntry)
		}
		seen[e] = true
	}
	return &Policy{entries: append([]Entry(nil), entries...)}, nil
}

// Vet decides whether providerID's audit may connect to ep at the resolved
// addrs. Every address must be permitted; the first one that is not refuses
// the whole set. It returns the addresses to dial, in resolver order and
// without duplicates; an IPv4-mapped address is returned as IPv4.
//
// An address is permitted by an entry (providerID, ep.Host, address,
// ep.Port), or, without an entry, when it is global and ep.Port is 443.
func (p *Policy) Vet(providerID string, ep Endpoint, addrs []netip.Addr) ([]netip.AddrPort, error) {
	if len(addrs) == 0 {
		return nil, refuse(RuleNoAddress)
	}
	if len(addrs) > MaxResolvedAddresses {
		return nil, refuse(RuleTooManyAddresses)
	}
	out := make([]netip.AddrPort, 0, len(addrs))
	seen := make(map[netip.Addr]bool, len(addrs))
	for _, a := range addrs {
		v := Classify(a)
		switch {
		case p.listed(providerID, ep, v.Dial):
		case v.Global && ep.Port == DefaultPort:
		case v.Global:
			return nil, refuse(RulePort)
		default:
			return nil, &Refusal{Rule: RuleAddress, Row: v.Deny}
		}
		if !seen[v.Dial] {
			seen[v.Dial] = true
			out = append(out, netip.AddrPortFrom(v.Dial, ep.Port))
		}
	}
	return out, nil
}

func (p *Policy) listed(providerID string, ep Endpoint, a netip.Addr) bool {
	want := Entry{ProviderID: providerID, Host: ep.Host, IP: a, Port: ep.Port}
	for _, e := range p.entries {
		if e == want {
			return true
		}
	}
	return false
}
