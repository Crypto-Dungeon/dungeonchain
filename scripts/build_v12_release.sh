#!/usr/bin/env bash
# Linux amd64 static release candidate, with the exact patched WasmVM archive.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
VERSION=${VERSION:-12.0.0-rc.1}
OUT_DIR=${OUT_DIR:-build/v12-release}
[[ "$(go env GOHOSTOS)/$(go env GOHOSTARCH)" == linux/amd64 ]] || {
  echo 'Run this builder on Linux amd64' >&2; exit 1;
}
command -v musl-gcc >/dev/null || { echo 'Install musl-tools' >&2; exit 1; }
# Ubuntu GCC 12+ libgcc unwinding depends on glibc's _dl_find_object. GCC 11
# supplies a musl-compatible unwinder without a shim or weakened linker check.
if [[ -z "${REALGCC:-}" ]] && command -v gcc-11 >/dev/null; then export REALGCC=gcc-11; fi
[[ "$(go list -m -f '{{.Version}}' github.com/cometbft/cometbft)" == v0.40.0 ]]
[[ "$(go list -m -f '{{.Version}}' github.com/CosmWasm/wasmvm/v3)" == v3.0.8 ]]
if git rev-parse --git-dir >/dev/null 2>&1; then
  commit=$(git rev-parse HEAD)
  [[ -z "${COMMIT:-}" || "$COMMIT" == "$commit" ]] || { echo 'COMMIT does not match checkout' >&2; exit 1; }
  dirty=$(git status --porcelain --untracked-files=normal)
else
  # Docker receives an explicit commit from its checkout-based CI context.
  # Archive builds have no .git directory and cannot prove cleanliness.
  commit=${COMMIT:?set full source COMMIT for an archive build}
  [[ "$commit" =~ ^[0-9a-f]{40}$ ]] || exit 1
  dirty=archive-context
fi
[[ -z "$dirty" || "$dirty" == archive-context || "$VERSION" == *-rc.* ]] || { echo 'Final releases require a clean checkout' >&2; exit 1; }
mkdir -p "$OUT_DIR"
OUT_DIR=$(cd "$OUT_DIR" && pwd)
archive=libwasmvm_muslc.x86_64.a
sha=b2299c85d49faccf3dcbb84984f30f55e8870111df98c10f017f86204d007470
curl -fsSL --retry 3 "https://github.com/CosmWasm/wasmvm/releases/download/v3.0.8/$archive" -o "$OUT_DIR/$archive"
printf '%s  %s\n' "$sha" "$OUT_DIR/$archive" | sha256sum --check
# Keep the upstream static archive names on the linker path.
ln -sf "$archive" "$OUT_DIR/libwasmvm_muslc.a"
export CGO_ENABLED=1 CC=musl-gcc CGO_LDFLAGS="-L$OUT_DIR"
binary="$OUT_DIR/dungeond"
go build -mod=readonly -p "${BUILD_PARALLELISM:-6}" -trimpath -tags netgo,muslc,pebbledb \
  -ldflags "-s -w -X github.com/cosmos/cosmos-sdk/version.Name=dungeonchain -X github.com/cosmos/cosmos-sdk/version.AppName=dungeond -X github.com/cosmos/cosmos-sdk/version.Version=$VERSION -X github.com/cosmos/cosmos-sdk/version.Commit=$commit -X github.com/cosmos/cosmos-sdk/version.BuildTags=netgo,muslc,pebbledb -linkmode=external -extldflags '-static -Wl,-z,muldefs'" \
  -o "$binary" ./cmd/dungeond
file "$binary" | grep -q 'statically linked' || { echo 'Binary is not static' >&2; exit 1; }
[[ "$("$binary" query wasm libwasmvm-version)" == 3.0.8 ]]
"$binary" version --long > "$OUT_DIR/version.txt" 2>&1
printf 'version=%s\nplan=v12\ncommit=%s\ngo=%s\ncometbft=0.40.0\nwasmvm=3.0.8\npost_quantum_signing_supported=true\nconsensus_activation=separate_governance_and_key_rotation\ndirty=%s\n' \
  "$VERSION" "$commit" "$(go version)" "$([[ -n "$dirty" ]] && echo true || echo false)" > "$OUT_DIR/provenance.txt"
(cd "$OUT_DIR" && sha256sum dungeond version.txt provenance.txt > SHA256SUMS)
tar -czf "$OUT_DIR/dungeonchain-$VERSION-linux-amd64.tar.gz" -C "$OUT_DIR" dungeond version.txt provenance.txt SHA256SUMS
echo "Built static candidate: $OUT_DIR/dungeonchain-$VERSION-linux-amd64.tar.gz"
