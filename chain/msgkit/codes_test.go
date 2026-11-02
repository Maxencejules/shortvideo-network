package msgkit

import (
	"testing"

	errorsmod "cosmossdk.io/errors"

	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

var allCodes = []Code{
	Unauthorized, Forbidden, Replay, DuplicateID, StaleVersion, Expired, Malformed,
	UnknownField, OutOfRange, Overflow, Underflow, WrongDomain, Capacity, Conflict,
	Withdrawn, NotFound, DecisionBlocked, Integrity, Bounds, Quorum, FeeRejected,
}

// The Code constants are exactly the 21 protocol failure names, in enum
// order, and each maps to the svnerr error with the same number.
func TestCodesMatchProtoEnum(t *testing.T) {
	values := typesv1.ErrorCode(0).Descriptor().Values()
	if len(allCodes) != values.Len()-1 {
		t.Fatalf("%d codes, enum has %d failure values", len(allCodes), values.Len()-1)
	}
	for i, code := range allCodes {
		number := int32(i + 1)
		if name := typesv1.ErrorCode(number).String(); string(code) != name {
			t.Fatalf("code %d is %q, enum name %q", number, code, name)
		}
		err := code.Err()
		codespace, abciCode, log := errorsmod.ABCIInfo(err, false)
		if codespace != svnerr.ModuleName || abciCode != uint32(number) {
			t.Fatalf("%s gives ABCI (%q, %d)", code, codespace, abciCode)
		}
		if log == "" {
			t.Fatalf("%s has an empty log", code)
		}
		registered, _ := svnerr.ByCode(uint32(number))
		if !registered.Is(err) {
			t.Fatalf("%s does not wrap %v", code, registered)
		}
	}
}

func TestCodeErrSuccessAndUnknown(t *testing.T) {
	if OK.Err() != nil {
		t.Fatal("OK.Err() is not nil")
	}
	for _, bad := range []Code{"ERROR_CODE_UNSPECIFIED", "ERROR_CODE_NOPE", "forbidden", " "} {
		err := bad.Err()
		codespace, code, _ := errorsmod.ABCIInfo(err, false)
		if codespace != svnerr.ModuleName || code != uint32(typesv1.ErrorCode_ERROR_CODE_MALFORMED) {
			t.Fatalf("%q gives ABCI (%q, %d), want MALFORMED", bad, codespace, code)
		}
	}
}
