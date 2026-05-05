package runtime

import (
	"strconv"
	"strings"
	"sync"

	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/store"
	"github.com/wf-pro-dev/tailflow/internal/topology"
)

type ProjectionIndex struct {
	mu                 sync.RWMutex
	scope              map[core.NodeName]topology.ScopedIDs
	scopeByPortKey     map[core.NodeName]map[string]topology.ScopedIDs
	scopeByForwardKey  map[core.NodeName]map[string]topology.ScopedIDs
	scopeByServiceKey  map[core.NodeName]map[string]topology.ScopedIDs
	scopeByContainerID map[core.NodeName]map[string]topology.ScopedIDs
}

func NewProjectionIndex() *ProjectionIndex {
	return &ProjectionIndex{
		scope:              make(map[core.NodeName]topology.ScopedIDs),
		scopeByPortKey:     make(map[core.NodeName]map[string]topology.ScopedIDs),
		scopeByForwardKey:  make(map[core.NodeName]map[string]topology.ScopedIDs),
		scopeByServiceKey:  make(map[core.NodeName]map[string]topology.ScopedIDs),
		scopeByContainerID: make(map[core.NodeName]map[string]topology.ScopedIDs),
	}
}

func (p *ProjectionIndex) Reset(snapshot topology.Snapshot) {
	p.mu.Lock()
	defer p.mu.Unlock()

	scopes, portScopes, forwardScopes, serviceScopes, containerScopes := buildProjectionScopes(snapshot)
	p.scope = scopes
	p.scopeByPortKey = portScopes
	p.scopeByForwardKey = forwardScopes
	p.scopeByServiceKey = serviceScopes
	p.scopeByContainerID = containerScopes
}

func (p *ProjectionIndex) ScopeForNode(nodeName core.NodeName) topology.ScopedIDs {
	p.mu.RLock()
	defer p.mu.RUnlock()

	scope, ok := p.scope[nodeName]
	if !ok {
		return topology.ScopedIDs{}
	}
	return cloneScopedIDs(scope)
}

func (p *ProjectionIndex) ScopeForForwardKeys(nodeName core.NodeName, keys []string) topology.ScopedIDs {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return scopeForKeys(p.scopeByForwardKey[nodeName], keys)
}

func (p *ProjectionIndex) ScopeForPortKeys(nodeName core.NodeName, keys []string) topology.ScopedIDs {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return scopeForKeys(p.scopeByPortKey[nodeName], keys)
}

func (p *ProjectionIndex) ScopeForServiceKeys(nodeName core.NodeName, keys []string) topology.ScopedIDs {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return scopeForKeys(p.scopeByServiceKey[nodeName], keys)
}

func (p *ProjectionIndex) ScopeForContainerIDs(nodeName core.NodeName, keys []string) topology.ScopedIDs {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return scopeForKeys(p.scopeByContainerID[nodeName], keys)
}

func buildProjectionScopes(snapshot topology.Snapshot) (map[core.NodeName]topology.ScopedIDs, map[core.NodeName]map[string]topology.ScopedIDs, map[core.NodeName]map[string]topology.ScopedIDs, map[core.NodeName]map[string]topology.ScopedIDs, map[core.NodeName]map[string]topology.ScopedIDs) {
	serviceIDsByNode := make(map[core.NodeName]map[string]struct{})
	runtimeIDsByNode := make(map[core.NodeName]map[string]struct{})
	exposureIDsByNode := make(map[core.NodeName]map[string]struct{})
	routeIDsByService := make(map[string]map[string]struct{})
	routeIDsByExposure := make(map[string]map[string]struct{})
	routeIDsByRuntime := make(map[string]map[string]struct{})
	hopIDsByRoute := make(map[string]map[string]struct{})
	evidenceIDsByRoute := make(map[string]map[string]struct{})
	exposureByID := make(map[string]store.Exposure)
	routesByID := make(map[string]store.Route)

	for _, service := range snapshot.Services {
		addProjectionNodeRef(serviceIDsByNode, service.PrimaryNode, service.ID)
	}
	for _, runtime := range snapshot.Runtimes {
		addProjectionNodeRef(runtimeIDsByNode, runtime.NodeID, runtime.ID)
	}
	for _, exposure := range snapshot.Exposures {
		addProjectionNodeRef(exposureIDsByNode, exposure.NodeID, exposure.ID)
		exposureByID[exposure.ID] = exposure
	}
	for _, route := range snapshot.Routes {
		routesByID[route.ID] = route
		addProjectionRef(routeIDsByService, route.SourceServiceID, route.ID)
		addProjectionRef(routeIDsByService, route.TargetServiceID, route.ID)
		addProjectionRef(routeIDsByExposure, route.SourceExposureID, route.ID)
		addProjectionRef(routeIDsByRuntime, route.TargetRuntimeID, route.ID)
	}
	for _, hop := range snapshot.RouteHops {
		addProjectionRef(hopIDsByRoute, hop.RouteID, hop.ID)
		if hop.EvidenceID != "" {
			addProjectionRef(evidenceIDsByRoute, hop.RouteID, hop.EvidenceID)
		}
	}

	out := make(map[core.NodeName]topology.ScopedIDs)
	portOut := make(map[core.NodeName]map[string]topology.ScopedIDs)
	forwardOut := make(map[core.NodeName]map[string]topology.ScopedIDs)
	serviceOut := make(map[core.NodeName]map[string]topology.ScopedIDs)
	containerOut := make(map[core.NodeName]map[string]topology.ScopedIDs)
	for _, node := range snapshot.Nodes {
		scope := newProjectionScope()
		for serviceID := range serviceIDsByNode[node.Name] {
			scope.ServiceIDs[serviceID] = struct{}{}
			for routeID := range routeIDsByService[serviceID] {
				scope.RouteIDs[routeID] = struct{}{}
			}
		}
		for runtimeID := range runtimeIDsByNode[node.Name] {
			scope.RuntimeIDs[runtimeID] = struct{}{}
			for routeID := range routeIDsByRuntime[runtimeID] {
				scope.RouteIDs[routeID] = struct{}{}
			}
		}
		for exposureID := range exposureIDsByNode[node.Name] {
			scope.ExposureIDs[exposureID] = struct{}{}
			for routeID := range routeIDsByExposure[exposureID] {
				scope.RouteIDs[routeID] = struct{}{}
			}
		}
		for routeID := range scope.RouteIDs {
			for hopID := range hopIDsByRoute[routeID] {
				scope.HopIDs[hopID] = struct{}{}
			}
			for evidenceID := range evidenceIDsByRoute[routeID] {
				scope.EvidenceIDs[evidenceID] = struct{}{}
			}
		}
		out[node.Name] = scope

		portScopes := make(map[string]topology.ScopedIDs)
		forwardScopes := make(map[string]topology.ScopedIDs)
		serviceScopes := make(map[string]topology.ScopedIDs)
		containerScopes := make(map[string]topology.ScopedIDs)

		for _, port := range node.Ports {
			portScope := newProjectionScope()
			for _, exposure := range snapshot.Exposures {
				if exposure.NodeID != node.Name || exposure.Port != port.Port {
					continue
				}
				portScope.ExposureIDs[exposure.ID] = struct{}{}
				if exposure.ServiceID != "" {
					portScope.ServiceIDs[exposure.ServiceID] = struct{}{}
					for routeID := range routeIDsByService[exposure.ServiceID] {
						portScope.RouteIDs[routeID] = struct{}{}
					}
				}
				for routeID := range routeIDsByExposure[exposure.ID] {
					portScope.RouteIDs[routeID] = struct{}{}
				}
			}
			for _, runtime := range snapshot.Runtimes {
				if runtime.NodeID != node.Name || !containsPort(runtime.Ports, port.Port) {
					continue
				}
				portScope.RuntimeIDs[runtime.ID] = struct{}{}
				if runtime.ServiceID != "" {
					portScope.ServiceIDs[runtime.ServiceID] = struct{}{}
					for routeID := range routeIDsByService[runtime.ServiceID] {
						portScope.RouteIDs[routeID] = struct{}{}
					}
				}
				for routeID := range routeIDsByRuntime[runtime.ID] {
					portScope.RouteIDs[routeID] = struct{}{}
				}
			}
			for routeID := range portScope.RouteIDs {
				for hopID := range hopIDsByRoute[routeID] {
					portScope.HopIDs[hopID] = struct{}{}
				}
				for evidenceID := range evidenceIDsByRoute[routeID] {
					portScope.EvidenceIDs[evidenceID] = struct{}{}
				}
			}
			portScopes[listenPortKey(port)] = portScope
		}

		for _, service := range snapshot.Services {
			if service.PrimaryNode != node.Name {
				continue
			}
			serviceScope := newProjectionScope()
			serviceScope.ServiceIDs[service.ID] = struct{}{}
			for _, runtimeID := range service.RuntimeIDs {
				if runtimeID == "" {
					continue
				}
				serviceScope.RuntimeIDs[runtimeID] = struct{}{}
			}
			for _, exposureID := range service.ExposureIDs {
				if exposureID == "" {
					continue
				}
				serviceScope.ExposureIDs[exposureID] = struct{}{}
			}
			for routeID := range routeIDsByService[service.ID] {
				serviceScope.RouteIDs[routeID] = struct{}{}
				for hopID := range hopIDsByRoute[routeID] {
					serviceScope.HopIDs[hopID] = struct{}{}
				}
				for evidenceID := range evidenceIDsByRoute[routeID] {
					serviceScope.EvidenceIDs[evidenceID] = struct{}{}
				}
			}
			for _, exposure := range snapshot.Exposures {
				if exposure.NodeID != node.Name || exposure.ServiceID != service.ID {
					continue
				}
				key := strings.Join([]string{
					exposure.ServiceID,
					strconv.FormatUint(uint64(exposure.Port), 10),
					strings.ToLower(exposure.Protocol),
					strings.ToLower(exposure.Source),
				}, "|")
				existing, ok := serviceScopes[key]
				if !ok {
					existing = newProjectionScope()
				}
				mergeScopedIDs(&existing, serviceScope)
				serviceScopes[key] = existing
			}
		}

		for _, runtime := range snapshot.Runtimes {
			if runtime.NodeID != node.Name || runtime.ContainerID == "" {
				continue
			}
			containerScope := newProjectionScope()
			containerScope.RuntimeIDs[runtime.ID] = struct{}{}
			if runtime.ServiceID != "" {
				containerScope.ServiceIDs[runtime.ServiceID] = struct{}{}
				for routeID := range routeIDsByService[runtime.ServiceID] {
					containerScope.RouteIDs[routeID] = struct{}{}
					for hopID := range hopIDsByRoute[routeID] {
						containerScope.HopIDs[hopID] = struct{}{}
					}
					for evidenceID := range evidenceIDsByRoute[routeID] {
						containerScope.EvidenceIDs[evidenceID] = struct{}{}
					}
				}
			}
			for _, exposure := range snapshot.Exposures {
				if exposure.RuntimeID == runtime.ID {
					containerScope.ExposureIDs[exposure.ID] = struct{}{}
				}
			}
			existing, ok := containerScopes[runtime.ContainerID]
			if !ok {
				existing = newProjectionScope()
			}
			mergeScopedIDs(&existing, containerScope)
			containerScopes[runtime.ContainerID] = existing
		}

		for routeID := range scope.RouteIDs {
			route, ok := routesByID[routeID]
			if !ok || route.Kind != "proxy_route" || route.SourceExposureID == "" {
				continue
			}
			exposure, ok := exposureByID[route.SourceExposureID]
			if !ok || exposure.NodeID != node.Name {
				continue
			}
			key := strings.Join([]string{
				string(node.Name),
				strconv.FormatUint(uint64(exposure.Port), 10),
				route.Input,
				strings.Join(route.Hostnames, ","),
			}, "|")
			forwardScope := newProjectionScope()
			if route.SourceServiceID != "" {
				forwardScope.ServiceIDs[route.SourceServiceID] = struct{}{}
			}
			if route.TargetServiceID != "" {
				forwardScope.ServiceIDs[route.TargetServiceID] = struct{}{}
			}
			if route.TargetRuntimeID != "" {
				forwardScope.RuntimeIDs[route.TargetRuntimeID] = struct{}{}
			}
			if route.SourceExposureID != "" {
				forwardScope.ExposureIDs[route.SourceExposureID] = struct{}{}
			}
			forwardScope.RouteIDs[route.ID] = struct{}{}
			for hopID := range hopIDsByRoute[route.ID] {
				forwardScope.HopIDs[hopID] = struct{}{}
			}
			for evidenceID := range evidenceIDsByRoute[route.ID] {
				forwardScope.EvidenceIDs[evidenceID] = struct{}{}
			}
			existing, ok := forwardScopes[key]
			if !ok {
				existing = newProjectionScope()
			}
			mergeScopedIDs(&existing, forwardScope)
			forwardScopes[key] = existing
		}
		portOut[node.Name] = portScopes
		forwardOut[node.Name] = forwardScopes
		serviceOut[node.Name] = serviceScopes
		containerOut[node.Name] = containerScopes
	}
	return out, portOut, forwardOut, serviceOut, containerOut
}

func newProjectionScope() topology.ScopedIDs {
	return topology.ScopedIDs{
		ServiceIDs:  make(map[string]struct{}),
		RuntimeIDs:  make(map[string]struct{}),
		ExposureIDs: make(map[string]struct{}),
		RouteIDs:    make(map[string]struct{}),
		HopIDs:      make(map[string]struct{}),
		EvidenceIDs: make(map[string]struct{}),
	}
}

func cloneScopedIDs(scope topology.ScopedIDs) topology.ScopedIDs {
	cloned := newProjectionScope()
	for id := range scope.ServiceIDs {
		cloned.ServiceIDs[id] = struct{}{}
	}
	for id := range scope.RuntimeIDs {
		cloned.RuntimeIDs[id] = struct{}{}
	}
	for id := range scope.ExposureIDs {
		cloned.ExposureIDs[id] = struct{}{}
	}
	for id := range scope.RouteIDs {
		cloned.RouteIDs[id] = struct{}{}
	}
	for id := range scope.HopIDs {
		cloned.HopIDs[id] = struct{}{}
	}
	for id := range scope.EvidenceIDs {
		cloned.EvidenceIDs[id] = struct{}{}
	}
	return cloned
}

func mergeScopedIDs(target *topology.ScopedIDs, source topology.ScopedIDs) {
	if target == nil {
		return
	}
	for id := range source.ServiceIDs {
		target.ServiceIDs[id] = struct{}{}
	}
	for id := range source.RuntimeIDs {
		target.RuntimeIDs[id] = struct{}{}
	}
	for id := range source.ExposureIDs {
		target.ExposureIDs[id] = struct{}{}
	}
	for id := range source.RouteIDs {
		target.RouteIDs[id] = struct{}{}
	}
	for id := range source.HopIDs {
		target.HopIDs[id] = struct{}{}
	}
	for id := range source.EvidenceIDs {
		target.EvidenceIDs[id] = struct{}{}
	}
}

func addProjectionNodeRef(index map[core.NodeName]map[string]struct{}, nodeName core.NodeName, value string) {
	if nodeName == "" || value == "" {
		return
	}
	values, ok := index[nodeName]
	if !ok {
		values = make(map[string]struct{})
		index[nodeName] = values
	}
	values[value] = struct{}{}
}

func addProjectionRef(index map[string]map[string]struct{}, key, value string) {
	if key == "" || value == "" {
		return
	}
	values, ok := index[key]
	if !ok {
		values = make(map[string]struct{})
		index[key] = values
	}
	values[value] = struct{}{}
}

func containsPort(values []uint16, port uint16) bool {
	for _, value := range values {
		if value == port {
			return true
		}
	}
	return false
}

func scopeForKeys(nodeScopes map[string]topology.ScopedIDs, keys []string) topology.ScopedIDs {
	if len(nodeScopes) == 0 || len(keys) == 0 {
		return topology.ScopedIDs{}
	}
	merged := newProjectionScope()
	found := false
	for _, key := range keys {
		scope, ok := nodeScopes[key]
		if !ok {
			continue
		}
		found = true
		mergeScopedIDs(&merged, scope)
	}
	if !found {
		return topology.ScopedIDs{}
	}
	return merged
}
