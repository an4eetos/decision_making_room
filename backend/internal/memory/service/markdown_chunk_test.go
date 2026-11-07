package service

import (
	"strings"
	"testing"
)

func texts(chunks []Chunk) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, c.Text)
	}
	return out
}

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
		t.Fatalf("expected 3 markdown sections, got %d: %v", len(chunks), texts(chunks))
	}
	if !strings.Contains(chunks[0].Text, "Morning focus") {
		t.Fatalf("first chunk missing daily content: %q", chunks[0].Text)
	}
	if !strings.Contains(chunks[1].Text, "Bench 3x8") {
		t.Fatalf("second chunk missing workout: %q", chunks[1].Text)
	}
}

// The breadcrumb is prefixed into the body so it reaches the embedding and the
// generated full-text vector, not just the display.
func TestChunksCarryTheirHeadingPath(t *testing.T) {
	t.Parallel()

	body := `# Database choice

Some preamble.

## Why

### Transactions

Transactional guarantees mattered more than scale.`

	chunks := ChunkContent(body, 2000)

	var deepest Chunk
	for _, c := range chunks {
		if strings.Contains(c.Text, "Transactional guarantees") {
			deepest = c
		}
	}

	if deepest.Breadcrumb != "Database choice > Why > Transactions" {
		t.Fatalf("breadcrumb = %q, want the full ancestor path", deepest.Breadcrumb)
	}
	if !strings.HasPrefix(deepest.Text, "> Database choice > Why > Transactions") {
		t.Fatalf("breadcrumb not prefixed into the body: %q", deepest.Text)
	}
	if len(deepest.Headings) != 3 {
		t.Fatalf("headings = %v, want three levels", deepest.Headings)
	}
}

// The old header list stopped at "### ", so a level-four heading was not a split
// point and its content was glued onto its parent.
func TestLevelFourHeadingsSplit(t *testing.T) {
	t.Parallel()

	body := `# Top

top body

#### Deep

deep body`

	chunks := ChunkContent(body, 2000)
	if len(chunks) != 2 {
		t.Fatalf("expected a level-four heading to split, got %d: %v", len(chunks), texts(chunks))
	}
}

// "#hashtag" is not a heading.
func TestHashWithoutSpaceIsNotAHeading(t *testing.T) {
	t.Parallel()

	chunks := ChunkContent("# Real\n\nbody with #hashtag inside\n\nmore", 2000)
	if len(chunks) != 1 {
		t.Fatalf("a hashtag should not split, got %d: %v", len(chunks), texts(chunks))
	}
}

// A document with exactly one heading used to fall through to blind word
// chunking, losing its structure — which covered most daily notes.
func TestSingleHeadingKeepsItsBreadcrumb(t *testing.T) {
	t.Parallel()

	chunks := ChunkContent("# 2026-09-17\n\n- 09:05 — shipped the fix", 2000)
	if len(chunks) != 1 {
		t.Fatalf("expected one chunk, got %d", len(chunks))
	}
	if chunks[0].Breadcrumb != "2026-09-17" {
		t.Fatalf("breadcrumb = %q; a single-heading document should keep it", chunks[0].Breadcrumb)
	}
}

func TestChunkMarkdownFallsBackForPlainText(t *testing.T) {
	t.Parallel()

	body := "plain text without headers that should use word chunking"
	chunks := ChunkContent(body, 2000)
	if len(chunks) != 1 || chunks[0].Text != body {
		t.Fatalf("expected single plain chunk, got %v", texts(chunks))
	}
	if chunks[0].Breadcrumb != "" {
		t.Fatalf("plain text has no headings, got breadcrumb %q", chunks[0].Breadcrumb)
	}
}

func TestChunkMarkdownSplitsOversizedSection(t *testing.T) {
	t.Parallel()

	section := "## Notes\n\n" + strings.Repeat("detail ", 400)
	chunks := ChunkContent(section, 200)
	if len(chunks) < 2 {
		t.Fatalf("expected oversized section to split, got %d", len(chunks))
	}
	for _, c := range chunks {
		if c.Breadcrumb != "Notes" {
			t.Fatalf("every piece of a split section keeps the breadcrumb, got %q", c.Breadcrumb)
		}
	}
}

// A point made across a chunk boundary should be retrievable from both sides.
func TestOversizedSectionsOverlap(t *testing.T) {
	t.Parallel()

	unique := "MARKERWORD"
	filler := strings.Repeat("padding ", 60)
	body := "## Notes\n\n" + filler + unique + ". " + filler

	chunks := ChunkContent(body, 260)
	if len(chunks) < 2 {
		t.Fatalf("expected a split, got %d", len(chunks))
	}

	appearances := 0
	for _, c := range chunks {
		if strings.Contains(c.Text, unique) {
			appearances++
		}
	}
	if appearances < 2 {
		t.Fatalf("the boundary word appears in %d chunks; overlap should carry it into two", appearances)
	}
}

func TestChunkIndexAndTotalAreSet(t *testing.T) {
	t.Parallel()

	chunks := ChunkContent("# A\n\nx\n\n# B\n\ny\n\n# C\n\nz", 2000)
	for i, c := range chunks {
		if c.Index != i || c.Total != len(chunks) {
			t.Fatalf("chunk %d has index %d total %d", i, c.Index, c.Total)
		}
	}
}
