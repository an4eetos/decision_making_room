package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A model that thinks for two minutes used to look exactly like a dead server:
// nothing on the wire. The keep-alive is what tells them apart.
func TestKeepAliveWritesWhileIdleAndStopsOnReturn(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	sse := &sseWriter{w: rec, rc: http.NewResponseController(rec)}

	stop := sse.keepAlive(5 * time.Millisecond)
	sse.send("stage", map[string]string{"stage": "thinking"})
	time.Sleep(30 * time.Millisecond)
	stop()

	body := rec.Body.String()
	if !strings.Contains(body, ": keep-alive\n\n") {
		t.Fatalf("no keep-alive in %q", body)
	}
	if !strings.Contains(body, "event: stage\n") {
		t.Fatalf("event lost: %q", body)
	}
	// Nothing may be written after stop: the handler has returned by then.
	after := rec.Body.Len()
	time.Sleep(20 * time.Millisecond)
	if rec.Body.Len() != after {
		t.Fatal("keep-alive wrote after stop")
	}
}
