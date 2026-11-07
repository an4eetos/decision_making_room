// Package settings wires the settings a client may change: provider, keys,
// models, tiers, check-ins.
package settings

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	chkservice "github.com/an4eetos/decision-room/internal/checkin/service"
	"github.com/an4eetos/decision-room/internal/infra/config"
	"github.com/an4eetos/decision-room/internal/memory/adapters/driven/aiswitch"
	settingspostgres "github.com/an4eetos/decision-room/internal/settings/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/settings/domain"
	"github.com/an4eetos/decision-room/internal/settings/port"
	"github.com/an4eetos/decision-room/internal/settings/usecase"
)

// loadTimeout bounds reading the table at boot.
const loadTimeout = 15 * time.Second

var Module = fx.Module("settings",
	fx.Provide(
		provideRepository,
		provideService,
	),
)

func provideRepository(pool *pgxpool.Pool) port.Repository {
	return settingspostgres.NewRepository(pool)
}

// provideService loads the table before returning, so anything that depends
// on the service, the settled config.Config above all, sees stored values.
func provideService(repo port.Repository, resolver *config.Resolver) (*usecase.Service, error) {
	var cipher *domain.Cipher
	if key := resolver.Current().SettingsKey; key != "" {
		c, err := domain.NewCipher(key)
		if err != nil {
			return nil, err
		}
		cipher = c
	}

	validators := map[string]usecase.Validator{
		"CHECKIN_SLOTS": func(v string) error {
			_, err := chkservice.ParseSlots(v)
			return err
		},
	}

	svc := usecase.NewService(repo, resolver, cipher, validators, probe)
	ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
	defer cancel()
	if err := svc.Load(ctx); err != nil {
		return nil, err
	}
	return svc, nil
}

func probe(ctx context.Context, cfg config.Config) usecase.TestResult {
	chat, embed := aiswitch.Probe(ctx, cfg)
	return usecase.TestResult{Chat: usecase.Check(chat), Embed: usecase.Check(embed)}
}
