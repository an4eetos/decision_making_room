// Package app wires the whole binary together. Everything that is not specific
// to one domain module lives here: config, the database pool, and the HTTP
// server that mounts every module's routes.
package app

import (
	"context"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/an4eetos/decision-room/internal/infra/config"
	httpserver "github.com/an4eetos/decision-room/internal/infra/http"
	"github.com/an4eetos/decision-room/internal/infra/postgres"
	"github.com/an4eetos/decision-room/internal/journal"
	"github.com/an4eetos/decision-room/internal/memory"
	"github.com/an4eetos/decision-room/internal/relocation"
	"github.com/an4eetos/decision-room/internal/web"
)

var Module = fx.Module("app",
	fx.Provide(
		provideConfig,
		providePool,
		provideServer,
	),
	memory.Module,
	journal.Module,
	relocation.Module,
	web.Module,
	fx.Invoke(startServer),
)

func provideConfig() (config.Config, error) {
	return config.Load()
}

// providePool opens the pool and migrates before anything else can use it, so a
// fresh clone needs one command rather than a separate migrate step.
func providePool(cfg config.Config, lc fx.Lifecycle) (*pgxpool.Pool, error) {
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
	Routes []httpserver.RouteRegistrar `group:"routes"`
}

func provideServer(p serverParams) *httpserver.Server {
	return httpserver.New(p.Config.HTTPAddr, p.Routes...)
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
