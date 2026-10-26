package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"cosmossdk.io/collections"
	coreheader "cosmossdk.io/core/header"
	corestore "cosmossdk.io/core/store"
	"cosmossdk.io/math"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/runtime"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/authz"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/stretchr/testify/require"

	"github.com/Crypto-Dungeon/dungeonchain/app/decorators"
	"github.com/Crypto-Dungeon/dungeonchain/app/operatorrecovery"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades"
	v13 "github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13operator"
	"github.com/Crypto-Dungeon/dungeonchain/app/upgrades/v13roles"
)

func TestOperatorRecoveryFrozenBoundary(t *testing.T) {
	disabled := decorators.NewFrozenSignerDecorator(nil, nil, 0)
	_, disabledErr := disabled.AnteHandle(sdk.Context{}, nil, false, func(c sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return c, nil })
	require.NoError(t, disabledErr, "disabled legacy gate remains a no-op")
	app := Setup(t)
	op := sdk.AccAddress([]byte("frozen-account------"))
	otherFrozen := sdk.AccAddress([]byte("frozen-other--------"))
	controller := sdk.AccAddress([]byte("clean-controller----"))
	intruder := sdk.AccAddress([]byte("intruder-account----"))
	active := false
	var activeErr error
	d, err := decorators.NewFrozenSignerDecorator(app.AppCodec(), [][]byte{op, otherFrozen}, 100).WithOperatorRecovery(decorators.FrozenOperatorRecovery{
		Operator: op, Controller: controller,
		Active: func(sdk.Context) (bool, error) { return active, activeErr },
		Allows: func(msg sdk.Msg) bool {
			switch m := msg.(type) {
			case *govtypes.MsgVote:
				return true
			case *banktypes.MsgSend:
				return m.ToAddress == controller.String()
			default:
				return false
			}
		},
	})
	require.NoError(t, err)
	build := func(granter sdk.AccAddress, msgs ...sdk.Msg) sdk.Tx {
		b := app.TxConfig().NewTxBuilder()
		require.NoError(t, b.SetMsgs(msgs...))
		if granter != nil {
			b.SetFeeGranter(granter)
		}
		return b.GetTx()
	}
	run := func(height int64, tx sdk.Tx) error {
		ctx := app.NewContextLegacy(false, cmtproto.Header{Height: height, Time: time.Now().UTC()})
		_, err := d.AnteHandle(ctx, tx, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil })
		return err
	}
	vote := &govtypes.MsgVote{ProposalId: 45, Voter: op.String(), Option: govtypes.OptionYes}
	exec := authz.NewMsgExec(controller, []sdk.Msg{vote})
	require.NoError(t, run(99, build(nil, vote)), "historical replay remains unchanged")
	require.ErrorContains(t, run(100, build(nil, &exec)), "frozen", "no recovery before governance activation")
	active = true
	require.NoError(t, run(100, build(nil, &exec)), "only the ante boundary is passed; SDK still checks its grant")
	require.ErrorContains(t, run(100, build(nil, vote)), "frozen", "old direct signer stays frozen")
	require.ErrorContains(t, run(100, build(op, &exec)), "frozen", "old fee granter stays frozen")
	wrong := authz.NewMsgExec(intruder, []sdk.Msg{vote})
	require.ErrorContains(t, run(100, build(nil, &wrong)), "frozen", "unapproved grantee")
	nested := authz.NewMsgExec(controller, []sdk.Msg{&exec})
	require.ErrorContains(t, run(100, build(nil, &nested)), "frozen", "no nested recovery bypass")
	nestedWrong := authz.NewMsgExec(intruder, []sdk.Msg{&exec})
	require.ErrorContains(t, run(100, build(nil, &nestedWrong)), "frozen")
	otherVote := &govtypes.MsgVote{ProposalId: 45, Voter: otherFrozen.String(), Option: govtypes.OptionYes}
	other := authz.NewMsgExec(controller, []sdk.Msg{otherVote})
	require.ErrorContains(t, run(100, build(nil, &other)), "frozen", "other stolen accounts stay frozen")
	coins := sdk.NewCoins(sdk.NewInt64Coin("udgn", 1))
	safeSend := authz.NewMsgExec(controller, []sdk.Msg{banktypes.NewMsgSend(op, controller, coins)})
	require.NoError(t, run(100, build(nil, &safeSend)))
	unsafeSend := authz.NewMsgExec(controller, []sdk.Msg{banktypes.NewMsgSend(op, intruder, coins)})
	require.ErrorContains(t, run(100, build(nil, &unsafeSend)), "frozen", "no bank recipient escape")
	grantChange := authz.NewMsgExec(controller, []sdk.Msg{&authz.MsgRevoke{Granter: op.String(), Grantee: intruder.String(), MsgTypeUrl: sdk.MsgTypeURL(&banktypes.MsgSend{})}})
	require.ErrorContains(t, run(100, build(nil, &grantChange)), "frozen", "grant administration is excluded")
	activeErr = errors.New("activation state unavailable")
	require.ErrorContains(t, run(100, build(nil, &exec)), "activation state unavailable", "fail closed on keeper error")
}

func TestOperatorRecoveryPolicyAndMarker(t *testing.T) {
	app := Setup(t)
	ctx := app.NewContextLegacy(false, cmtproto.Header{Height: 200, Time: time.Now().UTC()}).WithHeaderInfo(coreheader.Info{Height: 200})
	rule := app.operatorRecoveryRule()
	on, err := rule.Active(ctx)
	require.NoError(t, err)
	require.False(t, on)
	app.UpgradeKeeper.SetUpgradeHandler(operatorrecovery.Name, func(context.Context, upgradetypes.Plan, module.VersionMap) (module.VersionMap, error) {
		return module.VersionMap{}, nil
	})
	require.NoError(t, app.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: operatorrecovery.Name, Height: 200}))
	doneHeight, err := app.UpgradeKeeper.GetDoneHeight(ctx, operatorrecovery.Name)
	require.NoError(t, err)
	require.Equal(t, int64(200), doneHeight)
	on, err = rule.Active(ctx)
	require.NoError(t, err)
	require.True(t, on, "requires completed SDK upgrade marker")
	on, err = rule.Active(ctx.WithBlockHeight(199))
	require.NoError(t, err)
	require.False(t, on, "not active in earlier replay context")
	old, err := app.AccountKeeper.AddressCodec().StringToBytes(operatorrecovery.Operator)
	require.NoError(t, err)
	newAddr, err := app.AccountKeeper.AddressCodec().StringToBytes(operatorrecovery.Controller)
	require.NoError(t, err)
	require.Equal(t, old, rule.Operator)
	require.Equal(t, newAddr, rule.Controller)
	require.True(t, operatorrecovery.Allows(&govtypes.MsgVote{}))
	require.False(t, operatorrecovery.Allows(&authz.MsgExec{}))
	require.False(t, operatorrecovery.Allows(&authz.MsgGrant{}))
	require.True(t, operatorrecovery.Allows(&banktypes.MsgSend{ToAddress: operatorrecovery.Controller}))
	require.False(t, operatorrecovery.Allows(&banktypes.MsgSend{ToAddress: operatorrecovery.Operator}))
	handler := v13operator.CreateUpgradeHandler(nil, nil, &upgrades.AppKeepers{})
	_, err = handler(ctx, upgradetypes.Plan{Name: operatorrecovery.Name, Height: 200}, nil)
	require.ErrorContains(t, err, "approved dungeon-1 plan")
}

func TestOperatorRecoveryBeforeActivationPreservesFailureGas(t *testing.T) {
	app := Setup(t)
	frozen := mustDecodeAccounts(app.AccountKeeper.AddressCodec(), v13.FrozenAccounts())
	oldGate := decorators.NewFrozenSignerDecorator(app.AppCodec(), frozen, v13.ForkHeight)
	newGate, err := oldGate.WithOperatorRecovery(*app.operatorRecoveryRule())
	require.NoError(t, err)
	controller, err := app.AccountKeeper.AddressCodec().StringToBytes(operatorrecovery.Controller)
	require.NoError(t, err)
	msg := &govtypes.MsgVote{ProposalId: 45, Voter: operatorrecovery.Operator, Option: govtypes.OptionYes}
	exec := authz.NewMsgExec(sdk.AccAddress(controller), []sdk.Msg{msg})
	b := app.TxConfig().NewTxBuilder()
	require.NoError(t, b.SetMsgs(&exec))
	run := func(d decorators.FrozenSignerDecorator) uint64 {
		ctx := app.NewContextLegacy(false, cmtproto.Header{Height: v13.ForkHeight + 100, Time: time.Now().UTC()}).WithGasMeter(storetypes.NewGasMeter(1000000))
		_, err := d.AnteHandle(ctx, b.GetTx(), false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil })
		require.ErrorContains(t, err, "frozen")
		return ctx.GasMeter().GasConsumed()
	}
	require.Equal(t, run(oldGate), run(newGate), "pre-activation results/gas must not change")
}

func operatorRecoveryFixture(t *testing.T) (*ChainApp, sdk.Context, *upgrades.AppKeepers, v13operator.Target, sdk.AccAddress, sdk.AccAddress, sdk.AccAddress) {
	return operatorRecoveryTargetFixture(t, false)
}

func operatorRecoveryTargetFixture(t *testing.T, mainnet bool) (*ChainApp, sdk.Context, *upgrades.AppKeepers, v13operator.Target, sdk.AccAddress, sdk.AccAddress, sdk.AccAddress) {
	t.Helper()
	app := Setup(t)
	ctx := app.NewContextLegacy(false, cmtproto.Header{ChainID: "dungeon-1", Height: 2, Time: time.Now().UTC()})
	opKey, ctrlKey, consKey := secp256k1.GenPrivKey().PubKey(), secp256k1.GenPrivKey().PubKey(), ed25519.GenPrivKey().PubKey()
	if mainnet {
		opBytes, err := base64.StdEncoding.DecodeString(operatorrecovery.OperatorPublicKey)
		require.NoError(t, err)
		ctrlBytes, err := base64.StdEncoding.DecodeString(operatorrecovery.ControllerPublicKey)
		require.NoError(t, err)
		consBytes, err := base64.StdEncoding.DecodeString(operatorrecovery.ConsensusPublicKey)
		require.NoError(t, err)
		opKey, ctrlKey, consKey = &secp256k1.PubKey{Key: opBytes}, &secp256k1.PubKey{Key: ctrlBytes}, &ed25519.PubKey{Key: consBytes}
	}
	op, controller := sdk.AccAddress(opKey.Address()), sdk.AccAddress(ctrlKey.Address())
	intruder := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	for _, item := range []struct {
		address    sdk.AccAddress
		controller bool
	}{{op, false}, {controller, true}} {
		a := app.AccountKeeper.NewAccountWithAddress(ctx, item.address)
		if item.controller {
			require.NoError(t, a.SetPubKey(ctrlKey))
		} else {
			require.NoError(t, a.SetPubKey(opKey))
		}
		app.AccountKeeper.SetAccount(ctx, a)
	}
	valAddr, err := app.StakingKeeper.ValidatorAddressCodec().BytesToString(op)
	require.NoError(t, err)
	val, err := stakingtypes.NewValidator(valAddr, consKey, stakingtypes.Description{Moniker: "operator recovery fixture"})
	require.NoError(t, err)
	val.Tokens = math.NewInt(123000000)
	val.DelegatorShares = math.LegacyNewDec(123000000)
	require.NoError(t, app.StakingKeeper.SetValidator(ctx, val))
	require.NoError(t, app.StakingKeeper.SetDelegation(ctx, stakingtypes.Delegation{DelegatorAddress: intruder.String(), ValidatorAddress: valAddr, Shares: val.DelegatorShares}))
	require.NoError(t, app.DistrKeeper.SetWithdrawAddr(ctx, op, op))
	coins := sdk.NewCoins(sdk.NewInt64Coin("udgn", 1000000))
	require.NoError(t, app.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins))
	require.NoError(t, app.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, op, coins))
	require.NoError(t, app.AuthzKeeper.SaveGrant(ctx, intruder, op, authz.NewGenericAuthorization(sdk.MsgTypeURL(&banktypes.MsgSend{})), nil))
	require.NoError(t, app.AuthzKeeper.SaveGrant(ctx, intruder, controller, authz.NewGenericAuthorization(sdk.MsgTypeURL(&govtypes.MsgVote{})), nil))
	k := &upgrades.AppKeepers{AccountKeeper: &app.AccountKeeper, StakingKeeper: app.StakingKeeper, DistrKeeper: &app.DistrKeeper, AuthzKeeper: &app.AuthzKeeper, Codec: app.AppCodec()}
	target := v13operator.Target{Operator: op.String(), Validator: valAddr, Controller: controller.String(), OperatorPublicKey: base64.StdEncoding.EncodeToString(opKey.Bytes()), ControllerPublicKey: base64.StdEncoding.EncodeToString(ctrlKey.Bytes()), ConsensusPublicKey: base64.StdEncoding.EncodeToString(consKey.Bytes())}
	return app, ctx, k, target, op, controller, intruder
}

func TestOperatorRecoveryPreservesStateAndRestoresManagement(t *testing.T) {
	app, ctx, k, target, op, controller, intruder := operatorRecoveryFixture(t)
	val, err := app.StakingKeeper.GetValidator(ctx, sdk.ValAddress(op))
	require.NoError(t, err)
	before := app.AppCodec().MustMarshal(&val)
	delegation, err := app.StakingKeeper.GetDelegation(ctx, intruder, sdk.ValAddress(op))
	require.NoError(t, err)
	coins := app.BankKeeper.GetAllBalances(ctx, op)
	require.NoError(t, v13operator.Recover(ctx, k, target))
	withdraw, err := app.DistrKeeper.GetDelegatorWithdrawAddr(ctx, op)
	require.NoError(t, err)
	require.Equal(t, controller, withdraw)
	for _, msg := range operatorrecovery.MessageTypes() {
		a, expiration := app.AuthzKeeper.GetAuthorization(ctx, controller, op, msg)
		require.NotNil(t, a, msg)
		require.Nil(t, expiration)
	}
	stale, _ := app.AuthzKeeper.GetAuthorization(ctx, intruder, op, sdk.MsgTypeURL(&banktypes.MsgSend{}))
	require.Nil(t, stale, "old operator grants cleared")
	unrelated, _ := app.AuthzKeeper.GetAuthorization(ctx, intruder, controller, sdk.MsgTypeURL(&govtypes.MsgVote{}))
	require.NotNil(t, unrelated, "unrelated grants preserved")
	val, err = app.StakingKeeper.GetValidator(ctx, sdk.ValAddress(op))
	require.NoError(t, err)
	require.Equal(t, before, app.AppCodec().MustMarshal(&val), "validator, keys and stake unchanged")
	current, err := app.StakingKeeper.GetDelegation(ctx, intruder, sdk.ValAddress(op))
	require.NoError(t, err)
	require.Equal(t, delegation, current)
	require.Equal(t, coins, app.BankKeeper.GetAllBalances(ctx, op), "no automatic balance sweep")
	require.NoError(t, v13operator.Recover(ctx, k, target), "idempotent")
	require.NoError(t, app.GovKeeper.SetProposal(ctx, govtypes.Proposal{Id: 45, Status: govtypes.StatusVotingPeriod}))
	require.NoError(t, app.GovKeeper.VotingPeriodProposals.Set(ctx, 45, []byte{1}))
	vote := &govtypes.MsgVote{ProposalId: 45, Voter: op.String(), Option: govtypes.OptionYes}
	voteExec := authz.NewMsgExec(controller, []sdk.Msg{vote})
	_, err = app.AuthzKeeper.Exec(ctx, &voteExec)
	require.NoError(t, err, "controller can cast a real SDK operator vote")
	recorded, err := app.GovKeeper.Votes.Get(ctx, collections.Join(uint64(45), op))
	require.NoError(t, err)
	require.Equal(t, op.String(), recorded.Voter)
	require.Equal(t, govtypes.OptionYes, recorded.Options[0].Option)
	msg := &stakingtypes.MsgEditValidator{ValidatorAddress: target.Validator, Description: stakingtypes.Description{Moniker: "recovered operator"}}
	exec := authz.NewMsgExec(controller, []sdk.Msg{msg})
	_, err = app.AuthzKeeper.Exec(ctx, &exec)
	require.NoError(t, err, "real SDK authz management works")
	val, err = app.StakingKeeper.GetValidator(ctx, sdk.ValAddress(op))
	require.NoError(t, err)
	require.Equal(t, "recovered operator", val.Description.Moniker)
	_, err = app.AuthzKeeper.Exec(ctx, &exec)
	require.NoError(t, err)
	attacker := authz.NewMsgExec(intruder, []sdk.Msg{banktypes.NewMsgSend(op, intruder, sdk.NewCoins(sdk.NewInt64Coin("udgn", 1)))})
	_, err = app.AuthzKeeper.Exec(ctx, &attacker)
	require.Error(t, err, "legacy attacker grant no longer works even in keeper dispatch")
}

// Exercise the registered authz service, including runtime dispatch without
// ante consent; a keeper-only test would miss the service execution boundary.
func TestOperatorRecoveryExecutionGuard(t *testing.T) {
	app, ctx, k, target, op, controller, intruder := operatorRecoveryTargetFixture(t, true)
	height := v13.ForkHeight + 100
	ctx = ctx.WithBlockHeight(height).WithHeaderInfo(coreheader.Info{Height: height, ChainID: "dungeon-1", Time: ctx.BlockTime()})
	require.NoError(t, v13operator.Recover(ctx, k, target))
	require.NoError(t, app.GovKeeper.SetProposal(ctx, govtypes.Proposal{Id: 45, Status: govtypes.StatusVotingPeriod}))
	require.NoError(t, app.GovKeeper.VotingPeriodProposals.Set(ctx, 45, []byte{1}))
	vote := &govtypes.MsgVote{ProposalId: 45, Voter: op.String(), Option: govtypes.OptionYes}
	exec := authz.NewMsgExec(controller, []sdk.Msg{vote})
	handler := app.MsgServiceRouter().Handler(&exec)
	require.NotNil(t, handler)
	// Before activation the registered wrapper retains original SDK behavior.
	_, err := handler(ctx, &exec)
	require.NoError(t, err)
	app.UpgradeKeeper.SetUpgradeHandler(operatorrecovery.Name, func(context.Context, upgradetypes.Plan, module.VersionMap) (module.VersionMap, error) {
		return module.VersionMap{}, nil
	})
	require.NoError(t, app.UpgradeKeeper.ApplyUpgrade(ctx, upgradetypes.Plan{Name: operatorrecovery.Name, Height: height}))
	_, err = handler(ctx, &exec)
	require.ErrorContains(t, err, "approved top-level", "runtime dispatch without root consent is blocked")
	frozen := mustDecodeAccounts(app.AccountKeeper.AddressCodec(), v13.FrozenAccounts())
	gate, err := decorators.NewFrozenSignerDecorator(app.AppCodec(), frozen, v13.ForkHeight).WithOperatorRecovery(*app.operatorRecoveryRule())
	require.NoError(t, err)
	approve := func(msg *authz.MsgExec) sdk.Context {
		b := app.TxConfig().NewTxBuilder()
		require.NoError(t, b.SetMsgs(msg))
		approved, err := gate.AnteHandle(ctx, b.GetTx(), false, func(c sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return c, nil })
		require.NoError(t, err)
		return approved
	}
	approved := approve(&exec)
	ordinary := app.TxConfig().NewTxBuilder()
	require.NoError(t, ordinary.SetMsgs(banktypes.NewMsgSend(controller, intruder, sdk.NewCoins(sdk.NewInt64Coin("udgn", 1)))))
	nextCtx, err := gate.AnteHandle(approved, ordinary.GetTx(), false, func(c sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return c, nil })
	require.NoError(t, err)
	_, err = handler(nextCtx, &exec)
	require.ErrorContains(t, err, "approved top-level", "prior transaction consent cannot leak through a reused context")
	_, err = handler(approved, &exec)
	require.NoError(t, err, "registered service accepts an approved root action")
	_, err = handler(approved, &exec)
	require.ErrorContains(t, err, "approved top-level", "consent cannot be replayed by nested runtime calls")
	safe := authz.NewMsgExec(controller, []sdk.Msg{banktypes.NewMsgSend(op, controller, sdk.NewCoins(sdk.NewInt64Coin("udgn", 1)))})
	approved = approve(&safe)
	escape := authz.NewMsgExec(controller, []sdk.Msg{banktypes.NewMsgSend(op, intruder, sdk.NewCoins(sdk.NewInt64Coin("udgn", 1)))})
	_, err = handler(approved, &escape)
	require.ErrorContains(t, err, "outside operator recovery scope")
	bankURL := sdk.MsgTypeURL(&banktypes.MsgSend{})
	require.NoError(t, app.AuthzKeeper.DeleteGrant(ctx, controller, op, bankURL))
	_, err = handler(approved, &safe)
	require.Error(t, err, "the ordinary SDK grant is still required")
	require.NoError(t, app.AuthzKeeper.SaveGrant(ctx, controller, op, authz.NewGenericAuthorization(bankURL), nil))
	_, err = handler(approved, &safe)
	require.NoError(t, err, "failed execution restores unused consent")
	require.Equal(t, int64(1), app.BankKeeper.GetBalance(ctx, controller, "udgn").Amount.Int64())
	noVote := &govtypes.MsgVote{ProposalId: 45, Voter: op.String(), Option: govtypes.OptionNo}
	batch := authz.NewMsgExec(controller, []sdk.Msg{noVote, banktypes.NewMsgSend(op, controller, sdk.NewCoins(sdk.NewInt64Coin("udgn", 1000001)))})
	_, err = handler(approve(&batch), &batch)
	require.Error(t, err, "late insufficient-funds failure")
	recorded, err := app.GovKeeper.Votes.Get(ctx, collections.Join(uint64(45), op))
	require.NoError(t, err)
	require.Equal(t, govtypes.OptionYes, recorded.Options[0].Option, "a failing recovery batch rolls back its earlier vote")
}

func TestOperatorRecoveryRequiresCompletedRoleUpgrade(t *testing.T) {
	app, ctx, _, _, op, controller, _ := operatorRecoveryTargetFixture(t, true)
	height := v13.ForkHeight + 100
	ctx = ctx.WithBlockHeight(height).WithHeaderInfo(coreheader.Info{Height: height, ChainID: "dungeon-1", Time: ctx.BlockTime()})
	plan := upgradetypes.Plan{Name: operatorrecovery.Name, Height: height}
	err := app.UpgradeKeeper.ApplyUpgrade(ctx, plan)
	require.ErrorContains(t, err, "contract-role upgrade must complete")
	withdrawal, err := app.DistrKeeper.GetDelegatorWithdrawAddr(ctx, op)
	require.NoError(t, err)
	require.Equal(t, op, withdrawal, "rejected ordering changes no payout destination")
	done, err := app.UpgradeKeeper.GetDoneHeight(ctx, operatorrecovery.Name)
	require.NoError(t, err)
	require.Zero(t, done)
	// Only the prerequisite is stubbed; the operator handler is the real handler
	// registered by the application, with the pinned mainnet public identities.
	app.UpgradeKeeper.SetUpgradeHandler(v13roles.Name, func(_ context.Context, _ upgradetypes.Plan, vm module.VersionMap) (module.VersionMap, error) {
		return vm, nil
	})
	prior := ctx.WithBlockHeight(height - 1).WithHeaderInfo(coreheader.Info{Height: height - 1, ChainID: "dungeon-1", Time: ctx.BlockTime()})
	require.NoError(t, app.UpgradeKeeper.ApplyUpgrade(prior, upgradetypes.Plan{Name: v13roles.Name, Height: height - 1}))
	require.NoError(t, app.UpgradeKeeper.ApplyUpgrade(ctx, plan))
	done, err = app.UpgradeKeeper.GetDoneHeight(ctx, operatorrecovery.Name)
	require.NoError(t, err)
	require.Equal(t, height, done)
	withdrawal, err = app.DistrKeeper.GetDelegatorWithdrawAddr(ctx, op)
	require.NoError(t, err)
	require.Equal(t, controller, withdrawal)
	active, err := app.operatorRecoveryRule().Active(ctx)
	require.NoError(t, err)
	require.True(t, active)
}

type failingGrantStore struct {
	corestore.KVStore
	writes *int
}

func (s failingGrantStore) Set(key, value []byte) error {
	(*s.writes)++
	if *s.writes == 2 {
		return errors.New("injected late authz write failure")
	}
	return s.KVStore.Set(key, value)
}

type failingGrantService struct {
	corestore.KVStoreService
	writes *int
}

func (s failingGrantService) OpenKVStore(ctx context.Context) corestore.KVStore {
	return failingGrantStore{s.KVStoreService.OpenKVStore(ctx), s.writes}
}

func TestOperatorRecoveryLateFailureIsAtomic(t *testing.T) {
	app, ctx, k, target, op, controller, intruder := operatorRecoveryFixture(t)
	writes := 0
	faulty := authzkeeper.NewKeeper(failingGrantService{runtime.NewKVStoreService(app.GetKey(authzkeeper.StoreKey)), &writes}, app.AppCodec(), app.MsgServiceRouter(), app.AccountKeeper)
	k.AuthzKeeper = &faulty
	require.ErrorContains(t, v13operator.Recover(ctx, k, target), "late authz write failure")
	require.Equal(t, 2, writes, "fault occurs after a successful grant write and payout change in cache")
	withdraw, err := app.DistrKeeper.GetDelegatorWithdrawAddr(ctx, op)
	require.NoError(t, err)
	require.Equal(t, op, withdraw, "payout rollback")
	for _, msg := range operatorrecovery.MessageTypes() {
		a, _ := app.AuthzKeeper.GetAuthorization(ctx, controller, op, msg)
		require.Nil(t, a, "no partial recovery grant")
	}
	legacy, _ := app.AuthzKeeper.GetAuthorization(ctx, intruder, op, sdk.MsgTypeURL(&banktypes.MsgSend{}))
	require.NotNil(t, legacy, "revocations rollback too")
}

func TestOperatorRecoveryRejectsWrongKeys(t *testing.T) {
	app, ctx, k, target, op, controller, intruder := operatorRecoveryFixture(t)
	target.ControllerPublicKey = base64.StdEncoding.EncodeToString(secp256k1.GenPrivKey().PubKey().Bytes())
	require.ErrorContains(t, v13operator.Recover(ctx, k, target), "public-key precondition")
	withdraw, err := app.DistrKeeper.GetDelegatorWithdrawAddr(ctx, op)
	require.NoError(t, err)
	require.True(t, bytes.Equal(op, withdraw))
	a, _ := app.AuthzKeeper.GetAuthorization(ctx, controller, op, sdk.MsgTypeURL(&govtypes.MsgVote{}))
	require.Nil(t, a)
	a, _ = app.AuthzKeeper.GetAuthorization(ctx, intruder, op, sdk.MsgTypeURL(&banktypes.MsgSend{}))
	require.NotNil(t, a)
}
