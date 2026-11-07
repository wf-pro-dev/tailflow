// Mappers convert tailkit/Docker SDK types to state types. The container
// and swarm mapping logic is salvaged from the first Tailflow prototype
// (internal/collector/collector.go:995-1315) — pure functions over
// docker/container.Summary and swarm.Service with no tailkit coupling.
//
// Compose handling (README gap #3): the daemon exposes
// /integrations/docker/compose/projects (NOT /compose/services — the SDK's
// Compose().Services() call hits a non-existent path with a capital S).
// We bypass the SDK and call the projects endpoint via Backend.Do.

package collector

import (
        "context"
        "encoding/json"
        "errors"
        "strings"
        "time"

        "github.com/wf-pro-dev/tailkit/client"
        integrations "github.com/wf-pro-dev/tailkit/client/integrations"

        "github.com/wf-pro-dev/tailflow/internal/state"
)

// mapContainer converts a docker/container.Summary (aliased as
// client.Container) to a state.Container.
func mapContainer(c client.Container) state.Container {
        out := state.Container{
                ID:        c.ID,
                Name:      firstContainerName(c.Names),
                Image:     c.Image,
                ImageID:   c.ImageID,
                State:     c.State,
                Status:    c.Status,
                Command:   c.Command,
                Labels:    c.Labels,
        }
        // Ports: docker SDK types Ports as []PortBinding with IP, PrivatePort,
        // PublicPort, Type. We map to our flat struct.
        out.Ports = make([]state.ContainerPort, 0, len(c.Ports))
        for _, p := range c.Ports {
                out.Ports = append(out.Ports, state.ContainerPort{
                        IP:      p.IP,
                        Private:  p.PrivatePort,
                        Public:   p.PublicPort,
                        Proto:    p.Type,
                })
        }
        // Created is a unix timestamp in the docker SDK.
        out.CreatedAt = unixToTime(c.Created)
        // Health is buried in the Status string ("Up 2 hours (healthy)"); we
        // don't parse it here — the UI can extract if needed. Leave empty.
        return out
}

// firstContainerName returns the first container name without the leading
// "/" that docker prepends.
func firstContainerName(names []string) string {
        if len(names) == 0 {
                return ""
        }
        return strings.TrimPrefix(names[0], "/")
}

// mapSwarmService converts a swarm.Service (aliased as client.SwarwService —
// note the tailkit typo) to a state.SwarmService.
func mapSwarmService(s client.SwarwService) state.SwarmService {
        out := state.SwarmService{
                ID:    s.ID,
                Name:  s.Spec.Name,
                Image: imageFromSpec(s),
        }
        // Mode: replicated vs global.
        if s.Spec.Mode.Replicated != nil && s.Spec.Mode.Replicated.Replicas != nil {
                out.Mode = "replicated"
                r := int32(*s.Spec.Mode.Replicated.Replicas)
                out.Replicas = &r
        } else if s.Spec.Mode.Global != nil {
                out.Mode = "global"
                out.Replicas = nil
        }
        // Ports from Spec.EndpointSpec.Ports (note: EndpointSpec, not EndpointConfig).
        if s.Spec.EndpointSpec != nil {
                for _, p := range s.Spec.EndpointSpec.Ports {
                        out.Ports = append(out.Ports, state.SwarmPort{
                                Published: p.PublishedPort,
                                Target:    p.TargetPort,
                                Proto:     string(p.Protocol),
                        })
                }
        }
        return out
}

// imageFromSpec extracts the image reference from a swarm service spec.
func imageFromSpec(s client.SwarwService) string {
        return s.Spec.TaskTemplate.ContainerSpec.Image
}

// listComposeProjects fetches /integrations/docker/compose/projects directly
// via Backend.Do, working around the SDK bug (gap #3) where
// Compose().Services() hits /compose/Services (capital S, doesn't exist).
//
// The daemon returns an array of {Name, Status, ConfigFiles} objects.
func listComposeProjects(ctx context.Context, scope client.HostScope) ([]state.ComposeProject, error) {
        // We use Backend.DoRaw to avoid needing to know the exact response type
        // in the tailkit SDK (the SDK's ComposeService type doesn't match what
        // the daemon returns for /projects).
        var raw []struct {
                Name        string `json:"Name"`
                Status      string `json:"Status"`
                ConfigFiles string `json:"ConfigFiles"`
        }
        body, err := scope.Backend.DoRaw(ctx, scope.Hostname, "GET", "/integrations/docker/compose/projects", nil, "application/json")
        if err != nil {
                return nil, err
        }
        if len(body) == 0 {
                return nil, nil
        }
        if err := json.Unmarshal(body, &raw); err != nil {
                return nil, err
        }
        out := make([]state.ComposeProject, len(raw))
        for i, r := range raw {
                out[i] = state.ComposeProject{
                        Name:        r.Name,
                        Status:      r.Status,
                        ConfigFiles: r.ConfigFiles,
                }
        }
        return out, nil
}

// unixToTime converts a unix timestamp (as returned by the docker SDK's
// container.Summary.Created field) to a time.Time. Returns zero time on 0.
func unixToTime(unix int64) time.Time {
        if unix == 0 {
                return time.Time{}
        }
        return time.Unix(unix, 0)
}

// suppress unused-import error if integrations isn't referenced (it's used
// implicitly via client.DockerConfig which is integrations.DockerConfig).
var _ = integrations.DockerConfig{}

// errors.Is is used in the docker loop; keep the import here too.
var _ = errors.Is
