package decorators_test

import (
	"context"
	"testing"

	sdkmath "cosmossdk.io/math"
	"github.com/cometbft/cometbft/crypto/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/suite"

	"github.com/Crypto-Dungeon/dungeonchain/app/decorators"
)

type AnteTestSuite struct {
	suite.Suite

	ctx sdk.Context
}

func TestAnteTestSuite(t *testing.T) {
	suite.Run(t, new(AnteTestSuite))
}

// Test the change rate decorator with standard edit msgs,
func (s *AnteTestSuite) TestAnteMsgFilterLogic() {
	acc := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	msg := stakingtypes.NewMsgDelegate(acc.String(), acc.String(), sdk.NewCoin("stake", sdkmath.NewInt(1)))

	// test blocking vesting acc from delegating
	ante := decorators.NewMsgStakingVestingDeny(NewMockAccKeeper(&vestingtypes.BaseVestingAccount{}))
	_, err := ante.AnteHandle(s.ctx, decorators.NewMockTx(msg), false, decorators.EmptyAnte)
	s.Require().Contains(err.Error(), "vesting accounts cannot delegate tokens")

	ante = decorators.NewMsgStakingVestingDeny(NewMockAccKeeper(&vestingtypes.PeriodicVestingAccount{}))
	_, err = ante.AnteHandle(s.ctx, decorators.NewMockTx(msg), false, decorators.EmptyAnte)
	s.Require().Contains(err.Error(), "vesting accounts cannot delegate tokens")

	ante = decorators.NewMsgStakingVestingDeny(NewMockAccKeeper(&vestingtypes.ContinuousVestingAccount{}))
	_, err = ante.AnteHandle(s.ctx, decorators.NewMockTx(msg), false, decorators.EmptyAnte)
	s.Require().Contains(err.Error(), "vesting accounts cannot delegate tokens")

	// test not blocking a standard account from delegating
	ante = decorators.NewMsgStakingVestingDeny(NewMockAccKeeper(&authtypes.BaseAccount{}))
	_, err = ante.AnteHandle(s.ctx, decorators.NewMockTx(msg), false, decorators.EmptyAnte)
	s.Require().Nil(err)
}

// mock account keeper
type mockAccKeeper struct {
	accType sdk.AccountI
}

func NewMockAccKeeper(accType sdk.AccountI) mockAccKeeper {
	return mockAccKeeper{
		accType: accType,
	}
}

// GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI
func (m mockAccKeeper) GetAccount(ctx context.Context, addr sdk.AccAddress) sdk.AccountI {
	return m.accType
}
