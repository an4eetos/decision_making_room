package service

import "testing"

func TestIsExcludedFromJournalSync(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"about-me.md":            true,
		"context/work.md":        true,
		"context/deep/nested.md": true,
		"daily/2024-06-27.md":    false,
		"workouts/log.md":        false,
	}

	for path, want := range cases {
		if got := IsExcludedFromJournalSync(path); got != want {
			t.Fatalf("%s: got %v want %v", path, got, want)
		}
	}
}
