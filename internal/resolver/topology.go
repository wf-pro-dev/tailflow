package resolver

import (
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"

	"github.com/wf-pro-dev/tailflow/internal/core"
	"github.com/wf-pro-dev/tailflow/internal/parser"
	"github.com/wf-pro-dev/tailflow/internal/store"
)

// TopologyData is the richer service-centric topology snapshot derived from the latest inventory.
type TopologyData struct {
	Services  []store.Service
	Runtimes  []store.Runtime
	Exposures []store.Exposure
	Routes    []store.Route
	RouteHops []store.RouteHop
	Evidence  []store.Evidence
	Summary   store.TopologySummary
}

type topologyBuilder struct {
	index NodeIndex

	servicesByKey  map[string]*store.Service
	runtimesByKey  map[string]*store.Runtime
	exposuresByKey map[string]*store.Exposure
	routesByKey    map[string]*store.Route
	hopsByKey      map[string]*store.RouteHop
	evidenceByKey  map[string]*store.Evidence

	serviceIDsBySwarmName map[string]core.ID
	serviceIDsByContainer map[string]core.ID
	serviceIDsByProcess   map[string]core.ID
	serviceIDsByEndpoint  map[string]core.ID
	runtimeIDsByContainer map[string]core.ID
	runtimeIDsByProcess   map[string]core.ID
	exposureIDsByPortKey  map[string]core.ID
	serviceOrder          []string
	runtimeOrder          []string
	exposureOrder         []string
	routeOrder            []string
	hopOrder              []string
	evidenceOrder         []string
}

// BuildTopologyData builds the richer topology model from the latest snapshots.
func BuildTopologyData(snapshots []store.NodeSnapshot) TopologyData {
	index := BuildIndex(snapshots)
	builder := newTopologyBuilder(index)

	orderedSnapshots := append([]store.NodeSnapshot(nil), snapshots...)
	sort.Slice(orderedSnapshots, func(i, j int) bool {
		if orderedSnapshots[i].NodeName != orderedSnapshots[j].NodeName {
			return orderedSnapshots[i].NodeName < orderedSnapshots[j].NodeName
		}
		return orderedSnapshots[i].ID < orderedSnapshots[j].ID
	})

	for _, snapshot := range orderedSnapshots {
		builder.addInventory(snapshot)
	}
	for _, snapshot := range orderedSnapshots {
		builder.addRoutes(snapshot)
	}

	return builder.data()
}

func newTopologyBuilder(index NodeIndex) *topologyBuilder {
	return &topologyBuilder{
		index:                 index,
		servicesByKey:         make(map[string]*store.Service),
		runtimesByKey:         make(map[string]*store.Runtime),
		exposuresByKey:        make(map[string]*store.Exposure),
		routesByKey:           make(map[string]*store.Route),
		hopsByKey:             make(map[string]*store.RouteHop),
		evidenceByKey:         make(map[string]*store.Evidence),
		serviceIDsBySwarmName: make(map[string]core.ID),
		serviceIDsByContainer: make(map[string]core.ID),
		serviceIDsByProcess:   make(map[string]core.ID),
		serviceIDsByEndpoint:  make(map[string]core.ID),
		runtimeIDsByContainer: make(map[string]core.ID),
		runtimeIDsByProcess:   make(map[string]core.ID),
		exposureIDsByPortKey:  make(map[string]core.ID),
	}
}

func (b *topologyBuilder) data() TopologyData {
	services := make([]store.Service, 0, len(b.serviceOrder))
	for _, key := range b.serviceOrder {
		service := *b.servicesByKey[key]
		sortIDs(service.RuntimeIDs)
		sortIDs(service.ExposureIDs)
		sort.Strings(service.Tags)
		services = append(services, service)
	}
	sort.Slice(services, func(i, j int) bool {
		if services[i].PrimaryNode != services[j].PrimaryNode {
			return services[i].PrimaryNode < services[j].PrimaryNode
		}
		if services[i].Name != services[j].Name {
			return services[i].Name < services[j].Name
		}
		return services[i].ID < services[j].ID
	})

	runtimes := make([]store.Runtime, 0, len(b.runtimeOrder))
	for _, key := range b.runtimeOrder {
		runtime := *b.runtimesByKey[key]
		sortPorts(runtime.Ports)
		runtimes = append(runtimes, runtime)
	}
	sort.Slice(runtimes, func(i, j int) bool {
		if runtimes[i].NodeID != runtimes[j].NodeID {
			return runtimes[i].NodeID < runtimes[j].NodeID
		}
		if runtimes[i].RuntimeName != runtimes[j].RuntimeName {
			return runtimes[i].RuntimeName < runtimes[j].RuntimeName
		}
		return runtimes[i].ID < runtimes[j].ID
	})

	exposures := make([]store.Exposure, 0, len(b.exposureOrder))
	for _, key := range b.exposureOrder {
		exposures = append(exposures, *b.exposuresByKey[key])
	}
	sort.Slice(exposures, func(i, j int) bool {
		if exposures[i].NodeID != exposures[j].NodeID {
			return exposures[i].NodeID < exposures[j].NodeID
		}
		if exposures[i].Port != exposures[j].Port {
			return exposures[i].Port < exposures[j].Port
		}
		if exposures[i].Hostname != exposures[j].Hostname {
			return exposures[i].Hostname < exposures[j].Hostname
		}
		return exposures[i].ID < exposures[j].ID
	})

	routes := make([]store.Route, 0, len(b.routeOrder))
	for _, key := range b.routeOrder {
		route := *b.routesByKey[key]
		sort.Strings(route.Hostnames)
		routes = append(routes, route)
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].DisplayName != routes[j].DisplayName {
			return routes[i].DisplayName < routes[j].DisplayName
		}
		return routes[i].ID < routes[j].ID
	})

	hops := make([]store.RouteHop, 0, len(b.hopOrder))
	for _, key := range b.hopOrder {
		hops = append(hops, *b.hopsByKey[key])
	}
	sort.Slice(hops, func(i, j int) bool {
		if hops[i].RouteID != hops[j].RouteID {
			return hops[i].RouteID < hops[j].RouteID
		}
		if hops[i].Order != hops[j].Order {
			return hops[i].Order < hops[j].Order
		}
		return hops[i].ID < hops[j].ID
	})

	evidence := make([]store.Evidence, 0, len(b.evidenceOrder))
	for _, key := range b.evidenceOrder {
		evidence = append(evidence, *b.evidenceByKey[key])
	}
	sort.Slice(evidence, func(i, j int) bool {
		if evidence[i].MatchedBy != evidence[j].MatchedBy {
			return evidence[i].MatchedBy < evidence[j].MatchedBy
		}
		if evidence[i].RawValue != evidence[j].RawValue {
			return evidence[i].RawValue < evidence[j].RawValue
		}
		return evidence[i].ID < evidence[j].ID
	})

	summary := store.TopologySummary{
		NodeCount:     len(b.index.NodeState),
		ServiceCount:  len(services),
		RuntimeCount:  len(runtimes),
		ExposureCount: len(exposures),
		RouteCount:    len(routes),
	}
	for _, route := range routes {
		if !route.Resolved {
			summary.UnresolvedRouteCount++
		}
	}

	return TopologyData{
		Services:  services,
		Runtimes:  runtimes,
		Exposures: exposures,
		Routes:    routes,
		RouteHops: hops,
		Evidence:  evidence,
		Summary:   summary,
	}
}

func (b *topologyBuilder) addInventory(snapshot store.NodeSnapshot) {
	for _, port := range snapshot.Ports {
		serviceID, runtimeID := b.ensureProcessService(snapshot, port)
		b.ensurePortExposure(serviceID, runtimeID, snapshot.NodeName, snapshot.TailscaleIP, port.Port, port.Process, "listen_port", true)
	}

	for _, container := range snapshot.Containers {
		serviceID := b.ensureContainerService(snapshot, container)
		runtimeID := b.ensureContainerRuntime(snapshot, container, serviceID)
		for _, publish := range container.PublishedPorts {
			source := "container_publish"
			if strings.EqualFold(strings.TrimSpace(publish.Source), "service") {
				source = "swarm"
			}
			b.ensurePortExposure(serviceID, runtimeID, snapshot.NodeName, snapshot.TailscaleIP, publish.HostPort, container.ContainerName, source, false)
		}
	}

	for _, servicePort := range snapshot.Services {
		serviceID := b.ensureSwarmService(snapshot, servicePort)
		b.ensurePortExposure(serviceID, "", snapshot.NodeName, snapshot.TailscaleIP, servicePort.HostPort, servicePort.ServiceName, "swarm", false)
	}
}

func (b *topologyBuilder) addRoutes(snapshot store.NodeSnapshot) {
	for _, forward := range snapshot.Forwards {
		sourceServiceID, sourceRuntimeID := b.ensureListenerSource(snapshot, forward.Listener)
		sourceExposureID := b.ensureListenerExposure(snapshot, sourceServiceID, sourceRuntimeID, forward.Listener)

		routeKey := strings.Join([]string{
			string(snapshot.NodeName),
			strconv.FormatUint(uint64(forward.Listener.Port), 10),
			forward.Target.Kind,
			forward.Target.Host,
			strconv.FormatUint(uint64(forward.Target.Port), 10),
			forward.Target.Socket,
			forward.Target.Raw,
		}, "|")
		if _, ok := b.routesByKey[routeKey]; ok {
			continue
		}

		evidence := b.buildEvidence(snapshot, forward.Target)
		targetNode, targetPort, resolved := resolveForSource(snapshot, forward.Target, b.index)
		targetDetails := targetDetails{}
		targetServiceID := core.ID("")
		targetRuntimeID := core.ID("")
		targetDisplay := unresolvedTargetLabel(forward.Target)

		if resolved {
			targetDetails = targetMetadata(b.index, targetNode, targetPort)
			targetServiceID, targetRuntimeID, targetDisplay = b.ensureTargetService(targetNode, targetPort, targetDetails)
		}

		displayName := b.routeDisplayName(sourceServiceID, targetServiceID, targetDisplay)
		route := &store.Route{
			ID:               topologyID("route", routeKey),
			Kind:             "proxy_route",
			SourceServiceID:  sourceServiceID,
			SourceExposureID: sourceExposureID,
			TargetServiceID:  targetServiceID,
			TargetRuntimeID:  targetRuntimeID,
			DisplayName:      displayName,
			Resolved:         resolved,
			Health:           b.routeHealth(resolved, targetServiceID, targetRuntimeID),
			Hostnames:        parser.NormalizeHostnames(forward.Hostnames),
			Input:            forward.Target.Raw,
		}
		b.routesByKey[routeKey] = route
		b.routeOrder = append(b.routeOrder, routeKey)

		listenerHop := b.ensureRouteHop(
			route.ID,
			1,
			"gateway_listener",
			"client",
			b.exposureLabel(sourceExposureID, snapshot.NodeName, forward.Listener.Port),
			true,
			store.TopologyHealthHealthy,
			"",
		)
		forwardHop := b.ensureRouteHop(
			route.ID,
			2,
			"proxy_forward",
			b.exposureLabel(sourceExposureID, snapshot.NodeName, forward.Listener.Port),
			targetHopLabel(forward.Target, targetNode, targetPort, resolved),
			resolved,
			b.routeHealth(resolved, targetServiceID, targetRuntimeID),
			evidence.ID,
		)
		route.HopIDs = append(route.HopIDs, listenerHop.ID, forwardHop.ID)

		if resolved {
			targetHop := b.ensureRouteHop(
				route.ID,
				3,
				"direct_host_port",
				targetHopLabel(forward.Target, targetNode, targetPort, true),
				targetDisplay,
				true,
				b.routeHealth(true, targetServiceID, targetRuntimeID),
				evidence.ID,
			)
			route.HopIDs = append(route.HopIDs, targetHop.ID)
		}
	}
}

func (b *topologyBuilder) ensureProcessService(snapshot store.NodeSnapshot, port store.ListenPort) (core.ID, core.ID) {
	processName := strings.TrimSpace(port.Process)
	if processName == "" {
		processName = fmt.Sprintf("listener-%d", port.Port)
	}

	serviceKey := strings.Join([]string{"daemon", string(snapshot.NodeName), strings.ToLower(processName)}, "|")
	service := b.ensureService(serviceKey, store.Service{
		ID:          topologyID("svc", serviceKey),
		Name:        processName,
		Kind:        "daemon",
		Role:        inferServiceRole(processName, processName, ""),
		PrimaryNode: snapshot.NodeName,
		Health:      store.TopologyHealthHealthy,
		Tags:        []string{string(snapshot.NodeName), "process"},
	})
	b.serviceIDsByProcess[serviceLookupKey(snapshot.NodeName, processName)] = service.ID

	runtimeKey := strings.Join([]string{
		"process",
		string(snapshot.NodeName),
		strconv.Itoa(port.PID),
		strings.ToLower(processName),
		strconv.FormatUint(uint64(port.Port), 10),
	}, "|")
	runtime := b.ensureRuntime(runtimeKey, store.Runtime{
		ID:          topologyID("rt", runtimeKey),
		ServiceID:   service.ID,
		NodeID:      snapshot.NodeName,
		RuntimeKind: "process",
		RuntimeName: processName,
		PID:         port.PID,
		State:       "listening",
		Ports:       []uint16{port.Port},
		Health:      store.TopologyHealthHealthy,
		CollectedAt: snapshot.CollectedAt,
	})
	b.runtimeIDsByProcess[processRuntimeLookupKey(snapshot.NodeName, processName, port.Port)] = runtime.ID

	b.linkServiceRuntime(service.ID, runtime.ID)
	return service.ID, runtime.ID
}

func (b *topologyBuilder) ensureContainerService(snapshot store.NodeSnapshot, container store.Container) core.ID {
	name := strings.TrimSpace(container.ServiceName)
	kind := "container"
	serviceKey := ""

	if name != "" {
		kind = "swarm_service"
		serviceKey = strings.Join([]string{"swarm", strings.ToLower(name)}, "|")
	} else {
		name = strings.TrimSpace(container.ContainerName)
		serviceKey = strings.Join([]string{"container", string(snapshot.NodeName), strings.ToLower(name)}, "|")
	}
	if name == "" {
		name = strings.TrimSpace(container.Image)
	}
	if name == "" {
		name = "container"
	}

	service := b.ensureService(serviceKey, store.Service{
		ID:          topologyID("svc", serviceKey),
		Name:        name,
		Kind:        kind,
		Role:        inferServiceRole(name, container.ContainerName, container.Image),
		PrimaryNode: snapshot.NodeName,
		Health:      containerHealth(container),
		Tags:        []string{string(snapshot.NodeName), "container"},
		Description: container.Image,
	})
	if strings.TrimSpace(container.ServiceName) != "" {
		b.serviceIDsBySwarmName[strings.ToLower(strings.TrimSpace(container.ServiceName))] = service.ID
	}
	if strings.TrimSpace(container.ContainerName) != "" {
		b.serviceIDsByContainer[containerLookupKey(snapshot.NodeName, container.ContainerName)] = service.ID
	}
	return service.ID
}

func (b *topologyBuilder) ensureContainerRuntime(snapshot store.NodeSnapshot, container store.Container, serviceID core.ID) core.ID {
	containerName := strings.TrimSpace(container.ContainerName)
	runtimeKey := strings.Join([]string{"container", string(snapshot.NodeName), strings.ToLower(containerName), container.ContainerID}, "|")
	runtimePorts := make([]uint16, 0, len(container.PublishedPorts))
	for _, publish := range container.PublishedPorts {
		if publish.TargetPort > 0 {
			runtimePorts = append(runtimePorts, publish.TargetPort)
		} else if publish.HostPort > 0 {
			runtimePorts = append(runtimePorts, publish.HostPort)
		}
	}
	if len(runtimePorts) == 0 {
		runtimePorts = append(runtimePorts, publishedHostPorts(container.PublishedPorts)...)
	}

	runtime := b.ensureRuntime(runtimeKey, store.Runtime{
		ID:          topologyID("rt", runtimeKey),
		ServiceID:   serviceID,
		NodeID:      snapshot.NodeName,
		RuntimeKind: "container",
		RuntimeName: containerName,
		ContainerID: container.ContainerID,
		Image:       container.Image,
		State:       container.State,
		Ports:       runtimePorts,
		Health:      containerHealth(container),
		CollectedAt: snapshot.CollectedAt,
	})
	if containerName != "" {
		b.runtimeIDsByContainer[containerLookupKey(snapshot.NodeName, containerName)] = runtime.ID
	}
	b.linkServiceRuntime(serviceID, runtime.ID)
	return runtime.ID
}

func (b *topologyBuilder) ensureSwarmService(snapshot store.NodeSnapshot, servicePort store.SwarmServicePort) core.ID {
	serviceName := strings.TrimSpace(servicePort.ServiceName)
	serviceKey := strings.Join([]string{"swarm", strings.ToLower(serviceName)}, "|")
	service := b.ensureService(serviceKey, store.Service{
		ID:          topologyID("svc", serviceKey),
		Name:        serviceName,
		Kind:        "swarm_service",
		Role:        inferServiceRole(serviceName, serviceName, ""),
		PrimaryNode: snapshot.NodeName,
		Health:      store.TopologyHealthHealthy,
		Tags:        []string{string(snapshot.NodeName), "swarm"},
	})
	b.serviceIDsBySwarmName[strings.ToLower(serviceName)] = service.ID
	return service.ID
}

func (b *topologyBuilder) ensureListenerSource(snapshot store.NodeSnapshot, listener parser.Listener) (core.ID, core.ID) {
	port := store.ListenPort{
		Port:    listener.Port,
		Process: portProcess(snapshot.Ports, listener.Port),
	}
	return b.ensureProcessService(snapshot, port)
}

func (b *topologyBuilder) ensureListenerExposure(snapshot store.NodeSnapshot, serviceID, runtimeID core.ID, listener parser.Listener) core.ID {
	return b.ensurePortExposure(serviceID, runtimeID, snapshot.NodeName, snapshot.TailscaleIP, listener.Port, portProcess(snapshot.Ports, listener.Port), "listen_port", true)
}

func (b *topologyBuilder) ensureTargetService(nodeName core.NodeName, port uint16, details targetDetails) (core.ID, core.ID, string) {
	switch {
	case details.Service != "":
		serviceID := b.serviceIDsBySwarmName[strings.ToLower(strings.TrimSpace(details.Service))]
		if serviceID == "" {
			serviceKey := strings.Join([]string{"swarm", strings.ToLower(strings.TrimSpace(details.Service))}, "|")
			serviceID = b.ensureService(serviceKey, store.Service{
				ID:          topologyID("svc", serviceKey),
				Name:        details.Service,
				Kind:        "swarm_service",
				Role:        inferServiceRole(details.Service, details.Service, ""),
				PrimaryNode: nodeName,
				Health:      store.TopologyHealthHealthy,
				Tags:        []string{string(nodeName), "swarm"},
			}).ID
			b.serviceIDsBySwarmName[strings.ToLower(strings.TrimSpace(details.Service))] = serviceID
		}
		runtimeID := core.ID("")
		if details.RuntimeContainer != "" && details.RuntimeNode != "" {
			runtimeID = b.runtimeIDsByContainer[containerLookupKey(details.RuntimeNode, details.RuntimeContainer)]
			if runtimeID == "" {
				runtimeKey := strings.Join([]string{"container", string(details.RuntimeNode), strings.ToLower(details.RuntimeContainer), ""}, "|")
				runtimeID = b.ensureRuntime(runtimeKey, store.Runtime{
					ID:          topologyID("rt", runtimeKey),
					ServiceID:   serviceID,
					NodeID:      details.RuntimeNode,
					RuntimeKind: "container",
					RuntimeName: details.RuntimeContainer,
					State:       "running",
					Ports:       []uint16{port},
					Health:      store.TopologyHealthHealthy,
				}).ID
				b.runtimeIDsByContainer[containerLookupKey(details.RuntimeNode, details.RuntimeContainer)] = runtimeID
				b.linkServiceRuntime(serviceID, runtimeID)
			}
		}
		return serviceID, runtimeID, b.serviceLabel(serviceID, details.Service)
	case details.Container != "":
		serviceID := b.serviceIDsByContainer[containerLookupKey(nodeName, details.Container)]
		if serviceID == "" {
			serviceKey := strings.Join([]string{"container", string(nodeName), strings.ToLower(strings.TrimSpace(details.Container))}, "|")
			serviceID = b.ensureService(serviceKey, store.Service{
				ID:          topologyID("svc", serviceKey),
				Name:        details.Container,
				Kind:        "container",
				Role:        inferServiceRole(details.Container, details.Container, ""),
				PrimaryNode: nodeName,
				Health:      store.TopologyHealthHealthy,
				Tags:        []string{string(nodeName), "container"},
			}).ID
			b.serviceIDsByContainer[containerLookupKey(nodeName, details.Container)] = serviceID
		}
		runtimeID := b.runtimeIDsByContainer[containerLookupKey(nodeName, details.Container)]
		if runtimeID == "" {
			runtimeKey := strings.Join([]string{"container", string(nodeName), strings.ToLower(strings.TrimSpace(details.Container)), ""}, "|")
			runtimeID = b.ensureRuntime(runtimeKey, store.Runtime{
				ID:          topologyID("rt", runtimeKey),
				ServiceID:   serviceID,
				NodeID:      nodeName,
				RuntimeKind: "container",
				RuntimeName: details.Container,
				State:       "running",
				Ports:       []uint16{port},
				Health:      store.TopologyHealthHealthy,
			}).ID
			b.runtimeIDsByContainer[containerLookupKey(nodeName, details.Container)] = runtimeID
			b.linkServiceRuntime(serviceID, runtimeID)
		}
		return serviceID, runtimeID, b.serviceLabel(serviceID, details.Container)
	case details.Process != "":
		serviceKey := strings.Join([]string{"daemon", string(nodeName), strings.ToLower(strings.TrimSpace(details.Process))}, "|")
		serviceID := b.ensureService(serviceKey, store.Service{
			ID:          topologyID("svc", serviceKey),
			Name:        details.Process,
			Kind:        "daemon",
			Role:        inferServiceRole(details.Process, details.Process, ""),
			PrimaryNode: nodeName,
			Health:      store.TopologyHealthHealthy,
			Tags:        []string{string(nodeName), "process"},
		}).ID
		runtimeKey := processRuntimeLookupKey(nodeName, details.Process, port)
		runtimeID := b.runtimeIDsByProcess[runtimeKey]
		if runtimeID == "" {
			runtimeStorageKey := strings.Join([]string{"process", string(nodeName), strings.ToLower(strings.TrimSpace(details.Process)), strconv.FormatUint(uint64(port), 10)}, "|")
			runtimeID = b.ensureRuntime(runtimeStorageKey, store.Runtime{
				ID:          topologyID("rt", runtimeStorageKey),
				ServiceID:   serviceID,
				NodeID:      nodeName,
				RuntimeKind: "process",
				RuntimeName: details.Process,
				State:       "listening",
				Ports:       []uint16{port},
				Health:      store.TopologyHealthHealthy,
			}).ID
			b.runtimeIDsByProcess[runtimeKey] = runtimeID
			b.linkServiceRuntime(serviceID, runtimeID)
		}
		return serviceID, runtimeID, b.serviceLabel(serviceID, details.Process)
	default:
		endpointKey := endpointLookupKey(nodeName, port)
		serviceID := b.serviceIDsByEndpoint[endpointKey]
		if serviceID == "" {
			serviceKey := strings.Join([]string{"endpoint", string(nodeName), strconv.FormatUint(uint64(port), 10)}, "|")
			serviceID = b.ensureService(serviceKey, store.Service{
				ID:          topologyID("svc", serviceKey),
				Name:        fmt.Sprintf("%s:%d", nodeName, port),
				Kind:        "daemon",
				Role:        inferServiceRole(string(nodeName), string(nodeName), ""),
				PrimaryNode: nodeName,
				Health:      store.TopologyHealthUnknown,
				Tags:        []string{string(nodeName), "endpoint"},
			}).ID
			b.serviceIDsByEndpoint[endpointKey] = serviceID
		}
		return serviceID, "", b.serviceLabel(serviceID, fmt.Sprintf("%s:%d", nodeName, port))
	}
}

func (b *topologyBuilder) ensurePortExposure(serviceID, runtimeID core.ID, nodeName core.NodeName, tailscaleIP string, port uint16, label string, source string, primary bool) core.ID {
	if serviceID == "" || port == 0 {
		return ""
	}

	protocol := guessProtocol(port, label)
	host := string(nodeName)
	exposureKey := strings.Join([]string{string(serviceID), string(nodeName), strconv.FormatUint(uint64(port), 10), source, host}, "|")
	exposure := b.ensureExposure(exposureKey, store.Exposure{
		ID:               topologyID("exp", exposureKey),
		ServiceID:        serviceID,
		RuntimeID:        runtimeID,
		NodeID:           nodeName,
		Kind:             "host_port",
		Protocol:         protocol,
		Hostname:         host,
		Port:             port,
		URL:              buildExposureURL(protocol, host, port),
		IsPrimary:        primary,
		Visibility:       "tailnet",
		Source:           source,
		Health:           b.serviceHealth(serviceID),
		ResolutionStatus: "resolved",
	})
	b.linkServiceExposure(serviceID, exposure.ID)
	b.exposureIDsByPortKey[portExposureLookupKey(serviceID, nodeName, port, source)] = exposure.ID

	if strings.TrimSpace(tailscaleIP) != "" {
		tsKey := strings.Join([]string{string(serviceID), string(nodeName), strconv.FormatUint(uint64(port), 10), source, tailscaleIP}, "|")
		tsExposure := b.ensureExposure(tsKey, store.Exposure{
			ID:               topologyID("exp", tsKey),
			ServiceID:        serviceID,
			RuntimeID:        runtimeID,
			NodeID:           nodeName,
			Kind:             "tailscale_port",
			Protocol:         protocol,
			Hostname:         tailscaleIP,
			Port:             port,
			URL:              buildExposureURL(protocol, tailscaleIP, port),
			IsPrimary:        false,
			Visibility:       "tailnet",
			Source:           source,
			Health:           b.serviceHealth(serviceID),
			ResolutionStatus: "resolved",
		})
		b.linkServiceExposure(serviceID, tsExposure.ID)
	}

	return exposure.ID
}

func (b *topologyBuilder) buildEvidence(snapshot store.NodeSnapshot, target parser.ForwardTarget) *store.Evidence {
	key := strings.Join([]string{string(snapshot.NodeName), target.Kind, target.Host, strconv.FormatUint(uint64(target.Port), 10), target.Socket, target.Raw}, "|")
	if existing, ok := b.evidenceByKey[key]; ok {
		return existing
	}

	evidence := &store.Evidence{
		ID:         topologyID("ev", key),
		MatchedBy:  "unknown",
		Confidence: "low",
		RawValue:   target.Raw,
	}

	switch target.Kind {
	case parser.TargetKindDynamic:
		evidence.MatchedBy = "dynamic"
		evidence.Reason = "proxy target is a dynamic placeholder"
		evidence.Warnings = []string{"dynamic placeholders are kept as unresolved route diagnostics"}
	case parser.TargetKindUnix:
		evidence.MatchedBy = "unix"
		evidence.Reason = "proxy target points at a unix socket"
		evidence.Warnings = []string{"unix socket routes are not projected into cross-node topology yet"}
	case parser.TargetKindAddress:
		host := strings.TrimSpace(target.Host)
		switch {
		case isLocalHost(host):
			evidence.MatchedBy = "localhost"
			evidence.Confidence = "high"
			evidence.Reason = "target resolves against the source node listener inventory"
		case b.index.ByTailscaleIP[host] != "":
			evidence.MatchedBy = "tailscale_ip"
			evidence.Confidence = "high"
			evidence.Reason = "target host matches a node tailscale IP"
		case b.index.ByLANIP[host] != "":
			evidence.MatchedBy = "lan_ip"
			evidence.Confidence = "high"
			evidence.Reason = "target host matches a node LAN IP"
		case matchesNodeAlias(host, b.index):
			evidence.MatchedBy = "node_alias"
			evidence.Confidence = "high"
			evidence.Reason = "target host matches a node name or configured alias"
		case len(b.index.ByContainer[strings.ToLower(host)]) > 0:
			evidence.MatchedBy = "container_name"
			evidence.Confidence = "medium"
			evidence.Reason = "target host matches a discovered container name"
		case len(b.index.ByService[strings.ToLower(host)]) > 0:
			evidence.MatchedBy = "service_name"
			evidence.Confidence = "medium"
			evidence.Reason = "target host matches a discovered service name"
		case target.Port > 0 && len(b.index.ByPort[target.Port]) == 1:
			evidence.MatchedBy = "unique_port"
			evidence.Confidence = "medium"
			evidence.Reason = "target host is ambiguous but the port maps to one inventory entry"
		default:
			evidence.Reason = "no current inventory match was found"
		}
	default:
		evidence.Reason = "parser produced an unsupported target kind"
	}

	sort.Strings(evidence.Warnings)
	b.evidenceByKey[key] = evidence
	b.evidenceOrder = append(b.evidenceOrder, key)
	return evidence
}

func (b *topologyBuilder) routeDisplayName(sourceServiceID, targetServiceID core.ID, fallbackTarget string) string {
	source := b.serviceLabel(sourceServiceID, "gateway")
	target := b.serviceLabel(targetServiceID, fallbackTarget)
	return fmt.Sprintf("%s -> %s", source, target)
}

func (b *topologyBuilder) serviceLabel(serviceID core.ID, fallback string) string {
	for _, service := range b.servicesByKey {
		if service.ID == serviceID && strings.TrimSpace(service.Name) != "" {
			return service.Name
		}
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return "unknown"
}

func (b *topologyBuilder) exposureLabel(exposureID core.ID, fallbackNode core.NodeName, fallbackPort uint16) string {
	for _, exposure := range b.exposuresByKey {
		if exposure.ID != exposureID {
			continue
		}
		if strings.TrimSpace(exposure.URL) != "" {
			return exposure.URL
		}
		if exposure.Hostname != "" && exposure.Port > 0 {
			return fmt.Sprintf("%s:%d", exposure.Hostname, exposure.Port)
		}
	}
	if fallbackNode != "" && fallbackPort > 0 {
		return fmt.Sprintf("%s:%d", fallbackNode, fallbackPort)
	}
	return "listener"
}

func (b *topologyBuilder) routeHealth(resolved bool, serviceID, runtimeID core.ID) store.TopologyHealth {
	if !resolved {
		return store.TopologyHealthUnresolved
	}
	if runtimeID != "" {
		for _, runtime := range b.runtimesByKey {
			if runtime.ID == runtimeID {
				return runtime.Health
			}
		}
	}
	if serviceID != "" {
		return b.serviceHealth(serviceID)
	}
	return store.TopologyHealthHealthy
}

func (b *topologyBuilder) serviceHealth(serviceID core.ID) store.TopologyHealth {
	for _, service := range b.servicesByKey {
		if service.ID == serviceID {
			return service.Health
		}
	}
	return store.TopologyHealthUnknown
}

func (b *topologyBuilder) ensureService(key string, candidate store.Service) *store.Service {
	if existing, ok := b.servicesByKey[key]; ok {
		if existing.Name == "" {
			existing.Name = candidate.Name
		}
		if existing.Kind == "" {
			existing.Kind = candidate.Kind
		}
		if existing.Role == "" {
			existing.Role = candidate.Role
		}
		if existing.PrimaryNode == "" {
			existing.PrimaryNode = candidate.PrimaryNode
		}
		existing.Health = mergeHealth(existing.Health, candidate.Health)
		existing.Tags = appendUniqueStrings(existing.Tags, candidate.Tags...)
		if existing.Description == "" {
			existing.Description = candidate.Description
		}
		return existing
	}

	copyCandidate := candidate
	b.servicesByKey[key] = &copyCandidate
	b.serviceOrder = append(b.serviceOrder, key)
	return &copyCandidate
}

func (b *topologyBuilder) ensureRuntime(key string, candidate store.Runtime) *store.Runtime {
	if existing, ok := b.runtimesByKey[key]; ok {
		existing.Ports = appendUniquePorts(existing.Ports, candidate.Ports...)
		existing.Health = mergeHealth(existing.Health, candidate.Health)
		if existing.State == "" {
			existing.State = candidate.State
		}
		if existing.Image == "" {
			existing.Image = candidate.Image
		}
		if existing.ContainerID == "" {
			existing.ContainerID = candidate.ContainerID
		}
		if existing.CollectedAt.IsZero() {
			existing.CollectedAt = candidate.CollectedAt
		}
		return existing
	}

	copyCandidate := candidate
	b.runtimesByKey[key] = &copyCandidate
	b.runtimeOrder = append(b.runtimeOrder, key)
	return &copyCandidate
}

func (b *topologyBuilder) ensureExposure(key string, candidate store.Exposure) *store.Exposure {
	if existing, ok := b.exposuresByKey[key]; ok {
		existing.Health = mergeHealth(existing.Health, candidate.Health)
		existing.IsPrimary = existing.IsPrimary || candidate.IsPrimary
		if existing.URL == "" {
			existing.URL = candidate.URL
		}
		if existing.RuntimeID == "" {
			existing.RuntimeID = candidate.RuntimeID
		}
		return existing
	}

	copyCandidate := candidate
	b.exposuresByKey[key] = &copyCandidate
	b.exposureOrder = append(b.exposureOrder, key)
	return &copyCandidate
}

func (b *topologyBuilder) ensureRouteHop(routeID core.ID, order int, kind string, from string, to string, resolved bool, health store.TopologyHealth, evidenceID core.ID) *store.RouteHop {
	key := strings.Join([]string{string(routeID), strconv.Itoa(order), kind, from, to}, "|")
	if existing, ok := b.hopsByKey[key]; ok {
		return existing
	}

	hop := &store.RouteHop{
		ID:         topologyID("hop", key),
		RouteID:    routeID,
		Order:      order,
		Kind:       kind,
		From:       from,
		To:         to,
		Resolved:   resolved,
		Health:     health,
		EvidenceID: evidenceID,
	}
	b.hopsByKey[key] = hop
	b.hopOrder = append(b.hopOrder, key)
	return hop
}

func (b *topologyBuilder) linkServiceRuntime(serviceID, runtimeID core.ID) {
	if serviceID == "" || runtimeID == "" {
		return
	}
	for _, service := range b.servicesByKey {
		if service.ID == serviceID {
			service.RuntimeIDs = appendUniqueIDs(service.RuntimeIDs, runtimeID)
			service.Health = mergeHealth(service.Health, b.runtimeHealth(runtimeID))
			return
		}
	}
}

func (b *topologyBuilder) linkServiceExposure(serviceID, exposureID core.ID) {
	if serviceID == "" || exposureID == "" {
		return
	}
	for _, service := range b.servicesByKey {
		if service.ID == serviceID {
			service.ExposureIDs = appendUniqueIDs(service.ExposureIDs, exposureID)
			return
		}
	}
}

func (b *topologyBuilder) runtimeHealth(runtimeID core.ID) store.TopologyHealth {
	for _, runtime := range b.runtimesByKey {
		if runtime.ID == runtimeID {
			return runtime.Health
		}
	}
	return store.TopologyHealthUnknown
}

func topologyID(prefix string, parts ...string) core.ID {
	hasher := fnv.New128a()
	_, _ = hasher.Write([]byte(prefix))
	for _, part := range parts {
		_, _ = hasher.Write([]byte{0})
		_, _ = hasher.Write([]byte(part))
	}
	return core.ID(fmt.Sprintf("%s_%x", prefix, hasher.Sum(nil)))
}

func inferServiceRole(name string, process string, image string) string {
	combined := strings.ToLower(strings.Join([]string{name, process, image}, " "))
	switch {
	case strings.Contains(combined, "nginx"),
		strings.Contains(combined, "caddy"),
		strings.Contains(combined, "traefik"),
		strings.Contains(combined, "gateway"),
		strings.Contains(combined, "proxy"):
		return "gateway"
	case strings.Contains(combined, "grafana"),
		strings.Contains(combined, "prometheus"),
		strings.Contains(combined, "loki"),
		strings.Contains(combined, "monitor"):
		return "monitoring"
	case strings.Contains(combined, "postgres"),
		strings.Contains(combined, "mysql"),
		strings.Contains(combined, "mariadb"),
		strings.Contains(combined, "redis"),
		strings.Contains(combined, "db"):
		return "database"
	case strings.Contains(combined, "dns"):
		return "dns"
	case strings.Contains(combined, "ui"),
		strings.Contains(combined, "frontend"),
		strings.Contains(combined, "web"),
		strings.Contains(combined, "dashboard"):
		return "ui"
	case strings.Contains(combined, "api"),
		strings.Contains(combined, "backend"),
		strings.Contains(combined, "server"):
		return "api"
	case strings.Contains(combined, "registry"),
		strings.Contains(combined, "storage"),
		strings.Contains(combined, "s3"):
		return "storage"
	default:
		return ""
	}
}

func containerHealth(container store.Container) store.TopologyHealth {
	state := strings.ToLower(strings.TrimSpace(container.State))
	status := strings.ToLower(strings.TrimSpace(container.Status))
	if strings.Contains(status, "unhealthy") {
		return store.TopologyHealthDegraded
	}
	switch state {
	case "", "running", "created":
		return store.TopologyHealthHealthy
	default:
		return store.TopologyHealthDegraded
	}
}

func mergeHealth(current, next store.TopologyHealth) store.TopologyHealth {
	if current == "" {
		return next
	}
	if next == "" {
		return current
	}
	if current == store.TopologyHealthDegraded || next == store.TopologyHealthDegraded {
		return store.TopologyHealthDegraded
	}
	if current == store.TopologyHealthUnresolved || next == store.TopologyHealthUnresolved {
		return store.TopologyHealthUnresolved
	}
	if current == store.TopologyHealthHealthy || next == store.TopologyHealthHealthy {
		return store.TopologyHealthHealthy
	}
	return current
}

func guessProtocol(port uint16, label string) string {
	switch port {
	case 80, 8080, 3000:
		if looksHTTP(label) {
			return "http"
		}
	case 443, 8443:
		return "https"
	case 53:
		if strings.Contains(strings.ToLower(label), "dns") {
			return "udp"
		}
	}
	if looksHTTP(label) {
		return "http"
	}
	return "tcp"
}

func looksHTTP(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.Contains(value, "nginx") ||
		strings.Contains(value, "caddy") ||
		strings.Contains(value, "http") ||
		strings.Contains(value, "web") ||
		strings.Contains(value, "ui") ||
		strings.Contains(value, "grafana")
}

func buildExposureURL(protocol string, host string, port uint16) string {
	if host == "" || port == 0 {
		return ""
	}
	switch protocol {
	case "http":
		if port == 80 {
			return fmt.Sprintf("http://%s/", host)
		}
		return fmt.Sprintf("http://%s:%d/", host, port)
	case "https":
		if port == 443 {
			return fmt.Sprintf("https://%s/", host)
		}
		return fmt.Sprintf("https://%s:%d/", host, port)
	default:
		return fmt.Sprintf("%s:%d", host, port)
	}
}

func unresolvedTargetLabel(target parser.ForwardTarget) string {
	switch target.Kind {
	case parser.TargetKindUnix:
		if target.Socket != "" {
			return target.Socket
		}
	case parser.TargetKindDynamic:
		if target.Raw != "" {
			return target.Raw
		}
		return "dynamic target"
	}
	if strings.TrimSpace(target.Raw) != "" {
		return target.Raw
	}
	return "unresolved target"
}

func targetHopLabel(target parser.ForwardTarget, nodeName core.NodeName, port uint16, resolved bool) string {
	if resolved && nodeName != "" && port > 0 {
		return fmt.Sprintf("%s:%d", nodeName, port)
	}
	return unresolvedTargetLabel(target)
}

func matchesNodeAlias(host string, index NodeIndex) bool {
	for _, key := range hostLookupKeys(host) {
		if index.ByNodeName[key] != "" {
			return true
		}
	}
	return false
}

func serviceLookupKey(nodeName core.NodeName, name string) string {
	return strings.Join([]string{string(nodeName), strings.ToLower(strings.TrimSpace(name))}, "|")
}

func containerLookupKey(nodeName core.NodeName, name string) string {
	return strings.Join([]string{string(nodeName), strings.ToLower(strings.TrimSpace(name))}, "|")
}

func processRuntimeLookupKey(nodeName core.NodeName, process string, port uint16) string {
	return strings.Join([]string{string(nodeName), strings.ToLower(strings.TrimSpace(process)), strconv.FormatUint(uint64(port), 10)}, "|")
}

func endpointLookupKey(nodeName core.NodeName, port uint16) string {
	return strings.Join([]string{string(nodeName), strconv.FormatUint(uint64(port), 10)}, "|")
}

func portExposureLookupKey(serviceID core.ID, nodeName core.NodeName, port uint16, source string) string {
	return strings.Join([]string{string(serviceID), string(nodeName), strconv.FormatUint(uint64(port), 10), source}, "|")
}

func publishedHostPorts(ports []store.ContainerPublishedPort) []uint16 {
	out := make([]uint16, 0, len(ports))
	for _, port := range ports {
		if port.HostPort > 0 {
			out = append(out, port.HostPort)
		}
	}
	return out
}

func appendUniqueIDs(existing []core.ID, additions ...core.ID) []core.ID {
	seen := make(map[core.ID]struct{}, len(existing))
	for _, value := range existing {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		existing = append(existing, value)
	}
	return existing
}

func appendUniqueStrings(existing []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(existing))
	for _, value := range existing {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		existing = append(existing, value)
	}
	return existing
}

func appendUniquePorts(existing []uint16, additions ...uint16) []uint16 {
	seen := make(map[uint16]struct{}, len(existing))
	for _, value := range existing {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		existing = append(existing, value)
	}
	return existing
}

func sortIDs(values []core.ID) {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
}

func sortPorts(values []uint16) {
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
}
