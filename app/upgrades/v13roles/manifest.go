package v13roles

// This manifest is pinned to read-only queries of the halted v13.0.1 state.
// Only these five config owner/admin fields may change. Source byte hashes
// bind every other config field; deployed code hashes and migration admins
// must also match. Fantasy remains immutable (no migration administrator).
type Target struct {
	Address, Field       string
	CodeID               uint64
	CodeHash, ConfigHash string
	Canonical            bool
	MigrationAdmin       string
}

var Targets = []Target{
	{Address: "dungeon1643rdx7yzxfhmy6ru6456t2twxhshtt9k394jh8u7qk59qfgz0esjku43x", Field: "owner", CodeID: 160, CodeHash: "b3f3201a35a4c11892ff2007d9570a95028319a855ed3e1174507e9cfa643148", ConfigHash: "b31700bc4c3536634dd2e012c55f8b689cf9ed743f2172b3f93a83e1d4880eb8", Canonical: true, MigrationAdmin: "dungeon10cr0mweqjyz856kyyy03yzj6av3vmy0p0653lh"},  // active factory
	{Address: "dungeon1pk8tmuxs5724j0dwarhvuukvwkykf0pv94hdfp3ejjp2fmkf54rqdjyucm", Field: "owner", CodeID: 19, CodeHash: "01d4b0a893d32869897163645540ff8663843e1060a55fe4a8f8074a76397dbb", ConfigHash: "655380b050a6fed53565a916ba295d5840144794060850f786c0e4781299ef84", Canonical: true, MigrationAdmin: "dungeon10cr0mweqjyz856kyyy03yzj6av3vmy0p0653lh"},   // factory v2
	{Address: "dungeon1dpdsz90c24ckq850gkx37l58qs89y3jy7veuxk54cmx7cvtws7hqd6hec5", Field: "admin", CodeID: 74, CodeHash: "8f7875ed3c2257448aced5873c168bcb5ea53977b143a1194538c89f4599dd94", ConfigHash: "1e61a19a516a4117a9c273a10de868339897a7dc823e7be4c2528323c1425e4a", Canonical: false, MigrationAdmin: ""},                                                // Fantasy
	{Address: "dungeon1vlc6jr3cw06w08g2l0xz40lkykj3kv5scp8ul02pxh59v72lrgcsnkdh6t", Field: "admin", CodeID: 220, CodeHash: "e0a9377973e9a070a4f008652547b7c29c5bfb66f16ae0570db2e21da438f45b", ConfigHash: "95a9563f63a1771ce59dc7fa0395c028bb1647ed524971b3156369b95e47dda4", Canonical: false, MigrationAdmin: "dungeon10cr0mweqjyz856kyyy03yzj6av3vmy0p0653lh"}, // Castle v3
	{Address: "dungeon16w597rcqxvdwmgqry8npvy2jxctuu6p5puw7xjtzsz5792fd7kpse9q7an", Field: "admin", CodeID: 208, CodeHash: "6f43b6f21c4d8949db52ad7fe62de5b5e42fd6a3639c2eee30613d51e7cb7604", ConfigHash: "04f7f5c04b912ae3d35ae97301e295019b0f0eecfac1ed2e2a090d4591357a47", Canonical: false, MigrationAdmin: "dungeon10cr0mweqjyz856kyyy03yzj6av3vmy0p0653lh"}, // Castle test
}
