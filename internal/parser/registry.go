package parser

import (
	"fmt"
	"sort"
	"strings"
)

// Registry maps parser kinds to parser implementations.
type Registry map[string]ProxyParser

// NewRegistry returns a registry with the built-in parsers registered.
func NewRegistry() Registry {
	r := Registry{}
	r.Register(NginxParser{})
	r.Register(CaddyParser{})
	return r
}

// Register stores a parser by its normalized kind.
func (r Registry) Register(p ProxyParser) {
	r[strings.ToLower(strings.TrimSpace(p.Kind()))] = p
}

// Parse dispatches to the parser for the given kind.
func (r Registry) Parse(kind string, content string) (ParseResult, error) {
	parser, ok := r[strings.ToLower(strings.TrimSpace(kind))]
	if !ok {
		kinds := make([]string, 0, len(r))
		for kind := range r {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		return ParseResult{}, fmt.Errorf("unsupported parser kind %q (available: %s)", kind, strings.Join(kinds, ", "))
	}

	return parser.Parse(content)
}

// ParseBundle dispatches to a parser that can consume a config bundle.
func (r Registry) ParseBundle(kind string, mainPath string, files map[string]string) (ParseResult, error) {
	parser, ok := r[strings.ToLower(strings.TrimSpace(kind))]
	if !ok {
		kinds := make([]string, 0, len(r))
		for kind := range r {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		return ParseResult{}, fmt.Errorf("unsupported parser kind %q (available: %s)", kind, strings.Join(kinds, ", "))
	}

	if bundleParser, ok := parser.(BundleProxyParser); ok {
		return bundleParser.ParseBundle(mainPath, files)
	}

	content, ok := files[mainPath]
	if !ok {
		return ParseResult{}, fmt.Errorf("config bundle missing main file %q", mainPath)
	}
	return parser.Parse(content)
}
