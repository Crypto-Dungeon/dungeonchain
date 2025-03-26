# **Welcome to DungeonChain Hardfork – Mainnet Edition**

‼️ **WARNING: DO NOT LOSE OR EXPOSE YOUR `priv_validator_key.json` FILE!** ‼️  
This file is **critical** for your validator's security and operation. Losing or leaking it can lead to slashing, double-signing, or losing control over your validator. Ensure that you **safely copy** this file if required, and do not share it publicly!

---

Dear Validators,

This instruction describes the **mainnet hardfork** of the `dungeon-1` network and the final **exit from ICS**. Please follow the steps carefully to ensure a smooth transition.

---

## **1️⃣ Stopping the Old Chain**

This step should be executed by all validators **before 27 March 16:00 UTC**.

To stop the current chain at the designated block height, set the following parameter in your `app.toml`:

```toml
halt-height = 7824000
```

Then restart your node to apply the change:

```bash
systemctl restart dungeond
```

🔸 This ensures your node **automatically stops** when the chain reaches block height `7824000`.

Once your node stops:

```bash
systemctl stop dungeond
```

Wait for further instructions before making any changes.

---

## **2️⃣ Exporting State and Generating New Genesis**

The core team will:

- Export the current application state of the chain
- Generate a new `genesis.json` without ICS features
- Publish the new genesis file for all validators

Do **not** start your node until the new genesis is available.

---

## **3️⃣ Preparing New Environment**

This step should be performed **only after the old chain has been completely stopped.** We will post information about it.

Make sure you use a clean environment or wipe old data (keeping keys and configs):

```bash
rm -rf ~/.dungeonchain/data ~/.dungeonchain/wasm
```

Then, replace the old `genesis.json` with the new one:

```bash
cd ~/.dungeonchain/config
rm -f genesis.json
wget <new-genesis-url>
```

📌 **Genesis URL will be posted after the old chain is stopped.**

---

## **4️⃣ Upgrade to Version 4.0.0**

Install the updated binary:

```bash
git clone git@github.com:Crypto-Dungeon/dungeonchain.git
cd dungeonchain
git checkout v4.0.0
make build
cd build
```

Verify version:

```bash
./dungeond version
```

Should return: `4.0.0`

Replace the current binary with the one from the `build` directory.

---

## **5️⃣ Start the Node**

Start the node:
```bash
sudo systemctl start dungeond
```

This is the final transition of `dungeon-1` from ICS. Let's make it smooth and secure! 🛡️🚀
