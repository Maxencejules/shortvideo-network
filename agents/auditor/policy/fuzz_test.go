package policy

import (
	"errors"
	"net/netip"
	"strings"
	"testing"
)

// urlRules are the refusals ParseEndpoint can answer.
var urlRules = []Rule{RuleURLLength, RuleURLSyntax, RuleScheme, RuleUserinfo, RuleFragment, RulePath, RuleHost, RulePort}

// urlPolicySeeds is the seed corpus of FuzzURLPolicy. TestURLPolicySeeds
// checks that it reaches every URL rule and the accepting path.
func urlPolicySeeds() []string {
	return []string{
		"https://p.test/", "https://p.test", "https://P.Test:8443/", "HTTPS://p.test/",
		"https://127.0.0.1:8443/", "https://[::1]:8443/", "https://[::ffff:10.0.0.1]/", "https://xn--exmple-cua.test/",
		"http://p.test/", "//p.test/", "https:p.test", "https://u:pw@p.test/", "https://@p.test/",
		"https://p.test/#frag", "https://p.test/x", "https://p.test/?", "https://p.test/%2e",
		"https://exämple.test/", "https:///", "https://p..test/", "https://127.1/", "https://0x7f000001/",
		"https://[fe80::1%25eth0]/", "https://p%2etest/", "https://p.test:0/", "https://p.test:08443/",
		"https://p.test:/", "https://p.test:65536/", "https://p.test/" + strings.Repeat("a", MaxURLBytes),
		"https://p.test\x00/", "", "\x7f",
	}
}

// TestURLPolicySeeds: the seed corpus reaches every URL rule and at least
// one accepted URL, so a fuzz run starts from every branch of ParseEndpoint.
func TestURLPolicySeeds(t *testing.T) {
	seen := map[Rule]bool{}
	accepted := 0
	for _, raw := range urlPolicySeeds() {
		_, err := ParseEndpoint(raw)
		if err == nil {
			accepted++
			continue
		}
		var r *Refusal
		if !errors.As(err, &r) {
			t.Fatalf("%.60q: %v is not a refusal", raw, err)
		}
		seen[r.Rule] = true
	}
	for _, rule := range urlRules {
		if !seen[rule] {
			t.Errorf("no seed reaches %s", rule)
		}
	}
	if accepted < 5 {
		t.Errorf("only %d accepted seeds", accepted)
	}
}

// FuzzURLPolicy: ParseEndpoint never panics; a refusal is a URL rule whose
// text carries no part of the input; an accepted URL is https with no
// userinfo, query, fragment, or path beyond "/", an ASCII lower-case host,
// and a non-zero port, and its authority parses back to the same endpoint.
func FuzzURLPolicy(f *testing.F) {
	for _, raw := range urlPolicySeeds() {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		ep, err := ParseEndpoint(raw)
		if err != nil {
			var r *Refusal
			if !errors.As(err, &r) {
				t.Fatalf("%q: %v is not a refusal", raw, err)
			}
			known := false
			for _, rule := range urlRules {
				known = known || r.Rule == rule
			}
			if !known || err.Error() != "policy refused: "+string(r.Rule) {
				t.Fatalf("%q: refusal %q", raw, err.Error())
			}
			return
		}
		if len(raw) > MaxURLBytes || len(raw) < len("https://") || !strings.EqualFold(raw[:len("https://")], "https://") {
			t.Fatalf("%q: accepted", raw)
		}
		if strings.ContainsAny(raw, "#?@") {
			t.Fatalf("%q: accepted with a fragment, query, or userinfo", raw)
		}
		if ep.Host == "" || ep.Host != strings.ToLower(ep.Host) || !isASCII(ep.Host) || strings.Contains(ep.Host, "%") {
			t.Fatalf("%q: host %q", raw, ep.Host)
		}
		if ep.Port == 0 || (!ep.explicitPort && ep.Port != DefaultPort) {
			t.Fatalf("%q: port %d", raw, ep.Port)
		}
		if addr, perr := netip.ParseAddr(ep.Host); (perr == nil) != ep.Literal.IsValid() || (perr == nil && addr != ep.Literal) {
			t.Fatalf("%q: literal %v for host %q", raw, ep.Literal, ep.Host)
		}
		again, err := ParseEndpoint("https://" + ep.Authority() + "/")
		if err != nil || again != ep {
			t.Fatalf("%q: authority %q parses to %+v, %v; want %+v", raw, ep.Authority(), again, err, ep)
		}
	})
}
