package msgkit

import (
	"errors"
	"testing"

	errorsmod "cosmossdk.io/errors"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// TestQueryErrCarriesTheProtocolCode: for every protocol failure code the
// query error is still the svnerr error (ABCIInfo codespace svn and the
// number, errors.Is), its gRPC status has the mapped status code, and
// QueryCode reads the code back from the ErrorInfo detail, also after the
// status crosses a gRPC boundary (status.FromError of a status error).
func TestQueryErrCarriesTheProtocolCode(t *testing.T) {
	if OK.QueryErr() != nil {
		t.Fatal("OK has a query error")
	}
	for n := int32(1); n <= int32(svnerr.MaxCode); n++ {
		code := Code(typesv1.ErrorCode(n).String())
		err := code.QueryErr()
		space, number, _ := errorsmod.ABCIInfo(err, false)
		if space != svnerr.ModuleName || number != uint32(n) {
			t.Fatalf("%s: ABCIInfo %s/%d", code, space, number)
		}
		registered, _ := svnerr.ByCode(uint32(n))
		if !errors.Is(err, registered) {
			t.Fatalf("%s: errors.Is the svnerr error is false", code)
		}
		want := codes.Unknown
		switch code {
		case NotFound:
			want = codes.NotFound
		case Malformed, Bounds, OutOfRange:
			want = codes.InvalidArgument
		}
		st, ok := status.FromError(err)
		if !ok || st.Code() != want {
			t.Fatalf("%s: gRPC status %v, want %s", code, st, want)
		}
		got, ok := QueryCode(err)
		if !ok || got != code {
			t.Fatalf("%s: QueryCode %q %v", code, got, ok)
		}
		// A client sees the status rebuilt from the wire form.
		wire := status.FromProto(st.Proto()).Err()
		if got, ok := QueryCode(wire); !ok || got != code {
			t.Fatalf("%s: QueryCode after the wire %q %v", code, got, ok)
		}
	}
	if got := Code("ERROR_CODE_NOPE").QueryErr(); !errors.Is(got, svnerr.ErrMalformed) {
		t.Fatalf("unknown name: %v", got)
	} else if code, ok := QueryCode(got); !ok || code != Malformed {
		t.Fatalf("unknown name detail %q %v", code, ok)
	}
}

func TestQueryCodeRejects(t *testing.T) {
	with := func(details ...*errdetails.ErrorInfo) error {
		st := status.New(codes.NotFound, "x")
		for _, d := range details {
			var err error
			if st, err = st.WithDetails(d); err != nil {
				t.Fatal(err)
			}
		}
		return st.Err()
	}
	good := &errdetails.ErrorInfo{Domain: QueryErrorDomain, Reason: string(NotFound), Metadata: map[string]string{"code": "16"}}
	if code, ok := QueryCode(with(good)); !ok || code != NotFound {
		t.Fatalf("valid detail: %q %v", code, ok)
	}
	cases := map[string]error{
		"nil":                nil,
		"plain error":        errors.New("ERROR_CODE_NOT_FOUND"),
		"svnerr without":     svnerr.ErrNotFound.Wrap("x"),
		"status without":     status.Error(codes.NotFound, "ERROR_CODE_NOT_FOUND"),
		"other domain":       with(&errdetails.ErrorInfo{Domain: "sdk", Reason: string(NotFound), Metadata: map[string]string{"code": "16"}}),
		"unknown reason":     with(&errdetails.ErrorInfo{Domain: QueryErrorDomain, Reason: "ERROR_CODE_NOPE", Metadata: map[string]string{"code": "16"}}),
		"unspecified reason": with(&errdetails.ErrorInfo{Domain: QueryErrorDomain, Reason: "ERROR_CODE_UNSPECIFIED", Metadata: map[string]string{"code": "0"}}),
		"number mismatch":    with(&errdetails.ErrorInfo{Domain: QueryErrorDomain, Reason: string(NotFound), Metadata: map[string]string{"code": "19"}}),
		"no number":          with(&errdetails.ErrorInfo{Domain: QueryErrorDomain, Reason: string(NotFound)}),
		"two details":        with(good, &errdetails.ErrorInfo{Domain: QueryErrorDomain, Reason: string(Bounds), Metadata: map[string]string{"code": "19"}}),
	}
	for name, err := range cases {
		if code, ok := QueryCode(err); ok {
			t.Errorf("%s: read %q", name, code)
		}
	}
}
