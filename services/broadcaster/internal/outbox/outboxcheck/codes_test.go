// Package outboxcheck_test compares the chain codes that internal/outbox
// names with the pinned SDK and with the chain's svn error set. It lives in
// its own test-only package so the outbox package and its tests import only
// the standard library.
package outboxcheck_test

import (
	"testing"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
)

// registered is the part of a registered chain error the test reads.
type registered interface {
	Codespace() string
	ABCICode() uint32
}

// TestCodesMatchChain: every outbox code constant has the codespace and
// number of the error it names.
func TestCodesMatchChain(t *testing.T) {
	cases := []struct {
		name string
		got  outbox.Code
		want registered
	}{
		{"CodeSDKUnauthorized", outbox.CodeSDKUnauthorized, sdkerrors.ErrUnauthorized},
		{"CodeSDKOutOfGas", outbox.CodeSDKOutOfGas, sdkerrors.ErrOutOfGas},
		{"CodeSDKTimeoutHeight", outbox.CodeSDKTimeoutHeight, sdkerrors.ErrTxTimeoutHeight},
		{"CodeSDKWrongSequence", outbox.CodeSDKWrongSequence, sdkerrors.ErrWrongSequence},
		{"CodeSDKTimeout", outbox.CodeSDKTimeout, sdkerrors.ErrTxTimeout},
		{"CodeSVNUnauthorized", outbox.CodeSVNUnauthorized, svnerr.ErrUnauthorized},
		{"CodeSVNReplay", outbox.CodeSVNReplay, svnerr.ErrReplay},
	}
	for _, c := range cases {
		want := outbox.Code{Codespace: c.want.Codespace(), Number: c.want.ABCICode()}
		if c.got != want {
			t.Errorf("%s = %v, chain has %v", c.name, c.got, want)
		}
	}
	if outbox.CodespaceSDK != sdkerrors.RootCodespace || outbox.CodespaceSVN != svnerr.ModuleName {
		t.Errorf("codespaces %q %q", outbox.CodespaceSDK, outbox.CodespaceSVN)
	}
}
