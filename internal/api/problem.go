// Problem+json writer + error mapping (design §16.7, RFC 9457).
//
// Revised per README gap #2: the design's 409 mapping for capability issues
// was incorrect. The daemon actually returns:
//   - 503 for "integration not configured" → capability-unavailable
//   - 403 for "operation blocked by ACL/node config" → permission-denied
//   - 409 is reserved for /admin/* epoch conflicts in tailkitd; never used
//     by Tailflow for capability issues.

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/wf-pro-dev/tailkit/client"

	"github.com/wf-pro-dev/tailflow/internal/state"
)

// Problem is an RFC 9457 problem document. Mirrors the OpenAPI Problem schema.
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail"`
	Host      string `json:"host,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

// writeProblem emits a problem+json response. host is optional (empty string
// omits the field).
func (h *Handler) writeProblem(w http.ResponseWriter, r *http.Request, status int, typ, title, detail, host string) {
	p := Problem{
		Type:      typ,
		Title:     title,
		Status:    status,
		Detail:    detail,
		Host:      host,
		RequestID: requestIDFromContext(r.Context()),
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

// mapUpstreamError converts a tailkit error to an HTTP status + problem type.
// Used by every /hosts/{host}/* handler that calls into the collector.
//
// Mapping (revised per README gap #2):
//
//	ErrDockerUnavailable, ErrMetricsUnavailable → 503 capability-unavailable
//	ErrPermissionDenied                         → 403 permission-denied
//	ErrUnauthorized                            → 502 upstream-error
//	context.DeadlineExceeded                   → 504 upstream-timeout
//	other                                      → 502 upstream-error
func mapUpstreamError(err error) (status int, typ, title string) {
	switch {
	case errors.Is(err, client.ErrDockerUnavailable), errors.Is(err, client.ErrMetricsUnavailable):
		return http.StatusServiceUnavailable, "capability-unavailable", "Integration not configured"
	case errors.Is(err, client.ErrPermissionDenied):
		return http.StatusForbidden, "permission-denied", "Permission denied"
	case errors.Is(err, client.ErrUnauthorized):
		return http.StatusBadGateway, "upstream-error", "Upstream error"
	case errors.Is(err, context.Canceled):
		return http.StatusBadGateway, "upstream-error", "Client went away"
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout, "upstream-timeout", "Upstream timeout"
	default:
		return http.StatusBadGateway, "upstream-error", "Upstream error"
	}
}

// requestIDFromContext returns the request ID from the context, or empty
// string if none. The middleware that sets it lands in M2 with the full
// API; for M1 we return empty.
func requestIDFromContext(_ context.Context) string {
	return ""
}

// hostStatusToHTTP maps a state.Status to the HTTP status used when a client
// asks for a host that exists but is currently unreachable. Per design §16.7:
//
//	offline, no-agent → 503 host-unreachable
//	stale, degraded    → 200 (data is stale but we still return it)
//	healthy            → 200
func hostStatusToHTTP(s state.Status) int {
	switch s {
	case state.StatusOffline, state.StatusNoAgent:
		return http.StatusServiceUnavailable
	default:
		return http.StatusOK
	}
}
