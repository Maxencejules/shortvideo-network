package content

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
)

// Service names from docs/protocol/proto/svn/content/v1.
const (
	MsgServiceName   = "svn.content.v1.Msg"
	QueryServiceName = "svn.content.v1.Query"
)

// Content is svn.content.v1.Query/Content. It reads committed state only:
//
//   - nil request or empty content_id: MALFORMED
//   - content_id outside the id pattern: BOUNDS
//   - content_version 0: MALFORMED
//   - no record for (content_id, content_version): NOT_FOUND
//
// The response holds the record and, for a WITHDRAWN record, its
// tombstone. v0 has no paginated content list (DR-07-07).
func (k Keeper) Content(goCtx context.Context, req *contentv1.QueryContentRequest) (*contentv1.QueryContentResponse, error) {
	ctx, err := sdkContext(goCtx)
	if err != nil {
		return nil, err
	}
	out, code := k.queryContent(ctx, req)
	if code != msgkit.OK {
		// The svnerr error with the protocol code as a gRPC status detail
		// (msgkit.QueryCode reads it).
		return nil, code.QueryErr()
	}
	return out, nil
}

func (k Keeper) queryContent(ctx sdk.Context, req *contentv1.QueryContentRequest) (*contentv1.QueryContentResponse, msgkit.Code) {
	if req == nil {
		return nil, msgkit.Malformed
	}
	if code := msgkit.CheckID(req.GetContentId()); code != msgkit.OK {
		return nil, code
	}
	if req.GetContentVersion() == 0 {
		return nil, msgkit.Malformed
	}
	record, found := k.content(ctx, req.GetContentId(), req.GetContentVersion())
	if !found {
		return nil, msgkit.NotFound
	}
	out := &contentv1.QueryContentResponse{Content: record}
	if tomb, ok := k.tombstone(ctx, req.GetContentId(), req.GetContentVersion()); ok {
		out.Tombstone = tomb
	}
	return out, msgkit.OK
}

// handler builds one unary method handler on the pattern the SDK routers
// call: decode into a fresh request, then run through the interceptor when
// one is given. The Msg router decodes nothing and hands the decoded
// message to the interceptor, so fn uses the interceptor's request.
func handler(fullMethod string, template proto.Message, fn func(context.Context, proto.Message) (proto.Message, error)) grpc.MethodHandler {
	return func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		in := proto.Clone(template)
		if err := dec(in); err != nil {
			return nil, err
		}
		call := func(ctx context.Context, req any) (any, error) {
			typed, ok := req.(proto.Message)
			if !ok || typed.ProtoReflect().Descriptor() != template.ProtoReflect().Descriptor() {
				return nil, msgkit.Malformed.Err()
			}
			return fn(ctx, typed)
		}
		if interceptor == nil {
			return call(ctx, in)
		}
		return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: fullMethod}, call)
	}
}

// MsgServiceDesc is the svn.content.v1.Msg service bound to k. Register it
// on the SDK MsgServiceRouter (module.Configurator.MsgServer()). The
// handlers close over k and ignore the registered server value.
func MsgServiceDesc(k Keeper) *grpc.ServiceDesc {
	return &grpc.ServiceDesc{
		ServiceName: MsgServiceName,
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "RegisterContent",
				Handler: handler("/"+MsgServiceName+"/RegisterContent", &contentv1.MsgRegisterContent{},
					func(ctx context.Context, in proto.Message) (proto.Message, error) {
						return k.RegisterContent(ctx, in.(*contentv1.MsgRegisterContent))
					}),
			},
			{
				MethodName: "WithdrawContent",
				Handler: handler("/"+MsgServiceName+"/WithdrawContent", &contentv1.MsgWithdrawContent{},
					func(ctx context.Context, in proto.Message) (proto.Message, error) {
						return k.WithdrawContent(ctx, in.(*contentv1.MsgWithdrawContent))
					}),
			},
		},
		Metadata: "svn/content/v1/tx.proto",
	}
}

// QueryServiceDesc is the svn.content.v1.Query service bound to k.
// Registered on the GRPCQueryRouter (module.Configurator.QueryServer()), it
// answers ABCI queries at /svn.content.v1.Query/Content and the node's gRPC
// server.
func QueryServiceDesc(k Keeper) *grpc.ServiceDesc {
	return &grpc.ServiceDesc{
		ServiceName: QueryServiceName,
		HandlerType: (*any)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "Content",
				Handler: handler("/"+QueryServiceName+"/Content", &contentv1.QueryContentRequest{},
					func(ctx context.Context, in proto.Message) (proto.Message, error) {
						return k.Content(ctx, in.(*contentv1.QueryContentRequest))
					}),
			},
		},
		Metadata: "svn/content/v1/query.proto",
	}
}
