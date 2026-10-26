# Proposed follow-up: recover five remaining contract roles

Status: candidate only. Do not publish, install, or restart a validator with this
branch until the validator set agrees to this additional consensus transition.
It is separate from the published v13.0.1 recovery and must have a distinct release.

The halted v13.0.1 node at application height 22368769 still reports the stolen
operations address as the internal owner/admin of:

| Contract | Code ID | Role |
| --- | ---: | --- |
| Active AMM Pool Factory | 160 | owner |
| Dungeon Games Pool Factory v2 | 19 | owner |
| Dungeon Fantasy Sports | 74 | admin |
| castle-siege-v3 | 220 | admin |
| castle-siege-e2e-test | 208 | admin |

The target is the existing recovery address
`dungeon10cr0mweqjyz856kyyy03yzj6av3vmy0p0653lh`. It already holds the Wasm
migration administrator role of the factories and Castle contracts. Fantasy has
no migration administrator and no admin setter, so ordinary migration transactions
cannot complete all five repairs. The old signer remains frozen.

## Candidate transition

The proposed follow-up fork is **22368770**, the first height after the halted
v13.0.1 block. It is gated to dungeon-1. This height is usable only if the network
is still halted at 22368769 when validators agree. If that height has passed,
choose and test a future agreed activation instead; do not roll back production
to force this proposal.

Every participating validator must run the same reviewed follow-up binary before
the agreed height. Mixed recovery logic changes consensus state and can prolong
the halt. Do not modify or replace published v13.0.1 artifacts, its height, or its
recorded application hash. No instructions in this document authorize deploying
the candidate or communicating with validators.

The recovery uses one cached SDK context to change exactly one JSON role string
in each pinned config record. It preserves source bytes outside that string.
It checks each contract address, code ID, code hash, migration administrator,
and the SHA-256 of the entire source configuration before writing. Factory
canonical-address storage and game bech32-address storage are handled separately.
Each smart query must report the recovery address afterwards, or the whole
five-contract correction fails without writing any of it. A rerun is a no-op
only when reversing the role strings reproduces the pinned source hashes.

This is an explicit, narrowly pinned contract-storage state correction.
Fantasy remains immutable: its Wasm administrator is not added or changed.
Contract code, balances, other config fields, game progress, treasury state,
pool toggles, frozen-account rules, validator keys, and bridge pauses are unchanged.
No private key is needed and no contract transaction is broadcast.

## Validation

Unit tests exercise all five real config fixtures, byte preservation, idempotence,
unexpected source state, missing fields, and manifest scope.

`TestV13RolesOnMainnetState` requires an explicitly supplied disposable closed
copy of the halted pre-v13 application database and its public Wasm directory.
It applies the existing v13 recovery in memory, checks failure atomicity and
metadata guards, then verifies all five smart-query roles, other config fields,
code IDs, migration administrators, and balances. It never calls Commit, signs,
broadcasts, or starts a node. The existing v13 mainnet fork test also exercises
the proposed automatic follow-up height.

```sh
GOMAXPROCS=4 V13_STATE_HOME=/path/to/disposable/state \
  go test -p 2 -tags pebbledb ./app/upgrades/v13roles ./app \
  -run '^TestPinnedRole|^TestRecoveryManifest|^TestV13|^TestFrozenSigner' -count=1 -v
```

After coordinated activation, verify each owner/admin via a fresh RPC smart
query, confirm the old signer remains frozen, and verify the wBTC breaker is
using the new owner key. The pools' enabled/disabled settings are a separate
operational check; this correction does not lock or unlock a pool.
