# Dungeon IBC integration test harness

Copied from ibc-go v11.2.0/testing (Apache-2.0). The proof/handshake/packet
logic is unchanged. Only the default sample application, application accessors
and SDK55 transaction helper are adapted to Dungeon. Tests must supply an
explicit Dungeon AppCreator; no global SDK54 simapp is constructed.
