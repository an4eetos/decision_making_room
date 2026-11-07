package domain

import (
	"testing"
	"time"
)

// Late means less time, not more: halved per miss, never under five minutes.
func TestRearmWindowShrinksButNotToNothing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		minutes, expiries int
		want              time.Duration
	}{
		{40, 1, 20 * time.Minute},
		{40, 2, 10 * time.Minute},
		{40, 3, 5 * time.Minute},
		{40, 9, 5 * time.Minute},
		{6, 1, 5 * time.Minute},
		{1, 1, 5 * time.Minute},
	}
	for _, tc := range cases {
		if got := RearmWindow(tc.minutes, tc.expiries); got != tc.want {
			t.Errorf("RearmWindow(%d, %d) = %v, want %v", tc.minutes, tc.expiries, got, tc.want)
		}
	}
}
