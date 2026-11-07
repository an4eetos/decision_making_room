package fs

import "github.com/an4eetos/decision-room/internal/modes/domain"

// Registry is the loaded mode set, indexed by id. Immutable after construction.
type Registry struct {
	modes []domain.Mode
	byID  map[string]domain.Mode
}

func NewRegistry(r domain.Registry) *Registry {
	byID := make(map[string]domain.Mode, len(r.Modes))
	for _, m := range r.Modes {
		byID[m.ID] = m
	}
	return &Registry{modes: r.Modes, byID: byID}
}

func (r *Registry) Get(id string) (domain.Mode, bool) {
	m, ok := r.byID[id]
	return m, ok
}

func (r *Registry) List() []domain.Mode { return r.modes }

func (r *Registry) Default() domain.Mode { return r.byID[DefaultModeID] }
