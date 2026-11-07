package policy

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"net/netip"
	"os"
	"regexp"
	"strings"
	"testing"
)

// tableCase is one sample address for one table row. deny is the prefix of
// the row expected to deny the sample ("" when it is permitted).
type tableCase struct {
	row    string
	sample string
	deny   string
}

var ipv4Cases = []tableCase{
	{"0.0.0.0/8", "0.0.0.1", "0.0.0.0/8"},
	{"0.0.0.0/32", "0.0.0.0", "0.0.0.0/32"},
	{"10.0.0.0/8", "10.0.0.1", "10.0.0.0/8"},
	{"100.64.0.0/10", "100.64.0.1", "100.64.0.0/10"},
	{"127.0.0.0/8", "127.0.0.1", "127.0.0.0/8"},
	{"169.254.0.0/16", "169.254.169.254", "169.254.0.0/16"},
	{"172.16.0.0/12", "172.16.0.1", "172.16.0.0/12"},
	{"192.0.0.0/24", "192.0.0.100", "192.0.0.0/24"},
	{"192.0.0.0/29", "192.0.0.1", "192.0.0.0/29"},
	{"192.0.0.8/32", "192.0.0.8", "192.0.0.8/32"},
	// True rows inside 192.0.0.0/24 stay denied by the enclosing row.
	{"192.0.0.9/32", "192.0.0.9", "192.0.0.0/24"},
	{"192.0.0.10/32", "192.0.0.10", "192.0.0.0/24"},
	{"192.0.0.170/32", "192.0.0.170", "192.0.0.170/32"},
	{"192.0.0.171/32", "192.0.0.171", "192.0.0.171/32"},
	{"192.0.2.0/24", "192.0.2.1", "192.0.2.0/24"},
	{"192.31.196.0/24", "192.31.196.1", ""},
	{"192.52.193.0/24", "192.52.193.1", ""},
	{"192.88.99.0/24", "192.88.99.1", "192.88.99.0/24"},
	{"192.88.99.2/32", "192.88.99.2", "192.88.99.2/32"},
	{"192.168.0.0/16", "192.168.1.1", "192.168.0.0/16"},
	{"192.175.48.0/24", "192.175.48.1", ""},
	{"198.18.0.0/15", "198.18.0.1", "198.18.0.0/15"},
	{"198.51.100.0/24", "198.51.100.1", "198.51.100.0/24"},
	{"203.0.113.0/24", "203.0.113.1", "203.0.113.0/24"},
	{"240.0.0.0/4", "240.0.0.1", "240.0.0.0/4"},
	{"255.255.255.255/32", "255.255.255.255", "255.255.255.255/32"},
	{"224.0.0.0/4", "224.0.0.1", "224.0.0.0/4"},
	{"", "8.8.8.8", ""},
	{"", "1.1.1.1", ""},
}

var ipv6Cases = []tableCase{
	{"::1/128", "::1", "::1/128"},
	{"::/128", "::", "::/128"},
	// IPv4-mapped addresses are judged as their IPv4 address.
	{"::ffff:0:0/96", "::ffff:8.8.8.8", ""},
	// NAT64: the embedded address is global, but 64:ff9b::/96 is outside
	// 2000::/3.
	{"64:ff9b::/96", "64:ff9b::808:808", "outside"},
	{"64:ff9b:1::/48", "64:ff9b:1::1", "64:ff9b:1::/48"},
	{"100::/64", "100::1", "100::/64"},
	{"100:0:0:1::/64", "100:0:0:1::1", "100:0:0:1::/64"},
	{"2001::/23", "2001:100::1", "2001::/23"},
	{"2001::/32", "2001::1", "2001::/32"},
	{"2001:1::1/128", "2001:1::1", "2001::/23"},
	{"2001:1::2/128", "2001:1::2", "2001::/23"},
	{"2001:1::3/128", "2001:1::3", "2001::/23"},
	{"2001:2::/48", "2001:2::1", "2001:2::/48"},
	{"2001:3::/32", "2001:3::1", "2001::/23"},
	{"2001:4:112::/48", "2001:4:112::1", "2001::/23"},
	{"2001:10::/28", "2001:10::1", "2001:10::/28"},
	{"2001:20::/28", "2001:20::1", "2001::/23"},
	{"2001:30::/28", "2001:30::1", "2001::/23"},
	{"2001:db8::/32", "2001:db8::1", "2001:db8::/32"},
	{"2002::/16", "2002:808:808::1", "2002::/16"},
	{"2620:4f:8000::/48", "2620:4f:8000::1", ""},
	{"3fff::/20", "3fff::1", "3fff::/20"},
	{"5f00::/16", "5f00::1", "5f00::/16"},
	{"fc00::/7", "fd00::1", "fc00::/7"},
	{"fe80::/10", "fe80::1", "fe80::/10"},
	{"::/96", "::102:304", "::/96"},
	{"fec0::/10", "fec0::1", "fec0::/10"},
	{"ff00::/8", "ff02::1", "ff00::/8"},
	{"", "2606:4700:4700::1111", ""},
	{"", "4000::1", "outside"},
}

func checkVerdict(t *testing.T, sample string, v Verdict, deny string) {
	t.Helper()
	switch {
	case deny == "":
		if !v.Global || v.Deny != nil {
			t.Fatalf("%s: want permitted, got %+v", sample, v)
		}
	case deny == "outside":
		if v.Global || v.Deny != &outsideRow {
			t.Fatalf("%s: want outside 2000::/3, got %+v", sample, v)
		}
	default:
		if v.Global || v.Deny == nil || v.Deny.Prefix != netip.MustParsePrefix(deny) {
			t.Fatalf("%s: want denied by %s, got %+v", sample, deny, v)
		}
	}
}

// TestSpecialPurposeTable checks one address per row of the pinned table
// (plus control addresses), and for every IPv4 sample its 4-in-6 forms:
// IPv4-mapped gives the same verdict; NAT64 and 6to4 are denied by the
// embedded address's row when that row denies, and otherwise by the IPv6
// rule (outside 2000::/3, or the 6to4 N/A row).
func TestSpecialPurposeTable(t *testing.T) {
	exercised := map[netip.Prefix]bool{}
	mark := func(row string) {
		if row != "" {
			exercised[netip.MustParsePrefix(row)] = true
		}
	}
	for _, tc := range ipv4Cases {
		mark(tc.row)
		a := netip.MustParseAddr(tc.sample)
		checkVerdict(t, tc.sample, Classify(a), tc.deny)

		mapped := netip.AddrFrom16(a.As16())
		v := Classify(mapped)
		checkVerdict(t, mapped.String(), v, tc.deny)
		if v.Dial != a || v.Embedded != a {
			t.Fatalf("%s: dial %v embedded %v", mapped, v.Dial, v.Embedded)
		}

		b := a.As4()
		nat64 := netip.AddrFrom16([16]byte{0, 0x64, 0xff, 0x9b, 12: b[0], 13: b[1], 14: b[2], 15: b[3]})
		six := netip.AddrFrom16([16]byte{0x20, 0x02, b[0], b[1], b[2], b[3], 15: 1})
		wantNAT64, want6to4 := tc.deny, tc.deny
		if tc.deny == "" {
			wantNAT64, want6to4 = "outside", "2002::/16"
		}
		for _, w := range []struct {
			addr netip.Addr
			deny string
		}{{nat64, wantNAT64}, {six, want6to4}} {
			v := Classify(w.addr)
			checkVerdict(t, w.addr.String(), v, w.deny)
			if v.Embedded != a || v.Dial != w.addr {
				t.Fatalf("%s: embedded %v dial %v", w.addr, v.Embedded, v.Dial)
			}
		}
	}
	for _, tc := range ipv6Cases {
		mark(tc.row)
		checkVerdict(t, tc.sample, Classify(netip.MustParseAddr(tc.sample)), tc.deny)
	}
	for _, rows := range [][]Row{IPv4Rows, IPv6Rows} {
		for _, r := range rows {
			if !exercised[r.Prefix] {
				t.Errorf("row %s (%s) has no case", r.Prefix, r.Name)
			}
		}
	}
}

// The unmapping cases only an explicit unmap catches: Prefix.Contains does
// not unmap, so the private IPv4 row alone would miss them.
func TestUnmappingCases(t *testing.T) {
	cases := []struct {
		addr, deny string
		embedded   string
	}{
		{"::ffff:10.0.0.1", "10.0.0.0/8", "10.0.0.1"},
		{"::ffff:100.64.0.1", "100.64.0.0/10", "100.64.0.1"},
		{"::ffff:169.254.169.254", "169.254.0.0/16", "169.254.169.254"},
		{"::ffff:192.0.2.1", "192.0.2.0/24", "192.0.2.1"},
		{"::ffff:127.0.0.1", "127.0.0.0/8", "127.0.0.1"},
		// NAT64 embedding a private IPv4.
		{"64:ff9b::a00:1", "10.0.0.0/8", "10.0.0.1"},
		{"64:ff9b::a9fe:a9fe", "169.254.0.0/16", "169.254.169.254"},
		// Local-use NAT64 is denied outright.
		{"64:ff9b:1::a00:1", "64:ff9b:1::/48", ""},
		// 6to4 embedding a private IPv4.
		{"2002:a00:1::1", "10.0.0.0/8", "10.0.0.1"},
		{"2002:c0a8:101::1", "192.168.0.0/16", "192.168.1.1"},
		// Teredo (server 10.0.0.1, client 10.0.0.1 obfuscated) is denied
		// by the TEREDO row inside 2001::/23.
		{"2001:0:a00:1::f5ff:fffe", "2001::/32", ""},
	}
	for _, tc := range cases {
		a := netip.MustParseAddr(tc.addr)
		v := Classify(a)
		checkVerdict(t, tc.addr, v, tc.deny)
		if tc.embedded != "" && v.Embedded != netip.MustParseAddr(tc.embedded) {
			t.Fatalf("%s: embedded %v", tc.addr, v.Embedded)
		}
		if tc.embedded != "" && netip.MustParsePrefix(tc.deny).Contains(a) {
			t.Fatalf("%s: Prefix.Contains matched without unmapping", tc.addr)
		}
	}
}

// Go's own classification would permit private and unique-local addresses,
// which is why the table exists.
func TestGoBuiltinsAreNotEnough(t *testing.T) {
	for _, s := range []string{"10.0.0.1", "192.168.1.1", "100.64.0.1", "fd00::1", "198.18.0.1"} {
		a := netip.MustParseAddr(s)
		if !a.IsGlobalUnicast() {
			t.Fatalf("%s: IsGlobalUnicast is false; the reason for the table changed", s)
		}
		if Classify(a).Global {
			t.Fatalf("%s permitted", s)
		}
	}
}

func TestClassifyRejectsInvalidAndZoned(t *testing.T) {
	if v := Classify(netip.Addr{}); v.Global || v.Deny != &invalidAddrRow {
		t.Fatalf("invalid: %+v", v)
	}
	zoned := netip.MustParseAddr("2606:4700:4700::1111%eth0")
	if v := Classify(zoned); v.Global || v.Deny != &zonedAddrRow {
		t.Fatalf("zoned: %+v", v)
	}
}

// Registry snapshot files and their SHA-256 at fetch time (2026-11-02).
var snapshots = []struct {
	file   string
	sha256 string
	rows   []Row
}{
	{"testdata/iana-ipv4-special-registry-1.csv", "cbc4b7653c8bc640ba1e8e5d4ebbf11e7c7a1d867e7f1593aad001d3f8632aac", IPv4Rows},
	{"testdata/iana-ipv6-special-registry-1.csv", "bf56772aef97fbf9b4a27c98b8511f2bb1ba96da4b9ccc12ab203a2496faba66", IPv6Rows},
}

var footnote = regexp.MustCompile(`\s*\[\d+\]$`)

// TestTableMatchesRegistrySnapshot compares every IANA row of the table with
// the registry CSV fetched on 2026-11-02: same prefixes in the same order,
// same names, same Globally Reachable values. The CSV files are unchanged
// copies (their SHA-256 is checked).
func TestTableMatchesRegistrySnapshot(t *testing.T) {
	for _, snap := range snapshots {
		raw, err := os.ReadFile(snap.file)
		if err != nil {
			t.Fatal(err)
		}
		if sum := sha256.Sum256(raw); hex.EncodeToString(sum[:]) != snap.sha256 {
			t.Fatalf("%s: SHA-256 changed", snap.file)
		}
		records, err := csv.NewReader(strings.NewReader(string(raw))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		header := strings.Join(records[0], ",")
		if header != "Address Block,Name,RFC,Allocation Date,Termination Date,Source,Destination,Forwardable,Globally Reachable,Reserved-by-Protocol" {
			t.Fatalf("%s: header %q", snap.file, header)
		}
		var want []Row
		for _, rec := range records[1:] {
			reach := map[string]Reach{"True": ReachTrue, "False": ReachFalse, "N/A": ReachNA, "": ReachBlank}[footnote.ReplaceAllString(rec[8], "")]
			if reach == 0 {
				t.Fatalf("%s: unknown Globally Reachable %q", snap.file, rec[8])
			}
			for _, block := range strings.Split(rec[0], ",") {
				prefix := netip.MustParsePrefix(footnote.ReplaceAllString(strings.TrimSpace(block), ""))
				want = append(want, Row{Prefix: prefix, Name: rec[1], Reach: reach, Source: "IANA"})
			}
		}
		var got []Row
		for _, r := range snap.rows {
			if r.Source == "IANA" {
				got = append(got, r)
			} else if r.Reach != ReachNotListed {
				t.Fatalf("added row %s must be ReachNotListed", r.Prefix)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("%s: %d IANA rows in the table, %d in the registry", snap.file, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s row %d: table %+v, registry %+v", snap.file, i, got[i], want[i])
			}
		}
	}
}
