package api

import (
	"crypto/tls"
	"errors"
	"strings"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// principalScheme and principalHost form the caller principal URI SAN
// svn://principal/<id>.
const (
	principalScheme = "svn"
	principalHost   = "principal"
)

// errNoPrincipal is a connection without a verified client certificate
// that carries exactly one principal URI SAN.
var errNoPrincipal = errors.New("api: no verified client certificate with exactly one principal")

// PrincipalOf returns the caller principal of a verified TLS connection:
// the leaf certificate of the verified chain must carry exactly one URI
// SAN, and it must be svn://principal/<id> with id in the protocol id
// pattern and nothing else (no user, port, query or fragment).
func PrincipalOf(state *tls.ConnectionState) (string, error) {
	if state == nil || len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
		return "", errNoPrincipal
	}
	leaf := state.VerifiedChains[0][0]
	if len(leaf.URIs) != 1 {
		return "", errNoPrincipal
	}
	u := leaf.URIs[0]
	if u.Scheme != principalScheme || u.Host != principalHost || u.User != nil || u.Opaque != "" ||
		u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || u.RawPath != "" {
		return "", errNoPrincipal
	}
	id, ok := strings.CutPrefix(u.Path, "/")
	if !ok || !msgkit.ValidID(id) {
		return "", errNoPrincipal
	}
	return id, nil
}
