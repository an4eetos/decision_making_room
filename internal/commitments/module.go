package commitments

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	compostgres "github.com/an4eetos/decision-room/internal/commitments/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/commitments/port"
	"github.com/an4eetos/decision-room/internal/commitments/usecase"
	"github.com/an4eetos/decision-room/internal/infra/config"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
)

var Module = fx.Module("commitments",
	fx.Provide(
		provideRepository,
		provideManage,
		provideSweep,
		provideExtract,
		// Extraction subscribes to finished chat turns without chat knowing
		// that commitments exist.
		fx.Annotate(
			func(e *usecase.Extract) memport.TurnObserver { return e },
			fx.ResultTags(`group:"turn_observers"`),
		),
	),
	fx.Invoke(startSweep),
)

func provideRepository(pool *pgxpool.Pool) port.Repository {
	return compostgres.NewRepository(pool)
}

func provideManage(repo port.Repository) *usecase.Manage {
	return usecase.NewManage(repo)
}

func provideSweep(repo port.Repository, cfg config.Config) *usecase.Sweep {
	return usecase.NewSweep(repo, cfg.CommitmentStaleAfter)
}

func provideExtract(llm memport.LLM, repo port.Repository, cfg config.Config) *usecase.Extract {
	return usecase.NewExtract(llm, repo, cfg.CommitmentExtraction)
}

type sweepParams struct {
	fx.In

	Lifecycle fx.Lifecycle
	Sweep     *usecase.Sweep
	Listeners []port.StaleListener `group:"stale_listeners"`
}

// sweepInterval is hourly: staleness is measured in weeks, so running more often
// only costs queries.
const sweepInterval = time.Hour

// startSweep runs the stale sweep once at startup and then hourly. It runs
// whether or not check-ins are enabled, because the open-loop list should stay
// honest either way.
func startSweep(p sweepParams) {
	var cancel context.CancelFunc

	run := func(ctx context.Context) {
		stale, err := p.Sweep.Run(ctx)
		if err != nil {
			log.Printf("commitments: stale sweep failed: %v", err)
			return
		}
		if len(stale) == 0 {
			return
		}
		log.Printf("commitments: %d went stale", len(stale))
		for _, l := range p.Listeners {
			l.OnStale(ctx, stale)
		}
	}

	p.Lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			var ctx context.Context
			ctx, cancel = context.WithCancel(context.Background())

			go func() {
				run(ctx)
				ticker := time.NewTicker(sweepInterval)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						run(ctx)
					}
				}
			}()
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
