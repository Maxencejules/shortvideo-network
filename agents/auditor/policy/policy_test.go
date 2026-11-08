package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"testing"
)

func TestParseEndpointRules(t *testing.T) {
	cases := []struct {
		raw  string
		rule Rule
	}{
		{"http://p.test/", RuleScheme},
		{"ftp://p.test/", RuleScheme},
		{"//p.test/", RuleScheme},
		{"https://u:pw@p.test/", RuleUserinfo},
		{"https://@p.test/", RuleUserinfo},
		{"https://p.test/#frag", RuleFragment},
		{"https://p.test/#", RuleFragment},
		{"https://p.test/x", RulePath},
		{"https://p.test/packages/c1/1", RulePath},
		{"https://p.test/?a=1", RulePath},
		{"https://p.test/?", RulePath},
		{"https://p.test/%2e", RulePath},
		{"https://exämple.test/", RuleHost},
		{"https://xn--exmple-cua.test/", ""},
		{"https:///", RuleHost},
		{"https://p..test/", RuleHost},
		{"https://-p.test/", RuleHost},
		{"https://p_q.test/", RuleHost},
		{"https://p.test./", RuleHost},
		{"https://127.1/", RuleHost},
		{"https://2130706433/", RuleHost},
		{"https://0x7f.1/", RuleHost},
		{"https://0x7f000001/", RuleHost},
		{"https://017700000001/", RuleHost},
		{"https://1e1/", RuleHost},
		{"https://p.0x7f/", RuleHost},
		{"https://p.test1/", ""},
		{"https://1p.test/", ""},
		{"https://[fe80::1%25eth0]/", RuleHost},
		{"https://p%2etest/", RuleURLSyntax},
		{"https://p.test:0/", RulePort},
		{"https://p.test:08443/", RuleURLSyntax},
		{"https://p.test:/", RuleURLSyntax},
		{"https://p.test:65536/", RuleURLSyntax},
		{"https://p.test:x/", RuleURLSyntax},
		{"https://p.test/" + strings.Repeat("a", MaxURLBytes), RuleURLLength},
	}
	for _, tc := range cases {
		_, err := ParseEndpoint(tc.raw)
		if tc.rule == "" {
			if err != nil {
				t.Fatalf("%.60q: %v", tc.raw, err)
			}
			continue
		}
		var r *Refusal
		if !errors.As(err, &r) || r.Rule != tc.rule {
			t.Fatalf("%.60q: want %s, got %v", tc.raw, tc.rule, err)
		}
		if strings.Contains(err.Error(), "p.test") {
			t.Fatalf("%.60q: refusal text carries the URL: %v", tc.raw, err)
		}
	}
}

// A 2048-byte URL is not refused for its length; 2049 bytes is.
func TestURLLengthBoundary(t *testing.T) {
	base := "https://p.test/"
	at := base + strings.Repeat("a", MaxURLBytes-len(base))
	over := at + "a"
	var r *Refusal
	if _, err := ParseEndpoint(at); !errors.As(err, &r) || r.Rule != RulePath {
		t.Fatalf("2048 bytes: %v", err)
	}
	if _, err := ParseEndpoint(over); !errors.As(err, &r) || r.Rule != RuleURLLength {
		t.Fatalf("2049 bytes: %v", err)
	}
}

func TestParseEndpointValues(t *testing.T) {
	cases := []struct {
		raw, host, authority, dial string
		port                       uint16
		literal                    bool
	}{
		{"https://P.Test", "p.test", "p.test", "p.test:443", 443, false},
		{"HTTPS://p.test/", "p.test", "p.test", "p.test:443", 443, false},
		{"https://p.test:8443/", "p.test", "p.test:8443", "p.test:8443", 8443, false},
		{"https://127.0.0.1:9000", "127.0.0.1", "127.0.0.1:9000", "127.0.0.1:9000", 9000, true},
		{"https://[2001:DB8::1]/", "2001:db8::1", "[2001:db8::1]", "[2001:db8::1]:443", 443, true},
	}
	for _, tc := range cases {
		ep, err := ParseEndpoint(tc.raw)
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if ep.Host != tc.host || ep.Port != tc.port || ep.Authority() != tc.authority || ep.DialAuthority() != tc.dial || ep.Literal.IsValid() != tc.literal {
			t.Fatalf("%s: %+v authority %s dial %s", tc.raw, ep, ep.Authority(), ep.DialAuthority())
		}
	}
}

func entry(provider, host, ip string, port uint16) Entry {
	return Entry{ProviderID: provider, Host: host, IP: netip.MustParseAddr(ip), Port: port}
}

func TestAllowlistEntryRules(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.0.0.1", "8.8.8.8", "::1", "fd00::1", "64:ff9b::a00:1", "2001:db8::1", "2606:4700:4700::1111"} {
		if _, err := New([]Entry{entry("p1", "audit.test", ip, 8443)}); err != nil {
			t.Fatalf("valid entry %s: %v", ip, err)
		}
	}
	bad := map[string]Entry{
		"link-local ipv4 (metadata)": entry("p1", "audit.test", "169.254.169.254", 443),
		"link-local ipv6":            entry("p1", "audit.test", "fe80::1", 443),
		"unspecified ipv4":           entry("p1", "audit.test", "0.0.0.0", 443),
		"unspecified ipv6":           entry("p1", "audit.test", "::", 443),
		"multicast ipv4":             entry("p1", "audit.test", "224.0.0.1", 443),
		"multicast ipv6":             entry("p1", "audit.test", "ff02::1", 443),
		"broadcast":                  entry("p1", "audit.test", "255.255.255.255", 443),
		"ipv4-mapped":                entry("p1", "audit.test", "::ffff:127.0.0.1", 443),
		"zoned":                      {ProviderID: "p1", Host: "audit.test", IP: netip.MustParseAddr("fe80::1%eth0"), Port: 443},
		"nat64 of link-local":        entry("p1", "audit.test", "64:ff9b::a9fe:a9fe", 443),
		"6to4 of link-local":         entry("p1", "audit.test", "2002:a9fe:a9fe::1", 443),
		"ipv4-compatible link-local": entry("p1", "audit.test", "::a9fe:a9fe", 443),
		"ipv4-compatible loopback":   entry("p1", "audit.test", "::7f00:1", 443),
		"ipv4-translated link-local": entry("p1", "audit.test", "::ffff:0:a9fe:a9fe", 443),
		"local-use nat64":            entry("p1", "audit.test", "64:ff9b:1::a9fe:a9fe", 443),
		"site-local":                 entry("p1", "audit.test", "fec0::1", 443),
		"discard-only":               entry("p1", "audit.test", "100::1", 443),
		"hex single-label host":      entry("p1", "0x7f000001", "127.0.0.1", 443),
		"upper-case host":            entry("p1", "Audit.test", "127.0.0.1", 443),
		"invalid host":               entry("p1", "audit_test", "127.0.0.1", 443),
		"literal host differs":       entry("p1", "127.0.0.1", "127.0.0.2", 443),
		"port zero":                  entry("p1", "audit.test", "127.0.0.1", 0),
		"bad provider id":            entry("P1", "audit.test", "127.0.0.1", 443),
		"empty provider id":          entry("", "audit.test", "127.0.0.1", 443),
		"invalid ip":                 {ProviderID: "p1", Host: "audit.test", Port: 443},
	}
	for name, e := range bad {
		_, err := New([]Entry{e})
		if !errors.Is(err, ErrEntry) {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%s: %v", name, err)
	}
	dup := entry("p1", "audit.test", "127.0.0.1", 443)
	if _, err := New([]Entry{dup, dup}); !errors.Is(err, ErrEntry) {
		t.Fatalf("duplicate: %v", err)
	}
	many := make([]Entry, MaxEntries+1)
	for i := range many {
		many[i] = entry("p1", "audit.test", "127.0.0.1", uint16(1000+i))
	}
	if _, err := New(many); !errors.Is(err, ErrEntry) {
		t.Fatalf("too many: %v", err)
	}
	if _, err := New(many[:MaxEntries]); err != nil {
		t.Fatalf("%d entries: %v", MaxEntries, err)
	}
}

func vet(t *testing.T, p *Policy, provider, raw string, addrs ...string) ([]netip.AddrPort, error) {
	t.Helper()
	ep, err := ParseEndpoint(raw)
	if err != nil {
		t.Fatal(err)
	}
	var list []netip.Addr
	for _, a := range addrs {
		list = append(list, netip.MustParseAddr(a))
	}
	return p.Vet(provider, ep, list)
}

func wantRule(t *testing.T, err error, rule Rule) {
	t.Helper()
	var r *Refusal
	if !errors.As(err, &r) || r.Rule != rule {
		t.Fatalf("want %s, got %v", rule, err)
	}
}

func TestVet(t *testing.T) {
	p, err := New([]Entry{
		entry("p1", "audit.test", "127.0.0.1", 8443),
		entry("p1", "audit.test", "8.8.4.4", 8443),
		entry("p1", "nat64.test", "64:ff9b::a00:1", 443),
	})
	if err != nil {
		t.Fatal(err)
	}
	ok := func(provider, raw string, want []string, addrs ...string) {
		t.Helper()
		got, err := vet(t, p, provider, raw, addrs...)
		if err != nil {
			t.Fatalf("%s %v: %v", raw, addrs, err)
		}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s %v: got %v want %v", raw, addrs, got, want)
		}
	}
	ok("p1", "https://g.test/", []string{"8.8.8.8:443"}, "8.8.8.8")
	ok("p1", "https://g.test/", []string{"8.8.8.8:443", "[2606:4700:4700::1111]:443"}, "8.8.8.8", "2606:4700:4700::1111")
	ok("p1", "https://g.test/", []string{"8.8.8.8:443"}, "8.8.8.8", "::ffff:8.8.8.8")
	ok("p1", "https://audit.test:8443/", []string{"127.0.0.1:8443"}, "127.0.0.1")
	ok("p1", "https://audit.test:8443/", []string{"127.0.0.1:8443"}, "::ffff:127.0.0.1")
	ok("p1", "https://audit.test:8443/", []string{"8.8.4.4:8443"}, "8.8.4.4")
	ok("p1", "https://nat64.test/", []string{"[64:ff9b::a00:1]:443"}, "64:ff9b::a00:1")

	refused := []struct {
		provider, raw string
		addrs         []string
		rule          Rule
	}{
		{"p1", "https://g.test/", []string{"8.8.8.8", "10.0.0.1"}, RuleAddress},
		{"p1", "https://g.test/", []string{"10.0.0.1", "8.8.8.8"}, RuleAddress},
		{"p1", "https://audit.test:8443/", []string{"127.0.0.1", "127.0.0.2"}, RuleAddress},
		{"p2", "https://audit.test:8443/", []string{"127.0.0.1"}, RuleAddress},
		{"p1", "https://other.test:8443/", []string{"127.0.0.1"}, RuleAddress},
		{"p1", "https://audit.test:9443/", []string{"127.0.0.1"}, RuleAddress},
		{"p1", "https://audit.test/", []string{"127.0.0.1"}, RuleAddress},
		{"p1", "https://g.test:8443/", []string{"8.8.8.8"}, RulePort},
		{"p2", "https://audit.test:8443/", []string{"8.8.4.4"}, RulePort},
		{"p1", "https://g.test/", []string{"::ffff:169.254.169.254"}, RuleAddress},
		{"p1", "https://g.test/", []string{"64:ff9b::808:808"}, RuleAddress},
		{"p1", "https://nat64.test/", []string{"64:ff9b::a00:2"}, RuleAddress},
		{"p1", "https://g.test/", []string{}, RuleNoAddress},
	}
	for _, tc := range refused {
		got, err := vet(t, p, tc.provider, tc.raw, tc.addrs...)
		if got != nil {
			t.Fatalf("%s %v: returned %v", tc.raw, tc.addrs, got)
		}
		wantRule(t, err, tc.rule)
	}
	var many []string
	for i := 0; i <= MaxResolvedAddresses; i++ {
		many = append(many, fmt.Sprintf("8.8.8.%d", i+1))
	}
	_, err = vet(t, p, "p1", "https://g.test/", many...)
	wantRule(t, err, RuleTooManyAddresses)
	if _, err := vet(t, p, "p1", "https://g.test/", many[:MaxResolvedAddresses]...); err != nil {
		t.Fatalf("%d addresses: %v", MaxResolvedAddresses, err)
	}
}

// The refusal text names the rule and the row, never the address.
func TestRefusalTextHasNoAddress(t *testing.T) {
	p, _ := New(nil)
	_, err := vet(t, p, "p1", "https://g.test/", "10.1.2.3")
	if err == nil || strings.Contains(err.Error(), "10.1.2.3") || !strings.Contains(err.Error(), "Private-Use (10.0.0.0/8)") {
		t.Fatalf("%v", err)
	}
}

// MaxURLBytes is bounds.json safety_max_url_bytes.
func TestBoundsMatchProtocolFile(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/protocol/bounds.json")
	if err != nil {
		t.Fatal(err)
	}
	var bounds struct {
		URL *int `json:"safety_max_url_bytes"`
	}
	if err := json.Unmarshal(raw, &bounds); err != nil {
		t.Fatal(err)
	}
	if bounds.URL == nil || *bounds.URL != MaxURLBytes {
		t.Fatalf("safety_max_url_bytes %v, MaxURLBytes %d", bounds.URL, MaxURLBytes)
	}
}
