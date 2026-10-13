package usecase

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	"github.com/an4eetos/decision-room/internal/campaign/service"
)

// Sitreps reads the map the way the staff does, for anyone who asks: every
// answer, a check-in, the campaign page, and the staff's own appraisal. No
// model call anywhere in it.
type Sitreps struct {
	manage    *Manage
	snapshots *Snapshots
	loc       *time.Location
	// readsIntoAnswers is CAMPAIGN_STAFF_ENABLED: whether the map goes into
	// ordinary answers. Off, the room answers as it did before the map.
	readsIntoAnswers bool
	now              func() time.Time
}

func NewSitreps(manage *Manage, snapshots *Snapshots, loc *time.Location, readsIntoAnswers bool) *Sitreps {
	if loc == nil {
		loc = time.Local
	}
	return &Sitreps{manage: manage, snapshots: snapshots, loc: loc, readsIntoAnswers: readsIntoAnswers, now: time.Now}
}

// Sitrep reads the live map: the theatre with its orders by name, what moved
// in the last day, and the readings. Front lines are compared against the
// last snapshot before today, so "quiet" and "overextended" mean since
// yesterday's map. No model call.
func (s *Sitreps) Sitrep(ctx context.Context) (domain.Sitrep, error) {
	now := s.now().In(s.loc)
	m, err := s.manage.Map(ctx)
	if err != nil {
		return domain.Sitrep{}, err
	}
	var prior *domain.Theatre
	if s.snapshots != nil {
		prior, err = s.snapshots.LastBefore(ctx, now.Format("2006-01-02"))
		if err != nil {
			// The sitrep still reads without yesterday's line.
			log.Printf("campaign: sitrep prior snapshot: %v", err)
			prior = nil
		}
	}
	return service.Read(m.Fronts, m.Items, m.Orders, prior, now), nil
}

// Situation implements the memory module's SituationReader: the brief that
// goes under "About me" in every answer. Empty when the map is empty or the
// staff is switched off, which leaves the answer exactly as it was.
func (s *Sitreps) Situation(ctx context.Context) (string, error) {
	if !s.readsIntoAnswers {
		return "", nil
	}
	sitrep, err := s.Sitrep(ctx)
	if err != nil {
		return "", err
	}
	return service.Brief(sitrep), nil
}

// LastBefore returns the latest snapshot taken before a YYYY-MM-DD day, or nil
// when there is none: yesterday's map, usually. One row: it is read on every
// answer and every check-in, so it must not load the whole record the way
// the timelapse does.
func (s *Snapshots) LastBefore(ctx context.Context, day string) (*domain.Theatre, error) {
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return nil, errors.New("day must be YYYY-MM-DD")
	}
	snap, err := s.repo.GetSnapshotBefore(ctx, day)
	if errors.Is(err, port.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &snap.Theatre, nil
}
