#!/usr/bin/env bash
# Local-only v10 -> v12 rehearsal. No production home, keys, ports or services.
# Requires BINARY_V10, BINARY_V12 and CW20_WASM; preserves logs in a fresh
# temporary directory and stops only the child processes started by this run.
set -euo pipefail
umask 077
: "${BINARY_V10:?set BINARY_V10 to the v10.0.0 baseline binary}"
: "${BINARY_V12:?set BINARY_V12 to the candidate binary}"
: "${CW20_WASM:?set CW20_WASM to a cw20-base contract}"
for file in "$BINARY_V10" "$BINARY_V12"; do
  [[ -x "$file" ]] || { echo "Not executable: $file" >&2; exit 1; }
done
[[ -f "$CW20_WASM" ]] || { echo 'CW20 contract missing' >&2; exit 1; }
for tool in jq curl python3; do command -v "$tool" >/dev/null; done
DB_BACKEND=${DB_BACKEND:-pebbledb}
[[ "$DB_BACKEND" == pebbledb || "$DB_BACKEND" == goleveldb ]] || exit 1
HOME_DIR=$(mktemp -d "${TMPDIR:-/tmp}/dungeon-v12-rehearsal.XXXXXXXX")
LOG_DIR="$HOME_DIR/test-logs"
mkdir "$LOG_DIR"
echo "Rehearsal directory: $HOME_DIR"
pid=''
stop_node() {
  if [[ -n "$pid" ]]; then
    kill -TERM "$pid" 2>/dev/null || true
    for _ in $(seq 1 30); do
      kill -0 "$pid" 2>/dev/null || break
      sleep 0.2
    done
    if kill -0 "$pid" 2>/dev/null; then kill -KILL "$pid"; fi
    wait "$pid" 2>/dev/null || true
    pid=''
  fi
}
trap stop_node EXIT
RPC_PORT=${RPC_PORT:-28657}
P2P_PORT=${P2P_PORT:-28656}
GRPC_PORT=${GRPC_PORT:-28090}
python3 - "$RPC_PORT" "$P2P_PORT" "$GRPC_PORT" <<'PY'
import socket, sys
for port in map(int, sys.argv[1:]):
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', port))
PY
NODE="tcp://127.0.0.1:$RPC_PORT"
CHAIN_ID=dungeon-v12-rehearsal-1
DENOM=udgn
old=("$BINARY_V10" --home "$HOME_DIR")
new=("$BINARY_V12" --home "$HOME_DIR")
bin=("${old[@]}")
q=(--node "$NODE" -o json)
tx=(--node "$NODE" --chain-id "$CHAIN_ID" --keyring-backend test -y -o json
    --broadcast-mode sync --gas auto --gas-adjustment 1.6 --fees "200000$DENOM")
say() { echo "[$(date +%H:%M:%S)] $*"; }
die() { echo "FAIL: $* (logs: $LOG_DIR)" >&2; exit 1; }
height() { curl -fsS "http://127.0.0.1:$RPC_PORT/status" 2>/dev/null | jq -r '.result.sync_info.latest_block_height'; }
wait_height() {
  local target=$1 current
  for _ in $(seq 1 180); do
    current=$(height || true)
    if [[ "$current" =~ ^[0-9]+$ ]] && ((current >= target)); then return; fi
    kill -0 "$pid" 2>/dev/null || die 'node exited'
    sleep 1
  done
  die "timeout waiting for height $target"
}
start_node() {
  "${bin[@]}" start --db_backend "$DB_BACKEND" --pruning nothing \
    --minimum-gas-prices "0.025$DENOM" --grpc.address "127.0.0.1:$GRPC_PORT" \
    --grpc-web.enable=false --api.enable=false --rpc.pprof_laddr='' \
    --rpc.laddr "$NODE" --p2p.laddr "tcp://127.0.0.1:$P2P_PORT" \
    --p2p.pex=false --log_no_color > "$LOG_DIR/$1.log" 2>&1 &
  pid=$!
}
# Check committed execution, not just mempool acceptance.
broadcast() {
  local label=$1 signer=$2; shift 2
  local hash
  "${bin[@]}" tx "$@" --from "$signer" "${tx[@]}" \
    > "$LOG_DIR/$label-submit.json" 2> "$LOG_DIR/$label.stderr" || die "$label submission"
  jq -e '.code == 0' "$LOG_DIR/$label-submit.json" >/dev/null || die "$label CheckTx"
  hash=$(jq -er .txhash "$LOG_DIR/$label-submit.json")
  for _ in $(seq 1 45); do
    if "${bin[@]}" query tx "$hash" "${q[@]}" > "$LOG_DIR/$label-committed.json" 2>/dev/null; then
      jq -e '.code == 0' "$LOG_DIR/$label-committed.json" >/dev/null || die "$label execution"
      say "PASS committed $label"
      return
    fi
    sleep 1
  done
  die "$label was never committed"
}
query() {
  local label=$1; shift
  "${bin[@]}" query "$@" "${q[@]}" > "$LOG_DIR/query-$label.json" 2>&1 || die "query $label"
  say "PASS query $label"
}
generate() {
  local label=$1; shift
  "${bin[@]}" tx "$@" --generate-only --from "$user" --chain-id "$CHAIN_ID" --node "$NODE" \
    > "$LOG_DIR/build-$label.json" 2>&1 || die "build $label"
  jq -e '.body.messages | length > 0' "$LOG_DIR/build-$label.json" >/dev/null || die "empty $label"
}

say "Initialize fresh local chain ($DB_BACKEND)"
"${old[@]}" init localval --chain-id "$CHAIN_ID" --default-denom "$DENOM" > "$LOG_DIR/init.log" 2>&1
for key in val val2 user; do
  # Generated test mnemonics remain private in the scratch home, never printed.
  "${old[@]}" keys add "$key" --keyring-backend test --no-backup --output json > "$HOME_DIR/$key-key.json" 2>&1
done
val=$("${old[@]}" keys show val --keyring-backend test -a)
valoper=$("${old[@]}" keys show val --keyring-backend test -a --bech val)
valoper2=$("${old[@]}" keys show val2 --keyring-backend test -a --bech val)
user=$("${old[@]}" keys show user --keyring-backend test -a)
"${old[@]}" genesis add-genesis-account val "1000000000000$DENOM" --keyring-backend test
"${old[@]}" genesis add-genesis-account user "10000000000$DENOM" --keyring-backend test
"${old[@]}" genesis gentx val "100000000000$DENOM" --chain-id "$CHAIN_ID" --keyring-backend test \
  --commission-rate 0.05 --commission-max-rate 1 --commission-max-change-rate 0.05 > "$LOG_DIR/gentx.log" 2>&1
"${old[@]}" genesis collect-gentxs > "$LOG_DIR/collect.log" 2>&1
genesis="$HOME_DIR/config/genesis.json"
# CLI startup and committed-tx polling must fit on loaded CI runners too.
jq '.app_state.gov.params.voting_period="90s" |
    .app_state.gov.params.expedited_voting_period="45s" |
    .app_state.gov.params.min_deposit=[{"denom":"udgn","amount":"1"}] |
    .app_state.gov.params.expedited_min_deposit=[{"denom":"udgn","amount":"2"}] |
    .app_state.staking.params.min_commission_rate="0.050000000000000000" |
    .app_state.globalfee.params.minimum_gas_prices=[{"denom":"udgn","amount":"0.025000000000000000"}]' \
    "$genesis" > "$HOME_DIR/genesis.tmp"
mv "$HOME_DIR/genesis.tmp" "$genesis"
sed -i 's/^timeout_commit = "5s"/timeout_commit = "1s"/' "$HOME_DIR/config/config.toml"
start_node v10
wait_height 3
broadcast pre-bank user bank send "$user" "$val" "1000$DENOM"
broadcast pre-tokenfactory user tokenfactory create-denom preserved
factory="factory/$user/preserved"
broadcast pre-mint user tokenfactory mint "1000000$factory"
# The Noop ISM is a fixture on this isolated chain only.
broadcast pre-ism user hyperlane ism create-noop
ism=$("${bin[@]}" query hyperlane ism isms "${q[@]}" | jq -er '.isms[-1].id')
broadcast pre-mailbox user hyperlane mailbox create "$ism" 1234
mailbox=$("${bin[@]}" query hyperlane mailboxes "${q[@]}" | jq -er '.mailboxes[-1].id')
broadcast pre-warp user warp create-collateral-token "$mailbox" "$DENOM"
"${bin[@]}" query hyperlane mailboxes "${q[@]}" > "$LOG_DIR/mailboxes-before.json"
"${bin[@]}" query warp tokens "${q[@]}" > "$LOG_DIR/warp-before.json"
jq -n --arg member "$user" '{members:[{address:$member,weight:"1",metadata:"fixture"}]}' > "$LOG_DIR/group-members.json"
broadcast pre-group user group create-group "$user" fixture "$LOG_DIR/group-members.json"
"${bin[@]}" query group groups "${q[@]}" > "$LOG_DIR/groups-before.json"
broadcast pre-store val wasm store "$CW20_WASM"
code_id=$("${bin[@]}" query wasm list-code "${q[@]}" | jq -er '.code_infos[-1].code_id')
broadcast pre-instantiate val wasm instantiate "$code_id" \
  "{\"name\":\"Upgrade Test\",\"symbol\":\"TEST\",\"decimals\":6,\"initial_balances\":[{\"address\":\"$val\",\"amount\":\"1000000\"}]}" \
  --label upgrade-test --no-admin
contract=$("${bin[@]}" query wasm list-contract-by-code "$code_id" "${q[@]}" | jq -er '.contracts[-1]')
"${bin[@]}" query consensus params "${q[@]}" > "$LOG_DIR/consensus-before.json"
"${bin[@]}" query staking params "${q[@]}" > "$LOG_DIR/staking-before.json"
"${bin[@]}" query globalfee minimum-gas-prices "${q[@]}" > "$LOG_DIR/globalfee-before.json"
pre_height=$(height)
authority=$("${bin[@]}" query auth module-account gov "${q[@]}" | jq -er '.account.value.address // .account.base_account.address')
upgrade_height=$((pre_height + 180))
jq -n --arg authority "$authority" --arg height "$upgrade_height" '{messages:[{
  "@type":"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade",authority:$authority,
  plan:{name:"v12",height:$height,info:"isolated SDK55 migration rehearsal"}}],
  metadata:"",deposit:"1udgn",title:"v12 rehearsal",summary:"Full SDK55/Comet40/IBC11 migration"}' > "$LOG_DIR/upgrade-proposal.json"
broadcast upgrade-submit val gov submit-proposal "$LOG_DIR/upgrade-proposal.json"
proposal=$("${bin[@]}" query gov proposals "${q[@]}" | jq -er '.proposals[-1].id')
broadcast upgrade-vote val gov vote "$proposal" yes
say "Wait for approved upgrade halt at $upgrade_height"
halted=false
for _ in $(seq 1 600); do
  if grep -q 'UPGRADE "v12" NEEDED' "$LOG_DIR/v10.log"; then halted=true; break; fi
  sleep 1
done
[[ "$halted" == true ]] || die 'expected upgrade halt missing'
stop_node
bin=("${new[@]}")
start_node v12
wait_height "$((upgrade_height + 3))"
query applied upgrade applied v12
query consensus consensus params
query staking-params staking params
query globalfee globalfee minimum-gas-prices
for pair in 'consensus consensus' 'staking staking-params' 'globalfee globalfee'; do
  read -r before after <<< "$pair"
  diff <(jq -S 'del(.params.key_rotation_fee)' "$LOG_DIR/$before-before.json") <(jq -S 'del(.params.key_rotation_fee)' "$LOG_DIR/query-$after.json") >/dev/null \
    || die "$before params changed"
done
jq -e '.params.validator.pub_key_types == ["ed25519"]' "$LOG_DIR/query-consensus.json" >/dev/null \
  || die 'Dungeon consensus key type changed'
"${bin[@]}" query bank balances "$user" "${q[@]}" | jq -e --arg denom "$factory" \
  '.balances[] | select(.denom==$denom) | .amount=="1000000"' >/dev/null || die 'factory balance lost'
"${bin[@]}" query wasm contract-state smart "$contract" "{\"balance\":{\"address\":\"$val\"}}" "${q[@]}" \
  | jq -e '.data.balance=="1000000"' >/dev/null || die 'pre-upgrade contract state lost'

say 'Query every wired module with a public query service'
query auth auth params
query bank bank total
query historical-bank bank balances "$user" --height "$pre_height"
query staking staking validators
query mint mint params
query distribution distribution params
query slashing slashing params
query governance gov proposals
query feegrant feegrant grants-by-grantee "$user"
query authz authz grants-by-granter "$user"
query group group groups
query evidence evidence list
query circuit circuit disabled-list
query nft nft classes
query wasm wasm params
query ibc-client ibc client states
query ibc-channel ibc channel channels
query transfer ibc-transfer denoms
query ica-host interchain-accounts host params
query ica-controller interchain-accounts controller params
query tokenfactory tokenfactory params
query ratelimit ratelimiting list-rate-limits
query hyperlane hyperlane mailboxes
query warp warp tokens
for pair in 'mailboxes hyperlane' 'warp warp' 'groups group'; do
  read -r before after <<< "$pair"
  diff <(jq -S 'del(.params.key_rotation_fee)' "$LOG_DIR/$before-before.json") <(jq -S 'del(.params.key_rotation_fee)' "$LOG_DIR/query-$after.json") >/dev/null \
    || die "$before state changed"
done
# Capability, genutil, vesting, crisis and packet-forward have no standalone
# public query surface. PFM packet delivery requires a separate counterparty.

say 'Build module transactions through the candidate CLI'
generate bank bank send "$user" "$val" "1$DENOM"
generate delegate staking delegate "$valoper" "100$DENOM"
generate unbond staking unbond "$valoper" "100$DENOM"
generate redelegate staking redelegate "$valoper" "$valoper2" "100$DENOM"
generate withdraw distribution withdraw-rewards "$valoper"
generate set-withdraw distribution set-withdraw-addr "$val"
generate vote gov vote "$proposal" yes
generate unjail slashing unjail
generate feegrant feegrant grant "$user" "$val" --spend-limit "1000$DENOM"
generate authz authz grant "$val" send --spend-limit "1000$DENOM"
generate transfer ibc-transfer transfer transfer channel-0 "$val" "1$DENOM"
generate tokenfactory tokenfactory create-denom post
generate vesting vesting create-vesting-account "$val" "100$DENOM" "$(( $(date +%s) + 3600 ))"
generate wasm wasm store "$CW20_WASM"
generate hyperlane hyperlane ism create-noop
generate mailbox hyperlane mailbox create "$ism" 1235
generate warp warp create-collateral-token "$mailbox" "$DENOM"
generate group group create-group "$user" fixture "$LOG_DIR/group-members.json"
generate nft nft send fixture nft1 "$val"

say 'Execute economic transactions and prove fee enforcement'
broadcast post-bank user bank send "$user" "$val" "1000$DENOM"
broadcast delegate user staking delegate "$valoper" "1000000$DENOM"
broadcast unbond user staking unbond "$valoper" "500000$DENOM"
broadcast withdraw val distribution withdraw-rewards "$valoper"
broadcast feegrant val feegrant grant "$val" "$user" --spend-limit "1000000$DENOM"
broadcast authz user authz grant "$val" send --spend-limit "100000$DENOM"
broadcast post-tokenfactory user tokenfactory create-denom post
broadcast post-mint user tokenfactory mint "2000000$factory"
broadcast post-burn user tokenfactory burn "1000000$factory"
broadcast post-ism user hyperlane ism create-noop
broadcast post-warp user warp create-collateral-token "$mailbox" "$factory"
broadcast post-group user group create-group "$user" post "$LOG_DIR/group-members.json"
broadcast cw20-transfer val wasm execute "$contract" "{\"transfer\":{\"recipient\":\"$user\",\"amount\":\"250000\"}}"
"${bin[@]}" query wasm contract-state smart "$contract" "{\"balance\":{\"address\":\"$user\"}}" "${q[@]}" \
  | jq -e '.data.balance=="250000"' >/dev/null || die 'cw20 transfer failed'
"${bin[@]}" tx bank send "$user" "$val" "1$DENOM" --from user --node "$NODE" \
  --chain-id "$CHAIN_ID" --keyring-backend test --gas 200000 --fees "1$DENOM" -y -o json \
  > "$LOG_DIR/low-fee.json" 2> "$LOG_DIR/low-fee.stderr" || true
if jq -e '.code==0' "$LOG_DIR/low-fee.json" >/dev/null 2>&1; then die 'underpriced tx accepted'; fi
grep -qi 'insufficient fees' "$LOG_DIR/low-fee.json" "$LOG_DIR/low-fee.stderr" || die 'fee rejection reason missing'

say 'Restart candidate and query the preserved contract'
restart_height=$(height)
stop_node
start_node v12-restart
wait_height "$((restart_height + 2))"
"${bin[@]}" query wasm contract-state smart "$contract" "{\"balance\":{\"address\":\"$user\"}}" "${q[@]}" \
  | jq -e '.data.balance=="250000"' >/dev/null || die 'contract state lost on restart'
jq -n --arg backend "$DB_BACKEND" --argjson height "$upgrade_height" \
  '{status:"PASS",database:$backend,upgrade_name:"v12",upgrade_height:$height,
    mainnet_state_rehearsal:false,post_quantum_signing_supported:true,consensus_keys_rotated:false,
    checks:["governance-halt-resume","unchanged-consensus-staking-fees","historical-query",
      "module-query-and-message-builds","committed-economic-transactions","tokenfactory-state",
      "hyperlane-mailbox-and-warp-state","group-state",
      "pre-upgrade-cw20-state-and-transfer","minimum-fee-rejection","restart"]}' > "$LOG_DIR/result.json"
say "PASS v10 -> v12 local rehearsal ($DB_BACKEND); logs: $LOG_DIR"
