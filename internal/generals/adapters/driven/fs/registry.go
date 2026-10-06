package fs

import "github.com/an4eetos/decision-room/internal/generals/domain"

// Registry is the loaded roster, indexed by id. Immutable after construction.
type Registry struct {
	roster domain.Roster
	byID   map[string]domain.Lens
	traps  map[string]domain.Trap
}

func NewRegistry(roster domain.Roster) *Registry {
	names := make(map[string]string, len(roster.Generals))
	for _, g := range roster.Generals {
		names[g.ID] = g.Name
	}
	// Rivals are stored as ids and shown as names, resolved once here.
	for i := range roster.Generals {
		g := &roster.Generals[i]
		g.RivalNames = g.RivalNames[:0]
		for _, id := range g.Rivals {
			if name, ok := names[id]; ok {
				g.RivalNames = append(g.RivalNames, name)
			}
		}
	}

	traps := make(map[string]domain.Trap, len(roster.Traps))
	for _, trap := range roster.Traps {
		traps[trap.ID] = trap
	}
	// Kills are stored as ids and shown as names, the same way rivals are.
	for _, group := range [][]domain.Lens{roster.Generals, roster.Styles} {
		for i := range group {
			l := &group[i]
			l.KillNames = nil
			for _, id := range l.Kills {
				if trap, ok := traps[id]; ok {
					l.KillNames = append(l.KillNames, trap.Name)
				}
			}
		}
	}

	byID := make(map[string]domain.Lens, len(roster.Generals)+len(roster.Styles))
	for _, group := range [][]domain.Lens{roster.Generals, roster.Styles} {
		for _, lens := range group {
			byID[lens.ID] = lens
		}
	}
	return &Registry{roster: roster, byID: byID, traps: traps}
}

func (r *Registry) Get(id string) (domain.Lens, bool) {
	lens, ok := r.byID[id]
	return lens, ok
}

func (r *Registry) Generals() []domain.Lens { return r.roster.Generals }
func (r *Registry) Styles() []domain.Lens   { return r.roster.Styles }
func (r *Registry) Traps() []domain.Trap    { return r.roster.Traps }

func (r *Registry) Trap(id string) (domain.Trap, bool) {
	trap, ok := r.traps[id]
	return trap, ok
}

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
