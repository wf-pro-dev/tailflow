package parser

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy/fastcgi"
)

// CaddyParser extracts normalized forward actions from Caddy config.
type CaddyParser struct{}

func (CaddyParser) Kind() string {
	return "caddy"
}

func (CaddyParser) Parse(content string) (ParseResult, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return ParseResult{}, fmt.Errorf("empty config")
	}

	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return parseCaddyJSON(trimmed, nil)
	}
	return parseCaddyfile(trimmed)
}

func parseCaddyfile(content string) (ParseResult, error) {
	adapter := caddyfile.Adapter{ServerType: httpcaddyfile.ServerType{}}
	jsonConfig, warnings, err := adapter.Adapt([]byte(content), map[string]any{"filename": "Caddyfile"})
	if err != nil {
		return ParseResult{}, fmt.Errorf("adapt caddyfile: %w", err)
	}
	return parseCaddyJSON(string(jsonConfig), warnings)
}

func parseCaddyJSON(content string, warnings []caddyconfig.Warning) (ParseResult, error) {
	var config any
	if err := json.Unmarshal([]byte(content), &config); err != nil {
		return ParseResult{}, fmt.Errorf("parse caddy json: %w", err)
	}

	forwards := make([]ForwardAction, 0)
	walkCaddyJSON(config, caddyRouteContext{}, &forwards)
	forwards = DedupeForwards(forwards)
	if len(forwards) == 0 {
		return ParseResult{}, fmt.Errorf("no proxy forward actions found")
	}

	result := ParseResult{Forwards: forwards}
	for _, warning := range warnings {
		if msg := strings.TrimSpace(warning.Message); msg != "" {
			result.Errors = append(result.Errors, msg)
		}
	}
	return result, nil
}

type caddyRouteContext struct {
	listeners []Listener
	hostnames []string
}

func walkCaddyJSON(node any, ctx caddyRouteContext, forwards *[]ForwardAction) {
	switch value := node.(type) {
	case map[string]any:
		nextCtx := ctx
		if listeners := extractCaddyListeners(value["listen"]); len(listeners) > 0 {
			nextCtx.listeners = listeners
		}
		if hostnames := extractCaddyHostnames(value["match"]); len(hostnames) > 0 {
			nextCtx.hostnames = hostnames
		}

		if handler, _ := value["handler"].(string); handler == "reverse_proxy" {
			targets := extractCaddyTargets(value)
			appendCaddyForwards(nextCtx.listeners, nextCtx.hostnames, targets, forwards)
		}

		for _, child := range value {
			walkCaddyJSON(child, nextCtx, forwards)
		}
	case []any:
		for _, item := range value {
			walkCaddyJSON(item, ctx, forwards)
		}
	}
}

func appendCaddyForwards(listeners []Listener, hostnames []string, targets []ForwardTarget, forwards *[]ForwardAction) {
	if len(targets) == 0 {
		return
	}
	if len(listeners) == 0 {
		listeners = []Listener{{Port: 443}}
	}

	for _, listener := range listeners {
		if listener.Port == 0 {
			continue
		}
		for _, target := range targets {
			*forwards = append(*forwards, ForwardAction{
				Listener:  listener,
				Target:    target,
				Hostnames: NormalizeHostnames(hostnames),
			})
		}
	}
}

func extractCaddyHostnames(value any) []string {
	matches, ok := value.([]any)
	if !ok {
		return nil
	}

	hostnames := make([]string, 0)
	for _, match := range matches {
		matchMap, ok := match.(map[string]any)
		if !ok {
			continue
		}
		hosts, ok := matchMap["host"].([]any)
		if !ok {
			continue
		}
		for _, host := range hosts {
			hostValue, ok := host.(string)
			if !ok {
				continue
			}
			hostnames = append(hostnames, hostValue)
		}
	}

	return NormalizeHostnames(hostnames)
}

func extractCaddyListeners(value any) []Listener {
	entries, ok := value.([]any)
	if !ok {
		return nil
	}

	listeners := make([]Listener, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		raw, ok := entry.(string)
		if !ok {
			continue
		}
		listener := parseCaddyListener(raw)
		if listener.Port == 0 {
			continue
		}
		key := listener.Addr + "|" + strconv.FormatUint(uint64(listener.Port), 10)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		listeners = append(listeners, listener)
	}
	sort.Slice(listeners, func(i, j int) bool {
		if listeners[i].Port != listeners[j].Port {
			return listeners[i].Port < listeners[j].Port
		}
		return listeners[i].Addr < listeners[j].Addr
	})
	return listeners
}

func parseCaddyListener(raw string) Listener {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "http://")
	raw = strings.TrimPrefix(raw, "https://")

	if strings.HasPrefix(raw, ":") {
		port, _ := strconv.ParseUint(strings.TrimPrefix(raw, ":"), 10, 16)
		return Listener{Port: uint16(port)}
	}

	if host, portText, err := net.SplitHostPort(raw); err == nil {
		port, _ := strconv.ParseUint(portText, 10, 16)
		return Listener{Addr: host, Port: uint16(port)}
	}

	if strings.Contains(raw, ".") || strings.EqualFold(raw, "localhost") {
		return Listener{Addr: raw, Port: 443}
	}
	return Listener{Port: 443}
}

func extractCaddyTargets(handler map[string]any) []ForwardTarget {
	targets := extractStaticCaddyTargets(handler["upstreams"])
	if len(targets) > 0 {
		return targets
	}

	if dynamic, ok := handler["dynamic"].(map[string]any); ok {
		for moduleName := range dynamic {
			return []ForwardTarget{{
				Raw:  "dynamic:" + moduleName,
				Kind: TargetKindDynamic,
			}}
		}
		return []ForwardTarget{{Raw: "dynamic", Kind: TargetKindDynamic}}
	}
	return nil
}

func extractStaticCaddyTargets(value any) []ForwardTarget {
	entries, ok := value.([]any)
	if !ok {
		return nil
	}

	targets := make([]ForwardTarget, 0, len(entries))
	for _, entry := range entries {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		dial, _ := item["dial"].(string)
		target := NormalizeTarget(dial, 443)
		if target.Raw == "" {
			continue
		}
		targets = append(targets, target)
	}
	return DedupeTargets(targets)
}

func DedupeTargets(targets []ForwardTarget) []ForwardTarget {
	if len(targets) == 0 {
		return nil
	}
	out := make([]ForwardTarget, 0, len(targets))
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		key := strings.Join([]string{
			target.Kind,
			target.Host,
			strconv.FormatUint(uint64(target.Port), 10),
			target.Socket,
			target.Raw,
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Host != out[j].Host {
			return out[i].Host < out[j].Host
		}
		if out[i].Port != out[j].Port {
			return out[i].Port < out[j].Port
		}
		if out[i].Socket != out[j].Socket {
			return out[i].Socket < out[j].Socket
		}
		return out[i].Raw < out[j].Raw
	})
	return out
}
