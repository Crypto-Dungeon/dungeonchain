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
