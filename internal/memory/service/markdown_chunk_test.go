package service

import (
	"strings"
	"testing"
)

func TestChunkMarkdownByHeaders(t *testing.T) {
	t.Parallel()

	body := `# Daily

Morning focus: ship retrieval.

## Workout

Bench 3x8 @ 60kg.

## Books

Atomic Habits — systems over goals.`

	chunks := ChunkContent(body, 2000)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 markdown sections, got %d: %v", len(chunks), chunks)
	}
	if !strings.Contains(chunks[0], "Morning focus") {
		t.Fatalf("first chunk missing daily content: %q", chunks[0])
	}
	if !strings.Contains(chunks[1], "Bench 3x8") {
		t.Fatalf("second chunk missing workout: %q", chunks[1])
	}
}

func TestChunkMarkdownFallsBackForPlainText(t *testing.T) {
	t.Parallel()

	body := "plain text without headers that should use word chunking"
	chunks := ChunkContent(body, 2000)
	if len(chunks) != 1 || chunks[0] != body {
		t.Fatalf("expected single plain chunk, got %v", chunks)
	}
}

func TestChunkMarkdownSplitsOversizedSection(t *testing.T) {
	t.Parallel()

	section := "## Notes\n\n" + strings.Repeat("detail ", 400)
	chunks := ChunkContent(section, 200)
	if len(chunks) < 2 {
		t.Fatalf("expected oversized section to split, got %d", len(chunks))
	}
}
