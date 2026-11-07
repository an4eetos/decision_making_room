package usecase

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedCapture(t *testing.T, at time.Time) (*Capture, string) {
	t.Helper()
	root := t.TempDir()
	c := NewCapture(root)
	c.now = func() time.Time { return at }
	return c, root
}

func TestCaptureCreatesDatedNoteWithHeading(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 17, 9, 5, 0, 0, time.UTC)
	capture, root := fixedCapture(t, at)

	result, err := capture.Execute(context.Background(), "  shipped the retrieval fix  ")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Date != "2026-09-17" {
		t.Fatalf("date = %q", result.Date)
	}

	body := read(t, filepath.Join(root, "daily", "2026-09-17.md"))
	// The chunker splits on markdown headers, so a fresh file needs one or it
	// gets chunked blind.
	if !strings.HasPrefix(body, "# 2026-09-17\n\n") {
		t.Fatalf("missing heading:\n%s", body)
	}
	if !strings.Contains(body, "- 09:05 — shipped the retrieval fix\n") {
		t.Fatalf("entry not written as expected:\n%s", body)
	}
}

func TestCaptureAppendsWithoutRepeatingHeading(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 17, 9, 5, 0, 0, time.UTC)
	capture, root := fixedCapture(t, at)

	if _, err := capture.Execute(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	capture.now = func() time.Time { return at.Add(3 * time.Hour) }
	if _, err := capture.Execute(context.Background(), "second"); err != nil {
		t.Fatal(err)
	}

	body := read(t, filepath.Join(root, "daily", "2026-09-17.md"))
	if strings.Count(body, "# 2026-09-17") != 1 {
		t.Fatalf("heading repeated:\n%s", body)
	}
	if !strings.Contains(body, "- 09:05 — first") || !strings.Contains(body, "- 12:05 — second") {
		t.Fatalf("both entries should be present:\n%s", body)
	}
	if strings.Index(body, "first") > strings.Index(body, "second") {
		t.Fatal("entries should stay in chronological order")
	}
}

func TestCaptureRejectsEmptyText(t *testing.T) {
	t.Parallel()

	capture, _ := fixedCapture(t, time.Now())
	if _, err := capture.Execute(context.Background(), "   \n  "); err == nil {
		t.Fatal("expected an error for whitespace-only input")
	}
}

func TestCaptureRequiresConfiguredRoot(t *testing.T) {
	t.Parallel()

	if _, err := NewCapture("").Execute(context.Background(), "x"); err == nil {
		t.Fatal("expected an error when the journal directory is unset")
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
