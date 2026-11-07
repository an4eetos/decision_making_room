// Package app wires the whole binary together. Everything that is not specific
// to one domain module lives here: config, the database pool, and the HTTP
// server that mounts every module's routes.
package app

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/an4eetos/decision-room/internal/auth"
	"github.com/an4eetos/decision-room/internal/beast"
	"github.com/an4eetos/decision-room/internal/campaign"
	"github.com/an4eetos/decision-room/internal/checkin"
	"github.com/an4eetos/decision-room/internal/commitments"
	"github.com/an4eetos/decision-room/internal/generals"
	"github.com/an4eetos/decision-room/internal/infra/config"
	httpserver "github.com/an4eetos/decision-room/internal/infra/http"
	"github.com/an4eetos/decision-room/internal/infra/postgres"
	"github.com/an4eetos/decision-room/internal/journal"
	"github.com/an4eetos/decision-room/internal/memory"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
	memusecase "github.com/an4eetos/decision-room/internal/memory/usecase"
	"github.com/an4eetos/decision-room/internal/modes"
	"github.com/an4eetos/decision-room/internal/relocation"
	"github.com/an4eetos/decision-room/internal/settings"
	settingsdomain "github.com/an4eetos/decision-room/internal/settings/domain"
	settingsusecase "github.com/an4eetos/decision-room/internal/settings/usecase"
	"github.com/an4eetos/decision-room/internal/web"
)

var Module = fx.Module("app",
	fx.Provide(
		provideEnvConfig,
		providePool,
		provideResolver,
		provideConfig,
		provideServer,
	),
	settings.Module,
	auth.Module,
	generals.Module,
	modes.Module,
	memory.Module,
	journal.Module,
	relocation.Module,
	commitments.Module,
	campaign.Module,
	beast.Module,
	checkin.Module,
	web.Module,
	fx.Invoke(startServer),
	fx.Invoke(warnOnMixedEmbeddings),
)

// envConfig is the configuration from the environment alone: enough to open
// the database the stored settings live in. Everything else takes
// config.Config, which has them layered in.
type envConfig struct{ config.Config }

func provideEnvConfig() (envConfig, error) {
	cfg, err := config.Load()
	return envConfig{cfg}, err
}

func provideResolver(env envConfig) *config.Resolver {
	return config.NewResolver(env.Config, settingsdomain.Exposed)
}

// provideConfig depends on the settings service only for its side effect:
// the service loads the table when it is built, so the config handed to
// every constructor already carries the stored values.
func provideConfig(resolver *config.Resolver, _ *settingsusecase.Service) config.Config {
	cfg := resolver.Settle()
	config.LogProblems(cfg)
	return cfg
}

// providePool opens the pool and migrates before anything else can use it, so a
// fresh clone needs one command rather than a separate migrate step.
func providePool(env envConfig, lc fx.Lifecycle) (*pgxpool.Pool, error) {
	cfg := env.Config
	ctx := context.Background()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}

	if err := postgres.Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, err
	}

	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			pool.Close()
			return nil
		},
	})

	return pool, nil
}

type serverParams struct {
	fx.In

	Config config.Config
	// Required, not optional: a server without it would serve nothing private,
	// and fx failing at startup says why far more clearly.
	Auth   httpserver.Authenticator
	Routes []httpserver.RouteRegistrar `group:"routes"`
}

func provideServer(p serverParams) *httpserver.Server {
	return httpserver.New(p.Config.HTTPAddr, p.Auth, p.Routes...)
}

func startServer(lc fx.Lifecycle, server *httpserver.Server) {
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			go func() {
				if err := server.Start(); err != nil && err != http.ErrServerClosed {
					log.Printf("server error: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return server.Stop(ctx)
		},
	})
}

// warnOnMixedEmbeddings checks at startup whether the table holds vectors from a
// different embedding model. It cannot be an error — the app is still usable —
// but it must be loud, because the only other symptom is search quietly getting
// worse.
func warnOnMixedEmbeddings(repo memport.MemoryRepository, embedder memport.Embedder) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	memusecase.WarnOnMixedEmbeddings(ctx, repo, embedder)
}
