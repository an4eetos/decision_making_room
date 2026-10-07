package usecase

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
	comport "github.com/an4eetos/decision-room/internal/commitments/port"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
)

// memRepo is the campaign repository in memory, enforcing the same dedupe the
// partial unique indexes do.
type memRepo struct {
	mu     sync.Mutex
	fronts []domain.Front
	items  map[uuid.UUID]domain.Item
	order  []uuid.UUID
}

func newMemRepo() *memRepo { return &memRepo{items: map[uuid.UUID]domain.Item{}} }

func (m *memRepo) CreateFront(_ context.Context, name string) (domain.Front, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.fronts {
		if f.Status == domain.FrontActive && strings.EqualFold(f.Name, name) {
			return domain.Front{}, port.ErrDuplicate
		}
	}
	f := domain.Front{ID: uuid.New(), Name: name, Status: domain.FrontActive, Position: len(m.fronts)}
	m.fronts = append(m.fronts, f)
	return f, nil
}

func (m *memRepo) UpdateFront(_ context.Context, f domain.Front) (domain.Front, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.fronts {
		if m.fronts[i].ID == f.ID {
			m.fronts[i] = f
			return f, nil
		}
	}
	return domain.Front{}, port.ErrNotFound
}

func (m *memRepo) GetFront(_ context.Context, id uuid.UUID) (domain.Front, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.fronts {
		if f.ID == id {
			return f, nil
		}
	}
	return domain.Front{}, port.ErrNotFound
}

func (m *memRepo) ListFronts(_ context.Context, activeOnly bool) ([]domain.Front, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Front
	for _, f := range m.fronts {
		if !activeOnly || f.Status == domain.FrontActive {
			out = append(out, f)
		}
	}
	return out, nil
}

func (m *memRepo) liveDup(it domain.Item) bool {
	for id, other := range m.items {
		if id != it.ID && other.Type == it.Type && other.Fingerprint == it.Fingerprint && other.Status.Live() {
			return true
		}
	}
	return false
}

func (m *memRepo) CreateItem(_ context.Context, it domain.Item) (domain.Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if it.Status.Live() && m.liveDup(it) {
		return domain.Item{}, port.ErrDuplicate
	}
	it.ID = uuid.New()
	it.CreatedAt, it.UpdatedAt = time.Now(), time.Now()
	m.items[it.ID] = it
	m.order = append(m.order, it.ID)
	return it, nil
}

func (m *memRepo) GetItem(_ context.Context, id uuid.UUID) (domain.Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return domain.Item{}, port.ErrNotFound
	}
	return it, nil
}

func (m *memRepo) UpdateItem(_ context.Context, it domain.Item) (domain.Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.items[it.ID]; !ok {
		return domain.Item{}, port.ErrNotFound
	}
	if it.Status.Live() && m.liveDup(it) {
		return domain.Item{}, port.ErrDuplicate
	}
	if !it.Status.Live() {
		if it.ResolvedAt == nil {
			now := time.Now()
			it.ResolvedAt = &now
		}
	} else {
		it.ResolvedAt = nil
	}
	m.items[it.ID] = it
	return it, nil
}

func (m *memRepo) ListItems(_ context.Context, statuses []domain.Status, _ int) ([]domain.Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.Item
	for _, id := range m.order {
		it := m.items[id]
		if len(statuses) == 0 {
			out = append(out, it)
			continue
		}
		for _, s := range statuses {
			if it.Status == s {
				out = append(out, it)
			}
		}
	}
	return out, nil
}

func (m *memRepo) byText(text string) domain.Item {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, it := range m.items {
		if it.Text == text {
			return it
		}
	}
	return domain.Item{}
}

// memOrders is the commitments repository in memory.
type memOrders struct {
	mu   sync.Mutex
	rows map[uuid.UUID]comdomain.Commitment
}

func newMemOrders() *memOrders { return &memOrders{rows: map[uuid.UUID]comdomain.Commitment{}} }

func (m *memOrders) Create(_ context.Context, c comdomain.Commitment) (comdomain.Commitment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.Fingerprint == c.Fingerprint && r.Status.Live() {
			return comdomain.Commitment{}, comport.ErrDuplicate
		}
	}
	c.ID = uuid.New()
	m.rows[c.ID] = c
	return c, nil
}

func (m *memOrders) Get(_ context.Context, id uuid.UUID) (comdomain.Commitment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return comdomain.Commitment{}, comport.ErrNotFound
	}
	return c, nil
}

func (m *memOrders) List(_ context.Context, statuses []comdomain.Status, _ int) ([]comdomain.Commitment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []comdomain.Commitment
	for _, c := range m.rows {
		for _, s := range statuses {
			if c.Status == s {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func (m *memOrders) SetStatus(_ context.Context, id uuid.UUID, s comdomain.Status) (comdomain.Commitment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.rows[id]
	c.Status = s
	m.rows[id] = c
	return c, nil
}

func (m *memOrders) UpdateText(_ context.Context, id uuid.UUID, text, fp string, due *time.Time) (comdomain.Commitment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.rows[id]
	c.Text, c.Fingerprint, c.DueAt = text, fp, due
	m.rows[id] = c
	return c, nil
}

func (m *memOrders) SetTarget(_ context.Context, id uuid.UUID, target *uuid.UUID, kind comdomain.Kind) (comdomain.Commitment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok {
		return comdomain.Commitment{}, comport.ErrNotFound
	}
	c.TargetID, c.Kind = target, kind
	m.rows[id] = c
	return c, nil
}

func (m *memOrders) ListByTargets(_ context.Context, targets []uuid.UUID) ([]comdomain.Commitment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []comdomain.Commitment
	for _, c := range m.rows {
		for _, t := range targets {
			if c.TargetID != nil && *c.TargetID == t {
				out = append(out, c)
			}
		}
	}
	return out, nil
}

func (m *memOrders) MarkStale(context.Context, time.Time) ([]comdomain.Commitment, error) {
	return nil, nil
}

func (m *memOrders) all() []comdomain.Commitment {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []comdomain.Commitment
	for _, c := range m.rows {
		out = append(out, c)
	}
	return out
}

// fakeLLM returns a canned answer and records the prompt it was given.
type fakeLLM struct {
	answer string
	got    []memport.Message
}

func (f *fakeLLM) Chat(_ context.Context, messages []memport.Message) (string, error) {
	f.got = messages
	return f.answer, nil
}
