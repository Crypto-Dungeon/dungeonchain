package decorators

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// FeeExemptionAnteDecorator short-circuits fee checking for a configurable list
// of bech32 addresses. Wire this decorator BEFORE the fee decorator(s) in the
// ante chain and pass skipNext as the handler that represents "everything after
// the fee decorator(s)". When a signer is exempt this decorator calls skipNext
// directly, bypassing the fee decorator. For non-exempt signers it falls through
// to the normal next handler (which IS the fee decorator).
//
// Wiring pattern in NewAnteHandler:
//
//	postFeeHandler := sdk.ChainAnteDecorators(SetPubKey, ValidateSigCount, ...)
//	exemption := NewFeeExemptionAnteDecorator(exemptAddrs, postFeeHandler)
//	// Then exemption sits before globalfeeante.NewFeeDecorator in the outer chain.
//
// Because the SDK chains decorators by threading next pointers, the exemption
// decorator receives globalfee as its "next". When the address is exempt it
// calls skipToHandler (which skips globalfee); otherwise it calls next (globalfee
// runs normally).
type FeeExemptionAnteDecorator struct {
	exemptAddresses map[string]struct{}
	// skipToHandler is called directly when the fee payer is exempt,
	// bypassing whatever decorator comes next in the chain (the fee checker).
	skipToHandler sdk.AnteHandler
}

// NewFeeExemptionAnteDecorator constructs the decorator.
//
//   - addresses: bech32 addresses that pay zero fees.
//   - skipToHandler: the sdk.AnteHandler to invoke when an address is exempt
//     (i.e. the portion of the ante chain AFTER the fee decorator).
func NewFeeExemptionAnteDecorator(addresses []string, skipToHandler sdk.AnteHandler) FeeExemptionAnteDecorator {
	m := make(map[string]struct{}, len(addresses))
	for _, a := range addresses {
		m[a] = struct{}{}
	}
	return FeeExemptionAnteDecorator{
		exemptAddresses: m,
		skipToHandler:   skipToHandler,
	}
}

// AnteHandle implements sdk.AnteDecorator.
// Checks the fee payer (FeeTx.FeePayer) against the exempt list.
// If found, jumps directly to skipToHandler, bypassing all subsequent fee decorators.
func (d FeeExemptionAnteDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if simulate {
		return next(ctx, tx, simulate)
	}

	feeTx, ok := tx.(sdk.FeeTx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "Tx must implement sdk.FeeTx")
	}

	feePayer := feeTx.FeePayer()
	if len(feePayer) > 0 {
		feePayerAddr := sdk.AccAddress(feePayer).String()
		if _, exempt := d.exemptAddresses[feePayerAddr]; exempt {
			// Jump past fee decorator(s) directly to post-fee handlers.
			return d.skipToHandler(ctx, tx, simulate)
		}
	}

	// Not exempt — let the fee decorator (next in chain) run normally.
	return next(ctx, tx, simulate)
}
