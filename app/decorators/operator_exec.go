package decorators

import (
	"bytes"
	"context"
	"crypto/sha256"

	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/cosmos/gogoproto/grpc"
	"github.com/cosmos/gogoproto/proto"
	googlegrpc "google.golang.org/grpc"
)

type recoveryBudgetKey struct{}
type recoveryBudget map[[32]byte]int

func recoveryExecKey(msg *authz.MsgExec) ([32]byte, error) {
	bz, err := proto.Marshal(msg)
	return sha256.Sum256(bz), err
}

// WrapOperatorRecoveryConfigurator retains the SDK services and migrations,
// wrapping only authz Exec. No new message types or codec registrations.
func WrapOperatorRecoveryConfigurator(cfg module.Configurator, cdc codec.Codec, rule FrozenOperatorRecovery) module.Configurator {
	return recoveryConfigurator{Configurator: cfg, server: recoveryRegistration{Server: cfg.MsgServer(), cdc: cdc, rule: rule}}
}

type recoveryConfigurator struct {
	module.Configurator
	server grpc.Server
}

func (c recoveryConfigurator) MsgServer() grpc.Server { return c.server }

type recoveryRegistration struct {
	grpc.Server
	cdc  codec.Codec
	rule FrozenOperatorRecovery
}

func (s recoveryRegistration) RegisterService(desc *googlegrpc.ServiceDesc, impl any) {
	if desc.ServiceName == "cosmos.authz.v1beta1.Msg" {
		impl = recoveryMsgServer{MsgServer: impl.(authz.MsgServer), cdc: s.cdc, rule: s.rule}
	}
	s.Server.RegisterService(desc, impl)
}

type recoveryMsgServer struct {
	authz.MsgServer
	cdc  codec.Codec
	rule FrozenOperatorRecovery
}

// Exec requires a single-use consent budget created by the frozen ante gate
// for the exact, top-level MsgExec. Runtime contract/nested dispatch cannot
// manufacture that budget, or reuse a previously approved execution. Ordinary
// SDK grants and signature verification still apply.
func (s recoveryMsgServer) Exec(goCtx context.Context, exec *authz.MsgExec) (*authz.MsgExecResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	inner, err := exec.GetMessages()
	if err != nil {
		return s.MsgServer.Exec(goCtx, exec)
	}
	usesOperator := false
	for _, msg := range inner {
		signers, _, err := s.cdc.GetMsgV1Signers(msg)
		if err != nil {
			// Still inspect later messages: an invalid companion must not
			// bypass the guard for a recovery action elsewhere in the batch.
			continue
		}
		for _, signer := range signers {
			usesOperator = usesOperator || bytes.Equal(signer, s.rule.Operator)
		}
	}
	if !usesOperator {
		return s.MsgServer.Exec(goCtx, exec)
	}
	active, err := s.rule.Active(ctx)
	if err != nil {
		return nil, err
	}
	if !active {
		return s.MsgServer.Exec(goCtx, exec)
	}
	grantees, _, err := s.cdc.GetMsgV1Signers(exec)
	if err != nil {
		return nil, err
	}
	if len(grantees) != 1 || !bytes.Equal(grantees[0], s.rule.Controller) {
		return nil, sdkerrors.ErrUnauthorized.Wrap("unapproved operator controller")
	}
	for _, msg := range inner {
		signers, _, err := s.cdc.GetMsgV1Signers(msg)
		if err != nil || len(signers) != 1 || !bytes.Equal(signers[0], s.rule.Operator) || !s.rule.Allows(msg) {
			return nil, sdkerrors.ErrUnauthorized.Wrap("action outside operator recovery scope")
		}
	}
	key, err := recoveryExecKey(exec)
	if err != nil {
		return nil, err
	}
	budget, _ := ctx.Context().Value(recoveryBudgetKey{}).(recoveryBudget)
	if budget[key] <= 0 {
		return nil, sdkerrors.ErrUnauthorized.Wrap("operator recovery requires an approved top-level request")
	}
	budget[key]--
	cacheCtx, write := ctx.CacheContext()
	response, err := s.MsgServer.Exec(cacheCtx, exec)
	if err != nil {
		budget[key]++
		return nil, err
	}
	write()
	return response, nil
}
