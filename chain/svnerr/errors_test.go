package svnerr_test

import (
	"testing"

	errorsmod "cosmossdk.io/errors"

	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Every svn.types.v1.ErrorCode failure value has one registered error with
// the same number and the same name. UNSPECIFIED (0) is never registered.
func TestErrorRegistryMatchesProto(t *testing.T) {
	values := typesv1.ErrorCode(0).Descriptor().Values()
	if values.Len() != 22 {
		t.Fatalf("ErrorCode has %d values, want 22 (UNSPECIFIED + 21)", values.Len())
	}
	if svnerr.MaxCode != 21 {
		t.Fatalf("MaxCode = %d, want 21", svnerr.MaxCode)
	}
	for i := 0; i < values.Len(); i++ {
		value := values.Get(i)
		number := uint32(value.Number())
		name := string(value.Name())
		registered, ok := svnerr.ByCode(number)
		if number == 0 {
			if ok {
				t.Fatalf("code 0 (%s) must not be registered", name)
			}
			continue
		}
		if !ok {
			t.Fatalf("code %d (%s) is not registered", number, name)
		}
		if registered.Codespace() != svnerr.ModuleName {
			t.Fatalf("code %d codespace %q, want %q", number, registered.Codespace(), svnerr.ModuleName)
		}
		if registered.ABCICode() != number {
			t.Fatalf("code %d registered as %d", number, registered.ABCICode())
		}
		if registered.Error() != name {
			t.Fatalf("code %d name %q, want %q", number, registered.Error(), name)
		}
		// The SDK's global registry resolves the same pair to this error.
		resolved := errorsmod.ABCIError(svnerr.ModuleName, number, "probe")
		if !registered.Is(resolved) {
			t.Fatalf("code %d does not resolve through the SDK registry", number)
		}
		codespace, code, _ := errorsmod.ABCIInfo(registered.Wrap(name), false)
		if codespace != svnerr.ModuleName || code != number {
			t.Fatalf("wrapped code %d gives ABCI (%q, %d)", number, codespace, code)
		}
	}
	if _, ok := svnerr.ByCode(svnerr.MaxCode + 1); ok {
		t.Fatal("code above MaxCode resolved")
	}
}

func TestNamedErrorsUseTheirProtoNumbers(t *testing.T) {
	cases := []struct {
		err  *errorsmod.Error
		code typesv1.ErrorCode
	}{
		{svnerr.ErrUnauthorized, typesv1.ErrorCode_ERROR_CODE_UNAUTHORIZED},
		{svnerr.ErrForbidden, typesv1.ErrorCode_ERROR_CODE_FORBIDDEN},
		{svnerr.ErrReplay, typesv1.ErrorCode_ERROR_CODE_REPLAY},
		{svnerr.ErrDuplicate, typesv1.ErrorCode_ERROR_CODE_DUPLICATE_ID},
		{svnerr.ErrStale, typesv1.ErrorCode_ERROR_CODE_STALE_VERSION},
		{svnerr.ErrExpired, typesv1.ErrorCode_ERROR_CODE_EXPIRED},
		{svnerr.ErrMalformed, typesv1.ErrorCode_ERROR_CODE_MALFORMED},
		{svnerr.ErrUnknownField, typesv1.ErrorCode_ERROR_CODE_UNKNOWN_FIELD},
		{svnerr.ErrOutOfRange, typesv1.ErrorCode_ERROR_CODE_OUT_OF_RANGE},
		{svnerr.ErrOverflow, typesv1.ErrorCode_ERROR_CODE_OVERFLOW},
		{svnerr.ErrUnderflow, typesv1.ErrorCode_ERROR_CODE_UNDERFLOW},
		{svnerr.ErrWrongDomain, typesv1.ErrorCode_ERROR_CODE_WRONG_DOMAIN},
		{svnerr.ErrCapacity, typesv1.ErrorCode_ERROR_CODE_CAPACITY},
		{svnerr.ErrConflict, typesv1.ErrorCode_ERROR_CODE_CONFLICT},
		{svnerr.ErrWithdrawn, typesv1.ErrorCode_ERROR_CODE_WITHDRAWN},
		{svnerr.ErrNotFound, typesv1.ErrorCode_ERROR_CODE_NOT_FOUND},
		{svnerr.ErrDecisionBlocked, typesv1.ErrorCode_ERROR_CODE_DECISION_BLOCKED},
		{svnerr.ErrIntegrity, typesv1.ErrorCode_ERROR_CODE_INTEGRITY},
		{svnerr.ErrBounds, typesv1.ErrorCode_ERROR_CODE_BOUNDS},
		{svnerr.ErrQuorum, typesv1.ErrorCode_ERROR_CODE_QUORUM},
		{svnerr.ErrFeeRejected, typesv1.ErrorCode_ERROR_CODE_FEE_REJECTED},
	}
	if len(cases) != int(svnerr.MaxCode) {
		t.Fatalf("%d named errors, want %d", len(cases), svnerr.MaxCode)
	}
	for _, tc := range cases {
		if tc.err.ABCICode() != uint32(tc.code) || tc.err.Error() != tc.code.String() {
			t.Fatalf("%s registered as %d %q", tc.code, tc.err.ABCICode(), tc.err.Error())
		}
		byCode, ok := svnerr.ByCode(uint32(tc.code))
		if !ok || byCode != tc.err {
			t.Fatalf("ByCode(%d) is not %s", tc.code, tc.code)
		}
	}
}
