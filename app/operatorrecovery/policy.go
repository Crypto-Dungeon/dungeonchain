// Package operatorrecovery describes a separately governed CryptoDungeon
// operator recovery. It never makes the stolen operator key usable again.
package operatorrecovery

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

const (
	Name                = "v13-cryptodungeon-operator"
	Operator            = "dungeon1aj5jlmvqp8dd26rsec6624szthlazdn2vhxak9"
	Validator           = "dungeonvaloper1aj5jlmvqp8dd26rsec6624szthlazdn25leye3"
	Controller          = "dungeon10cr0mweqjyz856kyyy03yzj6av3vmy0p0653lh"
	OperatorPublicKey   = "A+dIhN5RKlRu3ep/q+bZpkt9U3zIufoPkVV6INQbeVFf"
	ControllerPublicKey = "A3SGVfZZEDxBQCc3xX2o1Cov7lwSF8m7ZO+tnUUugPQh"
	ConsensusPublicKey  = "i+lpF0YXMv1yEO/91obZnI2C4QElB/S+L4G6N+dcqNQ="
)

// MessageTypes is ordered, not a map: grant writes are deterministic.
func MessageTypes() []string {
	msgs := []sdk.Msg{
		&govtypes.MsgVote{}, &govtypes.MsgVoteWeighted{},
		&stakingtypes.MsgEditValidator{}, &stakingtypes.MsgRotateConsPubKey{},
		&stakingtypes.MsgDelegate{}, &stakingtypes.MsgUndelegate{},
		&stakingtypes.MsgBeginRedelegate{}, &stakingtypes.MsgCancelUnbondingDelegation{},
		&slashingtypes.MsgUnjail{}, &distrtypes.MsgWithdrawValidatorCommission{},
		&distrtypes.MsgWithdrawDelegatorReward{}, &distrtypes.MsgSetWithdrawAddress{},
		&banktypes.MsgSend{},
	}
	urls := make([]string, len(msgs))
	for i, msg := range msgs {
		urls[i] = sdk.MsgTypeURL(msg)
	}
	return urls
}

// Allows is an additional ante/execution restriction, not an authorization grant.
// Normal authz grant checks and normal message validation still run afterwards.
func Allows(msg sdk.Msg) bool {
	switch m := msg.(type) {
	case *govtypes.MsgVote, *govtypes.MsgVoteWeighted,
		*stakingtypes.MsgEditValidator, *stakingtypes.MsgRotateConsPubKey,
		*stakingtypes.MsgDelegate, *stakingtypes.MsgUndelegate,
		*stakingtypes.MsgBeginRedelegate, *stakingtypes.MsgCancelUnbondingDelegation,
		*slashingtypes.MsgUnjail, *distrtypes.MsgWithdrawValidatorCommission,
		*distrtypes.MsgWithdrawDelegatorReward:
		return true
	case *banktypes.MsgSend:
		return m.ToAddress == Controller
	case *distrtypes.MsgSetWithdrawAddress:
		return m.WithdrawAddress == Controller
	default:
		// No contract execution, grant creation, nested MsgExec or other escape.
		return false
	}
}
