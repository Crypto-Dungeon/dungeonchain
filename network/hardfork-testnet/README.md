# **Welcome to DungeonChain Hardfork Test – March 20, 15:00 UTC**

‼️ **WARNING: DO NOT LOSE OR EXPOSE YOUR `priv_validator_key.json` FILE!** ‼️  
This file is **critical** for your validator's security and operation. Losing or leaking it can lead to slashing, double-signing, or losing control over your validator. Ensure that you **safely copy** this file from your mainnet validator node to the testnet node without sharing it publicly!  

---
Dear Validators,

This instruction is a **test version** of how to exit the `dungeon-1` network from ICS. Please take it as seriously as possible.

In the current testnet run, you will need a **separate server from mainnet** to successfully run the testnet.

---

## **Introduction**

This testnet will simulate a **hardfork of the dungeon-1 network** and an **exit from ICS**. We will verify:

- Network consensus
- Balances
- Staked assets
- Governance  
  to ensure that no mistakes occur in the **mainnet hardfork**.

---

## **Preparation**

### **Server Requirements**

- You **must** use a separate server **without the dungeon-1 validator** running on it.
- However, you **must copy** the `priv_validator_key.json` file from your **mainnet validator node** to the testnet node.
- You also need to **restore your validator wallet** on the testnet node.

### **1️⃣ Install Go**

```bash
VERSION="1.23.6"
cd $HOME
wget "https://golang.org/dl/go$VERSION.linux-amd64.tar.gz"
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf "go$VERSION.linux-amd64.tar.gz"
rm "go$VERSION.linux-amd64.tar.gz"
echo "export PATH=$PATH:/usr/local/go/bin" >> ~/.profile
source ~/.profile
```

### **2️⃣ Install DungeonChain v4.0.0**

```bash
git clone git@github.com:Crypto-Dungeon/dungeonchain.git
cd dungeonchain
git checkout v4.0.0
make install
```

### **3️⃣ Initialize the Node**

```bash
./dungeond init <node-name> --chain-id dungeon-reborn-test
```

### **4️⃣ Restore Your Validator Key**

```bash
./dungeond keys add <wallet-name> --recover
```

**‼️ Important:**  
🔹 Remove the existing `priv_validator_key.json` on your testnet node.  
🔹 Copy `priv_validator_key.json` from your **mainnet validator node** to the testnet node.

### **5️⃣ Create a Systemd Service File**

```bash
SERVICE_FILE="/etc/systemd/system/dungeond.service"
USER=$(whoami)

sudo cat <<EOL > $SERVICE_FILE
[Unit]
Description=dungeond Daemon
After=network-online.target

[Service]
User=$USER
ExecStart=$HOME/go/bin/dungeond start
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOL

sudo chmod 644 $SERVICE_FILE
sudo systemctl daemon-reload
```

---

## **Testnet Start Date: March 20, 15:00 UTC**

### **6️⃣ Download New Genesis File**

A new `genesis.json` file will be required to launch the node.

🔹 A link to the new **genesis.json** will be published closer to the testnet date.

Run the following commands to update it:

```bash
cd ~/.dungeonchain/config
rm -f genesis.json
wget <new-genesis-url>
```

---

## **Launching the Node**

```bash
sudo systemctl enable dungeond.service && sudo systemctl start dungeond.service && journalctl -u dungeond.service -f
```

---

## **Tasks for Validators**

Since **validator public keys have changed**, all validators in the testnet should carefully check that all core functionalities work as expected:

✅ **Block signing**  
✅ **Commission claiming**  
✅ **Staking operations**  
✅ **Rewards distribution**  
✅ **Governance and voting**

Make sure your validator is properly participating in consensus and performing all operations correctly.

---

This test is crucial for ensuring a smooth transition for the **mainnet hardfork**. Please report any issues you encounter. 🚀
