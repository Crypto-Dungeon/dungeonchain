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


def main():
    src, dst, pubkey_b64, forker = sys.argv[1:5]
    denom = "udgn"

    with open(src) as f:
        g = json.load(f)

    g["chain_id"] = "dungeon-fork-1"
    g["validators"] = []

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
