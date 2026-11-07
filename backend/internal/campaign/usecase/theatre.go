package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	"github.com/an4eetos/decision-room/internal/campaign/service"
)

// Theatre lays out the live map.
func (u *Manage) Theatre(ctx context.Context) (domain.Theatre, error) {
	m, err := u.Map(ctx)
	if err != nil {
		return domain.Theatre{}, err
	}
	return service.Layout(m.Fronts, m.Items, m.Orders, u.now()), nil
}

// maxTimelapseDays bounds the record a timelapse plays: a little over a year.
const maxTimelapseDays = 400

// Snapshots keeps one theatre per day and plays them back.
//
// A ticker that refreshes today's snapshot, rather than a job at midnight,
// because a laptop asleep at midnight would skip the day. Every tick writes
// the map as it stands; the last write of the day is the day's snapshot, and
// a day with nothing changed costs one read.
type Snapshots struct {
	manage *Manage
	repo   port.SnapshotRepository
	loc    *time.Location
	every  time.Duration
	now    func() time.Time
}

func NewSnapshots(manage *Manage, repo port.SnapshotRepository, loc *time.Location, every time.Duration) *Snapshots {
	if loc == nil {
		loc = time.Local
	}
	if every <= 0 {
		every = 10 * time.Minute
	}
	return &Snapshots{manage: manage, repo: repo, loc: loc, every: every, now: time.Now}
}

// Take writes today's snapshot if the map has changed since the last one.
// It reports whether it wrote.
func (s *Snapshots) Take(ctx context.Context) (bool, error) {
	now := s.now().In(s.loc)
	theatre, err := s.manage.Theatre(ctx)
	if err != nil {
		return false, err
	}
	theatre.Day = now.Format("2006-01-02")
	theatre.TakenAt = now

	if current, err := s.repo.GetSnapshot(ctx, theatre.Day); err == nil && same(current.Theatre, theatre) {
		return false, nil
	} else if err != nil && !errors.Is(err, port.ErrNotFound) {
		return false, err
	}

	return true, s.repo.SaveSnapshot(ctx, domain.Snapshot{Day: theatre.Day, TakenAt: now, Theatre: theatre})
}

// same compares two theatres ignoring when they were taken.
func same(a, b domain.Theatre) bool {
	a.TakenAt, b.TakenAt = time.Time{}, time.Time{}
	a.Day, b.Day = "", ""
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// Timelapse is the campaign so far, oldest day first.
func (s *Snapshots) Timelapse(ctx context.Context) ([]domain.Snapshot, error) {
	return s.repo.ListSnapshots(ctx, maxTimelapseDays)
}

// Day returns the map as it stood on a YYYY-MM-DD day.
func (s *Snapshots) Day(ctx context.Context, day string) (domain.Theatre, error) {
	if _, err := time.Parse("2006-01-02", day); err != nil {
		return domain.Theatre{}, errors.New("day must be YYYY-MM-DD")
	}
	snap, err := s.repo.GetSnapshot(ctx, day)
	if err != nil {
		return domain.Theatre{}, err
	}
	return snap.Theatre, nil
}

// Prior finds the snapshot to draw the earlier front line against: the latest
// one at least a week before the day, or failing that the earliest one before
// it, so a campaign a few days old still shows how far the line moved.
func (s *Snapshots) Prior(ctx context.Context, day string) (*domain.Theatre, error) {
	target, err := time.Parse("2006-01-02", day)
	if err != nil {
		return nil, errors.New("day must be YYYY-MM-DD")
	}
	snaps, err := s.repo.ListSnapshots(ctx, maxTimelapseDays)
	if err != nil {
		return nil, err
	}
	weekBefore := target.AddDate(0, 0, -7).Format("2006-01-02")
	var earliest, weekOld *domain.Snapshot
	for i := range snaps {
		snap := &snaps[i]
		if snap.Day >= day {
			break
		}
		if earliest == nil {
			earliest = snap
		}
		if snap.Day <= weekBefore {
			weekOld = snap
		}
	}
	switch {
	case weekOld != nil:
		return &weekOld.Theatre, nil
	case earliest != nil:
		return &earliest.Theatre, nil
	default:
		return nil, nil
	}
}

// Today is the local date a live map is compared against.
func (s *Snapshots) Today() string {
	return s.now().In(s.loc).Format("2006-01-02")
}

// Run takes a snapshot now and then on every tick until the context ends.
func (s *Snapshots) Run(ctx context.Context) {
	s.tick(ctx)
	ticker := time.NewTicker(s.every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Snapshots) tick(ctx context.Context) {
	if _, err := s.Take(ctx); err != nil {
		log.Printf("campaign: snapshot failed: %v", err)
	}
}
