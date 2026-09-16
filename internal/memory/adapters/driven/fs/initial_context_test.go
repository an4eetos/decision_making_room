package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitialContextReaderRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	aboutMe := filepath.Join(dir, "about-me.md")
	contextDir := filepath.Join(dir, "context")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := os.WriteFile(aboutMe, []byte("engineer in Almaty"), 0o644); err != nil {
		t.Fatalf("write about-me: %v", err)
	}
	if err := os.WriteFile(filepath.Join(contextDir, "work.md"), []byte("Go stack"), 0o644); err != nil {
		t.Fatalf("write work: %v", err)
	}

	reader := NewInitialContextReader(aboutMe, contextDir, 0)
	got, err := reader.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got == "" || !contains(got, "engineer") || !contains(got, "Go stack") {
		t.Fatalf("unexpected content: %q", got)
	}
}

func TestInitialContextReaderMissingFiles(t *testing.T) {
	t.Parallel()

	reader := NewInitialContextReader(filepath.Join(t.TempDir(), "missing.md"), "", 0)
	got, err := reader.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && stringIndex(s, sub) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
