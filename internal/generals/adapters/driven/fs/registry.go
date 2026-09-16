package fs

import "github.com/an4eetos/decision-room/internal/generals/domain"

// Registry is the loaded roster, indexed by id. Immutable after construction.
type Registry struct {
	roster domain.Roster
	byID   map[string]domain.Lens
}

func NewRegistry(roster domain.Roster) *Registry {
	byID := make(map[string]domain.Lens, len(roster.Generals)+len(roster.Styles))
	for _, group := range [][]domain.Lens{roster.Generals, roster.Styles} {
		for _, lens := range group {
			byID[lens.ID] = lens
		}
	}
	return &Registry{roster: roster, byID: byID}
}

func (r *Registry) Get(id string) (domain.Lens, bool) {
	lens, ok := r.byID[id]
	return lens, ok
}

func (r *Registry) Generals() []domain.Lens { return r.roster.Generals }
func (r *Registry) Styles() []domain.Lens   { return r.roster.Styles }

// Resolve drops unknown ids rather than erroring. A chat session can hold an id
// from a general that has since been renamed or removed from an overlay, and
// that should not fail the question.
func (r *Registry) Resolve(ids []string) []domain.Lens {
	out := make([]domain.Lens, 0, len(ids))
	for _, id := range ids {
		if lens, ok := r.byID[id]; ok {
			out = append(out, lens)
		}
	}
	return out
}
