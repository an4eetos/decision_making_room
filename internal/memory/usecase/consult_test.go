package usecase

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSkipRetrieval(t *testing.T) {
	t.Parallel()

	cases := []struct {
		question string
		want     bool
	}{
		{"hi", true},
		{"Hello", true},
		{"thanks", true},
		{"What did I bench last week?", false},
		{"How is my lifting?", false},
	}

	for _, tc := range cases {
		if got := skipRetrieval(tc.question); got != tc.want {
			t.Fatalf("skipRetrieval(%q) = %v, want %v", tc.question, got, tc.want)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("a", 500)
	truncated := truncateRunes(body, 400)
	if utf8.RuneCountInString(truncated) > 401 {
		t.Fatalf("expected truncation around 400 runes, got %d", utf8.RuneCountInString(truncated))
	}
	if !strings.HasSuffix(truncated, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", truncated)
	}
}
