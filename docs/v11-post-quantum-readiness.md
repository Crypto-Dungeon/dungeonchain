# Dungeon v11: post-quantum counterparty compatibility

This candidate upgrades CometBFT from 0.38.21 to 0.38.26. Dungeon can verify
ML-DSA-65 signatures from IBC counterparties that adopt post-quantum consensus.
It also includes the intervening CometBFT fixes. **This does not make Dungeon's
accounts or validators quantum resistant.** Dungeon continues using its existing
account algorithms and Ed25519 consensus keys.

## Baseline and scope

The source baseline is `v10.0.0`, commit
`281010e4445cd2f478ed39b397926ce221e22c06`, verified against the live srv11 binary
on October 8, 2026. The `main` branch is older than the live chain; review this
candidate against `upgrade/v10`.

| Component | Live v10 | v11 candidate |
| --- | --- | --- |
| Cosmos SDK | 0.53.7 | 0.53.7 |
| CometBFT | 0.38.21 | 0.38.26 |
| wasmd | 0.60.9 | 0.60.9 |
| WasmVM | 2.3.5 | 2.3.5 |
| IBC | 10.7.0 | 10.7.0 |
| Hyperlane | 1.1.0 | 1.1.0 |
| Global fee | 0.50.1 | 0.50.1 |
| Token factory | 0.50.7-wasmvm2 | 0.50.7-wasmvm2 |

The current WasmVM already contains the CWA-2026-006 fix; v11 retains it.
No modules, stores, bridges, accounts, balances, fee exemptions or consensus
parameters are removed. The `v11` handler runs the module migration manager with
no store additions or deletions. It does not enable ML-DSA for Dungeon consensus.
The existing `upgrade/v10` Makefile fix keeps the `pebbledb` tag in all ordinary
builds, required by srv11's current database configuration.

## Build and validation

On Linux amd64 with Go 1.27.1, GCC and musl-tools:

```bash
go test -mod=readonly -count=1 -tags pebbledb ./...
bash scripts/build_v11_release.sh
```

The builder verifies the official WasmVM 2.3.5 static archive's SHA-256, produces
a statically linked binary, checks the actual loaded WasmVM version, and packages
the binary, version information, source provenance and checksums. The output is
`build/v11-release/dungeonchain-11.0.0-rc.1-linux-amd64.tar.gz`.
A dirty checkout is allowed only for an explicitly marked release candidate;
its provenance records that fact. Build the reviewed candidate from a clean
commit before staging it for an upgrade.

`TestIBCPostQuantumCounterparty` exercises Dungeon's registered Tendermint IBC
client route, including the protobuf header encoding. It checks valid Ed25519
and ML-DSA-65 signed headers, rejects a forged commit, verifies that client state
advances, and checks that Dungeon's consensus key allowance remains Ed25519.

The rehearsal uses a fresh private scratch home and newly generated local keys.
It stops only its own child node. It does not erase an existing home, kill a
process by port, connect to production, or submit a mainnet proposal.

```bash
export BINARY_V10=/absolute/path/to/exact-v10-dungeond
export BINARY_V11="$PWD/build/v11-release/dungeond"
export CW20_WASM="$(go env GOMODCACHE)/github.com/!cosm!wasm/wasmd@v0.60.9/benchmarks/testdata/cw20_base.wasm"
DB_BACKEND=pebbledb bash scripts/test_v11_upgrade.sh
DB_BACKEND=goleveldb bash scripts/test_v11_upgrade.sh
```

Checks include a passed governance software-upgrade proposal, the old binary
halting at its scheduled height, the candidate resuming, unchanged consensus,
staking and global-fee parameters, historical queries, module queries, unsigned
transaction construction and committed economic transactions. A CW20 contract
created on v10 is queried and executed on v11 and survives a candidate restart.
Token-factory balances survive, and mint/burn still execute. An underpriced
transaction must be rejected. Hyperlane mailboxes, collateral tokens and groups
created before the upgrade survive; their creation messages also execute on the
candidate. Broadcast assertions inspect the committed result;
mempool acceptance alone is insufficient.

The GitHub `v11 compatibility rehearsal` workflow repeats the unit suite and
governance rehearsal for both supported database backends. Existing full
interchain tests remain separate. A single-validator local rehearsal is not a
mainnet-state or multi-validator dress rehearsal, and unsigned IBC transactions
do not prove end-to-end packet forwarding.

## Before any mainnet upgrade

Review the exact source and checksum, rehearse a recent mainnet-state copy with
the same database backend, and exercise the active bridge and IBC routes on an
isolated network. Coordinate the upgrade height and binary with validators and
stage the reviewed artifact in their Cosmovisor `upgrades/v11/bin/dungeond`
directories. Governance must approve the plan named `v11`; an RC filename or
version string is not the plan name. This document supplies no height and submits
no proposal. The live chain had no pending upgrade plan at inspection.

Do not restart an old binary against state committed after the upgrade. Use the
chain's coordinated recovery process and verified backups if a rehearsal or
upgrade fails.

## Full post-quantum signing: a separate migration

Cosmos SDK 0.55.0 and CometBFT 0.40.0 support opt-in ML-DSA-65 account and
validator keys plus validator consensus-key rotation. A direct SDK 0.53 to 0.55
upgrade is supported, but it requires both the 0.54 and 0.55 application ports.
It should use a distinct later release/plan if this v11 candidate is adopted.

Dungeon's dependency probe fails before compilation on removed `x/group` and
`x/params` packages. The current application and its external modules also use
old store/log imports and legacy parameter migration APIs. Replacing version
numbers alone is insufficient. The complete migration must preserve:

- Global fees and both branches of the custom fee-exemption ante handler.
- Token-factory state, authority and CosmWasm custom bindings.
- Hyperlane mailbox, ISM, collateral-token, router and message state.
- IBC clients/channels, transfer, ICA, rate limits and packet forwarding.
- Group, NFT, circuit and crisis functionality and their existing state.

The full port needs Store/Log v2, IBC v11, compatible external modules, migration
of any remaining legacy parameters before deleting their store, removal of
textual signing, the mandatory `key_rotation_fee_pool` burner account, updated
staking hooks and cumulative auth/staking migrations. SDK 0.55 requires Go
1.26.5 or newer. The SDK's former group package moved to Cosmos Enterprise; a
licensed replacement or a maintained port of the existing open-source module
must be chosen deliberately. Other deprecated modules moved to contrib.

Enable ML-DSA consensus only after every relevant IBC counterparty can verify
the new signatures. Otherwise their light clients reject Dungeon headers and
packets stop. Keeping Ed25519 enabled supports gradual migration; merely adding
ML-DSA to the allowlist does not replace existing keys or protect their signatures.
Validator rotation, signer support, evidence/slashing across rotation, larger
commit sizes and mixed validator sets require isolated integration tests.
Wallet support and user account-key adoption are separate from consensus-key
migration. No key rotation or new consensus algorithm is activated by v11.

## Primary references

- [CometBFT 0.38.26 release and ML-DSA verification backport](https://github.com/cometbft/cometbft/releases/tag/v0.38.26)
- [SDK 0.55 upgrade guide, direct 0.53 migration and key-rotation requirements](https://github.com/cosmos/cosmos-sdk/blob/v0.55.0/UPGRADING.md)
- [SDK 0.54 upgrade guide and external module/API changes](https://github.com/cosmos/cosmos-sdk/blob/v0.54.3/UPGRADING.md)
- [wasmd 0.70.4 dependency pins](https://github.com/CosmWasm/wasmd/blob/v0.70.4/go.mod)
- [WasmVM 2.3.5 release](https://github.com/CosmWasm/wasmvm/releases/tag/v2.3.5)
