# Security Policy

dungeonchain is the node software for **dungeon-1**, the Dungeon mainnet. It is a Cosmos SDK chain with CosmWasm smart contracts, maintained by Crypto Dungeon LLC.

## Reporting a vulnerability

**Do not open a public issue for security problems.**

Email **admin@dungeongames.io** with the subject line `SECURITY: dungeonchain`. Please include:

- the affected component (the chain binary, a module, or a CosmWasm contract) and its version or commit
- the steps to reproduce, or a proof of concept
- the impact as you understand it: a chain halt, loss of funds, or a consensus failure

We aim to acknowledge a report within 48 hours and to send an initial assessment within 5 days.

## Disclosure process

- Reports are handled privately until a fix has been deployed to dungeon-1.
- Fixes for critical issues ship as a coordinated validator upgrade. The upgrade is announced to validators before the details are made public.
- We credit reporters in the release notes unless they ask us not to.
- Issues we find in upstream dependencies, including the Cosmos SDK, CometBFT, ibc-go, wasmd and wasmvm, are reported privately to those projects through their own security policies.

## Upstream advisories

dungeon-1 follows the security advisories of the Cosmos SDK, CometBFT, ibc-go and CosmWasm. We commit to sharing any CosmWasm-related issue we discover with the CosmWasm maintainers and the wider CosmWasm community.
