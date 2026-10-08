package app

import (
	"fmt"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	clienttypes "github.com/cosmos/ibc-go/v11/modules/core/02-client/types"
	ibcexported "github.com/cosmos/ibc-go/v11/modules/core/exported"
)

// Decode historical records written by IBC v8; client routing remains SDK
// controlled. Adapted from dungeonchain PR #27; wire schema matches
// ibc-go v8.8.0 proto/ibc/lightclients/localhost/v2/localhost.proto.
func registerLegacyLocalhostClientState(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*ibcexported.ClientState)(nil), &LocalhostClientStateV2{})
}

// LocalhostClientStateV2 exists only for historical client-state decoding.
type LocalhostClientStateV2 struct {
	LatestHeight clienttypes.Height `protobuf:"bytes,1,opt,name=latest_height,json=latestHeight,proto3" json:"latest_height"`
}

func (*LocalhostClientStateV2) ProtoMessage()    {}
func (m *LocalhostClientStateV2) Reset()         { *m = LocalhostClientStateV2{} }
func (m *LocalhostClientStateV2) String() string { return m.LatestHeight.String() }
func (*LocalhostClientStateV2) XXX_MessageName() string {
	return "ibc.lightclients.localhost.v2.ClientState"
}
func (*LocalhostClientStateV2) ClientType() string { return ibcexported.Localhost }
func (m *LocalhostClientStateV2) Validate() error {
	if m.LatestHeight.RevisionHeight == 0 {
		return fmt.Errorf("legacy localhost revision height cannot be zero")
	}
	return nil
}
