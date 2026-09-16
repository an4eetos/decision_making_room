package port

import "github.com/an4eetos/decision-room/internal/modes/domain"

// Registry serves the loaded modes. Immutable after startup.
type Registry interface {
	Get(id string) (domain.Mode, bool)
	List() []domain.Mode
	// Default is the open mode, used when nothing more specific fits.
	Default() domain.Mode
}
