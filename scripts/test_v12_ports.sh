#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
# Resolve through the chain's lockfile and local replacements. Nested modules
# do not independently select older SDK dependencies.
go test -mod=readonly -p "${TEST_PARALLELISM:-6}" -count=1 -tags pebbledb \
 github.com/cosmos/cosmos-sdk/x/group/... \
 github.com/cosmos/cosmos-sdk/x/params/... \
 github.com/strangelove-ventures/globalfee/x/... \
 github.com/strangelove-ventures/tokenfactory/x/... \
 github.com/bcp-innovations/hyperlane-cosmos/x/...
