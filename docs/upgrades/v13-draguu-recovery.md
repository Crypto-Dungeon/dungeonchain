# DRAGUU recovery in v13

The compromised hot wallet owns DRAGUU's sole 1,000-token bootstrap stake.
Freezing its transactions alone would strand governance. Ordinary unstaking
also leaves proposal permissions based on historical voting snapshots.

The v13 recovery must include `RecoverDRAGUU` **before the halted network resumes**.
This changes consensus state at height 22368769. Every validator participating in
the coordinated v13 restart must run the same reviewed recovery implementation.
Do not mix an earlier v13 binary with one containing this recovery.

## Result

- Keep DAO `dungeon15hww9s7t9en85zxu94xkjtxevhm8nqwpd2pg22cj5tcakj89084qvzds5w`.
- Keep the existing `factory/dungeon13x4pynlp86prhcmtns742kgsgu7pjtzj72eycc/draguudao`
  token: 1 billion fixed supply, 6 decimals, mint authority retired.
- Preserve all treasury balances, including 999,999,000 DRAGUU, and the existing
  immutable, DAO-controlled rewards contract. No rewards are paid by this recovery.
- Recover the 1,000 staked bootstrap tokens to the verified recovery address
  `dungeon10cr0mweqjyz856kyyy03yzj6av3vmy0p0653lh`, staked in a fresh voting module.
- Disable the original proposal module in DAO core. Its existing proposals,
  including registration proposal A1, remain historical records and cannot
  execute through core. Recreate any wanted registration proposal with the clean
  wallet after recovery; this upgrade does not execute it or pay rewards.
- Install fresh voting, proposal and pre-propose modules using the exact pinned
  code hashes from the original audited deployment: IDs 116, 117 and 114.
  Core remains immutable; module migration administrators remain DAO core.
- Preserve five-day voting, two-day minimum voting, 20% staked quorum, 67%
  approval, revoting, member-only proposals/execution, seven-day unstaking,
  and the original 100-DRAGUU activation threshold.

The clean bootstrap wallet remains the sole voter until approved community
distributions are made and recipients stake. This is a custody recovery, not
an assertion that governance is already decentralized.

## Preconditions and atomicity

The handler checks the original self-admin, immutable core, code hashes, supply,
reserve, original module identities, and sole compromised-wallet stake. It refuses
if unstaking claims or hooks exist. Unexpected state halts recovery for review.

It uses normal contract execute messages inside a cached SDK context. Temporarily
removing the old module's withdrawal delay permits a single atomic recovery of the
verified bootstrap stake; the seven-day delay is restored before completion.
No raw contract storage, historical snapshots, token metadata or private keys are
edited. The new modules remove old-wallet membership even if a transaction-level
freeze were bypassed. Core itself rejects the disabled old proposal module.

The final checks require unchanged DAO, old-wallet and recovery-wallet liquid
balances, an empty old staking balance, and exactly the recovered clean stake.
A rerun verifies the recovery marker, installed modules and stake and makes no
further changes. The upgrade has both the planned-upgrade and fork entry points.

## Verification

`TestV13ForkOnMainnetState` runs the fork and two following blocks on a disposable
copy of the halted application database. `assertV13DRAGUURecovery` then verifies:

- Supply, treasury, clean/old voting powers, module ownership and approved settings.
- A second recovery is a no-op.
- Core rejects a treasury send originating from the old proposal module, even
  when testing directly without the ante freeze.
- The stolen wallet cannot propose, vote or execute through the fresh modules.
- The clean wallet can propose, vote, wait the approved period and execute an
  actual one-base-unit treasury payment on the disposable copy.

Use a separate closed state copy, its public WASM code directory, and no signer
files. Set `V13_STATE_HOME` and run:

```sh
GOMAXPROCS=4 V13_STATE_HOME=/path/to/disposable/home \
  go test -p 2 -tags pebbledb ./app -run '^TestV13|^TestFrozenSigner' -count=1 -v
```

The test never calls `Commit`, broadcasts a transaction, loads a private key,
starts a node or restarts the network. Passing it is preparation evidence;
live security still requires coordinated v13 activation and on-chain verification.
