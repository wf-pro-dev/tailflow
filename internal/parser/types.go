package parser

import (
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/wf-pro-dev/tailflow/internal/core"
)

const (
	TargetKindAddress = "address"
	TargetKindUnix    = "unix"
	TargetKindDynamic = "dynamic"
	TargetKindUnknown = "unknown"
)

// Listener is the source socket on the proxy node that accepts traffic.
type Listener struct {
	Addr string `json:"addr,omitempty"`
	Port uint16 `json:"port"`
}

// ForwardTarget is the normalized backend target for one forwarding action.
type ForwardTarget struct {
	Raw    string `json:"raw"`
	Kind   string `json:"kind"`
	Host   string `json:"host,omitempty"`
	Port   uint16 `json:"port,omitempty"`
	Socket string `json:"socket,omitempty"`
}

// ForwardAction is the only parsed proxy shape the resolver needs:
// one listener on the proxy node forwarding to one backend target.
type ForwardAction struct {
	Listener Listener      `json:"listener"`
	Target   ForwardTarget `json:"target"`
}

// ProxyRule is the legacy parsed shape kept only for backward-compatible reads.
type ProxyRule struct {
	ListenPort uint16 `json:"listen_port"`
	ServerName string `json:"server_name"`
	Upstream   string `json:"upstream"`
	Proto      string `json:"proto"`
}

// ProxyConfigInput is the user-declared proxy config for one node.
type ProxyConfigInput struct {
	ID          core.ID           `json:"id" db:"id"`
	NodeName    core.NodeName     `json:"node_name" db:"node_name"`
	Kind        string            `json:"kind"`
	ConfigPath  string            `json:"config_path" db:"config_path"`
	Content     string            `json:"-" db:"content"`
	BundleFiles map[string]string `json:"-" db:"-"`
	UpdatedAt   core.Timestamp    `json:"updated_at" db:"updated_at"`
}

// ParseResult carries the output of one parse attempt.
type ParseResult struct {
	Forwards []ForwardAction `json:"forwards"`
	Errors   []string        `json:"errors,omitempty"`
}

// ProxyParser is the strategy interface for one proxy kind.
type ProxyParser interface {
	Kind() string
	Parse(configContent string) (ParseResult, error)
}

// BundleProxyParser can parse one config plus any fetched related files.
type BundleProxyParser interface {
	ProxyParser
	ParseBundle(mainPath string, files map[string]string) (ParseResult, error)
}

func NormalizeTarget(raw string, defaultPort uint16) ForwardTarget {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ForwardTarget{Raw: raw, Kind: TargetKindUnknown}
	}
	if strings.Contains(raw, "$") || strings.Contains(raw, "{") {
		return ForwardTarget{Raw: raw, Kind: TargetKindDynamic}
	}
	if socket, ok := extractUnixSocket(raw); ok {
		return ForwardTarget{
			Raw:    raw,
			Kind:   TargetKindUnix,
			Socket: socket,
		}
	}

	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		host, port, ok := splitHostPortWithDefault(parsed.Host, defaultPortForScheme(parsed.Scheme, defaultPort))
		if ok {
			return ForwardTarget{
				Raw:  raw,
				Kind: TargetKindAddress,
				Host: host,
				Port: port,
			}
		}
	}

	if host, port, ok := splitHostPortWithDefault(raw, defaultPort); ok {
		return ForwardTarget{
			Raw:  raw,
			Kind: TargetKindAddress,
			Host: host,
			Port: port,
		}
	}

	return ForwardTarget{Raw: raw, Kind: TargetKindUnknown}
}

func ForwardFromLegacyRule(rule ProxyRule) ForwardAction {
	return ForwardAction{
		Listener: Listener{Port: rule.ListenPort},
		Target:   NormalizeTarget(rule.Upstream, 0),
	}
}

func SortForwards(forwards []ForwardAction) {
	sort.Slice(forwards, func(i, j int) bool {
		if forwards[i].Listener.Port != forwards[j].Listener.Port {
			return forwards[i].Listener.Port < forwards[j].Listener.Port
		}
		if forwards[i].Listener.Addr != forwards[j].Listener.Addr {
			return forwards[i].Listener.Addr < forwards[j].Listener.Addr
		}
		if forwards[i].Target.Kind != forwards[j].Target.Kind {
			return forwards[i].Target.Kind < forwards[j].Target.Kind
		}
		if forwards[i].Target.Host != forwards[j].Target.Host {
			return forwards[i].Target.Host < forwards[j].Target.Host
		}
		if forwards[i].Target.Port != forwards[j].Target.Port {
			return forwards[i].Target.Port < forwards[j].Target.Port
		}
		if forwards[i].Target.Socket != forwards[j].Target.Socket {
			return forwards[i].Target.Socket < forwards[j].Target.Socket
		}
		return forwards[i].Target.Raw < forwards[j].Target.Raw
	})
}

func DedupeForwards(forwards []ForwardAction) []ForwardAction {
	if len(forwards) == 0 {
		return nil
	}
	out := make([]ForwardAction, 0, len(forwards))
	seen := make(map[string]struct{}, len(forwards))
	for _, forward := range forwards {
		key := strings.Join([]string{
			forward.Listener.Addr,
			strconv.FormatUint(uint64(forward.Listener.Port), 10),
			forward.Target.Kind,
			forward.Target.Host,
			strconv.FormatUint(uint64(forward.Target.Port), 10),
			forward.Target.Socket,
			forward.Target.Raw,
		}, "|")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, forward)
	}
	SortForwards(out)
	return out
}

func splitHostPortWithDefault(value string, defaultPort uint16) (string, uint16, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, false
	}

	if host, portText, err := net.SplitHostPort(value); err == nil {
		port, err := strconv.ParseUint(portText, 10, 16)
		if err != nil {
			return "", 0, false
		}
		return host, uint16(port), true
	}

	lastColon := strings.LastIndex(value, ":")
	if lastColon > 0 && lastColon < len(value)-1 {
		port, err := strconv.ParseUint(value[lastColon+1:], 10, 16)
		if err == nil {
			return value[:lastColon], uint16(port), true
		}
	}

	if defaultPort > 0 {
		return value, defaultPort, true
	}
	return "", 0, false
}

func defaultPortForScheme(scheme string, fallback uint16) uint16 {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "http", "h2c":
		return 80
	case "https":
		return 443
	case "grpc":
		return 80
	case "grpcs":
		return 443
	default:
		return fallback
	}
}

func extractUnixSocket(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	for _, prefix := range []string{"unix//", "unix:"} {
		idx := strings.Index(raw, prefix)
		if idx < 0 {
			continue
		}
		socket := raw[idx+len(prefix):]
		if prefix == "unix//" {
			socket = "/" + strings.TrimLeft(socket, "/")
		}
		if sep := strings.Index(socket, ":/"); sep >= 0 {
			socket = socket[:sep]
		}
		socket = strings.TrimSpace(socket)
		if socket == "" {
			return "", false
		}
		return socket, true
	}
	return "", false
}
