package policy

import "net/netip"

// Reach is the "Globally Reachable" column of an IANA special-purpose row.
type Reach uint8

const (
	// ReachTrue is "True". Such a row does not deny on its own.
	ReachTrue Reach = iota + 1
	// ReachFalse is "False", with or without a footnote.
	ReachFalse
	// ReachNA is "N/A", with or without a footnote.
	ReachNA
	// ReachBlank is an empty column. The registry leaves it empty on the
	// deprecated rows (192.88.99.0/24, 2001:10::/28). The table denies them.
	ReachBlank
	// ReachNotListed marks a row this package adds that is not in the
	// special-purpose registry. Every such row denies.
	ReachNotListed
)

// Denies reports whether an address inside a row with this value is denied.
// Only ReachTrue does not deny.
func (r Reach) Denies() bool { return r != ReachTrue }

// Row is one address block of the pinned table.
type Row struct {
	Prefix netip.Prefix
	Name   string
	Reach  Reach
	// Source is "IANA" for registry rows, or the RFC an added row cites.
	Source string
}

func row(prefix, name string, reach Reach, source string) Row {
	return Row{Prefix: netip.MustParsePrefix(prefix), Name: name, Reach: reach, Source: source}
}

// Provenance of the pinned table.
//
// IPv4 rows: IANA "IPv4 Special-Purpose Address Space" (formerly the IANA
// IPv4 Special-Purpose Address Registry),
// https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry-1.csv,
// registry Last Updated 2025-10-09 (the <updated> element of
// https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry.xml).
// Fetched 2026-11-02 through the session proxy; CSV SHA-256
// cbc4b7653c8bc640ba1e8e5d4ebbf11e7c7a1d867e7f1593aad001d3f8632aac.
//
// IPv6 rows: IANA "IPv6 Special-Purpose Address Space",
// https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry-1.csv,
// registry Last Updated 2025-10-09 (iana-ipv6-special-registry.xml).
// Fetched 2026-11-02 through the session proxy; CSV SHA-256
// bf56772aef97fbf9b4a27c98b8511f2bb1ba96da4b9ccc12ab203a2496faba66.
//
// The two CSV files are kept unchanged in policy/testdata, and
// TestTableMatchesRegistrySnapshot fails if a registry row below differs from
// them (prefix, name, or Globally Reachable value). A registry change after
// 2025-10-09 is not reflected here; refreshing the table is a code change.
//
// Rows with Source other than "IANA" are not in the special-purpose
// registries. They deny multicast and deprecated ranges that IANA lists in
// other registries.
const (
	RegistryLastUpdated = "2025-10-09"
	RegistryIPv4URL     = "https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry-1.csv"
	RegistryIPv6URL     = "https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry-1.csv"
)

// IPv4Rows is the IPv4 table in registry order, then the added rows.
var IPv4Rows = []Row{
	row("0.0.0.0/8", `"This network"`, ReachFalse, "IANA"),
	row("0.0.0.0/32", `"This host on this network"`, ReachFalse, "IANA"),
	row("10.0.0.0/8", "Private-Use", ReachFalse, "IANA"),
	row("100.64.0.0/10", "Shared Address Space", ReachFalse, "IANA"),
	row("127.0.0.0/8", "Loopback", ReachFalse, "IANA"),
	row("169.254.0.0/16", "Link Local", ReachFalse, "IANA"),
	row("172.16.0.0/12", "Private-Use", ReachFalse, "IANA"),
	row("192.0.0.0/24", "IETF Protocol Assignments", ReachFalse, "IANA"),
	row("192.0.0.0/29", "IPv4 Service Continuity Prefix", ReachFalse, "IANA"),
	row("192.0.0.8/32", "IPv4 dummy address", ReachFalse, "IANA"),
	row("192.0.0.9/32", "Port Control Protocol Anycast", ReachTrue, "IANA"),
	row("192.0.0.10/32", "Traversal Using Relays around NAT Anycast", ReachTrue, "IANA"),
	row("192.0.0.170/32", "NAT64/DNS64 Discovery", ReachFalse, "IANA"),
	row("192.0.0.171/32", "NAT64/DNS64 Discovery", ReachFalse, "IANA"),
	row("192.0.2.0/24", "Documentation (TEST-NET-1)", ReachFalse, "IANA"),
	row("192.31.196.0/24", "AS112-v4", ReachTrue, "IANA"),
	row("192.52.193.0/24", "AMT", ReachTrue, "IANA"),
	row("192.88.99.0/24", "Deprecated (6to4 Relay Anycast)", ReachBlank, "IANA"),
	row("192.88.99.2/32", "6a44-relay anycast address", ReachFalse, "IANA"),
	row("192.168.0.0/16", "Private-Use", ReachFalse, "IANA"),
	row("192.175.48.0/24", "Direct Delegation AS112 Service", ReachTrue, "IANA"),
	row("198.18.0.0/15", "Benchmarking", ReachFalse, "IANA"),
	row("198.51.100.0/24", "Documentation (TEST-NET-2)", ReachFalse, "IANA"),
	row("203.0.113.0/24", "Documentation (TEST-NET-3)", ReachFalse, "IANA"),
	row("240.0.0.0/4", "Reserved", ReachFalse, "IANA"),
	row("255.255.255.255/32", "Limited Broadcast", ReachFalse, "IANA"),
	// Added: IPv4 multicast is in the IANA multicast registry, not the
	// special-purpose one.
	row("224.0.0.0/4", "Multicast", ReachNotListed, "RFC 5771"),
}

// IPv6Rows is the IPv6 table in registry order, then the added rows.
var IPv6Rows = []Row{
	row("::1/128", "Loopback Address", ReachFalse, "IANA"),
	row("::/128", "Unspecified Address", ReachFalse, "IANA"),
	row("::ffff:0:0/96", "IPv4-mapped Address", ReachFalse, "IANA"),
	row("64:ff9b::/96", "IPv4-IPv6 Translat.", ReachTrue, "IANA"),
	row("64:ff9b:1::/48", "IPv4-IPv6 Translat.", ReachFalse, "IANA"),
	row("100::/64", "Discard-Only Address Block", ReachFalse, "IANA"),
	row("100:0:0:1::/64", "Dummy IPv6 Prefix", ReachFalse, "IANA"),
	row("2001::/23", "IETF Protocol Assignments", ReachFalse, "IANA"),
	row("2001::/32", "TEREDO", ReachNA, "IANA"),
	row("2001:1::1/128", "Port Control Protocol Anycast", ReachTrue, "IANA"),
	row("2001:1::2/128", "Traversal Using Relays around NAT Anycast", ReachTrue, "IANA"),
	row("2001:1::3/128", "DNS-SD Service Registration Protocol Anycast", ReachTrue, "IANA"),
	row("2001:2::/48", "Benchmarking", ReachFalse, "IANA"),
	row("2001:3::/32", "AMT", ReachTrue, "IANA"),
	row("2001:4:112::/48", "AS112-v6", ReachTrue, "IANA"),
	row("2001:10::/28", "Deprecated (previously ORCHID)", ReachBlank, "IANA"),
	row("2001:20::/28", "ORCHIDv2", ReachTrue, "IANA"),
	row("2001:30::/28", "Drone Remote ID Protocol Entity Tags (DETs) Prefix", ReachTrue, "IANA"),
	row("2001:db8::/32", "Documentation", ReachFalse, "IANA"),
	row("2002::/16", "6to4", ReachNA, "IANA"),
	row("2620:4f:8000::/48", "Direct Delegation AS112 Service", ReachTrue, "IANA"),
	row("3fff::/20", "Documentation", ReachFalse, "IANA"),
	row("5f00::/16", "Segment Routing (SRv6) SIDs", ReachFalse, "IANA"),
	row("fc00::/7", "Unique-Local", ReachFalse, "IANA"),
	row("fe80::/10", "Link-Local Unicast", ReachFalse, "IANA"),
	// Added rows that the special-purpose registry no longer lists.
	row("::/96", "IPv4-Compatible Address (deprecated)", ReachNotListed, "RFC 4291 section 2.5.5.1"),
	row("fec0::/10", "Site-Local Unicast (deprecated)", ReachNotListed, "RFC 3879"),
	row("ff00::/8", "Multicast", ReachNotListed, "RFC 4291 section 2.7"),
}

// GlobalUnicast6 is the IPv6 global unicast space. An IPv6 address outside
// it is denied even when no row matches (RFC 4291 section 2.4, IANA IPv6
// Address Space registry).
var GlobalUnicast6 = netip.MustParsePrefix("2000::/3")

// Translation prefixes whose embedded IPv4 address is checked.
var (
	nat64WellKnown = netip.MustParsePrefix("64:ff9b::/96")
	sixToFour      = netip.MustParsePrefix("2002::/16")
)
