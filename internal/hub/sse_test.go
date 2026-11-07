package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriterSend(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w, err := NewWriter(rec, req)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	if err := w.Send("host.updated", map[string]string{"name": "vps-1"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: host.updated\n") {
		t.Errorf("body missing event line:\n%s", body)
	}
	if !strings.Contains(body, "id: 1\n") {
		t.Errorf("body missing id line:\n%s", body)
	}
	if !strings.Contains(body, "data: {\"name\":\"vps-1\"}\n") {
		t.Errorf("body missing data line:\n%s", body)
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Errorf("body should end with blank line; got:\n%s", body)
	}
}

func TestWriterHeartbeat(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w, _ := NewWriter(rec, req)

	if err := w.Heartbeat(); err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if got := rec.Body.String(); got != ": heartbeat\n\n" {
		t.Errorf("Heartbeat body = %q, want %q", got, ": heartbeat\n\n")
	}
}

func TestWriterWriteTypedEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w, _ := NewWriter(rec, req)

	// Event[T] is aliased from client.Event[T]; using it here proves the
	// shared type works end-to-end.
	e := Event[map[string]string]{
		Name: "metrics.sample",
		Data: map[string]string{"host": "vps-1", "cpu": "12.4"},
	}
	if err := Write(w, e); err != nil {
		t.Fatalf("Write: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "event: metrics.sample\n") {
		t.Errorf("body missing event line:\n%s", body)
	}
}

func TestWriterWriteWithID(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w, _ := NewWriter(rec, req)

	// Replay path: forward an event with its original ID (e.g. from the hub's
	// replay buffer). The writer should emit that exact ID and prime its
	// counter so subsequent auto-numbered events continue past it.
	e := Event[string]{Name: "host.updated", ID: 42, Data: "vps-1"}
	if err := WriteWithID(w, e); err != nil {
		t.Fatalf("WriteWithID: %v", err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "id: 42\n") {
		t.Errorf("body missing id 42:\n%s", body)
	}

	// Next auto-numbered event should be 43, not 1.
	if err := w.Send("host.removed", "vps-2"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	body = rec.Body.String()
	if !strings.Contains(body, "id: 43\n") {
		t.Errorf("body missing id 43 (counter should continue past replayed 42):\n%s", body)
	}
}

func TestWriterErrorEvent(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w, _ := NewWriter(rec, req)

	w.Error("upstream timeout")
	body := rec.Body.String()
	// EventError is aliased from client.EventError == "error".
	if !strings.Contains(body, "event: error\n") {
		t.Errorf("body missing error event line:\n%s", body)
	}
	if !strings.Contains(body, `"error":"upstream timeout"`) {
		t.Errorf("body missing error message:\n%s", body)
	}
}

func TestResumeFrom(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Last-Event-ID", "42")
	if got := ResumeFrom(req); got != 42 {
		t.Errorf("ResumeFrom = %d, want 42", got)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := ResumeFrom(req2); got != 0 {
		t.Errorf("ResumeFrom with no header = %d, want 0", got)
	}

	req3 := httptest.NewRequest(http.MethodGet, "/", nil)
	req3.Header.Set("Last-Event-ID", "not-a-number")
	if got := ResumeFrom(req3); got != 0 {
		t.Errorf("ResumeFrom with bad header = %d, want 0", got)
	}
}

func TestWriterNoCORSHeader(t *testing.T) {
	// Design §9: "no CORS by default". Unlike tailkitd's writer, ours must
	// NOT set Access-Control-Allow-Origin: *.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	_, _ = NewWriter(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty (no CORS by default)", got)
	}
}
