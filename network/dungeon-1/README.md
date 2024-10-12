# Installation

```bash
git clone https://github.com/Crypto-Dungeon/dungeonchain.git
cd dungeonchain
git checkout v0.1.0

# install the binary:
make install

# for docker:
make local-image

# e44429f3f67c2c57ced7be208092264aed712e6d7b4140e03878552d50dbb2e8
shasum -a 256 $(which dungeond)
```

<!-- cp $(which dungeond) . && tar -czvf dungeond-0.1.0-amd64.tar.gz ./dungeond -->
<!-- tar -xvzf dungeond-0.1.0-amd64.tar.gz -->


<!--

```bash
# decompress archive
tar -xvzf genesis.json.tar.gz "./network/dungeon-1/genesis.json" --one-top-level=genesis.json --strip-components 4

# Move to testnets dir
cp genesis.json.tar.gz /home/reece/Desktop/ics-testnets/interchain-security/dungeon-1

cat genesis.json | jq .app_state.ccvconsumer > dungeon-ccv.json

cp genesis.json tmp-without-ccv.json
jq 'del(.app_state.ccvconsumer)' tmp-without-ccv.json > dungeon-genesis-tmp-without-ccv.json
rm tmp-without-ccv.json genesis.json
```

-->

