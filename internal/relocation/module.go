package relocation

import (
	"log"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/an4eetos/decision-room/internal/infra/config"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
	relfs "github.com/an4eetos/decision-room/internal/relocation/adapters/driven/fs"
	relpostgres "github.com/an4eetos/decision-room/internal/relocation/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/relocation/assets"
	"github.com/an4eetos/decision-room/internal/relocation/port"
	"github.com/an4eetos/decision-room/internal/relocation/usecase"
)

var Module = fx.Module("relocation",
	fx.Provide(
		provideCatalog,
		providePlanRepository,
		providePriceRepository,
		providePrice,
		provideBuild,
	),
)

// provideCatalog loads and validates at startup. Returning an error here aborts
// the boot, which is the right outcome: a silently dropped catalogue entry is a
// checklist quietly missing the one thing you needed.
func provideCatalog(cfg config.Config) (port.CatalogReader, error) {
	catalog, err := relfs.Load(assets.Catalog(), assets.Pitfalls(), cfg.RelocationCatalogDir)
	if err != nil {
		return nil, err
	}
	log.Printf("relocation: loaded %d catalogue items and %d pitfalls",
		len(catalog.Items), len(catalog.Pitfalls))
	return relfs.NewCatalogReader(catalog), nil
}

func providePlanRepository(pool *pgxpool.Pool) port.PlanRepository {
	return relpostgres.NewRepository(pool)
}

func providePriceRepository(pool *pgxpool.Pool) port.PriceRepository {
	return relpostgres.NewPriceRepository(pool)
}

func providePrice(llm memport.LLM) *usecase.Price {
	return usecase.NewPrice(llm)
}

func provideBuild(
	plans port.PlanRepository,
	prices port.PriceRepository,
	catalog port.CatalogReader,
	pricer *usecase.Price,
) *usecase.Build {
	return usecase.NewBuild(plans, prices, catalog, pricer)
}
