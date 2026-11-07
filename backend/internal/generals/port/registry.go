package port

import "github.com/an4eetos/decision-room/internal/generals/domain"

// Registry serves the loaded roster. It is immutable after startup, so the read
// path needs no locking.
type Registry interface {
	Get(id string) (domain.Lens, bool)
	Generals() []domain.Lens
	Styles() []domain.Lens
	// Resolve maps ids to lenses, silently dropping unknown ones: a stale id
	// saved on an old chat session should not fail the request.
	Resolve(ids []string) []domain.Lens
	// Traps is the cognitive-trap catalogue the lenses' kills refer to.
	Traps() []domain.Trap
	Trap(id string) (domain.Trap, bool)
}
