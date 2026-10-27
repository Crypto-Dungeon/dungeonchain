# CryptoDungeon operator recovery — local draft

This candidate is separate from v13.0.2 and proposal 45. No release tag,
activation height, second proposal or deployment has been created.

The user selected the existing recovery wallet
`dungeon10cr0mweqjyz856kyyy03yzj6av3vmy0p0653lh` as controller.
The affected operator is
`dungeon1aj5jlmvqp8dd26rsec6624szthlazdn2vhxak9`, whose validator identity is
`dungeonvaloper1aj5jlmvqp8dd26rsec6624szthlazdn25leye3`.

## Proposed mechanism

The separate scheduled handler `v13-cryptodungeon-operator` verifies the existing
operator and controller account public keys, validator identity, rotated
consensus key and payout destination. It requires the contract-role upgrade to
have completed first. It then atomically removes legacy grants made by this
operator, redirects payouts to the recovery wallet, and installs scoped SDK
authorization grants from the old operator to the new controller.

The stolen key remains frozen for direct transactions and fee grants. After
the SDK records this separate upgrade as completed, a top-level `MsgExec`
signed by the selected recovery wallet can exercise the permitted operator
actions. Normal signature verification, SDK authorization and message checks
still run. No other frozen account gains an exception. Nested requests,
grant creation, contract execution and transfers to other recipients remain
blocked. An execution guard requires single-use consent from the ante gate for
the exact top-level request, also preventing runtime contract dispatch from
bypassing those restrictions. Failed execution restores unused consent and
rolls back its batch. A read-only activation check preserves pre-activation
failure gas.

Permitted actions cover native and weighted votes, validator editing and future
consensus-key rotation, unjailing, management of the operator's own staking
positions, commission/reward withdrawal, and sending existing operator funds to
the recovery wallet. Payout changes are restricted to that same recovery wallet.
This does not grant control over third-party delegators' accounts or funds.

The validator record, current signing key, stake, delegations, accrued rewards
and account balances are preserved at activation. The handler does not sweep
balances or withdraw commissions automatically. Failed preconditions or a late
write failure apply none of the recovery changes.

## Custody and management

The old wallet profile will stay frozen. Management uses the recovery wallet
through SDK authorization, rather than logging into the old operator wallet.
The recovery private key must remain on a clean signing device. It is not
copied to a validator server. On-chain metadata cannot prove exclusive key
custody. The recovery wallet's account number is a large uint64; tooling must
preserve it exactly and must not cast it to a JavaScript Number.

## Scheduling

The SDK stores only one pending software-upgrade plan. Another software-upgrade
proposal can replace the plan approved by proposal 45. Do not submit this
candidate alongside 45 without an explicit coordinated sequencing/replacement
decision. The default path is to finish 45, then schedule this separate upgrade.
An earlier recovery would require an explicitly reviewed governance replacement
and a new published binary; v13.0.2 and proposal 45 must not be silently changed.

## Required validation before any release

Run the app tests, rejection/permission matrix, pre-activation gas regression,
real SDK management and operator voting, idempotence, precondition failures,
legacy-grant cleanup and late-failure rollback tests. Rehearse against a coherent
post-v13 state copy, verify clean-device control of the selected recovery wallet,
and coordinate validator review before building/publishing or submitting a new
proposal. No mainnet recovery has been executed by this draft.
