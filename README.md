> This branch contains the **v12 full Cosmos SDK55 upgrade candidate**. See
> [migration, validation and activation gates](docs/upgrades/v12.md). Production
> activation requires coordinated governance and validator/IBC preparation.

# Dungeon Chain

## Content Generation

- `make proto-gen` *Generates golang code from proto files, stubs interfaces*

## Testnet

- `make testnet` *IBC testnet from chain <-> local cosmos-hub*
- `make sh-testnet` *Single node, no IBC. quick iteration*

## Local Images

- `make install`      *Builds the chain's binary*
- `make local-image`  *Builds the chain's docker image*

## Running

by default the chain has a 1s timeout commit in [cmd/dungeond/root.go](./cmd/dungeond/root.go). You can change the blocktime by setting the env variable `DUNGEOND_CONSENSUS_TIMEOUT_COMMIT` to some time duration i.e. 500ms, 1s, 4s, etc.