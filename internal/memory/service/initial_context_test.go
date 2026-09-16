package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadInitialContextAboutMeAndDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	aboutMe := filepath.Join(root, "about-me.md")
	contextDir := filepath.Join(root, "context")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(aboutMe, []byte("# Me\n\nEngineer."), 0o644); err != nil {
		t.Fatalf("write about-me: %v", err)
	}
	if err := os.WriteFile(filepath.Join(contextDir, "work.md"), []byte("# Work\n\nGo backend."), 0o644); err != nil {
		t.Fatalf("write work: %v", err)
	}
	if err := os.WriteFile(filepath.Join(contextDir, "health.md"), []byte("# Health\n\nGym 3x/week."), 0o644); err != nil {
		t.Fatalf("write health: %v", err)
	}

	got, err := LoadInitialContext(aboutMe, contextDir, 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	for _, part := range []string{"Engineer.", "Go backend.", "Gym 3x/week."} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in:\n%s", part, got)
		}
	}
}

func TestLoadInitialContextNoLimitByDefault(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "about-me.md")
	long := strings.Repeat("word ", 5000)
	if err := os.WriteFile(path, []byte(long), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := LoadInitialContext(path, "", 0)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != strings.TrimSpace(long) {
		t.Fatalf("expected full content, got len=%d want=%d", len(got), len(long))
	}
}

func TestLoadInitialContextMaxRunesTruncates(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "about-me.md")
	if err := os.WriteFile(path, []byte("abcdefghij"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := LoadInitialContext(path, "", 5)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != "abcde" {
		t.Fatalf("got %q", got)
	}
}
