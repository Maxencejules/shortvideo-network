package policy

import "net/netip"

// Verdict is the address rule's answer for one resolved address.
type Verdict struct {
	// Addr is the address as resolved.
	Addr netip.Addr
	// Dial is the address a connection uses: Addr, or its IPv4 form when
	// Addr is IPv4-mapped (::ffff:a.b.c.d is the IPv4 host a.b.c.d).
	Dial netip.Addr
	// Embedded is the IPv4 address inside an IPv4-mapped, NAT64
	// (64:ff9b::/96), or 6to4 (2002::/16) address. It is the zero Addr for
	// any other address.
	Embedded netip.Addr
	// Global is true when the address needs no allowlist entry.
	Global bool
	// Deny is the row that denies the address, nil when Global. For an
	// address in several deny rows it is the most specific one.
	Deny *Row
}

// Pseudo-rows for denials that no prefix row expresses.
var (
	invalidAddrRow = Row{Name: "not an IP address", Reach: ReachNotListed, Source: "policy"}
	zonedAddrRow   = Row{Name: "IPv6 address with a zone", Reach: ReachNotListed, Source: "policy"}
	outsideRow     = Row{Name: "IPv6 outside 2000::/3", Reach: ReachNotListed, Source: "RFC 4291 section 2.4"}
)

// Classify applies the address rule: deny unless globally reachable.
//
// An address is global only when no deny row of the pinned table contains
// it and, for IPv6, it is inside 2000::/3. A True row inside a deny row
// (for example 192.0.0.9/32 inside 192.0.0.0/24, or 2001:1::1/128 inside
// 2001::/23) is still denied: the registry footnotes make those blocks
// unusable "unless by virtue of a more specific reservation", and an audit
// endpoint has no reason to sit on an anycast service address.
//
// Unmapping happens before the lookup, because netip.Prefix.Contains does
// not unmap and netip.Addr.IsGlobalUnicast is true for RFC 1918 and ULA
// addresses:
//
//   - IPv4-mapped ::ffff:a.b.c.d is judged as the IPv4 address a.b.c.d and
//     dialed as that address;
//   - NAT64 64:ff9b::/96 and 6to4 2002::/16 carry an IPv4 address, which is
//     judged first, so an embedded private address is denied with its own
//     row; the IPv6 address itself is then judged too. 64:ff9b::/96 is
//     outside 2000::/3 and 2002::/16 is a Globally Reachable N/A row, so no
//     NAT64 or 6to4 address is global under this table. An operator who
//     must reach a provider through one needs an exact allowlist entry;
//   - local-use NAT64 64:ff9b:1::/48 and 2001::/23 (Teredo included) are
//     deny rows, so they are denied outright.
//
// Network-specific NAT64 prefixes (RFC 6052) cannot be recognised. An
// address inside one is judged as an ordinary IPv6 address.
func Classify(a netip.Addr) Verdict {
	v := Verdict{Addr: a, Dial: a}
	switch {
	case !a.IsValid():
		v.Deny = &invalidAddrRow
		return v
	case a.Zone() != "":
		v.Deny = &zonedAddrRow
		return v
	case a.Is4In6():
		v.Dial = a.Unmap()
		v.Embedded = v.Dial
		v.Deny = denyRow(IPv4Rows, v.Dial)
	case a.Is4():
		v.Deny = denyRow(IPv4Rows, a)
	default:
		if nat64WellKnown.Contains(a) || sixToFour.Contains(a) {
			v.Embedded = embeddedIPv4(a)
			v.Deny = denyRow(IPv4Rows, v.Embedded)
		}
		if v.Deny == nil {
			v.Deny = denyRow(IPv6Rows, a)
		}
		if v.Deny == nil && !GlobalUnicast6.Contains(a) {
			v.Deny = &outsideRow
		}
	}
	v.Global = v.Deny == nil
	return v
}

// denyRow returns the most specific deny row containing a, or nil.
func denyRow(rows []Row, a netip.Addr) *Row {
	var found *Row
	for i := range rows {
		r := &rows[i]
		if !r.Reach.Denies() || !r.Prefix.Contains(a) {
			continue
		}
		if found == nil || r.Prefix.Bits() > found.Prefix.Bits() {
			found = r
		}
	}
	return found
}

// embeddedIPv4 returns the IPv4 address carried by a NAT64 64:ff9b::/96
// address (its last 32 bits) or a 6to4 2002::/16 address (bits 16-47).
func embeddedIPv4(a netip.Addr) netip.Addr {
	b := a.As16()
	if sixToFour.Contains(a) {
		return netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]})
	}
	return netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]})
}
