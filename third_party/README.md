# Dungeon SDK55 compatibility ports

These are source replacements, not binaries fetched at build time. Their
production state formats, module names and store keys are preserved. The root
`go.mod` owns dependency resolution. Build and test from the root; the nested
module manifests are intentionally minimal and are not standalone release
manifests for those upstream sample daemons.

`upstream-manifest.json` identifies the exact source versions and original Go
module ZIP sums/hashes. Each source directory retains its upstream license.
`legacy-api` contains SDK API0.9.2 group/params descriptors omitted from API1.1;
the protobuf names/wires remain unchanged despite the local Go import path.

| Port | Main adaptation |
| --- | --- |
| group | Apache SDK53 group implementation; log/store v2 and retained API descriptors |
| params | Historical SDK53 params module/API and removed baseapp consensus migration helper |
| capability | Historical capability store; IBC11 does not use it for routing |
| globalfee | SDK55 module imports and log/store APIs; existing fee rules retained |
| tokenfactory | SDK55 imports, WasmVM3 bindings; module state/capabilities retained |
| Hyperlane | SDK55 imports and constructor changes; existing ISM/mailbox/warp interfaces retained |
| wasmd | Patched0.70.4 module code adapted to SDK55 constructors/removed legacy subspaces |

Tests are retained. SDK test configurator helpers removed in SDK55 are provided
locally for the group tests. Hyperlane's test app gains bank end-block/rotation
fee-account wiring. Tokenfactory's old SDK50/IBC8 sample test fixture now wraps
the real Dungeon application, with the fixture-only sudo capability enabled
for tests that explicitly inject a sudo authorizer; production Dungeon keeps
sudo mint disabled. Every fixture gets its own WasmVM home/lock.

The maintained PFM and rate-limit modules come directly from IBC11 and do not
need copied forks. Dungeon tests their actual migrations and middleware order.
`internal/ibctest` is an Apache-licensed adaptation of the upstream IBC11.2
proof/packet test harness using an explicitly supplied Dungeon app.

Run `bash scripts/test_v12_ports.sh` from the root. Root application tests and
the old-to-new binary rehearsal are required in addition to individual module
tests. Future upstream bumps must retain/review these ports until compatible
upstream versions are available; do not remove replacements without proving
state and interface compatibility.
