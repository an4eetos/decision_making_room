package usecase

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	comusecase "github.com/an4eetos/decision-room/internal/commitments/usecase"
)

type memSnapshots struct {
	mu    sync.Mutex
	rows  map[string]domain.Snapshot
	saves int
}

func newMemSnapshots() *memSnapshots { return &memSnapshots{rows: map[string]domain.Snapshot{}} }

func (m *memSnapshots) SaveSnapshot(_ context.Context, s domain.Snapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[s.Day] = s
	m.saves++
	return nil
}

func (m *memSnapshots) GetSnapshot(_ context.Context, day string) (domain.Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.rows[day]
	if !ok {
		return domain.Snapshot{}, port.ErrNotFound
	}
	return s, nil
}

func (m *memSnapshots) ListSnapshots(_ context.Context, _ int) ([]domain.Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var days []string
	for d := range m.rows {
		days = append(days, d)
	}
	// Oldest first, as the repository does.
	for i := range days {
		for j := i + 1; j < len(days); j++ {
			if days[j] < days[i] {
				days[i], days[j] = days[j], days[i]
			}
		}
	}
	out := make([]domain.Snapshot, 0, len(days))
	for _, d := range days {
		out = append(out, m.rows[d])
	}
	return out, nil
}

func TestSnapshotsTakeOnlyOnChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repo, orders := newMemRepo(), newMemOrders()
	manage := NewManage(repo, comusecase.NewManage(orders))
	store := newMemSnapshots()
	snaps := NewSnapshots(manage, store, time.UTC, time.Minute)
	clock := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	snaps.now = func() time.Time { return clock }
	manage.now = snaps.now

	wrote, err := snaps.Take(ctx)
	if err != nil || !wrote {
		t.Fatalf("first snapshot should write: %v %v", wrote, err)
	}
	if wrote, _ = snaps.Take(ctx); wrote {
		t.Fatal("nothing changed; the second take should not write")
	}

	front, _ := manage.CreateFront(ctx, "Work")
	if _, err := manage.CreateItem(ctx, CreateInput{Type: domain.TypeObjective, Text: "Ship", FrontID: &front.ID}); err != nil {
		t.Fatal(err)
	}
	if wrote, _ = snaps.Take(ctx); !wrote {
		t.Fatal("the map changed; the take should write")
	}
	if store.saves != 2 || len(store.rows) != 1 {
		t.Fatalf("one day, two writes: saves=%d days=%d", store.saves, len(store.rows))
	}

	theatre, err := snaps.Day(ctx, "2026-10-13")
	if err != nil || len(theatre.Fronts) != 1 || theatre.Fronts[0].Name != "Work" {
		t.Fatalf("day: %+v %v", theatre, err)
	}
	if _, err := snaps.Day(ctx, "yesterday"); err == nil {
		t.Fatal("a bad day should be refused")
	}
	if _, err := snaps.Day(ctx, "2026-10-12"); err == nil {
		t.Fatal("a day with no snapshot is not found")
	}

	// A new day gets its own row; the timelapse plays oldest first.
	clock = clock.AddDate(0, 0, 1)
	if wrote, _ = snaps.Take(ctx); !wrote {
		t.Fatal("a new day writes even when nothing changed")
	}
	lapse, _ := snaps.Timelapse(ctx)
	if len(lapse) != 2 || lapse[0].Day != "2026-10-13" || lapse[1].Day != "2026-10-14" {
		t.Fatalf("timelapse: %+v", lapse)
	}
}

func TestSnapshotsPrior(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newMemSnapshots()
	snaps := NewSnapshots(NewManage(newMemRepo(), comusecase.NewManage(newMemOrders())), store, time.UTC, time.Minute)

	if prior, err := snaps.Prior(ctx, "2026-10-13"); err != nil || prior != nil {
		t.Fatalf("no record, no prior: %v %v", prior, err)
	}
	for _, day := range []string{"2026-10-01", "2026-10-05", "2026-10-11", "2026-10-13"} {
		_ = store.SaveSnapshot(ctx, domain.Snapshot{Day: day, Theatre: domain.Theatre{Day: day}})
	}
	prior, _ := snaps.Prior(ctx, "2026-10-13")
	if prior == nil || prior.Day != "2026-10-05" {
		t.Fatalf("want the latest snapshot at least a week old, got %+v", prior)
	}
	prior, _ = snaps.Prior(ctx, "2026-10-06")
	if prior == nil || prior.Day != "2026-10-01" {
		t.Fatalf("with nothing a week old, want the earliest before the day, got %+v", prior)
	}
	prior, _ = snaps.Prior(ctx, "2026-10-01")
	if prior != nil {
		t.Fatalf("nothing before the first day: %+v", prior)
	}
}
