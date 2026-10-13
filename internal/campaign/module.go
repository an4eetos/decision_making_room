package campaign

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	campostgres "github.com/an4eetos/decision-room/internal/campaign/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	"github.com/an4eetos/decision-room/internal/campaign/usecase"
	comport "github.com/an4eetos/decision-room/internal/commitments/port"
	comusecase "github.com/an4eetos/decision-room/internal/commitments/usecase"
	"github.com/an4eetos/decision-room/internal/infra/config"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
)

var Module = fx.Module("campaign",
	fx.Provide(
		provideRepository,
		provideSnapshotRepository,
		provideManage,
		provideExtract,
		provideSnapshots,
		// Extraction subscribes to finished chat turns without chat knowing the
		// campaign exists.
		fx.Annotate(
			func(e *usecase.Extract) memport.TurnObserver { return e },
			fx.ResultTags(`group:"turn_observers"`),
		),
		// Open loops ask the campaign which objectives a new commitment can
		// serve, without importing it.
		func(m *usecase.Manage) comport.ObjectiveSource { return m },
	),
	fx.Invoke(startSnapshots),
)

func provideRepository(pool *pgxpool.Pool) port.Repository {
	return campostgres.NewRepository(pool)
}

func provideSnapshotRepository(pool *pgxpool.Pool) port.SnapshotRepository {
	return campostgres.NewRepository(pool)
}

func provideManage(repo port.Repository, orders *comusecase.Manage) *usecase.Manage {
	return usecase.NewManage(repo, orders)
}

func provideExtract(llm memport.LLM, repo port.Repository, orders *comusecase.Manage, cfg config.Config) *usecase.Extract {
	return usecase.NewExtract(llm, repo, orders, cfg.CampaignExtraction)
}

// provideSnapshots shares the check-in timezone, so a day on the map ends when
// the evening check-in's day does.
func provideSnapshots(manage *usecase.Manage, repo port.SnapshotRepository, loc *time.Location) *usecase.Snapshots {
	return usecase.NewSnapshots(manage, repo, loc, 10*time.Minute)
}

func startSnapshots(lc fx.Lifecycle, s *usecase.Snapshots) {
	var cancel context.CancelFunc
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())
			go s.Run(ctx)
			return nil
		},
		OnStop: func(context.Context) error {
			if cancel != nil {
				cancel()
			}
			return nil
		},
	})
}
