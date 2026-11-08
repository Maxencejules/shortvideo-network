package authority

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
)

// QueryServiceName is the query service of the frozen protocol
// (docs/protocol/proto/svn/authority/v1/query.proto).
const QueryServiceName = "svn.authority.v1.Query"

// QueryServiceDesc is svn.authority.v1.Query for the SDK gRPC query router,
// which serves it over ABCI query (/svn.authority.v1.Query/Governance) and
// the node gRPC server. It is written by hand: the protocol module ships
// protobuf-go messages without generated gRPC stubs. The handler decodes
// into a fresh request, applies the interceptor when one is given (the node
// gRPC server passes one; the ABCI query router passes nil), and calls the
// keeper. The srv argument is ignored; the keeper is captured.
func QueryServiceDesc(k Keeper) *grpc.ServiceDesc {
	return &grpc.ServiceDesc{
		ServiceName: QueryServiceName,
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			{MethodName: "Governance", Handler: unary(QueryServiceName, "Governance", k.Governance)},
		},
		Metadata: "svn/authority/v1/query.proto",
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
