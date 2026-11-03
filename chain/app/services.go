package app

import (
	"context"
	"fmt"

	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// registerAuthority wires the x/authority council messages here.
// x/providers, x/content, x/leases, and x/evidence register
// svn.providers.v1.Msg, svn.content.v1.Msg, svn.leases.v1.Msg, and
// svn.evidence.v1.Msg themselves (RegisterServices). There is no
// MsgCreateValidator and no single-signer admit, disable, or force-withdraw
// message (DEC-009).
func registerAuthority(router *baseapp.MsgServiceRouter, keeper authority.Keeper) {
	type call struct {
		name string
		msg  proto.Message
		fn   func(context.Context, proto.Message) (proto.Message, error)
	}
	methods := []call{
		{name: "ProposeChange", msg: &authorityv1.MsgProposeChange{}, fn: func(ctx context.Context, in proto.Message) (proto.Message, error) {
			return keeper.Propose(ctx, in.(*authorityv1.MsgProposeChange))
		}},
		{name: "ApproveChange", msg: &authorityv1.MsgApproveChange{}, fn: func(ctx context.Context, in proto.Message) (proto.Message, error) {
			return keeper.Approve(ctx, in.(*authorityv1.MsgApproveChange))
		}},
		{name: "ExecuteChange", msg: &authorityv1.MsgExecuteChange{}, fn: func(ctx context.Context, in proto.Message) (proto.Message, error) {
			return keeper.Execute(ctx, in.(*authorityv1.MsgExecuteChange))
		}},
	}
	descs := make([]grpc.MethodDesc, len(methods))
	for i, method := range methods {
		descs[i] = grpc.MethodDesc{MethodName: method.name, Handler: acceptHandler(method.msg, method.fn)}
	}
	router.RegisterService(&grpc.ServiceDesc{
		ServiceName: "svn.authority.v1.Msg",
		Methods:     descs,
		HandlerType: (*sdk.Msg)(nil),
	}, struct{}{})
}

func acceptHandler(template proto.Message, fn func(context.Context, proto.Message) (proto.Message, error)) grpc.MethodHandler {
	return func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		in := proto.Clone(template)
		if err := dec(in); err != nil {
			return nil, err
		}
		call := func(ctx context.Context, req any) (any, error) {
			return fn(ctx, req.(proto.Message))
		}
		if interceptor == nil {
			return call(ctx, in)
		}
		return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv}, call)
	}
}

// gogoCheck keeps the registration panic readable in tests.
func init() {
	for _, msg := range protoMessages() {
		if _, ok := any(msg).(gogoproto.Message); !ok {
			panic(fmt.Sprintf("%T is not an SDK message", msg))
		}
	}
}
