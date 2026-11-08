package evidence

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
)

// Service names of the frozen protocol (docs/protocol/proto/svn/evidence/v1).
const (
	MsgServiceName   = "svn.evidence.v1.Msg"
	QueryServiceName = "svn.evidence.v1.Query"
)

// The service descriptors are written by hand: the protocol module ships
// protobuf-go messages without generated gRPC stubs. Each handler decodes
// into a fresh request, applies the interceptor when one is given (the SDK
// message router and the node gRPC server pass one; the ABCI query router
// passes nil), and calls the keeper. The srv argument is ignored; the
// keeper is captured.

// MsgServiceDesc is svn.evidence.v1.Msg for the SDK message router. The
// handler is the committed handler of this keeper.
func MsgServiceDesc(k Keeper) *grpc.ServiceDesc {
	return &grpc.ServiceDesc{
		ServiceName: MsgServiceName,
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			{MethodName: "SubmitAuditBatch", Handler: unary(MsgServiceName, "SubmitAuditBatch", k.SubmitAuditBatch)},
		},
		Metadata: "svn/evidence/v1/tx.proto",
	}
}

// QueryServiceDesc is svn.evidence.v1.Query for the SDK gRPC query router,
// which serves it over ABCI query (/svn.evidence.v1.Query/Evidence) and the
// node gRPC server.
func QueryServiceDesc(k Keeper) *grpc.ServiceDesc {
	return &grpc.ServiceDesc{
		ServiceName: QueryServiceName,
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			{MethodName: "Evidence", Handler: unary(QueryServiceName, "Evidence", k.Evidence)},
		},
		Metadata: "svn/evidence/v1/query.proto",
	}
}

// unary adapts one typed method to a grpc.MethodHandler.
func unary[Req proto.Message, Res proto.Message](service, method string, fn func(context.Context, Req) (Res, error)) grpc.MethodHandler {
	full := "/" + service + "/" + method
	return func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		var zero Req
		in, ok := zero.ProtoReflect().New().Interface().(Req)
		if !ok {
			return nil, svnerr.ErrMalformed.Wrapf("%s: cannot build request", full)
		}
		if err := dec(in); err != nil {
			return nil, err
		}
		call := func(ctx context.Context, req any) (any, error) {
			typed, ok := req.(Req)
			if !ok {
				return nil, svnerr.ErrMalformed.Wrapf("%s: request type %T", full, req)
			}
			res, err := fn(ctx, typed)
			if err != nil {
				return nil, err
			}
			return res, nil
		}
		if interceptor == nil {
			return call(ctx, in)
		}
		return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: full}, call)
	}
}
