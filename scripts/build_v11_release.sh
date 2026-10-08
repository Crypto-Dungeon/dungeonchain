#!/usr/bin/env bash
# Linux amd64 static release candidate, with the exact patched WasmVM archive.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
VERSION=${VERSION:-11.0.0-rc.1}
OUT_DIR=${OUT_DIR:-build/v11-release}
[[ "$(go env GOHOSTOS)/$(go env GOHOSTARCH)" == linux/amd64 ]] || {
  echo 'Run this builder on Linux amd64' >&2; exit 1;
}
command -v musl-gcc >/dev/null || { echo 'Install musl-tools' >&2; exit 1; }
# Ubuntu GCC 12+ libgcc unwinding depends on glibc's _dl_find_object. GCC 11
# supplies a musl-compatible unwinder without a shim or weakened linker check.
if [[ -z "${REALGCC:-}" ]] && command -v gcc-11 >/dev/null; then export REALGCC=gcc-11; fi
[[ "$(go list -m -f '{{.Version}}' github.com/cometbft/cometbft)" == v0.38.26 ]]
[[ "$(go list -m -f '{{.Version}}' github.com/CosmWasm/wasmvm/v2)" == v2.3.5 ]]
commit=$(git rev-parse HEAD)
dirty=$(git status --porcelain --untracked-files=normal)
[[ -z "$dirty" || "$VERSION" == *-rc.* ]] || { echo 'Final releases require a clean checkout' >&2; exit 1; }
mkdir -p "$OUT_DIR"
OUT_DIR=$(cd "$OUT_DIR" && pwd)
archive=libwasmvm_muslc.x86_64.a
sha=3b769d0e6a95724f71c7555c33b91270a207b7119ce7750c69b7de28b247ba14
curl -fsSL --retry 3 "https://github.com/CosmWasm/wasmvm/releases/download/v2.3.5/$archive" -o "$OUT_DIR/$archive"
printf '%s  %s\n' "$sha" "$OUT_DIR/$archive" | sha256sum --check
# wasmvm's muslc build tag looks for libwasmvm_muslc.a on the linker path.
ln -sf "$archive" "$OUT_DIR/libwasmvm_muslc.a"
export CGO_ENABLED=1 CC=musl-gcc CGO_LDFLAGS="-L$OUT_DIR"
binary="$OUT_DIR/dungeond"
go build -mod=readonly -p "${BUILD_PARALLELISM:-6}" -trimpath -tags netgo,muslc,pebbledb \
  -ldflags "-s -w -X github.com/cosmos/cosmos-sdk/version.Name=dungeonchain -X github.com/cosmos/cosmos-sdk/version.AppName=dungeond -X github.com/cosmos/cosmos-sdk/version.Version=$VERSION -X github.com/cosmos/cosmos-sdk/version.Commit=$commit -X github.com/cosmos/cosmos-sdk/version.BuildTags=netgo,muslc,pebbledb -linkmode=external -extldflags '-static -Wl,-z,muldefs'" \
  -o "$binary" ./cmd/dungeond
file "$binary" | grep -q 'statically linked' || { echo 'Binary is not static' >&2; exit 1; }
[[ "$("$binary" query wasm libwasmvm-version)" == 2.3.5 ]]
"$binary" version --long > "$OUT_DIR/version.txt" 2>&1
printf 'version=%s\nplan=v11\ncommit=%s\ngo=%s\ncometbft=0.38.26\nwasmvm=2.3.5\nfull_post_quantum_signing=false\ndirty=%s\n' \
  "$VERSION" "$commit" "$(go version)" "$([[ -n "$dirty" ]] && echo true || echo false)" > "$OUT_DIR/provenance.txt"
(cd "$OUT_DIR" && sha256sum dungeond version.txt provenance.txt > SHA256SUMS)
tar -czf "$OUT_DIR/dungeonchain-$VERSION-linux-amd64.tar.gz" -C "$OUT_DIR" dungeond version.txt provenance.txt SHA256SUMS
echo "Built static candidate: $OUT_DIR/dungeonchain-$VERSION-linux-amd64.tar.gz"
