package app

import (
	"context"
	"fmt"

	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// registerApplicationMessages rejects every protocol message.
//
// Goal 05 wires the services so an unknown type URL fails decode and a known
// message fails closed. Goals 06-10 replace these handlers. There is no
// MsgCreateValidator and no single-signer admit, disable, or force-withdraw.
func registerApplicationMessages(router *baseapp.MsgServiceRouter) {
	register(router, "svn.authority.v1.Msg", []methodSpec{
		{name: "ProposeChange", msg: &authorityv1.MsgProposeChange{}, err: svnerr.ErrDecisionBlocked},
		{name: "ApproveChange", msg: &authorityv1.MsgApproveChange{}, err: svnerr.ErrDecisionBlocked},
		{name: "ExecuteChange", msg: &authorityv1.MsgExecuteChange{}, err: svnerr.ErrDecisionBlocked},
	})
	register(router, "svn.providers.v1.Msg", []methodSpec{
		{name: "RegisterProvider", msg: &providersv1.MsgRegisterProvider{}, err: svnerr.ErrForbidden},
		{name: "UpdateProvider", msg: &providersv1.MsgUpdateProvider{}, err: svnerr.ErrForbidden},
		{name: "RotateProviderKey", msg: &providersv1.MsgRotateProviderKey{}, err: svnerr.ErrForbidden},
		{name: "DrainProvider", msg: &providersv1.MsgDrainProvider{}, err: svnerr.ErrForbidden},
	})
	register(router, "svn.content.v1.Msg", []methodSpec{
		{name: "RegisterContent", msg: &contentv1.MsgRegisterContent{}, err: svnerr.ErrForbidden},
		{name: "WithdrawContent", msg: &contentv1.MsgWithdrawContent{}, err: svnerr.ErrForbidden},
	})
	register(router, "svn.leases.v1.Msg", []methodSpec{
		{name: "OfferLease", msg: &leasesv1.MsgOfferLease{}, err: svnerr.ErrForbidden},
		{name: "AcceptLease", msg: &leasesv1.MsgAcceptLease{}, err: svnerr.ErrForbidden},
		{name: "RenewLease", msg: &leasesv1.MsgRenewLease{}, err: svnerr.ErrForbidden},
		{name: "EndLease", msg: &leasesv1.MsgEndLease{}, err: svnerr.ErrForbidden},
	})
	register(router, "svn.evidence.v1.Msg", []methodSpec{
		{name: "SubmitAuditBatch", msg: &evidencev1.MsgSubmitAuditBatch{}, err: svnerr.ErrForbidden},
	})
}

type methodSpec struct {
	name string
	msg  proto.Message
	err  error
}

func register(router *baseapp.MsgServiceRouter, service string, methods []methodSpec) {
	descs := make([]grpc.MethodDesc, len(methods))
	for i, method := range methods {
		descs[i] = grpc.MethodDesc{
			MethodName: method.name,
			Handler:    rejectHandler(service, method),
		}
	}
	router.RegisterService(&grpc.ServiceDesc{
		ServiceName: service,
		Methods:     descs,
		HandlerType: (*sdk.Msg)(nil),
	}, struct{}{})
}

func rejectHandler(service string, method methodSpec) grpc.MethodHandler {
	full := fmt.Sprintf("/%s/%s", service, method.name)
	return func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		in := proto.Clone(method.msg)
		if err := dec(in); err != nil {
			return nil, err
		}
		fail := func(context.Context, any) (any, error) {
			return nil, method.err
		}
		if interceptor == nil {
			return fail(ctx, in)
		}
		return interceptor(ctx, in, &grpc.UnaryServerInfo{Server: srv, FullMethod: full}, fail)
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
