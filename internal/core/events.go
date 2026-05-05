package core

type EventName string

const (
	EventNodeStatusChanged      EventName = "node.status.changed"
	EventNodePortsReplaced      EventName = "node.ports.replaced"
	EventNodePortUpserted       EventName = "node.port.upserted"
	EventNodePortRemoved        EventName = "node.port.removed"
	EventNodeContainersReplaced EventName = "node.containers.replaced"
	EventNodeServicesReplaced   EventName = "node.services.replaced"
	EventNodeForwardsReplaced   EventName = "node.forwards.replaced"

	EventSnapshotUpdated EventName = "snapshot.updated"

	EventTopologyPatch    EventName = "topology.patch"
	EventTopologyReset    EventName = "topology.reset"
	EventTopologySnapshot EventName = "topology.snapshot"
)

func (n EventName) String() string {
	return string(n)
}
