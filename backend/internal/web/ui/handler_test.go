package ui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPagesRenderDistinctContent(t *testing.T) {
	t.Parallel()

	handler, err := NewHandler(assetsDir(t))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	tests := []struct {
		path    string
		contain string
		exclude string
	}{
		{"/", "Briefing", "Semantic search"},
		{"/ingest", "Ingest Memory", "consult-form"},
		{"/memories", "Semantic search", "consult-form"},
		{"/campaign", "campaign-fronts", "consult-form"},
	}

	mux := http.NewServeMux()
	handler.Register(mux)

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}

			body := rec.Body.String()
			if !strings.Contains(body, tc.contain) {
				t.Fatalf("body missing %q", tc.contain)
			}
			if tc.exclude != "" && strings.Contains(body, tc.exclude) {
				t.Fatalf("body should not contain %q", tc.exclude)
			}
		})
	}
}

// Static files are served from WEB_ROOT, so CSS edits need no rebuild.
func TestWebRootServesFromDisk(t *testing.T) {
	t.Parallel()

	handler, err := NewHandler(assetsDir(t))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	mux := http.NewServeMux()
	handler.Register(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("static asset status = %d, want 200", rec.Code)
	}
}

// A bad WEB_ROOT must fail loudly: there is no embedded fallback any more, and
// a server with no pages is worse than one that refuses to start.
func TestWebRootMissingIsAnError(t *testing.T) {
	t.Parallel()

	if _, err := NewHandler(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("NewHandler with a missing WEB_ROOT returned nil error")
	}
	if _, err := NewHandler(""); err == nil {
		t.Fatal("NewHandler with an empty WEB_ROOT returned nil error")
	}
}

func assetsDir(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// The web client lives outside the Go module, at client/web from the
	// repository root.
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "client", "web")
}
