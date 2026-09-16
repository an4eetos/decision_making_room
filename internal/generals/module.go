package generals

import (
	"log"

	"go.uber.org/fx"

	genfs "github.com/an4eetos/decision-room/internal/generals/adapters/driven/fs"
	"github.com/an4eetos/decision-room/internal/generals/assets"
	"github.com/an4eetos/decision-room/internal/generals/port"
	"github.com/an4eetos/decision-room/internal/infra/config"
)

var Module = fx.Module("generals",
	fx.Provide(provideRegistry),
)

// provideRegistry loads and validates at startup. An error aborts the boot on
// purpose: a lens missing its blind spot still renders a card, so nothing
// downstream could detect the degradation.
func provideRegistry(cfg config.Config) (port.Registry, error) {
	roster, err := genfs.Load(assets.Generals(), assets.Styles(), cfg.GeneralsDir)
	if err != nil {
		return nil, err
	}
	log.Printf("generals: loaded %d generals and %d working styles",
		len(roster.Generals), len(roster.Styles))
	return genfs.NewRegistry(roster), nil
}
