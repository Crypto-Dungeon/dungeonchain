#!/usr/bin/env bash
# Local-only PQ accounts and validator activation rehearsal. No production home, keys, ports or services.
# Requires BINARY_V10, BINARY_V12 and CW20_WASM; preserves logs in a fresh
# temporary directory and stops only the child processes started by this run.
set -euo pipefail
umask 077
: "${BINARY_V12:?set BINARY_V12 to the candidate binary}"
for file in "$BINARY_V12"; do
  [[ -x "$file" ]] || { echo "Not executable: $file" >&2; exit 1; }
done
for tool in jq curl python3; do command -v "$tool" >/dev/null; done
DB_BACKEND=${DB_BACKEND:-pebbledb}
[[ "$DB_BACKEND" == pebbledb || "$DB_BACKEND" == goleveldb ]] || exit 1
HOME_DIR=$(mktemp -d "${TMPDIR:-/tmp}/dungeon-v12-quantum.XXXXXXXX")
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
CHAIN_ID=dungeon-v12-quantum-1
DENOM=udgn
old=("$BINARY_V12" --home "$HOME_DIR")
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

say "Initialize isolated candidate chain ($DB_BACKEND)"
"${bin[@]}" init localval --chain-id "$CHAIN_ID" --default-denom "$DENOM" > "$LOG_DIR/init.log" 2>&1
"${bin[@]}" keys add val --keyring-backend test --no-backup > "$HOME_DIR/val-key.json" 2>&1
"${bin[@]}" keys add pq --key-type ml_dsa_65 --keyring-backend test --no-backup > "$HOME_DIR/pq-key.json" 2>&1
val=$("${bin[@]}" keys show val --keyring-backend test -a)
pq=$("${bin[@]}" keys show pq --keyring-backend test -a)
"${bin[@]}" genesis add-genesis-account val "1000000000000$DENOM" --keyring-backend test
"${bin[@]}" genesis add-genesis-account pq "1000000000$DENOM" --keyring-backend test
"${bin[@]}" genesis gentx val "100000000000$DENOM" --chain-id "$CHAIN_ID" --keyring-backend test \
 --commission-rate 0.05 --commission-max-rate 1 --commission-max-change-rate 0.05 > "$LOG_DIR/gentx.log" 2>&1
"${bin[@]}" genesis collect-gentxs > "$LOG_DIR/collect.log" 2>&1
genesis="$HOME_DIR/config/genesis.json"
# Leave time for CLI startup and committed-tx polling on shared CI runners.
jq '.app_state.gov.params.voting_period="90s" |
 .app_state.gov.params.expedited_voting_period="45s" |
 .app_state.gov.params.min_deposit=[{"denom":"udgn","amount":"1"}] |
 .app_state.gov.params.expedited_min_deposit=[{"denom":"udgn","amount":"2"}] |
 .app_state.staking.params.min_commission_rate="0.050000000000000000" |
 .app_state.globalfee.params.minimum_gas_prices=[{"denom":"udgn","amount":"0.025000000000000000"}]' \
 "$genesis" > "$HOME_DIR/genesis.tmp"
mv "$HOME_DIR/genesis.tmp" "$genesis"
sed -i 's/^timeout_commit = "5s"/timeout_commit = "1s"/' "$HOME_DIR/config/config.toml"
start_node ed25519
wait_height 3
broadcast pq-send pq bank send "$pq" "$val" "123$DENOM"
"${bin[@]}" query auth account "$pq" "${q[@]}" > "$LOG_DIR/pq-account.json"
jq -e '.. | objects | select((."@type"? // .type?) == "/cosmos.crypto.mldsa65.PubKey")' "$LOG_DIR/pq-account.json" >/dev/null || die 'PQ account public key missing'

# Only generated fixture keys are used. This script must never be pointed at
# a production home or signer. The new consensus key has never signed before.
pq_signer="$HOME_DIR/generated-pq-signer"
"$BINARY_V12" init pq-signer --home "$pq_signer" --chain-id "$CHAIN_ID" --consensus-key-algo ml_dsa_65 > "$LOG_DIR/pq-init.log" 2>&1
"$BINARY_V12" comet show-validator --home "$pq_signer" > "$LOG_DIR/pq-consensus-public-key.json"
public_key=$(cat "$LOG_DIR/pq-consensus-public-key.json")
"${bin[@]}" tx staking rotate-cons-pub-key "$public_key" --from val "${tx[@]}" > "$LOG_DIR/rotation-disabled.json" 2> "$LOG_DIR/rotation-disabled.stderr" || true
if jq -e '.code==0' "$LOG_DIR/rotation-disabled.json" >/dev/null 2>&1; then die 'PQ rotation allowed before governance'; fi
grep -Eqi 'pubkey|public key|pub.key|key type' "$LOG_DIR/rotation-disabled.json" "$LOG_DIR/rotation-disabled.stderr" || die 'rotation rejection missing'
authority=$("${bin[@]}" query auth module-account gov "${q[@]}" | jq -er '.account.value.address // .account.base_account.address')
"${bin[@]}" query consensus params "${q[@]}" > "$LOG_DIR/consensus-before.json"
jq --arg authority "$authority" '{messages:[{
 "@type":"/cosmos.consensus.v1.MsgUpdateParams",authority:$authority,
 block:.params.block,evidence:.params.evidence,
 validator:{pub_key_types:["ed25519","ml_dsa_65"]},abci:.params.abci,
 auth:.params.auth}],
 metadata:"",deposit:"1udgn",title:"Fixture PQ activation",summary:"Isolated validator rotation test"}' \
 "$LOG_DIR/consensus-before.json" > "$LOG_DIR/activation-proposal.json"
broadcast activation-submit val gov submit-proposal "$LOG_DIR/activation-proposal.json"
proposal=$("${bin[@]}" query gov proposals "${q[@]}" | jq -er '.proposals[-1].id')
broadcast activation-vote val gov vote "$proposal" yes
for _ in $(seq 1 180); do
 "${bin[@]}" query gov proposal "$proposal" "${q[@]}" > "$LOG_DIR/activation-status.json"
 if jq -e '.proposal.status=="PROPOSAL_STATUS_PASSED"' "$LOG_DIR/activation-status.json" >/dev/null; then break; fi
 sleep 1
done
jq -e '.proposal.status=="PROPOSAL_STATUS_PASSED"' "$LOG_DIR/activation-status.json" >/dev/null || die 'activation governance did not pass'
broadcast rotation val staking rotate-cons-pub-key "$public_key"
rotation_height=$(jq -er '.height|tonumber' "$LOG_DIR/rotation-committed.json")
wait_height "$((rotation_height+1))"
stop_node
[[ "$HOME_DIR" == */dungeon-v12-quantum.* && "$pq_signer" == "$HOME_DIR/generated-pq-signer" ]] || die 'fixture home guard'
cp "$pq_signer/config/priv_validator_key.json" "$HOME_DIR/config/priv_validator_key.json"
cp "$pq_signer/data/priv_validator_state.json" "$HOME_DIR/data/priv_validator_state.json"
start_node ml-dsa
wait_height "$((rotation_height+5))"
curl -fsS "http://127.0.0.1:$RPC_PORT/validators" > "$LOG_DIR/comet-validators.json"
jq -e '.result.validators | length==1 and .[0].pub_key.type=="cometbft/PubKeyMlDsa65"' "$LOG_DIR/comet-validators.json" >/dev/null || die 'Comet not signing with ML-DSA'
broadcast pq-after-rotation pq bank send "$pq" "$val" "456$DENOM"
restart_height=$(height)
stop_node
start_node ml-dsa-restart
wait_height "$((restart_height+3))"
broadcast pq-after-restart pq bank send "$pq" "$val" "789$DENOM"
jq -n --arg backend "$DB_BACKEND" --argjson rotation_height "$rotation_height" \
 '{status:"PASS",database:$backend,rotation_height:$rotation_height,
 checks:["committed-ml-dsa-account-transactions","rotation-permission-gating","governance-consensus-activation",
 "validator-key-rotation","comet-ml-dsa-block-signing","post-rotation-transfers","pq-signer-restart"]}' > "$LOG_DIR/result.json"
say "PASS candidate PQ activation ($DB_BACKEND); logs: $LOG_DIR"
