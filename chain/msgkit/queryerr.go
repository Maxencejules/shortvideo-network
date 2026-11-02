package msgkit

import (
	"errors"
	"strconv"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// QueryErrorDomain is the google.rpc.ErrorInfo domain of a rejected
// registry query.
const QueryErrorDomain = "svn"

// queryError is a protocol failure of a query handler. It is the svnerr
// error of its code (Cause and Unwrap return it, so errorsmod.ABCIInfo and
// errors.Is see codespace "svn" and the protocol number) and it carries a
// gRPC status with a typed detail.
type queryError struct {
	err    error
	status *status.Status
}

func (e *queryError) Error() string              { return e.err.Error() }
func (e *queryError) Unwrap() error              { return e.err }
func (e *queryError) Cause() error               { return e.err }
func (e *queryError) GRPCStatus() *status.Status { return e.status }

// QueryErr is the error a query handler returns for c. Its gRPC status code
// is NotFound for NOT_FOUND, InvalidArgument for MALFORMED, BOUNDS, and
// OUT_OF_RANGE, and Unknown otherwise. The status holds one
// google.rpc.ErrorInfo detail: domain "svn", reason the protocol name, and
// metadata "code" the protocol number. The node gRPC server returns that
// status to the client as it is; QueryCode reads it back. Over ABCI query
// (abci_query) the SDK keeps only the gRPC status code, as codespace sdk
// ErrKeyNotFound or ErrInvalidRequest, and the error text: the detail does
// not cross that path. OK returns nil.
func (c Code) QueryErr() error {
	err := c.Err()
	if err == nil {
		return nil
	}
	name := string(c)
	number, known := typesv1.ErrorCode_value[name]
	if !known || number <= 0 {
		// Code.Err maps an unknown name to MALFORMED; the detail says so.
		name, number = string(Malformed), int32(typesv1.ErrorCode_ERROR_CODE_MALFORMED)
	}
	grpcCode := codes.Unknown
	switch Code(name) {
	case NotFound:
		grpcCode = codes.NotFound
	case Malformed, Bounds, OutOfRange:
		grpcCode = codes.InvalidArgument
	}
	st, detailErr := status.New(grpcCode, err.Error()).WithDetails(&errdetails.ErrorInfo{
		Reason:   name,
		Domain:   QueryErrorDomain,
		Metadata: map[string]string{"code": strconv.FormatInt(int64(number), 10)},
	})
	if detailErr != nil {
		// ErrorInfo always marshals; keep the svn error if it ever does not.
		return err
	}
	return &queryError{err: err, status: st}
}

// QueryCode reads the protocol code of a rejected query from its gRPC
// status: the one ErrorInfo detail with domain "svn", whose reason is a
// protocol failure name and whose "code" metadata is that name's number.
// It returns false for an error without such a detail, such as an error a
// client got over ABCI query.
func QueryCode(err error) (Code, bool) {
	var carrier interface{ GRPCStatus() *status.Status }
	if err == nil || !errors.As(err, &carrier) {
		return OK, false
	}
	var found Code
	for _, detail := range carrier.GRPCStatus().Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok || info.GetDomain() != QueryErrorDomain {
			continue
		}
		number, known := typesv1.ErrorCode_value[info.GetReason()]
		if !known || number <= 0 || info.GetMetadata()["code"] != strconv.FormatInt(int64(number), 10) || found != OK {
			return OK, false
		}
		found = Code(info.GetReason())
	}
	return found, found != OK
}
