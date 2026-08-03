#!/usr/bin/env python3
"""fork_surgery.py — turn an exported dungeon-1 mainnet genesis into a fork-test genesis.

Surgery performed (same technique as the Passage v4.0.2 forkval):
  1. chain_id -> dungeon-fork-1; top-level validators cleared (InitChain derives
     the set from staking state).
  2. The LARGEST bonded validator's consensus pubkey is replaced with the fork
     node's pubkey, so our single node signs as ~85% of voting power.
  3. gov params: voting_period 60s, expedited 30s, min_deposit 1udgn.
  4. A forker account is injected with a balance LARGER than total bonded stake
     (bank balance + auth account + bank supply bumped consistently), so
     delegate-then-vote passes any proposal inside the 60s window.

Usage: fork_surgery.py <exported_genesis.json> <out_genesis.json> \
           <fork_ed25519_pubkey_base64> <forker_bech32_addr>
"""
import json
import sys
from decimal import Decimal, getcontext

getcontext().prec = 80  # token amounts are ~15 digits + 18 decimal places


def credit(balances, addr, denom, delta):
    """Add delta of denom to an account's bank balance."""
    for e in balances:
        if e["address"] == addr:
            for c in e["coins"]:
                if c["denom"] == denom:
                    c["amount"] = str(int(c["amount"]) + delta)
                    return
            e["coins"].append({"denom": denom, "amount": str(delta)})
            return
    raise SystemExit(f"no bank balance entry for {addr}")


def add_supply(supply, denom, delta):
    for s in supply:
        if s["denom"] == denom:
            s["amount"] = str(int(s["amount"]) + delta)
            return
    supply.append({"denom": denom, "amount": str(delta)})


def main():
    src, dst, pubkey_b64, forker = sys.argv[1:5]
    denom = "udgn"

    with open(src) as f:
        g = json.load(f)

    g["chain_id"] = "dungeon-fork-1"
    g["validators"] = []
    # SDK 0.50+ AppGenesis nests the comet validator set under consensus.validators.
    # Clearing only the top-level list leaves the real mainnet set in the genesis
    # doc -> comet handshake dies with genesisValidators[i] != req.Validators[i].
    if isinstance(g.get("consensus"), dict):
        g["consensus"]["validators"] = []

    app = g["app_state"]

    # --- 2. swap the largest bonded validator's consensus key ---
    vals = app["staking"]["validators"]
    bonded = [v for v in vals if v["status"] == "BOND_STATUS_BONDED"]
    target = max(bonded, key=lambda v: int(v["tokens"]))
    old_key = target["consensus_pubkey"]["key"]
    target["consensus_pubkey"]["key"] = pubkey_b64
    print(f"swapped consensus key of {target['description']['moniker']} "
          f"({int(target['tokens'])/1e6:.0f} DGN): {old_key[:12]}... -> {pubkey_b64[:12]}...")

    total_bonded = sum(int(v["tokens"]) for v in bonded)

    # --- 2b. scale that validator to >2/3 of the bonded set ---
    # Swapping the key is not enough: our node is the only one that can sign, so
    # anything under 2/3 prevotes forever (observed 2026-08-03 — the fork proposed
    # 17861900 and stalled at step 4, prevotes 94595257/368955792 = 0.26). The
    # "~85%" this script was written for is long gone; the top validator is ~26%
    # of 21 bonded validators today. Scale tokens AND delegator_shares by the same
    # integer factor so the share exchange rate — and every existing delegation —
    # stays valid.
    others = total_bonded - int(target["tokens"])
    factor = max(1, -(-(others * 4) // int(target["tokens"])))  # ceil -> >=80% VP
    if factor > 1:
        old_tokens = int(target["tokens"])
        delta = old_tokens * (factor - 1)
        target["tokens"] = str(old_tokens * factor)
        target["delegator_shares"] = f"{Decimal(target['delegator_shares']) * factor:.18f}"

        # staking InitGenesis panics unless the bonded pool holds exactly the sum
        # of bonded tokens, so the pool balance and total supply move with it
        pool = next(a["base_account"]["address"] for a in app["auth"]["accounts"]
                    if a.get("name") == "bonded_tokens_pool")
        credit(app["bank"]["balances"], pool, denom, delta)
        add_supply(app["bank"]["supply"], denom, delta)

        # An EXPORTED genesis takes each validator's comet power from
        # last_validator_powers, NOT from .tokens — miss this and the scale-up is
        # invisible to consensus.
        op = target["operator_address"]
        lv = next(p for p in app["staking"]["last_validator_powers"]
                  if p["address"] == op)
        new_power = old_tokens * factor // 1_000_000
        app["staking"]["last_total_power"] = str(
            int(app["staking"]["last_total_power"]) - int(lv["power"]) + new_power)
        lv["power"] = str(new_power)

        # both of these are exactly what the node would have died on, checked
        # before we spend two minutes writing a 243MB genesis
        bal = next(int(c["amount"]) for e in app["bank"]["balances"]
                   if e["address"] == pool for c in e["coins"] if c["denom"] == denom)
        bonded_now = sum(int(v["tokens"]) for v in bonded)
        assert bal == bonded_now, f"bonded pool {bal} != bonded tokens {bonded_now}"
        assert new_power * 3 > int(app["staking"]["last_total_power"]) * 2, \
            "still under 2/3 voting power — the fork would never commit a block"

        total_bonded += delta
        print(f"scaled {target['description']['moniker']} x{factor} -> "
              f"{new_power * 100 // int(app['staking']['last_total_power'])}% of voting power")

    # --- 3. gov params ---
    gov = app["gov"]["params"]
    gov["voting_period"] = "60s"
    gov["expedited_voting_period"] = "30s"
    gov["min_deposit"] = [{"denom": denom, "amount": "1"}]
    gov["expedited_min_deposit"] = [{"denom": denom, "amount": "2"}]

    # --- 4. forker account: balance = 10x total bonded stake ---
    fund = str(total_bonded * 10)
    app["bank"]["balances"].append(
        {"address": forker, "coins": [{"denom": denom, "amount": fund}]})
    for s in app["bank"]["supply"]:
        if s["denom"] == denom:
            s["amount"] = str(int(s["amount"]) + int(fund))
            break
    else:
        app["bank"]["supply"].append({"denom": denom, "amount": fund})

    # auth account (account_number = current next; exported genesis keeps
    # accounts + params, next number = max existing + 1)
    nums = [int(a.get("account_number", 0)) for a in app["auth"]["accounts"]
            if "account_number" in a]
    acct_num = str(max(nums) + 1 if nums else 0)
    app["auth"]["accounts"].append({
        "@type": "/cosmos.auth.v1beta1.BaseAccount",
        "address": forker,
        "pub_key": None,
        "account_number": acct_num,
        "sequence": "0",
    })
    print(f"forker {forker} funded {int(fund)/1e6:.0f} DGN (acct #{acct_num}); "
          f"total bonded {total_bonded/1e6:.0f} DGN")

    with open(dst, "w") as f:
        json.dump(g, f, separators=(",", ":"))
    print(f"wrote {dst}")


if __name__ == "__main__":
    main()
