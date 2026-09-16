package service

import (
	"testing"

	"github.com/an4eetos/decision-room/internal/memory/domain"
)

func TestClassifyRelativePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		kind domain.MemoryKind
		tags []string
	}{
		{"workouts/2024-01-15.md", domain.KindDailyLog, []string{"workout"}},
		{"books/atomic-habits.md", domain.KindNote, []string{"book"}},
		{"daily/2024-06-27.md", domain.KindDailyLog, []string{"daily"}},
		{"notes/random.md", domain.KindNote, nil},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			kind, tags := ClassifyRelativePath(tt.path)
			if kind != tt.kind {
				t.Fatalf("kind = %q, want %q", kind, tt.kind)
			}
			if len(tags) != len(tt.tags) {
				t.Fatalf("tags = %v, want %v", tags, tt.tags)
			}
			for i := range tags {
				if tags[i] != tt.tags[i] {
					t.Fatalf("tags = %v, want %v", tags, tt.tags)
				}
			}
		})
	}
}

func TestIsJournalFile(t *testing.T) {
	t.Parallel()

	if !IsJournalFile("note.md") {
		t.Fatal("expected .md to be a journal file")
	}
	if IsJournalFile("photo.jpg") {
		t.Fatal("expected .jpg to be ignored")
	}
}

func TestTitleFromPath(t *testing.T) {
	t.Parallel()

	if got := TitleFromPath("books/atomic-habits.md"); got != "atomic-habits" {
		t.Fatalf("title = %q, want atomic-habits", got)
	}
}
