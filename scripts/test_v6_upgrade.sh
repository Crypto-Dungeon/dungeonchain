#!/bin/bash
# test_v6_upgrade.sh — Simulate v5 -> v6 chain upgrade on an isolated testnet.
#
# What this verifies:
#   1. A chain booted with the v5 binary halts at the upgrade height.
#   2. Restarting with the v6 binary resumes block production.
#   3. The v6 handler sets x/globalfee MinimumGasPrices to 0.01udgn.
#   4. Fee enforcement post-upgrade: a non-exempt tx with --gas-prices=0udgn
#      is rejected; the same tx with --gas-prices=0.01udgn succeeds.
#
# Not yet verified (needs app.go env-var hook, separate change):
#   - Admin-wallet fee exemption (production hardcoded address; no test key).
#
# Ports chosen to NOT collide with the live dungeond on server 11
# (26657/26656/9090/11317).
set -eu

BINARY_V5=${BINARY_V5:-$HOME/bin/dungeond-v5}
BINARY_V6=${BINARY_V6:-$HOME/bin/dungeond-v6}
HOME_DIR=${HOME_DIR:-$HOME/.dungeonchain-v6test}
CHAIN_ID=${CHAIN_ID:-dungeontest-1}
DENOM=${DENOM:-udgn}
KEYRING=test

RPC_PORT=${RPC_PORT:-46657}
P2P_PORT=${P2P_PORT:-46656}
GRPC_PORT=${GRPC_PORT:-19090}
GRPC_WEB_PORT=${GRPC_WEB_PORT:-19091}
REST_PORT=${REST_PORT:-21317}
PPROF_PORT=${PPROF_PORT:-36060}

LOG_DIR=$HOME_DIR/test-logs
UPGRADE_OFFSET=${UPGRADE_OFFSET:-40}
VOTING_PERIOD=30s
EXPEDITED_VOTING_PERIOD=15s
BLOCK_TIME=${BLOCK_TIME:-2s}

say() { echo "[$(date +%H:%M:%S)] $*"; }
die() { echo "FAIL: $*" >&2; exit 1; }

for bin in "$BINARY_V5" "$BINARY_V6"; do
  [ -x "$bin" ] || die "binary not found/executable: $bin"
done
command -v jq >/dev/null || die "jq is required"

say "--- killing any leftover testnet daemons ---"
pkill -f 'dungeond-v[56] .*--home.*v6test' 2>/dev/null || true
# Also by port
fuser -k $RPC_PORT/tcp 2>/dev/null || true
sleep 2

say "--- wiping $HOME_DIR ---"
[ ${#HOME_DIR} -gt 5 ] || die "HOME_DIR too short, refusing to rm"
rm -rf "$HOME_DIR"
mkdir -p "$LOG_DIR"

BIN5=("$BINARY_V5" --home "$HOME_DIR")
BIN6=("$BINARY_V6" --home "$HOME_DIR")
KR=(--keyring-backend $KEYRING)
NODE_URL="tcp://127.0.0.1:$RPC_PORT"

say "--- init chain (v5) ---"
"${BIN5[@]}" init localval --chain-id $CHAIN_ID --default-denom $DENOM > /dev/null 2>&1

"${BIN5[@]}" keys add val "${KR[@]}" --output json > $LOG_DIR/val.json 2>&1
"${BIN5[@]}" keys add user "${KR[@]}" --output json > $LOG_DIR/user.json 2>&1
VAL_ADDR=$("${BIN5[@]}" keys show val "${KR[@]}" -a)
USER_ADDR=$("${BIN5[@]}" keys show user "${KR[@]}" -a)
say "  validator: $VAL_ADDR"
say "  user:      $USER_ADDR"

"${BIN5[@]}" genesis add-genesis-account val 1000000000000$DENOM "${KR[@]}"
"${BIN5[@]}" genesis add-genesis-account user 1000000000$DENOM "${KR[@]}"

"${BIN5[@]}" genesis gentx val 100000000000$DENOM --chain-id $CHAIN_ID "${KR[@]}" > /dev/null 2>&1
"${BIN5[@]}" genesis collect-gentxs > /dev/null 2>&1

GEN=$HOME_DIR/config/genesis.json
tmp=$(mktemp)
jq ".app_state.gov.params.voting_period=\"$VOTING_PERIOD\" |
    .app_state.gov.params.expedited_voting_period=\"$EXPEDITED_VOTING_PERIOD\" |
    .app_state.gov.params.min_deposit=[{\"denom\":\"$DENOM\",\"amount\":\"1\"}] |
    .app_state.gov.params.expedited_min_deposit=[{\"denom\":\"$DENOM\",\"amount\":\"1\"}] |
    .app_state.globalfee.params.minimum_gas_prices=[{\"denom\":\"$DENOM\",\"amount\":\"0.000000000000000000\"}]" $GEN > $tmp && mv $tmp $GEN

CFG=$HOME_DIR/config/config.toml
sed -i "s#^laddr = \"tcp://127.0.0.1:26657\"#laddr = \"tcp://127.0.0.1:$RPC_PORT\"#" $CFG
sed -i "s#^laddr = \"tcp://0.0.0.0:26656\"#laddr = \"tcp://0.0.0.0:$P2P_PORT\"#" $CFG
sed -i "s#^pprof_laddr = \"localhost:6060\"#pprof_laddr = \"localhost:$PPROF_PORT\"#" $CFG
sed -i "s#^timeout_commit = \"5s\"#timeout_commit = \"$BLOCK_TIME\"#" $CFG
sed -i "s#^cors_allowed_origins = \\[\\]#cors_allowed_origins = [\"*\"]#" $CFG

APP=$HOME_DIR/config/app.toml
sed -i "s#^address = \"tcp://localhost:1317\"#address = \"tcp://localhost:$REST_PORT\"#" $APP
sed -i "s#^address = \"localhost:9090\"#address = \"localhost:$GRPC_PORT\"#" $APP
sed -i "s#^address = \"localhost:9091\"#address = \"localhost:$GRPC_WEB_PORT\"#" $APP
sed -i "s#^minimum-gas-prices = \"\"#minimum-gas-prices = \"0$DENOM\"#" $APP
sed -i "/^\\[api\\]/,/^\\[/{s/^enable = false/enable = true/;}" $APP

say "--- starting v5 daemon ---"
nohup "${BIN5[@]}" start \
  --pruning=nothing \
  --minimum-gas-prices=0$DENOM \
  > $LOG_DIR/v5.log 2>&1 &
V5_PID=$!
say "  v5 pid=$V5_PID, waiting for block 5"

wait_height() {
  local target=$1 timeout=${2:-120}
  local start=$(date +%s)
  while true; do
    h=$(curl -s http://127.0.0.1:$RPC_PORT/status 2>/dev/null | jq -r '.result.sync_info.latest_block_height // "0"')
    if [ -n "$h" ] && [ "$h" != "null" ] && [ "$h" -ge "$target" ] 2>/dev/null; then
      echo $h
      return 0
    fi
    now=$(date +%s)
    [ $((now-start)) -gt $timeout ] && die "timeout waiting for height $target (at $h)"
    sleep 1
  done
}

wait_height 5 60 > /dev/null
say "  chain is producing blocks"

CUR=$(curl -s http://127.0.0.1:$RPC_PORT/status | jq -r '.result.sync_info.latest_block_height')
UP_HEIGHT=$((CUR + UPGRADE_OFFSET))
say "--- submitting upgrade prop: v6 at height $UP_HEIGHT (current $CUR) ---"

PROP_FILE=$LOG_DIR/prop.json
AUTHORITY=$("${BIN5[@]}" query auth module-account gov --node $NODE_URL -o json 2>/dev/null | jq -r '.account.value.address' 2>/dev/null)
[ -n "$AUTHORITY" ] && [ "$AUTHORITY" != "null" ] || AUTHORITY="dungeon10d07y265gmmuvt4z0w9aw880jnsr700j53vrug"
say "  authority: $AUTHORITY"

cat > $PROP_FILE <<PROPEOF
{
  "messages": [
    {
      "@type": "/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade",
      "authority": "$AUTHORITY",
      "plan": {
        "name": "v6",
        "height": "$UP_HEIGHT",
        "info": "test upgrade",
        "upgraded_client_state": null
      }
    }
  ],
  "metadata": "",
  "deposit": "1$DENOM",
  "title": "v6 test upgrade",
  "summary": "scripted v5 to v6 upgrade test"
}
PROPEOF

"${BIN5[@]}" tx gov submit-proposal $PROP_FILE --from val --chain-id $CHAIN_ID "${KR[@]}" \
  --node $NODE_URL --gas auto --gas-adjustment 1.5 --fees 10000$DENOM -y -o json \
  > $LOG_DIR/submit.json 2>&1 || true
sleep 6

PROP_ID=$("${BIN5[@]}" query gov proposals --node $NODE_URL -o json 2>/dev/null | jq -r '.proposals[-1].id')
say "  prop id=$PROP_ID"
[ -n "$PROP_ID" ] && [ "$PROP_ID" != "null" ] || die "failed to submit proposal (see $LOG_DIR/submit.json)"

say "--- voting yes ---"
"${BIN5[@]}" tx gov vote $PROP_ID yes --from val --chain-id $CHAIN_ID "${KR[@]}" \
  --node $NODE_URL --gas auto --gas-adjustment 1.5 --fees 10000$DENOM -y > $LOG_DIR/vote.json 2>&1
sleep 4

say "--- waiting for upgrade halt at height $UP_HEIGHT ---"
for i in $(seq 1 600); do
  if ! kill -0 $V5_PID 2>/dev/null; then
    say "  v5 process exited"
    break
  fi
  if grep -qE 'UPGRADE "v6" NEEDED|ERR CONSENSUS FAILURE' $LOG_DIR/v5.log 2>/dev/null; then
    say "  upgrade panic observed in log"
    for j in $(seq 1 10); do
      kill -0 $V5_PID 2>/dev/null || break
      sleep 1
    done
    break
  fi
  sleep 2
done
kill $V5_PID 2>/dev/null || true
sleep 3

say "--- starting v6 daemon (same home) ---"
nohup "${BIN6[@]}" start \
  --pruning=nothing \
  --minimum-gas-prices=0$DENOM \
  > $LOG_DIR/v6.log 2>&1 &
V6_PID=$!
say "  v6 pid=$V6_PID, waiting for height > $UP_HEIGHT"
NEW_H=$(wait_height $((UP_HEIGHT+3)) 180)
say "  chain resumed, at height $NEW_H"

say "--- assertion 1: globalfee MinimumGasPrices = 0.01$DENOM ---"
GF_JSON=$("${BIN6[@]}" query globalfee minimum-gas-prices --node $NODE_URL -o json 2>&1)
echo "$GF_JSON" > $LOG_DIR/globalfee.json
GF_AMT=$(echo "$GF_JSON" | jq -r '.minimum_gas_prices[0].amount // .params.minimum_gas_prices[0].amount // ""')
GF_DENOM=$(echo "$GF_JSON" | jq -r '.minimum_gas_prices[0].denom // .params.minimum_gas_prices[0].denom // ""')
say "  globalfee = $GF_AMT$GF_DENOM"
[ "$GF_DENOM" = "$DENOM" ] || die "unexpected denom: $GF_DENOM"
case "$GF_AMT" in
  0.01*) ;;
  *) die "expected MinimumGasPrices ~0.01$DENOM, got $GF_AMT$GF_DENOM" ;;
esac
say "  OK"

say "--- assertion 2: zero-fee tx is rejected ---"
ZERO_OUT=$("${BIN6[@]}" tx bank send user $VAL_ADDR 1$DENOM \
  --chain-id $CHAIN_ID --node $NODE_URL "${KR[@]}" \
  --gas 200000 --gas-prices 0$DENOM -y -o json 2>&1 || true)
echo "$ZERO_OUT" > $LOG_DIR/zero_fee.json
if echo "$ZERO_OUT" | grep -qiE 'insufficient fee|provided fee|minimum-gas-prices|below minimum'; then
  say "  rejected as expected"
else
  CODE=$(echo "$ZERO_OUT" | jq -r '.code // empty' 2>/dev/null)
  if [ -n "$CODE" ] && [ "$CODE" != "0" ]; then
    say "  rejected with code $CODE"
  else
    die "zero-fee tx was NOT rejected (see $LOG_DIR/zero_fee.json)"
  fi
fi

say "--- assertion 3: 0.01$DENOM fee tx succeeds ---"
OK_OUT=$("${BIN6[@]}" tx bank send user $VAL_ADDR 1$DENOM \
  --chain-id $CHAIN_ID --node $NODE_URL "${KR[@]}" \
  --gas 200000 --gas-prices 0.01$DENOM -y -o json 2>&1)
echo "$OK_OUT" > $LOG_DIR/paid_fee.json
CODE=$(echo "$OK_OUT" | jq -r '.code')
[ "$CODE" = "0" ] || die "paid-fee tx failed with code $CODE (see $LOG_DIR/paid_fee.json)"
say "  accepted, code=0"

say ""
say "========================================="
say "  v6 UPGRADE TEST PASSED"
say "========================================="
say "  logs: $LOG_DIR/"
say "  v6 still running as pid $V6_PID (kill with: kill $V6_PID)"
