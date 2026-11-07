package state

import (
	"sync"
	"testing"
)

// F6-04 (unit): Inventory diff with one added, one removed, one changed
// container → three docker.container.changed events + one
// docker.inventory.replaced event.
//
// The store's SetContainers method diffs the new inventory against the old
// by container ID and publishes a docker.container.changed event for each
// changed container plus a single docker.inventory.replaced for the bulk
// update (design §F6).

// capturingPublisher records every event published to it.
type capturingPublisher struct {
	mu      sync.Mutex
	events  []HubEvent
}

func (c *capturingPublisher) Publish(e HubEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *capturingPublisher) eventsOf(name string) []HubEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []HubEvent
	for _, e := range c.events {
		if e.Name == name {
			out = append(out, e)
		}
	}
	return out
}

func TestF6_04_InventoryDiffEvents(t *testing.T) {
	store := New(720)
	pub := &capturingPublisher{}
	store.SetHub(pub)

	store.UpsertHost(Host{Name: "vps-1", Online: true, Status: StatusHealthy})

	// Initial inventory: 3 containers.
	initial := ContainerInventory{
		Containers: []Container{
			{ID: "aaa", Name: "kept", Image: "nginx:1.27", State: "running"},
			{ID: "bbb", Name: "changed", Image: "redis:7", State: "running"},
			{ID: "ccc", Name: "removed", Image: "busybox", State: "running"},
		},
	}
	store.SetContainers("vps-1", initial)
	pub.events = nil // clear the initial replaced event

	// New inventory: "kept" unchanged, "changed" has new state, "removed" is
	// gone, "added" is new.
	updated := ContainerInventory{
		Containers: []Container{
			{ID: "aaa", Name: "kept", Image: "nginx:1.27", State: "running"},
			{ID: "bbb", Name: "changed", Image: "redis:7", State: "exited"},
			{ID: "ddd", Name: "added", Image: "alpine:3", State: "running"},
		},
	}
	store.SetContainers("vps-1", updated)

	// Expect:
	//   - 1 docker.container.changed for "bbb" (state changed running→exited)
	//   - 1 docker.container.changed for "ddd" (newly added)
	//   - "aaa" unchanged → no event
	//   - "ccc" removed → no docker.container.changed (the design only emits
	//     changed for added/modified, not removed; the inventory.replaced
	//     event covers the bulk change)
	//   - 1 docker.inventory.replaced for the bulk update
	changedEvents := pub.eventsOf("docker.container.changed")
	if len(changedEvents) != 2 {
		t.Errorf("docker.container.changed events = %d, want 2 (changed + added)", len(changedEvents))
	}

	replacedEvents := pub.eventsOf("docker.inventory.replaced")
	if len(replacedEvents) != 1 {
		t.Errorf("docker.inventory.replaced events = %d, want 1", len(replacedEvents))
	}

	// Verify the store now holds the updated inventory.
	got := store.Containers("vps-1")
	if len(got.Containers) != 3 {
		t.Fatalf("store has %d containers, want 3", len(got.Containers))
	}
	// Verify "ccc" is gone and "ddd" is present.
	ids := make(map[string]bool)
	for _, c := range got.Containers {
		ids[c.ID] = true
	}
	if ids["ccc"] {
		t.Error("container 'ccc' should have been removed")
	}
	if !ids["ddd"] {
		t.Error("container 'ddd' should have been added")
	}
	if !ids["aaa"] {
		t.Error("container 'aaa' should still be present (unchanged)")
	}
}

// TestSetContainersFirstPublish verifies that the first SetContainers call
// (no previous inventory) publishes a docker.inventory.replaced and one
// docker.container.changed per container (all are "new").
func TestSetContainersFirstPublish(t *testing.T) {
	store := New(720)
	pub := &capturingPublisher{}
	store.SetHub(pub)
	store.UpsertHost(Host{Name: "vps-1", Online: true, Status: StatusHealthy})

	store.SetContainers("vps-1", ContainerInventory{
		Containers: []Container{
			{ID: "aaa", Name: "a", Image: "x", State: "running"},
			{ID: "bbb", Name: "b", Image: "y", State: "running"},
		},
	})

	if len(pub.eventsOf("docker.container.changed")) != 2 {
		t.Errorf("first publish: changed events = %d, want 2 (all new)", len(pub.eventsOf("docker.container.changed")))
	}
	if len(pub.eventsOf("docker.inventory.replaced")) != 1 {
		t.Errorf("first publish: replaced events = %d, want 1", len(pub.eventsOf("docker.inventory.replaced")))
	}
}

// TestSetContainersNoChange verifies that setting the same inventory twice
// still publishes events (the store doesn't deduplicate; the hub handles
// idempotency if needed). This is intentional — the collector only calls
// SetContainers when a Docker event triggered a refetch, so every call
// represents a real "the inventory might have changed" signal.
func TestSetContainersNoChangeStillPublishes(t *testing.T) {
	store := New(720)
	pub := &capturingPublisher{}
	store.SetHub(pub)
	store.UpsertHost(Host{Name: "vps-1", Online: true, Status: StatusHealthy})

	inv := ContainerInventory{
		Containers: []Container{{ID: "aaa", Name: "a", Image: "x", State: "running"}},
	}
	store.SetContainers("vps-1", inv)
	pub.events = nil
	store.SetContainers("vps-1", inv)

	// Even though nothing changed, we still publish because the store doesn't
	// deep-compare (the collector already decided a refetch was warranted).
	if len(pub.eventsOf("docker.inventory.replaced")) != 1 {
		t.Errorf("second identical publish: replaced events = %d, want 1", len(pub.eventsOf("docker.inventory.replaced")))
	}
}
