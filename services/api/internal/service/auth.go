package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
)

// Principal is the account resolved from a session. Authorization compares this
// value with the row owner. It does not compare either one with a fixed account id.
type Principal struct {
	AccountID string
	Role      string
}

// Verifier resolves a session token. A transfer token is not a session.
type Verifier interface {
	Verify(token string) (Principal, error)
}

type devVerifier struct {
	byHash map[string]Principal
}

var accountRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// NewDevVerifier builds the development session map. Each token must carry the
// dev-fixture- prefix so a production-looking bearer cannot be dropped into the file.
func NewDevVerifier(fixtures []Fixture) (Verifier, error) {
	out := devVerifier{byHash: map[string]Principal{}}
	for _, fixture := range fixtures {
		if !strings.HasPrefix(fixture.Token, "dev-fixture-") || len(fixture.Token) < 20 {
			return nil, errors.New("dev fixture token must start with dev-fixture-")
		}
		if !accountRE.MatchString(fixture.AccountID) {
			return nil, errors.New("dev fixture account id is not an identifier")
		}
		if fixture.Role != "creator" && fixture.Role != "viewer" {
			return nil, errors.New("dev fixture role must be creator or viewer")
		}
		sum := sha256.Sum256([]byte(fixture.Token))
		key := hex.EncodeToString(sum[:])
		if _, ok := out.byHash[key]; ok {
			return nil, errors.New("duplicate dev fixture token")
		}
		out.byHash[key] = Principal{AccountID: fixture.AccountID, Role: fixture.Role}
	}
	if len(out.byHash) == 0 {
		return nil, errors.New("dev fixtures are empty")
	}
	return out, nil
}

func (v devVerifier) Verify(token string) (Principal, error) {
	if token == "" {
		return Principal{}, errUnauthenticated
	}
	sum := sha256.Sum256([]byte(token))
	principal, ok := v.byHash[hex.EncodeToString(sum[:])]
	if !ok || principal.AccountID == "" || principal.Role == "" {
		return Principal{}, errUnauthenticated
	}
	return principal, nil
}

// CreatorOwns reports whether this principal may act on a row owned by owner.
func CreatorOwns(principal Principal, owner string) bool {
	return principal.Role == "creator" && principal.AccountID != "" && principal.AccountID == owner
}
