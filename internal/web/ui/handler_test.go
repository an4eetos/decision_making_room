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

	handler, err := NewHandler("")
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	tests := []struct {
		path    string
		contain string
		exclude string
	}{
		{"/", "Daily Consult", "Semantic search"},
		{"/ingest", "Ingest Memory", "consult-form"},
		{"/memories", "Semantic search", "consult-form"},
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

// The embedded assets are the default, but WEB_ROOT has to keep working or
// nobody can iterate on CSS without a rebuild.
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

// A bad WEB_ROOT must fail loudly. Silently falling back to the embedded assets
// would mean edits appear to do nothing, which is the worst way to lose an hour.
func TestWebRootMissingIsAnError(t *testing.T) {
	t.Parallel()

	if _, err := NewHandler(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("NewHandler with a missing WEB_ROOT returned nil error")
	}
}

func assetsDir(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "assets")
}
