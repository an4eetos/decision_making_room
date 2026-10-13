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
	memusecase "github.com/an4eetos/decision-room/internal/memory/usecase"
)

var Module = fx.Module("campaign",
	fx.Provide(
		provideRepository,
		provideSnapshotRepository,
		provideManage,
		provideExtract,
		provideSnapshots,
		provideSitreps,
		provideAppraisalRepository,
		provideStaff,
		// The staff reads the map into every answer, through the memory
		// module's own port, so memory never imports the campaign.
		func(s *usecase.Sitreps) memport.SituationReader { return s },
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

func provideSitreps(manage *usecase.Manage, snapshots *usecase.Snapshots, loc *time.Location, cfg config.Config) *usecase.Sitreps {
	return usecase.NewSitreps(manage, snapshots, loc, cfg.CampaignStaff)
}

func provideAppraisalRepository(pool *pgxpool.Pool) port.AppraisalRepository {
	return campostgres.NewRepository(pool)
}

// provideStaff is the one place the campaign calls the model on request. It
// depends on the consult, which depends on the sitreps: the two are separate
// so the graph has no cycle.
func provideStaff(sitreps *usecase.Sitreps, manage *usecase.Manage, repo port.AppraisalRepository, consult *memusecase.Consult, chat *memusecase.Chat) *usecase.Staff {
	return usecase.NewStaff(sitreps, manage, repo, consult, chat)
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
