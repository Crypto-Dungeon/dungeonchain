import json, sys
p = sys.argv[1]
g = json.load(open(p))
cg = g.get("app_state", {}).get("ibc", {}).get("client_genesis", {})
before = len(cg.get("clients", []))
cg['clients'] = [c for c in cg.get("clients", []) if not str(c.get("client_id", "")).startswith("09-localhost")]
cg["clients_consensus"] = [c for c in cg.get("clients_consensus", []) if not str(c.get("client_id", "")).startswith("09-localhost")]
json.dump(g, open(p, "w"))
print(f"stripped {before - len(cg['clients'])} localhost client(s)")
