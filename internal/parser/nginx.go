package parser

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	gonginxconfig "github.com/tufanbarisyildirim/gonginx/config"
	gonginxparser "github.com/tufanbarisyildirim/gonginx/parser"
)

var nginxForwardDirectivePorts = map[string]uint16{
	"proxy_pass":   0,
	"grpc_pass":    0,
	"fastcgi_pass": 0,
	"uwsgi_pass":   0,
	"scgi_pass":    0,
}

// NginxParser extracts normalized forward actions from nginx configs.
type NginxParser struct{}

func (NginxParser) Kind() string {
	return "nginx"
}

func (p NginxParser) Parse(content string) (ParseResult, error) {
	return p.ParseBundle("/nginx.conf", map[string]string{"/nginx.conf": content})
}

func (p NginxParser) ParseBundle(mainPath string, files map[string]string) (ParseResult, error) {
	bundle, err := parseNginxBundle(mainPath, files)
	if err != nil {
		return ParseResult{}, err
	}

	upstreams := collectNginxUpstreams(bundle)
	forwards := make([]ForwardAction, 0)
	warnings := make([]string, 0)
	walkNginxFile(filepath.Clean(mainPath), bundle, upstreams, nil, nil, &forwards, &warnings)

	forwards = DedupeForwards(forwards)
	if len(forwards) == 0 {
		return ParseResult{Errors: warnings}, fmt.Errorf("no proxy forward actions found")
	}
	return ParseResult{Forwards: forwards, Errors: warnings}, nil
}

// NginxIncludePaths returns the include paths referenced by one nginx config file.
func NginxIncludePaths(currentPath string, content string) ([]string, error) {
	ast, err := gonginxparser.NewStringParser(content).Parse()
	if err != nil {
		return nil, fmt.Errorf("parse nginx config: %w", err)
	}

	directives := ast.FindDirectives("include")
	paths := make([]string, 0, len(directives))
	seen := make(map[string]struct{}, len(directives))
	for _, directive := range directives {
		params := directive.GetParameters()
		if len(params) == 0 {
			continue
		}
		includePath := strings.TrimSpace(params[0].GetValue())
		if includePath == "" {
			continue
		}
		if !filepath.IsAbs(includePath) {
			includePath = filepath.Join(filepath.Dir(currentPath), includePath)
		}
		includePath = filepath.Clean(includePath)
		if _, ok := seen[includePath]; ok {
			continue
		}
		seen[includePath] = struct{}{}
		paths = append(paths, includePath)
	}
	sort.Strings(paths)
	return paths, nil
}

type nginxBundleFile struct {
	Path     string
	Content  string
	AST      *gonginxconfig.Config
	Includes []string
}

func parseNginxBundle(mainPath string, files map[string]string) (map[string]nginxBundleFile, error) {
	bundle := make(map[string]nginxBundleFile, len(files))
	visiting := make(map[string]struct{})
	if err := parseNginxBundleRecursive(filepath.Clean(mainPath), files, bundle, visiting); err != nil {
		return nil, err
	}
	return bundle, nil
}

func parseNginxBundleRecursive(path string, files map[string]string, bundle map[string]nginxBundleFile, visiting map[string]struct{}) error {
	if _, ok := bundle[path]; ok {
		return nil
	}
	if _, ok := visiting[path]; ok {
		return fmt.Errorf("nginx include cycle at %s", path)
	}
	content, ok := files[path]
	if !ok {
		return fmt.Errorf("nginx config bundle missing %q", path)
	}
	visiting[path] = struct{}{}
	defer delete(visiting, path)

	ast, err := gonginxparser.NewStringParser(content).Parse()
	if err != nil {
		return fmt.Errorf("parse nginx config %s: %w", path, err)
	}
	ast.FilePath = path
	includePaths, err := NginxIncludePaths(path, content)
	if err != nil {
		return err
	}
	bundle[path] = nginxBundleFile{
		Path:     path,
		Content:  content,
		AST:      ast,
		Includes: includePaths,
	}

	for _, includePath := range includePaths {
		for _, match := range expandNginxBundleIncludePaths(path, includePath, files) {
			if err := parseNginxBundleRecursive(match, files, bundle, visiting); err != nil {
				return err
			}
		}
	}
	return nil
}

func collectNginxUpstreams(bundle map[string]nginxBundleFile) map[string][]ForwardTarget {
	upstreams := make(map[string][]ForwardTarget)
	for _, file := range bundle {
		for _, upstream := range file.AST.FindUpstreams() {
			key := strings.ToLower(strings.TrimSpace(upstream.UpstreamName))
			if key == "" {
				continue
			}
			targets := make([]ForwardTarget, 0, len(upstream.UpstreamServers))
			for _, server := range upstream.UpstreamServers {
				target := NormalizeTarget(strings.TrimSpace(server.Address), 0)
				if target.Raw == "" {
					continue
				}
				targets = append(targets, target)
			}
			targets = DedupeTargets(append(upstreams[key], targets...))
			if len(targets) > 0 {
				upstreams[key] = targets
			}
		}
	}
	return upstreams
}

func walkNginxFile(path string, bundle map[string]nginxBundleFile, upstreams map[string][]ForwardTarget, listeners []Listener, hostnames []string, forwards *[]ForwardAction, warnings *[]string) {
	file, ok := bundle[path]
	if !ok {
		return
	}
	walkNginxDirectives(path, file.AST.GetDirectives(), bundle, upstreams, listeners, hostnames, forwards, warnings)
}

func walkNginxDirectives(currentPath string, directives []gonginxconfig.IDirective, bundle map[string]nginxBundleFile, upstreams map[string][]ForwardTarget, listeners []Listener, hostnames []string, forwards *[]ForwardAction, warnings *[]string) {
	for _, directive := range directives {
		switch typed := directive.(type) {
		case *gonginxconfig.Server:
			serverListeners := collectServerListeners(currentPath, typed.GetDirectives(), bundle)
			if len(serverListeners) == 0 {
				serverListeners = []Listener{{Port: 80}}
			}
			serverHostnames := collectServerNames(currentPath, typed.GetDirectives(), bundle)
			walkNginxDirectives(currentPath, typed.GetDirectives(), bundle, upstreams, serverListeners, serverHostnames, forwards, warnings)
			continue
		case *gonginxconfig.Location:
			walkNginxDirectives(currentPath, typed.GetDirectives(), bundle, upstreams, listeners, hostnames, forwards, warnings)
			continue
		}

		if directive.GetName() == "include" {
			for _, includePath := range includeMatchesFromDirective(currentPath, directive, bundle) {
				walkNginxFile(includePath, bundle, upstreams, listeners, hostnames, forwards, warnings)
			}
			continue
		}

		if defaultPort, ok := nginxForwardDirectivePorts[directive.GetName()]; ok {
			appendNginxForwards(listeners, hostnames, directive, upstreams, defaultPort, forwards)
		}

		if block := directive.GetBlock(); block != nil {
			walkNginxDirectives(currentPath, block.GetDirectives(), bundle, upstreams, listeners, hostnames, forwards, warnings)
		}
	}
}

func appendNginxForwards(listeners []Listener, hostnames []string, directive gonginxconfig.IDirective, upstreams map[string][]ForwardTarget, defaultPort uint16, forwards *[]ForwardAction) {
	if len(listeners) == 0 {
		return
	}
	params := directive.GetParameters()
	if len(params) == 0 {
		return
	}
	raw := strings.TrimSpace(params[0].GetValue())
	if raw == "" {
		return
	}

	targets := expandNginxTargets(raw, defaultPort, upstreams)
	if len(targets) == 0 {
		return
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

func collectServerNames(currentPath string, directives []gonginxconfig.IDirective, bundle map[string]nginxBundleFile) []string {
	names := make([]string, 0)
	seen := make(map[string]struct{})
	var walk func(string, []gonginxconfig.IDirective)
	walk = func(path string, directives []gonginxconfig.IDirective) {
		for _, directive := range directives {
			if directive.GetName() == "include" {
				for _, includePath := range includeMatchesFromDirective(path, directive, bundle) {
					file, ok := bundle[includePath]
					if !ok {
						continue
					}
					walk(includePath, file.AST.GetDirectives())
				}
				continue
			}
			if directive.GetName() == "server_name" {
				for _, param := range directive.GetParameters() {
					value := strings.TrimSpace(param.GetValue())
					if value == "" || value == "_" {
						continue
					}
					value = strings.ToLower(strings.TrimSuffix(value, "."))
					if _, ok := seen[value]; ok {
						continue
					}
					seen[value] = struct{}{}
					names = append(names, value)
				}
			}
			if block := directive.GetBlock(); block != nil {
				walk(path, block.GetDirectives())
			}
		}
	}
	walk(currentPath, directives)
	sort.Strings(names)
	return names
}

func collectServerListeners(currentPath string, directives []gonginxconfig.IDirective, bundle map[string]nginxBundleFile) []Listener {
	listeners := make([]Listener, 0)
	seen := make(map[string]struct{})
	var walk func(string, []gonginxconfig.IDirective)
	walk = func(path string, directives []gonginxconfig.IDirective) {
		for _, directive := range directives {
			if directive.GetName() == "include" {
				for _, includePath := range includeMatchesFromDirective(path, directive, bundle) {
					file, ok := bundle[includePath]
					if !ok {
						continue
					}
					walk(includePath, file.AST.GetDirectives())
				}
				continue
			}
			if directive.GetName() == "listen" {
				if listener, ok := parseNginxListenDirective(directive); ok {
					key := listener.Addr + "|" + strconv.FormatUint(uint64(listener.Port), 10)
					if _, exists := seen[key]; !exists {
						seen[key] = struct{}{}
						listeners = append(listeners, listener)
					}
				}
			}
			if block := directive.GetBlock(); block != nil {
				walk(path, block.GetDirectives())
			}
		}
	}
	walk(currentPath, directives)

	sort.Slice(listeners, func(i, j int) bool {
		if listeners[i].Port != listeners[j].Port {
			return listeners[i].Port < listeners[j].Port
		}
		return listeners[i].Addr < listeners[j].Addr
	})
	return listeners
}

func parseNginxListenDirective(directive gonginxconfig.IDirective) (Listener, bool) {
	for _, param := range directive.GetParameters() {
		if listener, ok := parseNginxListenValue(param.GetValue()); ok {
			return listener, true
		}
	}
	return Listener{}, false
}

func parseNginxListenValue(value string) (Listener, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "unix:") {
		return Listener{}, false
	}
	if port, err := strconv.ParseUint(value, 10, 16); err == nil {
		return Listener{Port: uint16(port)}, true
	}
	if strings.HasPrefix(value, "[") {
		if idx := strings.LastIndex(value, "]:"); idx > 0 {
			if port, err := strconv.ParseUint(value[idx+2:], 10, 16); err == nil {
				return Listener{Addr: value[1:idx], Port: uint16(port)}, true
			}
		}
	}
	if idx := strings.LastIndex(value, ":"); idx > 0 {
		if port, err := strconv.ParseUint(value[idx+1:], 10, 16); err == nil {
			return Listener{Addr: value[:idx], Port: uint16(port)}, true
		}
	}
	return Listener{}, false
}

func expandNginxTargets(raw string, defaultPort uint16, upstreams map[string][]ForwardTarget) []ForwardTarget {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.Contains(raw, "$") {
		return []ForwardTarget{{Raw: raw, Kind: TargetKindDynamic}}
	}
	if socket, ok := extractUnixSocket(raw); ok {
		return []ForwardTarget{{Raw: raw, Kind: TargetKindUnix, Socket: socket}}
	}

	hostKey := nginxUpstreamReferenceKey(raw)
	if targets, ok := upstreams[hostKey]; ok && hostKey != "" {
		return DedupeTargets(targets)
	}

	target := NormalizeTarget(raw, defaultPort)
	if target.Raw == "" {
		return nil
	}
	return []ForwardTarget{target}
}

func nginxUpstreamReferenceKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if parsed, err := urlParseHostOnly(raw); err == nil {
		return parsed
	}
	return strings.ToLower(raw)
}

func urlParseHostOnly(raw string) (string, error) {
	if strings.Contains(raw, "://") {
		parts := strings.SplitN(raw, "://", 2)
		raw = parts[1]
		if idx := strings.Index(raw, "/"); idx >= 0 {
			raw = raw[:idx]
		}
	}
	host, port, ok := splitHostPortWithDefault(raw, 0)
	if ok && port > 0 {
		return "", fmt.Errorf("explicit port present")
	}
	if host != "" {
		return strings.ToLower(host), nil
	}
	return strings.ToLower(raw), nil
}

func includeMatchesFromDirective(currentPath string, directive gonginxconfig.IDirective, bundle map[string]nginxBundleFile) []string {
	params := directive.GetParameters()
	if len(params) == 0 {
		return nil
	}
	includePath := strings.TrimSpace(params[0].GetValue())
	if includePath == "" {
		return nil
	}
	return expandNginxBundleIncludePaths(currentPath, includePath, bundleKeys(bundle))
}

func bundleKeys(bundle map[string]nginxBundleFile) map[string]string {
	files := make(map[string]string, len(bundle))
	for path, file := range bundle {
		files[path] = file.Content
	}
	return files
}

func expandNginxBundleIncludePaths(currentPath string, includePath string, files map[string]string) []string {
	if !filepath.IsAbs(includePath) {
		includePath = filepath.Join(filepath.Dir(currentPath), includePath)
	}
	includePath = filepath.Clean(includePath)
	if !hasGlobMeta(includePath) {
		if _, ok := files[includePath]; ok {
			return []string{includePath}
		}
		return nil
	}

	matches := make([]string, 0)
	for candidate := range files {
		if strings.HasPrefix(filepath.Base(candidate), ".") {
			continue
		}
		matched, err := filepath.Match(includePath, candidate)
		if err != nil || !matched {
			continue
		}
		matches = append(matches, candidate)
	}
	sort.Strings(matches)
	return matches
}

func hasGlobMeta(path string) bool {
	return strings.ContainsAny(path, "*?[")
}
