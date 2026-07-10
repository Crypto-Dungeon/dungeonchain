#!/bin/bash
# test_v9_upgrade.sh — Simulate v8 -> v9 chain upgrade on an isolated testnet.
#
# What this verifies:
#   1. A chain booted with the v8 binary halts at the upgrade height.
#   2. Restarting with the v9 binary resumes block production.
#   3. The v9 handler sets staking MinCommissionRate to 5% and raises the
#      validator (gentx'd at 1%) to the floor.
#   4. The ratelimit module store/queries are live post-upgrade.
#   5. QUERY BATTERY: one query per wired module (incl. historical height).
#   6. MSG BATTERY (per feedback_upgrade_test_gate_must_cover_all_msg_types):
#      one message per wired module builds via --generate-only, and the
#      economic core additionally sign+broadcasts with code=0.
#
# Ports chosen to NOT collide with the live dungeond on server 11
# (26657/26656/9090/11317) or the v6 test script (46657 etc).
set -eu

BINARY_V8=${BINARY_V8:-$HOME/bin/dungeond-v8}
BINARY_V9=${BINARY_V9:-$HOME/bin/dungeond-v9}
HOME_DIR=${HOME_DIR:-$HOME/.dungeonchain-v9test}
CHAIN_ID=${CHAIN_ID:-dungeontest-1}
DENOM=${DENOM:-udgn}
KEYRING=test

RPC_PORT=${RPC_PORT:-47657}
P2P_PORT=${P2P_PORT:-47656}
GRPC_PORT=${GRPC_PORT:-19190}
GRPC_WEB_PORT=${GRPC_WEB_PORT:-19191}
REST_PORT=${REST_PORT:-22317}
PPROF_PORT=${PPROF_PORT:-36061}

LOG_DIR=$HOME_DIR/test-logs
UPGRADE_OFFSET=${UPGRADE_OFFSET:-40}
VOTING_PERIOD=30s
EXPEDITED_VOTING_PERIOD=15s
BLOCK_TIME=${BLOCK_TIME:-2s}

FEES="--gas auto --gas-adjustment 1.6 --fees 20000$DENOM"

say() { echo "[$(date +%H:%M:%S)] $*"; }
die() { echo "FAIL: $*" >&2; exit 1; }

for bin in "$BINARY_V8" "$BINARY_V9"; do
  [ -x "$bin" ] || die "binary not found/executable: $bin"
done
command -v jq >/dev/null || die "jq is required"

say "--- killing any leftover testnet daemons ---"
pkill -f 'dungeond-v[89] .*--home.*v9test' 2>/dev/null || true
fuser -k $RPC_PORT/tcp 2>/dev/null || true
sleep 2

say "--- wiping $HOME_DIR ---"
[ ${#HOME_DIR} -gt 5 ] || die "HOME_DIR too short, refusing to rm"
rm -rf "$HOME_DIR"
mkdir -p "$LOG_DIR"

BIN8=("$BINARY_V8" --home "$HOME_DIR")
BIN9=("$BINARY_V9" --home "$HOME_DIR")
KR=(--keyring-backend $KEYRING)
NODE_URL="tcp://127.0.0.1:$RPC_PORT"
TX8=(--chain-id $CHAIN_ID --node $NODE_URL "${KR[@]}" -y -o json)
TX9=(--chain-id $CHAIN_ID --node $NODE_URL "${KR[@]}" -y -o json)

say "--- init chain (v8) ---"
"${BIN8[@]}" init localval --chain-id $CHAIN_ID --default-denom $DENOM > /dev/null 2>&1

"${BIN8[@]}" keys add val "${KR[@]}" --output json > $LOG_DIR/val.json 2>&1
"${BIN8[@]}" keys add val2 "${KR[@]}" --output json > $LOG_DIR/val2.json 2>&1
"${BIN8[@]}" keys add user "${KR[@]}" --output json > $LOG_DIR/user.json 2>&1
VAL_ADDR=$("${BIN8[@]}" keys show val "${KR[@]}" -a)
VALOPER=$("${BIN8[@]}" keys show val "${KR[@]}" -a --bech val)
VALOPER2=$("${BIN8[@]}" keys show val2 "${KR[@]}" -a --bech val)
USER_ADDR=$("${BIN8[@]}" keys show user "${KR[@]}" -a)
say "  validator: $VAL_ADDR ($VALOPER)"
say "  user:      $USER_ADDR"

"${BIN8[@]}" genesis add-genesis-account val 1000000000000$DENOM "${KR[@]}"
"${BIN8[@]}" genesis add-genesis-account user 10000000000$DENOM "${KR[@]}"

# 1% commission on purpose: the v9 handler must raise it to the 5% floor.
"${BIN8[@]}" genesis gentx val 100000000000$DENOM --chain-id $CHAIN_ID "${KR[@]}" \
  --commission-rate 0.01 --commission-max-rate 0.02 --commission-max-change-rate 0.01 > /dev/null 2>&1
"${BIN8[@]}" genesis collect-gentxs > /dev/null 2>&1

GEN=$HOME_DIR/config/genesis.json
tmp=$(mktemp)
jq ".app_state.gov.params.voting_period=\"$VOTING_PERIOD\" |
    .app_state.gov.params.expedited_voting_period=\"$EXPEDITED_VOTING_PERIOD\" |
    .app_state.gov.params.min_deposit=[{\"denom\":\"$DENOM\",\"amount\":\"1\"}] |
    .app_state.gov.params.expedited_min_deposit=[{\"denom\":\"$DENOM\",\"amount\":\"1\"}] |
    .app_state.staking.params.min_commission_rate=\"0.000000000000000000\" |
    .app_state.globalfee.params.minimum_gas_prices=[{\"denom\":\"$DENOM\",\"amount\":\"0.000000000000000000\"}]" $GEN > $tmp && mv $tmp $GEN

CFG=$HOME_DIR/config/config.toml
sed -i "s#^laddr = \"tcp://127.0.0.1:26657\"#laddr = \"tcp://127.0.0.1:$RPC_PORT\"#" $CFG
sed -i "s#^laddr = \"tcp://0.0.0.0:26656\"#laddr = \"tcp://0.0.0.0:$P2P_PORT\"#" $CFG
sed -i "s#^pprof_laddr = \"localhost:6060\"#pprof_laddr = \"localhost:$PPROF_PORT\"#" $CFG
sed -i "s#^timeout_commit = \"5s\"#timeout_commit = \"$BLOCK_TIME\"#" $CFG

APP=$HOME_DIR/config/app.toml
sed -i "s#^address = \"tcp://localhost:1317\"#address = \"tcp://localhost:$REST_PORT\"#" $APP
sed -i "s#^address = \"localhost:9090\"#address = \"localhost:$GRPC_PORT\"#" $APP
sed -i "s#^address = \"localhost:9091\"#address = \"localhost:$GRPC_WEB_PORT\"#" $APP
sed -i "s#^minimum-gas-prices = \"\"#minimum-gas-prices = \"0$DENOM\"#" $APP
sed -i "/^\\[api\\]/,/^\\[/{s/^enable = false/enable = true/;}" $APP

say "--- starting v8 daemon ---"
nohup "${BIN8[@]}" start --pruning=nothing --minimum-gas-prices=0$DENOM \
  > $LOG_DIR/v8.log 2>&1 &
V8_PID=$!

wait_height() {
  local target=$1 timeout=${2:-120}
  local start=$(date +%s)
  while true; do
    h=$(curl -s http://127.0.0.1:$RPC_PORT/status 2>/dev/null | jq -r '.result.sync_info.latest_block_height // "0"')
    if [ -n "$h" ] && [ "$h" != "null" ] && [ "$h" -ge "$target" ] 2>/dev/null; then
      echo $h; return 0
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
say "--- submitting upgrade prop: v9 at height $UP_HEIGHT (current $CUR) ---"

AUTHORITY=$("${BIN8[@]}" query auth module-account gov --node $NODE_URL -o json 2>/dev/null | jq -r '.account.value.address' 2>/dev/null)
[ -n "$AUTHORITY" ] && [ "$AUTHORITY" != "null" ] || AUTHORITY="dungeon10d07y265gmmuvt4z0w9aw880jnsr700j53vrug"

PROP_FILE=$LOG_DIR/prop.json
cat > $PROP_FILE <<PROPEOF
{
  "messages": [
    {
      "@type": "/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade",
      "authority": "$AUTHORITY",
      "plan": { "name": "v9", "height": "$UP_HEIGHT", "info": "test upgrade", "upgraded_client_state": null }
    }
  ],
  "metadata": "", "deposit": "1$DENOM",
  "title": "v9 test upgrade", "summary": "scripted v8 to v9 upgrade test"
}
PROPEOF

"${BIN8[@]}" tx gov submit-proposal $PROP_FILE --from val "${TX8[@]}" $FEES > $LOG_DIR/submit.json 2>&1 || true
sleep 6
PROP_ID=$("${BIN8[@]}" query gov proposals --node $NODE_URL -o json 2>/dev/null | jq -r '.proposals[-1].id')
[ -n "$PROP_ID" ] && [ "$PROP_ID" != "null" ] || die "failed to submit proposal (see $LOG_DIR/submit.json)"
"${BIN8[@]}" tx gov vote $PROP_ID yes --from val "${TX8[@]}" $FEES > $LOG_DIR/vote.json 2>&1
sleep 4

say "--- waiting for upgrade halt at height $UP_HEIGHT ---"
for i in $(seq 1 600); do
  kill -0 $V8_PID 2>/dev/null || { say "  v8 process exited"; break; }
  if grep -qE 'UPGRADE "v9" NEEDED|ERR CONSENSUS FAILURE' $LOG_DIR/v8.log 2>/dev/null; then
    say "  upgrade panic observed in log"
    for j in $(seq 1 10); do kill -0 $V8_PID 2>/dev/null || break; sleep 1; done
    break
  fi
  sleep 2
done
kill $V8_PID 2>/dev/null || true
sleep 3

say "--- starting v9 daemon (same home) ---"
nohup "${BIN9[@]}" start --pruning=nothing --minimum-gas-prices=0$DENOM \
  > $LOG_DIR/v9.log 2>&1 &
V9_PID=$!
NEW_H=$(wait_height $((UP_HEIGHT+3)) 180)
say "  chain resumed on v9, at height $NEW_H"

Q=(--node $NODE_URL -o json)

say "--- assertion 1: staking MinCommissionRate = 0.05 ---"
MINC=$("${BIN9[@]}" query staking params "${Q[@]}" | jq -r '.params.min_commission_rate')
say "  min_commission_rate = $MINC"
case "$MINC" in 0.05*) ;; *) die "expected 0.05, got $MINC" ;; esac

say "--- assertion 2: validator commission raised 1% -> 5% ---"
VRATE=$("${BIN9[@]}" query staking validator $VALOPER "${Q[@]}" | jq -r '.validator.commission.commission_rates.rate')
say "  validator rate = $VRATE"
case "$VRATE" in 0.05*) ;; *) die "expected validator raised to 0.05, got $VRATE" ;; esac

say "--- assertion 3: ratelimit module live ---"
"${BIN9[@]}" query ratelimit list-rate-limits "${Q[@]}" > $LOG_DIR/ratelimits.json 2>&1 \
  || die "ratelimit query surface dead (see $LOG_DIR/ratelimits.json)"
say "  OK: $(cat $LOG_DIR/ratelimits.json | head -c 120)"

say "--- assertion 4: ratelimit MsgAddRateLimit registered (gov path) ---"
RL_PROP=$LOG_DIR/rl_prop.json
cat > $RL_PROP <<RLEOF
{
  "messages": [
    {
      "@type": "/ratelimit.v1.MsgAddRateLimit",
      "authority": "$AUTHORITY",
      "denom": "$DENOM",
      "channel_or_client_id": "channel-0",
      "max_percent_send": "10",
      "max_percent_recv": "10",
      "duration_hours": "24"
    }
  ],
  "metadata": "", "deposit": "1$DENOM",
  "title": "add rate limit", "summary": "msg-battery: proves MsgAddRateLimit type registration"
}
RLEOF
RL_OUT=$("${BIN9[@]}" tx gov submit-proposal $RL_PROP --from val "${TX9[@]}" $FEES 2>&1)
echo "$RL_OUT" > $LOG_DIR/rl_submit.json
echo "$RL_OUT" | grep -qiE 'unable to resolve type URL|unknown proposal message' \
  && die "MsgAddRateLimit type NOT registered"
say "  submitted OK (execution may fail on fake channel; registration is what we test)"

say "--- QUERY BATTERY: one query per wired module ---"
QFAIL=0
q() { # q <label> <args...>
  local label=$1; shift
  if "${BIN9[@]}" query "$@" "${Q[@]}" > $LOG_DIR/q_$label.json 2>&1; then
    say "  OK   query $label"
  else
    say "  FAIL query $label"; QFAIL=$((QFAIL+1))
  fi
}
q auth            auth params
q bank            bank total
q bank_hist       bank balances $VAL_ADDR --height $((NEW_H-5))
q staking         staking validators
q mint            mint params
q distribution    distribution params
q slashing        slashing params
q gov             gov proposals
q feegrant        feegrant grants-by-grantee $USER_ADDR
q authz           authz grants-by-granter $USER_ADDR
q group           group groups
q evidence        evidence list
q upgrade         upgrade applied v9
q consensus       consensus params
q circuit         circuit disabled-list
q wasm            wasm params
q ibc_client      ibc client states
q ibc_channel     ibc channel channels
q transfer        ibc-transfer denoms
q ica_host        interchain-accounts host params
q tokenfactory    tokenfactory params
q globalfee       globalfee minimum-gas-prices
q ratelimit       ratelimit list-rate-limits
q hyperlane       hyperlane mailboxes
q warp            warp tokens
# PFM registers no CLI query in v10 — exercise its query surface via REST.
if curl -sf http://127.0.0.1:$REST_PORT/packetforward/v1/params > $LOG_DIR/q_pfm.json 2>&1; then
  say "  OK   query pfm (REST)"
else
  say "  FAIL query pfm (REST)"; QFAIL=$((QFAIL+1))
fi
[ $QFAIL -eq 0 ] || die "$QFAIL module queries failed (see $LOG_DIR/q_*.json)"

say "--- MSG BATTERY (generate-only): every module's tx builds ---"
GFAIL=0
GEN=(--generate-only --from $USER_ADDR --chain-id $CHAIN_ID --node $NODE_URL)
g() { # g <label> <args...>
  local label=$1; shift
  if "${BIN9[@]}" tx "$@" "${GEN[@]}" > $LOG_DIR/g_$label.json 2>&1; then
    say "  OK   build $label"
  else
    say "  FAIL build $label"; GFAIL=$((GFAIL+1))
  fi
}
g bank_send        bank send $USER_ADDR $VAL_ADDR 1$DENOM
g staking_del      staking delegate $VALOPER 100$DENOM
g staking_undel    staking unbond $VALOPER 100$DENOM
g staking_redel    staking redelegate $VALOPER $VALOPER2 100$DENOM
g distr_withdraw   distribution withdraw-rewards $VALOPER
g distr_setaddr    distribution set-withdraw-addr $VAL_ADDR
g gov_vote         gov vote 1 yes
g slashing_unjail  slashing unjail
g feegrant_grant   feegrant grant $USER_ADDR $VAL_ADDR --spend-limit 1000$DENOM
g authz_grant      authz grant $VAL_ADDR send --spend-limit 1000$DENOM
g ibc_transfer     ibc-transfer transfer transfer channel-0 $VAL_ADDR 1$DENOM
g tf_create        tokenfactory create-denom btest
g vesting          vesting create-vesting-account $VALOPER2 100$DENOM $(( $(date +%s) + 3600 ))
printf '\x00asm\x01\x00\x00\x00' > $LOG_DIR/dummy.wasm
g wasm_store       wasm store $LOG_DIR/dummy.wasm
[ $GFAIL -eq 0 ] || die "$GFAIL module tx builds failed (see $LOG_DIR/g_*.json) — Passage-class breakage"

say "--- MSG BATTERY (broadcast): economic core executes with code=0 ---"
b() { # b <label> <from> <args...>
  local label=$1 from=$2; shift 2
  local out code
  out=$("${BIN9[@]}" tx "$@" --from $from "${TX9[@]}" $FEES 2>&1) || true
  echo "$out" > $LOG_DIR/b_$label.json
  code=$(echo "$out" | jq -r '.code // "parse-error"' 2>/dev/null)
  [ "$code" = "0" ] || die "broadcast $label failed (code=$code, see $LOG_DIR/b_$label.json)"
  say "  OK   broadcast $label"
  sleep 3
}
b bank_send      user bank send $USER_ADDR $VAL_ADDR 1000$DENOM
b staking_del    user staking delegate $VALOPER 1000000$DENOM
b staking_undel  user staking unbond $VALOPER 500000$DENOM
b distr_withdraw val  distribution withdraw-rewards $VALOPER
b feegrant       val  feegrant grant $VAL_ADDR $USER_ADDR --spend-limit 100000$DENOM
b authz          user authz grant $VAL_ADDR send --spend-limit 100000$DENOM
b tf_create      user tokenfactory create-denom bat9

say ""
say "========================================="
say "  v9 UPGRADE TEST PASSED"
say "  - halt/resume at $UP_HEIGHT: OK"
say "  - min-commission floor 5%: OK (param + validator raise)"
say "  - ratelimit store/query/msg-registration: OK"
say "  - query battery: all wired modules OK"
say "  - msg battery: builds OK, core broadcasts code=0"
say "========================================="
say "  logs: $LOG_DIR/"
say "  v9 still running as pid $V9_PID (kill with: kill $V9_PID)"
