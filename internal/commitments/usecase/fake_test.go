package usecase

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/commitments/domain"
	"github.com/an4eetos/decision-room/internal/commitments/port"
	"github.com/an4eetos/decision-room/internal/commitments/service"
)

// memRepo mirrors the Postgres constraints that matter, in particular the
// partial unique index: one live row per fingerprint.
type memRepo struct {
	mu   sync.Mutex
	rows map[uuid.UUID]domain.Commitment
}

func newMemRepo() *memRepo { return &memRepo{rows: map[uuid.UUID]domain.Commitment{}} }

func (m *memRepo) liveDup(fp string, except uuid.UUID) bool {
	for id, r := range m.rows {
		if id != except && r.Fingerprint == fp && r.Status.Live() {
			return true
		}
	}
	return false
}

func (m *memRepo) Create(_ context.Context, c domain.Commitment) (domain.Commitment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.Status.Live() && m.liveDup(c.Fingerprint, uuid.Nil) {
		return domain.Commitment{}, port.ErrDuplicate
	}
	c.ID = uuid.New()
	c.CreatedAt, c.UpdatedAt = time.Now(), time.Now()
	m.rows[c.ID] = c
	return c, nil
}

func (m *memRepo) Get(_ context.Context, id uuid.UUID) (domain.Commitment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return domain.Commitment{}, port.ErrNotFound
	}
	return c, nil
}

func (m *memRepo) List(_ context.Context, statuses []domain.Status, _ int) ([]domain.Commitment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Commitment
	for _, r := range m.rows {
		if len(statuses) == 0 {
			out = append(out, r)
			continue
		}
		for _, s := range statuses {
			if r.Status == s {
				out = append(out, r)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *memRepo) SetStatus(_ context.Context, id uuid.UUID, s domain.Status) (domain.Commitment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return domain.Commitment{}, port.ErrNotFound
	}
	if s.Live() && m.liveDup(c.Fingerprint, id) {
		return domain.Commitment{}, port.ErrDuplicate
	}
	c.Status = s
	c.UpdatedAt = time.Now()
	m.rows[id] = c
	return c, nil
}

func (m *memRepo) UpdateText(_ context.Context, id uuid.UUID, text, fp string, due *time.Time) (domain.Commitment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return domain.Commitment{}, port.ErrNotFound
	}
	c.Text, c.Fingerprint, c.DueAt = text, fp, due
	m.rows[id] = c
	return c, nil
}

func (m *memRepo) MarkStale(_ context.Context, cutoff time.Time) ([]domain.Commitment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Commitment
	for id, r := range m.rows {
		if r.Status == domain.StatusOpen && r.UpdatedAt.Before(cutoff) {
			r.Status = domain.StatusStale
			m.rows[id] = r
			out = append(out, r)
		}
	}
	return out, nil
}

// touch backdates a row, for staleness tests.
func (m *memRepo) touch(id uuid.UUID, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.rows[id]
	r.UpdatedAt = at
	m.rows[id] = r
}

func fp(text string) string { return service.Fingerprint(text) }
