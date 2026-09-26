package usecase

import (
	"context"
	"time"

	"github.com/an4eetos/decision-room/internal/commitments/domain"
	"github.com/an4eetos/decision-room/internal/commitments/port"
)

// Sweep moves commitments that have gone untouched to stale.
//
// About two weeks is roughly the half-life of an unkept promise: after that,
// reminding you about it is nagging, not help. Stale rows drop out of check-in
// prompts but stay visible, and reopening one is a click.
type Sweep struct {
	repo  port.Repository
	after time.Duration
	now   func() time.Time
}

func NewSweep(repo port.Repository, after time.Duration) *Sweep {
	if after <= 0 {
		after = 14 * 24 * time.Hour
	}
	return &Sweep{repo: repo, after: after, now: time.Now}
}

// Run returns the commitments it just marked stale, so a nudge can name them.
func (u *Sweep) Run(ctx context.Context) ([]domain.Commitment, error) {
	return u.repo.MarkStale(ctx, u.now().Add(-u.after))
}
