package decorators

import (
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
}

func NewFrozenSignerDecorator(cdc codec.Codec, frozen [][]byte, fromHeight int64) FrozenSignerDecorator {
	m := make(map[string]struct{}, len(frozen))
	for _, a := range frozen {
		m[string(a)] = struct{}{}
	}
	return FrozenSignerDecorator{cdc: cdc, frozen: m, fromHeight: fromHeight}
}

func (d FrozenSignerDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
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
	if err := d.checkExec(tx.GetMsgs()); err != nil {
		return ctx, err
	}
	return next(ctx, tx, simulate)
}

func (d FrozenSignerDecorator) checkExec(msgs []sdk.Msg) error {
	for _, msg := range msgs {
		exec, ok := msg.(*authz.MsgExec)
		if !ok {
			continue
		}
		inner, err := exec.GetMessages()
		if err != nil {
			return err
		}
		for _, m := range inner {
			signers, _, err := d.cdc.GetMsgV1Signers(m)
			if err != nil {
				return err
			}
			if err := d.check(signers); err != nil {
				return err
			}
		}
		if err := d.checkExec(inner); err != nil {
			return err
		}
	}
	return nil
}

func (d FrozenSignerDecorator) check(addrs [][]byte) error {
	for _, a := range addrs {
		if _, ok := d.frozen[string(a)]; ok {
			return errorsmod.Wrap(sdkerrors.ErrUnauthorized, fmt.Sprintf("account %s is frozen", sdk.AccAddress(a)))
		}
	}
	return nil
}
