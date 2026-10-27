package decorators

import (
	"bytes"
	"context"
	"fmt"

	errorsmod "cosmossdk.io/errors"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// FrozenSignerDecorator rejects, from FromHeight on, any tx that a frozen
// account signs, pays fees for as fee granter, or acts for through authz
// MsgExec (at any nesting depth). Added by v13 for the keys stolen on
// 2026-10-25. Height-gated so a node replaying older blocks on this binary
// computes the same results as the binary that originally ran them.
type FrozenSignerDecorator struct {
	cdc        codec.Codec
	frozen     map[string]struct{}
	fromHeight int64
	recovery   *FrozenOperatorRecovery
}

// FrozenOperatorRecovery permits narrowly scoped, top-level authz only after
// its separate governance upgrade has executed. Direct stolen-key signatures
// and use of frozen fee granters remain forbidden.
type FrozenOperatorRecovery struct {
	Operator   []byte
	Controller []byte
	Active     func(sdk.Context) (bool, error)
	Allows     func(sdk.Msg) bool
}

func (d FrozenSignerDecorator) WithOperatorRecovery(r FrozenOperatorRecovery) (FrozenSignerDecorator, error) {
	if len(r.Operator) == 0 || len(r.Controller) == 0 || r.Active == nil || r.Allows == nil {
		return d, fmt.Errorf("incomplete operator recovery rule")
	}
	if _, frozen := d.frozen[string(r.Operator)]; !frozen {
		return d, fmt.Errorf("recovery operator is not frozen")
	}
	if _, frozen := d.frozen[string(r.Controller)]; frozen {
		return d, fmt.Errorf("recovery controller is frozen")
	}
	r.Operator = bytes.Clone(r.Operator)
	r.Controller = bytes.Clone(r.Controller)
	d.recovery = &r
	return d, nil
}

func NewFrozenSignerDecorator(cdc codec.Codec, frozen [][]byte, fromHeight int64) FrozenSignerDecorator {
	m := make(map[string]struct{}, len(frozen))
	for _, a := range frozen {
		m[string(a)] = struct{}{}
	}
	return FrozenSignerDecorator{cdc: cdc, frozen: m, fromHeight: fromHeight}
}

func (d FrozenSignerDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// Consent belongs to this ante invocation, even if a caller reuses a
	// context returned while checking a different transaction.
	if ctx.Context() != nil && ctx.Context().Value(recoveryBudgetKey{}) != nil {
		ctx = ctx.WithContext(context.WithValue(ctx.Context(), recoveryBudgetKey{}, nil))
	}
	if d.fromHeight <= 0 || ctx.BlockHeight() < d.fromHeight || len(d.frozen) == 0 {
		return next(ctx, tx, simulate)
	}
	if sigTx, ok := tx.(authsigning.SigVerifiableTx); ok {
		signers, err := sigTx.GetSigners()
		if err != nil {
			return ctx, err
		}
		if err := d.check(signers); err != nil {
			return ctx, err
		}
	}
	if feeTx, ok := tx.(sdk.FeeTx); ok {
		if g := feeTx.FeeGranter(); len(g) > 0 {
			if err := d.check([][]byte{g}); err != nil {
				return ctx, err
			}
		}
	}
	budget := recoveryBudget{}
	if err := d.checkExec(ctx, tx.GetMsgs(), 0, budget); err != nil {
		return ctx, err
	}
	if len(budget) > 0 {
		ctx = ctx.WithContext(context.WithValue(ctx.Context(), recoveryBudgetKey{}, budget))
	}
	return next(ctx, tx, simulate)
}

func (d FrozenSignerDecorator) checkExec(ctx sdk.Context, msgs []sdk.Msg, depth int, budget recoveryBudget) error {
	for _, msg := range msgs {
		exec, ok := msg.(*authz.MsgExec)
		if !ok {
			continue
		}
		inner, err := exec.GetMessages()
		if err != nil {
			return err
		}
		usesRecovery := false
		for _, m := range inner {
			signers, _, err := d.cdc.GetMsgV1Signers(m)
			if err != nil {
				return err
			}
			if err := d.check(signers); err != nil {
				allowed, recoveryErr := d.recoveredExec(ctx, exec, m, signers, depth)
				if recoveryErr != nil {
					return recoveryErr
				}
				if !allowed {
					return err
				}
				usesRecovery = true
			}
		}
		if err := d.checkExec(ctx, inner, depth+1, budget); err != nil {
			return err
		}
		if usesRecovery {
			for _, m := range inner {
				signers, _, err := d.cdc.GetMsgV1Signers(m)
				if err != nil || len(signers) != 1 || !bytes.Equal(signers[0], d.recovery.Operator) || !d.recovery.Allows(m) {
					return errorsmod.Wrap(sdkerrors.ErrUnauthorized, "mixed operator recovery batch")
				}
			}
			key, err := recoveryExecKey(exec)
			if err != nil {
				return err
			}
			budget[key]++
		}
	}
	return nil
}

func (d FrozenSignerDecorator) recoveredExec(ctx sdk.Context, exec *authz.MsgExec, msg sdk.Msg, signers [][]byte, depth int) (bool, error) {
	r := d.recovery
	if r == nil || depth != 0 || len(signers) != 1 || !bytes.Equal(signers[0], r.Operator) || !r.Allows(msg) {
		return false, nil
	}
	grantees, _, err := d.cdc.GetMsgV1Signers(exec)
	if err != nil {
		return false, err
	}
	if len(grantees) != 1 || !bytes.Equal(grantees[0], r.Controller) {
		return false, nil
	}
	return r.Active(ctx)
}

func (d FrozenSignerDecorator) check(addrs [][]byte) error {
	for _, a := range addrs {
		if _, ok := d.frozen[string(a)]; ok {
			return errorsmod.Wrap(sdkerrors.ErrUnauthorized, fmt.Sprintf("account %s is frozen", sdk.AccAddress(a)))
		}
	}
	return nil
}
