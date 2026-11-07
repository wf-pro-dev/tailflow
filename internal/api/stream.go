// SSE /api/v1/stream handler. Subscribes to the hub, replays missed events
// on reconnect (Last-Event-ID), and emits `resync` when the gap is too old.
//
// Design §F8, §16.6. The hub owns the replay buffer; this handler is a thin
// adapter that reads from the hub's Sub channel and writes SSE frames via
// the hub.Writer.
//
// Topic syntax (per the OpenAPI spec):
//   hosts              → host.updated, host.removed
//   metrics:{host}     → metrics.sample
//   docker:{host}      → docker.container.changed, docker.inventory.replaced
//   ports:{host}       → port.bound, port.released

package api

import (
	"net/http"
	"strings"
	"time"

	hubpkg "github.com/wf-pro-dev/tailflow/internal/hub"
)

// handleStream is the GET /api/v1/stream SSE endpoint.
func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request) {
	// Check the SSE client cap (design §9, §16.7 too-many-streams).
	// For M2 we don't enforce a hard cap; that lands with the load test in M4.
	// If we did, we'd return 503 problem+json here.

	topicsParam := r.URL.Query().Get("topics")
	if topicsParam == "" {
		h.writeProblem(w, r, http.StatusBadRequest, "invalid-parameter",
			"Missing topics", "The topics query parameter is required", "")
		return
	}
	topics := strings.Split(topicsParam, ",")
	for i := range topics {
		topics[i] = strings.TrimSpace(topics[i])
	}

	lastID := hubpkg.ResumeFrom(r)

	sub, err := h.hub.Subscribe(topics, uint64(lastID))
	if err != nil {
		// ErrGapTooOld → emit resync and end.
		// We still need to open the SSE stream to send the resync event.
		sw, werr := hubpkg.NewWriter(w, r)
		if werr != nil {
			h.writeProblem(w, r, http.StatusInternalServerError, "internal-error",
				"Streaming not supported", werr.Error(), "")
			return
		}
		_ = sw.Send(hubpkg.EventResync, map[string]any{"reason": "replay-gap-too-old"})
		return
	}
	defer sub.Close()

	sw, err := hubpkg.NewWriter(w, r)
	if err != nil {
		h.writeProblem(w, r, http.StatusInternalServerError, "internal-error",
			"Streaming not supported", err.Error(), "")
		return
	}

	// Heartbeat ticker — every 15s per design §F8.
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	// If the sub was closed (e.g. slow-consumer), emit resync before ending.
	closed := sub.Closed()
	emitResyncOnClose := true

	for {
		select {
		case <-r.Context().Done():
			// Browser disconnected; just return.
			return
		case <-closed:
			if emitResyncOnClose {
				_ = sw.Send(hubpkg.EventResync, map[string]any{"reason": sub.Reason()})
			}
			return
		case e, ok := <-sub.C():
			if !ok {
				// Channel closed (same as closed above). Emit resync if we
				// haven't already.
				if emitResyncOnClose {
					_ = sw.Send(hubpkg.EventResync, map[string]any{"reason": sub.Reason()})
				}
				return
			}
			// Forward the event with its original ID so Last-Event-ID works
			// on reconnect.
			if err := sw.SendWithID(e.Name, int64(e.ID), e.Data); err != nil {
				return
			}
		case <-heartbeat.C:
			if err := sw.Heartbeat(); err != nil {
				return
			}
		}
	}
}
