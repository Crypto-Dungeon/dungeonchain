package app

import (
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	ibcclienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
)

// registerLegacyLocalhostClientState registers the pre-v10 localhost ClientState
// so ClientStates queries can unpack store data written by older ibc-go.
// Codec-only; does not touch store (no apphash impact).
func registerLegacyLocalhostClientState(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations(
		(*ibcexported.ClientState)(nil),
		&LocalhostClientStateV2{},
	)
}

// LocalhostClientStateV2 matches /ibc.lightclients.localhost.v2.ClientState.
type LocalhostClientStateV2 struct {
	LatestHeight ibcclienttypes.Height `protobuf:"bytes,1,opt,name=latest_height,json=latestHeight,proto3" json:"latest_height"`
}

func (*LocalhostClientStateV2) ProtoMessage()    {}
func (m *LocalhostClientStateV2) Reset()         { *m = LocalhostClientStateV2{} }
func (m *LocalhostClientStateV2) String() string { return "ClientState" }

func (*LocalhostClientStateV2) XXX_MessageName() string {
	return "ibc.lightclients.localhost.v2.ClientState"
}

func (*LocalhostClientStateV2) ClientType() string { return ibcexported.Localhost }
func (*LocalhostClientStateV2) Validate() error    { return nil }
