package modes

import (
	"log"

	"go.uber.org/fx"

	"github.com/an4eetos/decision-room/internal/infra/config"
	modefs "github.com/an4eetos/decision-room/internal/modes/adapters/driven/fs"
	"github.com/an4eetos/decision-room/internal/modes/assets"
	"github.com/an4eetos/decision-room/internal/modes/port"
	"github.com/an4eetos/decision-room/internal/modes/service"
)

var Module = fx.Module("modes",
	fx.Provide(
		provideRegistry,
		provideDetector,
	),
)

// provideRegistry loads and validates at startup. A mode missing its system
// prompt still resolves and still answers — just without the behaviour it exists
// for — so a hard failure here is the only way that surfaces.
func provideRegistry(cfg config.Config) (port.Registry, error) {
	registry, err := modefs.Load(assets.Modes(), cfg.ModesDir)
	if err != nil {
		return nil, err
	}
	log.Printf("modes: loaded %d conversation modes", len(registry.Modes))
	return modefs.NewRegistry(registry), nil
}

func provideDetector(registry port.Registry) *service.Detector {
	return service.NewDetector(registry)
}
