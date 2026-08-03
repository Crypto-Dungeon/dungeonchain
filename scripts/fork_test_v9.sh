#!/bin/bash
# fork_test_v9.sh — v8 -> v9 upgrade test on REAL dungeon-1 mainnet state (crypto7).
#
# Same technique as the Passage v4.0.2 forkval: state-sync real state from
# srv11 (zero prod downtime), export it, genesis-surgery a single-validator
# fork, run the actual gov-upgrade sequence, then battery against the REAL
# DEX contracts / hyperlane state a fresh testnet can't test.
#
# Subcommands: statesync | export | surgery | run | battery
# All ports below the ephemeral range (32768+). Zero impact on the live
# HOH validator (26656/26657) on this box.
set -eu

BIN_V8=${BIN_V8:-$HOME/.dungeonchain/cosmovisor/upgrades/v8/bin/dungeond}
BIN_V9=${BIN_V9:-$HOME/bin/dungeond-v9}
WORK=$HOME/dungeon-fork
SS_HOME=$WORK/ss-node          # throwaway state-sync node
FORK_HOME=$WORK/node           # the fork chain
EXPORTED=$WORK/exported_genesis.json
FORKGEN=$WORK/fork_genesis.json
CHAIN=dungeon-fork-1
DENOM=udgn
KR=(--keyring-backend test --home $FORK_HOME)

# HOH's local RPC = light-client source; srv11 = snapshot peer.
TRUST_RPC=http://127.0.0.1:26657
SRV11_PEER="72535d78b44a864f68e019d05b8ad7b15087be53@192.168.88.21:26656"

RPC_PORT=27657; P2P_PORT=27656; GRPC_PORT=27090; GRPC_WEB=27091; REST_PORT=27317; PPROF=27060
NODE_URL="tcp://127.0.0.1:$RPC_PORT"
LOG=$WORK/logs
FEES="--gas auto --gas-adjustment 1.6 --fees 200000$DENOM"

say() { echo "[$(date +%H:%M:%S)] $*"; }
die() { echo "FAIL: $*" >&2; exit 1; }
mkdir -p $WORK $LOG
# one log file per phase invocation — reruns used to clobber the log that held
# the failure we were trying to read
new_log() { echo "$LOG/$1-$(date +%Y%m%d-%H%M%S).log"; }

set_ports() { # set_ports <home>
  local H=$1
  sed -i "s#^laddr = \"tcp://127.0.0.1:26657\"#laddr = \"tcp://127.0.0.1:$RPC_PORT\"#" $H/config/config.toml
  sed -i "s#^laddr = \"tcp://0.0.0.0:26656\"#laddr = \"tcp://0.0.0.0:$P2P_PORT\"#" $H/config/config.toml
  sed -i "s#^pprof_laddr = \"localhost:6060\"#pprof_laddr = \"localhost:$PPROF\"#" $H/config/config.toml
  sed -i "s#^address = \"tcp://localhost:1317\"#address = \"tcp://localhost:$REST_PORT\"#" $H/config/app.toml
  sed -i "s#^address = \"localhost:9090\"#address = \"localhost:$GRPC_PORT\"#" $H/config/app.toml
  sed -i "s#^address = \"localhost:9091\"#address = \"localhost:$GRPC_WEB\"#" $H/config/app.toml
  sed -i "s#^minimum-gas-prices = \"\"#minimum-gas-prices = \"0$DENOM\"#" $H/config/app.toml
  sed -i "/^\\[api\\]/,/^\\[/{s/^enable = false/enable = true/;}" $H/config/app.toml
}

wait_height() { # wait_height <target> <logfile> [timeout_s]
  # InitChain over the 243MB fork genesis takes a long, unpredictable time, so
  # the real liveness signal is "the process is still up", not a wall clock.
  local target=$1 log=$2 timeout=${3:-4200} start=$(date +%s) h
  while true; do
    pgrep -f -- "--home $FORK_HOME" > /dev/null || {
      echo "--- last 30 lines of $log ---" >&2; tail -30 "$log" >&2
      die "node exited before reaching height $target"
    }
    h=$(curl -s http://127.0.0.1:$RPC_PORT/status 2>/dev/null | jq -r '.result.sync_info.latest_block_height // "0"')
    [ -n "$h" ] && [ "$h" != "null" ] && [ "$h" -ge "$target" ] 2>/dev/null && { echo $h; return 0; }
    [ $(($(date +%s)-start)) -gt $timeout ] && die "timeout waiting for height $target (at ${h:-none}, see $log)"
    sleep 5
  done
}

kill_fork() {
  pkill -f -- "--home $SS_HOME" 2>/dev/null || true
  pkill -f -- "--home $FORK_HOME" 2>/dev/null || true
  for i in $(seq 1 30); do
    pgrep -f -- "--home $WORK" > /dev/null 2>&1 || break
    [ $i -eq 15 ] && pkill -9 -f -- "--home $WORK" 2>/dev/null
    sleep 1
  done
}

cmd_statesync() {
  say "--- state-sync throwaway from srv11 (real mainnet state, zero prod impact) ---"
  kill_fork
  rm -rf $SS_HOME
  $BIN_V8 init ss-throwaway --chain-id dungeon-1 --home $SS_HOME > /dev/null 2>&1
  # real network genesis so light client + p2p handshake work
  cp $HOME/.dungeonchain/config/genesis.json $SS_HOME/config/genesis.json
  set_ports $SS_HOME

  LATEST=$(curl -s $TRUST_RPC/status | jq -r '.result.sync_info.latest_block_height')
  TRUST_H=$((LATEST - 2000))
  TRUST_HASH=$(curl -s "$TRUST_RPC/block?height=$TRUST_H" | jq -r '.result.block_id.hash')
  say "  trust: height=$TRUST_H hash=$TRUST_HASH"
  CFG=$SS_HOME/config/config.toml
  sed -i "s/^enable = false/enable = true/" $CFG          # [statesync]
  sed -i "s#^rpc_servers = \"\"#rpc_servers = \"$TRUST_RPC,$TRUST_RPC\"#" $CFG
  sed -i "s/^trust_height = 0/trust_height = $TRUST_H/" $CFG
  sed -i "s/^trust_hash = \"\"/trust_hash = \"$TRUST_HASH\"/" $CFG

  # peer via CLI flag — sed on persistent_peers proved unreliable (a silent
  # no-match left the node peerless, discovering snapshots forever)
  SSLOG=$(new_log ss)
  nohup $BIN_V8 start --home $SS_HOME \
    --p2p.persistent_peers "$SRV11_PEER" > $SSLOG 2>&1 &
  say "  syncing (pid $!) — waiting for catching_up=false (log: $SSLOG)"
  for i in $(seq 1 240); do
    sleep 10
    CU=$(curl -s http://127.0.0.1:$RPC_PORT/status 2>/dev/null | jq -r '.result.sync_info.catching_up | if . == null then "starting" else tostring end')
    H=$(curl -s http://127.0.0.1:$RPC_PORT/status 2>/dev/null | jq -r '.result.sync_info.latest_block_height // "0"')
    [ "$CU" = "false" ] && [ "$H" -gt "$TRUST_H" ] 2>/dev/null && { say "  synced at $H"; break; }
    [ $((i % 6)) -eq 0 ] && say "  ... catching_up=$CU height=$H"
    pgrep -f -- "--home $SS_HOME" > /dev/null || die "ss node died (see $SSLOG)"
    [ $i -eq 240 ] && die "state-sync timeout (see $SSLOG)"
  done
  pkill -f -- "--home $SS_HOME"; sleep 5
  say "STATESYNC DONE"
}

cmd_dbcopy() {
  # State-sync from srv11 is blocked by its allow_duplicate_ip=false (HOH on
  # this box is already its peer). Instead: live rsync of HOH's data (dirty),
  # brief validator stop, delta rsync (seconds), restart. Downtime well under
  # the ~12.5min jail margin; HOH is ~9.5% VP so consensus is unaffected.
  [ -n "${SUDO_PW:-}" ] || die "set SUDO_PW for the brief HOH stop/start"
  # Derive the unit from the RUNNING node's cgroup — name-grepping matched
  # dungeon-frost-daemon.service (the bridge signer!) on this box.
  NODE_PID=$(pgrep -f "cosmovisor run start --home $HOME/.dungeonchain" | head -1)
  [ -n "$NODE_PID" ] || die "cannot find the running HOH cosmovisor process"
  UNIT=$(grep -oE "[a-zA-Z0-9_.@-]+\.service" /proc/$NODE_PID/cgroup | head -1)
  [ -n "$UNIT" ] || die "cannot map HOH pid $NODE_PID to a systemd unit"
  case "$UNIT" in *frost*|*bridge*|*relayer*) die "refusing to touch $UNIT";; esac
  say "--- live pre-copy of HOH data (no downtime) — unit=$UNIT ---"
  kill_fork
  rm -rf $SS_HOME
  $BIN_V8 init ss-throwaway --chain-id dungeon-1 --home $SS_HOME > /dev/null 2>&1
  cp $HOME/.dungeonchain/config/genesis.json $SS_HOME/config/genesis.json
  set_ports $SS_HOME
  # dirty copy of a live DB: exit 24 (files vanished mid-copy) is expected
  rsync -a --delete $HOME/.dungeonchain/data/ $SS_HOME/data/ || [ $? -eq 24 ]
  [ ! -d $HOME/.dungeonchain/wasm ] || rsync -a --delete $HOME/.dungeonchain/wasm/ $SS_HOME/wasm/ || [ $? -eq 24 ]
  say "  pre-copy done ($(du -sh $SS_HOME/data | cut -f1))"

  say "--- brief HOH stop for the consistent delta copy ---"
  echo "$SUDO_PW" | sudo -S systemctl stop $UNIT
  trap "echo \"$SUDO_PW\" | sudo -S systemctl start $UNIT" EXIT
  T0=$(date +%s)
  rsync -a --delete $HOME/.dungeonchain/data/ $SS_HOME/data/
  [ -d $HOME/.dungeonchain/wasm ] && rsync -a --delete $HOME/.dungeonchain/wasm/ $SS_HOME/wasm/
  echo "$SUDO_PW" | sudo -S systemctl start $UNIT
  trap - EXIT
  say "  HOH downtime: $(( $(date +%s) - T0 ))s — verifying it resumes"
  for i in $(seq 1 30); do
    sleep 5
    CU=$(curl -s http://127.0.0.1:26657/status 2>/dev/null | jq -r '.result.sync_info.catching_up // "down"')
    [ "$CU" = "false" ] && { say "  HOH back in sync"; break; }
    [ $i -eq 30 ] && die "HOH did not come back cleanly — CHECK $UNIT NOW"
  done
  say "DBCOPY DONE"
}

cmd_export() {
  say "--- exporting real state from the copied home ---"
  pgrep -f -- "--home $SS_HOME" > /dev/null && { pkill -f -- "--home $SS_HOME"; sleep 5; }
  EXPERR=$(new_log export)
  # v8 can't export this state cleanly; v6 can, and the result is what v8 boots.
  ${EXPORT_BIN:-$WORK/dungeond-v6} export --home $SS_HOME > $EXPORTED 2> $EXPERR \
    || die "export failed (see $EXPERR)"
  STRIP=$WORK/strip_localhost.py
  [ -f "$STRIP" ] || STRIP="$(dirname "$0")/strip_localhost.py"
  python3 "$STRIP" "$EXPORTED" || die "localhost strip failed"
  say "EXPORT DONE: $(du -h $EXPORTED | cut -f1) at $EXPORTED"
}

cmd_surgery() {
  say "--- genesis surgery ---"
  rm -rf $FORK_HOME
  $BIN_V8 init forkval --chain-id $CHAIN --home $FORK_HOME > /dev/null 2>&1
  $BIN_V8 keys add forker "${KR[@]}" --output json > $WORK/forker.json 2>&1
  FORKER=$($BIN_V8 keys show forker -a "${KR[@]}")
  PUB=$(jq -r '.pub_key.value' $FORK_HOME/config/priv_validator_key.json)
  python3 "$(dirname "$0")/fork_surgery.py" $EXPORTED $FORKGEN "$PUB" "$FORKER"
  cp $FORKGEN $FORK_HOME/config/genesis.json
  set_ports $FORK_HOME
  sed -i "s#^timeout_commit = \"5s\"#timeout_commit = \"2s\"#" $FORK_HOME/config/config.toml
  say "SURGERY DONE (forker=$FORKER)"
}

start_node() { # start_node <binary> <logfile>
  nohup $1 start --home $FORK_HOME --x-crisis-skip-assert-invariants > $2 2>&1 &
  say "  started $(basename $1) (pid $!)"
}

cmd_run() {
  say "--- booting v8 on the fork (InitChain over real state — can take a while) ---"
  kill_fork
  [ -f $FORKGEN ] || die "no $FORKGEN — run the surgery phase first"
  [ -f $FORK_HOME/config/priv_validator_key.json ] || die "no $FORK_HOME — run the surgery phase first"
  # EVERY run starts from a clean comet state AND the CURRENT fork genesis.
  # Comet persists the genesis validator set in state.db, so a leftover data/
  # replays the pre-surgery set ("genesisValidators[1] != req.Validators[1]"),
  # and a genesis.json left from an earlier surgery boots the wrong doc entirely.
  rm -rf $FORK_HOME/data $FORK_HOME/wasm
  mkdir -p $FORK_HOME/data
  echo '{"height":"0","round":0,"step":0}' > $FORK_HOME/data/priv_validator_state.json
  cp $FORKGEN $FORK_HOME/config/genesis.json
  V8LOG=$(new_log v8)
  start_node $BIN_V8 $V8LOG
  say "  log: $V8LOG"
  INITIAL=$(jq -r '.initial_height // "1"' $FORKGEN)
  H=$(wait_height $((INITIAL+3)) $V8LOG)
  say "  fork producing blocks at $H"

  FORKER=$($BIN_V8 keys show forker -a "${KR[@]}")
  VALOPER=$(jq -r '.app_state.staking.validators | map(select(.status=="BOND_STATUS_BONDED")) | max_by(.tokens|tonumber) | .operator_address' $FORKGEN)
  TX=(--chain-id $CHAIN --node $NODE_URL --keyring-backend test --home $FORK_HOME -y -o json)

  say "--- delegating forker's stake (gov weight) to $VALOPER ---"
  TOTAL=$(jq -r '[.app_state.staking.validators[] | select(.status=="BOND_STATUS_BONDED") | .tokens|tonumber] | add' $FORKGEN)
  DELEG=$(python3 -c "print(int($TOTAL*9))")
  $BIN_V8 tx staking delegate $VALOPER $DELEG$DENOM --from forker "${TX[@]}" $FEES > $LOG/delegate.json 2>&1
  sleep 6

  CUR=$(curl -s http://127.0.0.1:$RPC_PORT/status | jq -r '.result.sync_info.latest_block_height')
  UPH=$((CUR + 90))
  AUTH=$($BIN_V8 query auth module-account gov --node $NODE_URL -o json | jq -r '.account.value.address')
  say "--- gov: v9 upgrade at height $UPH (current $CUR, voting 60s) ---"
  cat > $WORK/prop.json <<EOF
{"messages":[{"@type":"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade","authority":"$AUTH","plan":{"name":"v9","height":"$UPH","info":"fork test","upgraded_client_state":null}}],
 "metadata":"","deposit":"1$DENOM","title":"v9 fork test","summary":"v9 on real state"}
EOF
  $BIN_V8 tx gov submit-proposal $WORK/prop.json --from forker "${TX[@]}" $FEES > $LOG/submit.json 2>&1
  sleep 6
  PID=$($BIN_V8 query gov proposals --node $NODE_URL -o json | jq -r '.proposals[-1].id')
  [ -n "$PID" ] && [ "$PID" != "null" ] || die "prop submit failed (see $LOG/submit.json)"
  $BIN_V8 tx gov vote $PID yes --from forker "${TX[@]}" $FEES > $LOG/vote.json 2>&1
  say "  prop $PID submitted + voted"

  say "--- waiting for halt at $UPH ---"
  for i in $(seq 1 300); do
    grep -qE "UPGRADE \"v9\" NEEDED|CONSENSUS FAILURE" $V8LOG && { say "  halt observed"; break; }
    pgrep -f -- "--home $FORK_HOME" > /dev/null || { say "  v8 exited"; break; }
    sleep 3
  done
  pkill -f -- "--home $FORK_HOME" 2>/dev/null || true; sleep 5

  say "--- swapping to v9 on REAL migrated state ---"
  V9LOG=$(new_log v9)
  start_node $BIN_V9 $V9LOG
  say "  log: $V9LOG"
  NH=$(wait_height $((UPH+3)) $V9LOG)
  grep -q "applying upgrade \"v9\"" $V9LOG || say "  (upgrade-apply line not in log tail — verify below)"
  say "  v9 producing blocks at $NH — REAL-STATE MIGRATION SUCCEEDED"
  cmd_battery
}

cmd_battery() {
  say "--- REAL-STATE BATTERY (v9 binary, forked mainnet state) ---"
  # standalone rerun: the battery only reads/writes through a LIVE fork node
  curl -sf http://127.0.0.1:$RPC_PORT/status > /dev/null \
    || die "no fork node answering on :$RPC_PORT — run the 'run' phase first"
  Q=(--node $NODE_URL -o json)
  FORKER=$($BIN_V9 keys show forker -a "${KR[@]}")
  TX=(--chain-id $CHAIN --node $NODE_URL --keyring-backend test --home $FORK_HOME -y -o json)

  say "[1] min-commission floor on the REAL validator set"
  MINC=$($BIN_V9 query staking params "${Q[@]}" | jq -r '.params.min_commission_rate')
  case "$MINC" in 0.05*) say "  param OK ($MINC)";; *) die "min_commission_rate=$MINC";; esac
  LOW=$($BIN_V9 query staking validators "${Q[@]}" | jq -r '[.validators[] | select((.commission.commission_rates.rate|tonumber) < 0.05)] | length')
  [ "$LOW" = "0" ] || die "$LOW real validators still below the 5% floor"
  say "  all real validators >= 5% OK"

  say "[2] ratelimit module live on migrated state"
  $BIN_V9 query ratelimit list-rate-limits "${Q[@]}" > $LOG/rl.json || die "ratelimit query dead"
  say "  OK $(head -c 60 $LOG/rl.json)"

  say "[3] REAL wasm contracts under wasmvm 2.3.4 (smart queries)"
  FEE_COLLECTOR=dungeon1psz9xsme3rsktg5vk36m0teqdy6w3a78k6el0p83rz7r9nnm4aqsp9epre
  POOL_ROUTER=dungeon1xgv9sxr6cdk0l5pqqx52ark73mf7nrt0g6ae5y5zzgs64zl3nkuslk6l58
  BONDING=dungeon1y3q77hwpu47w8r3lz4zcx94tnze2jdly6lru026ucckdfvld6vusa2hjan
  $BIN_V9 query wasm contract-state smart $FEE_COLLECTOR '{"config":{}}' "${Q[@]}" > $LOG/wq_fc.json \
    || die "fee_collector smart query failed — wasm VM/state broken"
  say "  fee_collector config OK: $(jq -c '.data' $LOG/wq_fc.json | head -c 100)"
  $BIN_V9 query wasm contract-state smart $BONDING '{"config":{}}' "${Q[@]}" > $LOG/wq_bond.json 2>&1 \
    && say "  bonding config OK" || say "  (bonding config query schema mismatch — non-fatal)"
  $BIN_V9 query wasm contract-state all $POOL_ROUTER --limit 2 "${Q[@]}" > $LOG/wq_router.json \
    || die "pool_router raw state read failed"
  say "  pool_router raw state OK"

  say "[4] REAL wasm execute (VM write path)"
  LPSTAKE=dungeon18pp3yplvdlalhk3lzm9tz60nxxqchy29a3nrs9cgwgf9kzdwy90se2mnem
  OUT=$($BIN_V9 tx wasm execute $LPSTAKE '{"claim":{}}' --from forker "${TX[@]}" $FEES 2>&1) || true
  echo "$OUT" > $LOG/wx_claim.json
  CODE=$(echo "$OUT" | grep -m1 '^{' | jq -r '.code // "parse-error"' 2>/dev/null || echo parse-error)
  RAW=$(echo "$OUT" | grep -m1 '^{' | jq -r '.raw_log // ""' 2>/dev/null || echo "")
  if [ "$CODE" = "0" ]; then say "  execute OK (code 0)"
  elif echo "$RAW" | grep -qiE "nothing to claim|no claim|unauthorized|not found|no rewards|no bond"; then
    say "  execute reached contract logic (expected contract-level reject: ${RAW:0:80})"
  else die "wasm execute failed outside contract logic: code=$CODE raw=${RAW:0:120}"; fi

  say "[5] REAL hyperlane/warp + IBC state under new deps"
  $BIN_V9 query hyperlane mailboxes "${Q[@]}" > $LOG/hyp.json || die "hyperlane query dead"
  say "  mailboxes: $(jq -c '.mailboxes | length' $LOG/hyp.json 2>/dev/null || echo '?') entries"
  $BIN_V9 query warp tokens "${Q[@]}" > $LOG/warp.json || die "warp query dead"
  say "  warp tokens: $(jq -c '.tokens | length' $LOG/warp.json 2>/dev/null || echo '?') entries"
  $BIN_V9 query ibc client states "${Q[@]}" > $LOG/ibc.json || die "ibc client query dead"
  say "  ibc clients: $(jq -c '.client_states | length' $LOG/ibc.json 2>/dev/null || echo '?')"

  say "[6] economic core broadcasts on real state"
  VALOPER=$(jq -r '.app_state.staking.validators | map(select(.status=="BOND_STATUS_BONDED")) | max_by(.tokens|tonumber) | .operator_address' $FORKGEN)
  for t in "bank send $FORKER $FORKER 1000000$DENOM" "staking delegate $VALOPER 1000000$DENOM" "distribution set-withdraw-addr $FORKER"; do
    OUT=$($BIN_V9 tx $t --from forker "${TX[@]}" $FEES 2>&1) || true
    CODE=$(echo "$OUT" | grep -m1 '^{' | jq -r '.code // "x"' 2>/dev/null || echo x)
    [ "$CODE" = "0" ] || die "broadcast '$t' code=$CODE"
    say "  OK  tx ${t%% *}"; sleep 4
  done

  say ""
  say "========================================================"
  say "  v9 REAL-STATE FORK TEST PASSED"
  say "  fork node still running for manual poking on :$RPC_PORT"
  say "========================================================"
}

case "${1:-all}" in
  statesync) cmd_statesync ;;
  dbcopy)    cmd_dbcopy ;;
  export)    cmd_export ;;
  surgery)   cmd_surgery ;;
  run)       cmd_run ;;
  battery)   cmd_battery ;;
  all)       cmd_dbcopy; cmd_export; cmd_surgery; cmd_run ;;
  *) die "usage: $0 statesync|dbcopy|export|surgery|run|battery|all" ;;
esac
