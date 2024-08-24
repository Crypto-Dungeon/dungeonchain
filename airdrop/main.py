"""
ref: readme.md
"""

import json

from atypes import Delegation, User, address_convert

# BASE

JUNO_USD = 0.1207 # time at snapshot, 2024-07-21 19:14:09
ATOM_USD = 6.32

TOTAL_AIRDROP = 100_000_000 * 1_000000 # 100m udragon
MAX_SHARE_LIMIT = 40

# SHARE_CONVERSION_RATE = TOTAL_AIRDROP / total_share
# print(f"1 share = 245144905udragon, rounding to 245DRAGON per share

# shares
ATOM_TIERS = {
    # uATOM -> SHARES
    1000 * 1_000_000: 5,
    200 * 1_000_000: 3,
    50 * 1_000_000: 1,  # Holding 51-199 still is only the 50 group
    0: 0,
}
ATOM_TIERS = dict(sorted(ATOM_TIERS.items(), reverse=True))


def get_tier(uatomAmt: int) -> int:
    for tier, value in ATOM_TIERS.items():
        if uatomAmt >= tier:
            return value

    return 0

# convert_juno_to_atom takes the USD prices and converts perfectly between them as if they had staked atom
def convert_juno_to_atom(ujuno: int) -> int:
    whole_juno = ujuno / 1_000_000
    usd_value = whole_juno * JUNO_USD

    atom_equivalent = usd_value / ATOM_USD
    return int(atom_equivalent * 1_000_000)


# Gets all JUNO delegators to crypto dungeon
# Converts their ujuno to uatom based on the JUNO_ATOM_CONVERSION rate
# Returns a dictionary of cosmos1 address -> uatom increase to add to their uatom stake on the hub
def get_juno_atom_conversion_boost() -> dict[str, int]:
    CRYPTO_DUNGEON_JUNO_VALIDATOR = "junovaloper13x4pynlp86prhcmtns742kgsgu7pjtzjeknkwx"

    with open(f"./snapshots/juno_delegators.json") as f:
        juno_delegators = Delegation.load_all(json.load(f))

    juno_shares = {}
    for jd in juno_delegators:
        if jd.validator_address != CRYPTO_DUNGEON_JUNO_VALIDATOR:
            continue

        addr = address_convert(jd.delegator_address, "cosmos")

        if addr not in juno_shares:
            juno_shares[addr] = 0

        juno_shares[addr] += int(jd.shares)

    # remove any delegators with less than 1_000_000ujuno delegated (too small to convert)
    updated_shares = {k: v for k, v in juno_shares.items() if v >= 1_000_000}

    # convert to their ATOM equivalent
    converted = {
        k: int(convert_juno_to_atom(ujuno)) for k, ujuno in updated_shares.items()
    }

    converted = dict(sorted(converted.items(), key=lambda x: x[1], reverse=True))

    return converted


def get_dungeon_nft_holders() -> dict[str, None]:
    with open(f"./snapshots/crytpodungeon.json") as f:
        nft_holders = dict(json.load(f))["holders"]

    holders = {}
    for addr, _ in nft_holders.items():
        holders[address_convert(addr, "cosmos")] = None

    return holders

# get_streamswap_buyers returns the addresses & udgn tokens purchased
def get_streamswap_buyers() -> dict[str, int]:
    # https://app.streamswap.io/osmosis/stream/ATOM/4
    # snapshot from the streamswap team
    with open(f"./snapshots/streamswap_osmosis.csv") as f:
        data = f.readlines()

    # skip line 0
    # Account Address,Spent(ATOM),Purchased(ssDRAGON)
    data = data[1:]

    buyers = {}
    for line in data:
        addr, _, purchased = line.split(",")
        buyers[address_convert(addr, "cosmos")] = int(float(purchased) * 1_000_000)

    # buyer validation check
    total_purchased = sum(buyers.values())
    if total_purchased < 99999999.99:
        print(f"Total DRAGON purchased: {total_purchased}DRAGON")
        print("Streamswap total purchased is less than 100m, please check")
        exit(1)

    return buyers

# manual_additions returns the addresses & udgn tokens manually given by the team,
# despite these users not meeting the airdrop requirements in any way.
def manual_additions() -> dict[str, int]:
    with open(f"./snapshots/manual_additions.txt") as f:
        data = f.readlines()

    manual: dict[str, int] = {}
    for line in data:
        # Judestru	cosmos12dq9cytc55u24r2raw2puuh8xpcrtvd3528jjf					490 DGN
        try:
            name, address, _, _, _, _, dragon = line.split("\t")

            address = address.strip()
            if not address.startswith('cosmos'):
                address = address_convert(address, "cosmos")
                print("errr", address)
                continue

            # remove any white sopace or non numerical cahracters from dragon
            dragon = dragon.lower().replace(" ", "").replace("dgn", "").replace("ddgn", "").replace("d", "")

            manual[address] = int(float(dragon) * 1_000_000)
        except Exception as e:
            print(e)
            print(line)
    return manual

def get_mad_sci_holders() -> dict[str, None]:
    holders = {}

    with open(f"./snapshots/mad_sci_stars.json") as f:
        nft_holders: dict = dict(json.load(f))["holders"]
        for addr, _ in nft_holders.items():
            holders[address_convert(addr, "cosmos")] = None

    with open(f"./snapshots/mad_sci_osmo.json") as f:
        nft_holders: dict = dict(json.load(f))["holders"]
        for addr, _ in nft_holders.items():
            holders[address_convert(addr, "cosmos")] = None

    return holders

def get_cosmos_delegators() -> list[Delegation]:
    with open(f"./snapshots/cosmos_delegators.json") as f:
        cosmos_delegators = Delegation.load_all(json.load(f))

    return cosmos_delegators

def get_cosmos_delegators_combined_total() -> dict[str, int]:
    cosmos_delegators = get_cosmos_delegators()

    total_staked = {}
    for d in cosmos_delegators:
        if d.delegator_address not in total_staked:
            total_staked[d.delegator_address] = 0

        total_staked[d.delegator_address] += int(d.shares)

    return total_staked

def main():
    total_share_airdrop_allocations: dict[str, User] = {}

    j = get_juno_atom_conversion_boost()
    cdNFT = get_dungeon_nft_holders()
    madSci = get_mad_sci_holders()

    # get which teir they are in regardless of delegations
    combined_total: dict[str, int] = get_cosmos_delegators_combined_total()

    streamswap: dict[str, int] = get_streamswap_buyers()

    # added on at the end
    manual = manual_additions()

    for d in get_cosmos_delegators():
        # do not repeat since we already get all staked shares
        if d.delegator_address in total_share_airdrop_allocations:
            continue

        staked_shares = combined_total[d.delegator_address]

        # == APPLY JUNO CONVERSION INCREASE TO BASE ATOM SHARES IF APPLICABLE ==
        # Must be done before getting the cosmos tier
        if d.delegator_address in j:
            staked_shares += j[d.delegator_address]

        # get their share of the airdrop (100m)
        share_alloc = get_tier(staked_shares)

        u = User(d.delegator_address, share_alloc)

        # == MULTIPLIERS ==

        # CD atom staker
        if d.is_crypto_dungeon_delegator == True:
            u.mult *= 2

        # OG NFT holder
        if d.delegator_address in cdNFT:
            u.mult *= 2

        # Mad Sci holder (stargaze or Osmosis)
        if d.delegator_address in madSci:
            u.mult *= 2

        u.shares *= u.mult
        if u.shares > MAX_SHARE_LIMIT:
            u.shares = MAX_SHARE_LIMIT

        total_share_airdrop_allocations[d.delegator_address] = u

    allocs = [{
        "address": a.address,
        "shares": a.shares,
        "dragon": a.get_allocation(),
        # "multiplier": a.mult # confusing to see, just visual. already handled with `u.shares *= u.mult`
    } for a in total_share_airdrop_allocations.values()]
    get_unique_shares(allocs)

    # iterate styreamswap addresses & add to allocs if found, if not found, append new
    for addr, udgn in streamswap.items():
        # update the value in allocs if found
        found = False
        for a in allocs:
            if a["address"] == addr:
                a["dragon"] += (udgn/1_000_000)
                found = True
                break

        if not found:
            print(f"Streamswap buyer {addr} not found in airdrop allocations, adding new entry")
            allocs.append({
                "address": addr,
                "shares": 0,
                "dragon": (udgn/1_000_000)
            })

    for addr, udgn in manual.items():
        # update the value in allocs if found
        found = False
        for a in allocs:
            if a["address"] == addr:
                a["dragon"] += (udgn/1_000_000)
                found = True
                break

        if not found:
            print(f"Manual buyer {addr} not found in airdrop allocations, adding new entry")
            allocs.append({
                "address": addr,
                "shares": 0,
                "dragon": (udgn/1_000_000)
            })

    s = sorted(allocs, key=lambda x: x["dragon"], reverse=True)

    with open(f"FINAL_ALLOCATION.json", "w") as f:
        print(f"Output saved to FINAL_ALLOCATION.json")
        json.dump(s, f, indent=2)



def get_unique_shares(v: list[dict]):
    unique_num_of_shares = {}

    # print total number of dragon
    total_dragon = sum([a["dragon"] for a in v])
    print(f"Total Dragon being Airdroped: {total_dragon:,}DRAGON")

    # for each number of a.shares, add +=1

    for a in v:
        if a["shares"] not in unique_num_of_shares:
            unique_num_of_shares[a["shares"]] = 0

        unique_num_of_shares[a["shares"]] += 1

    print("Unique number of shares:")
    print(unique_num_of_shares)

if __name__ == "__main__":
    main()
