package checkin

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	chkpostgres "github.com/an4eetos/decision-room/internal/checkin/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/checkin/port"
	"github.com/an4eetos/decision-room/internal/checkin/service"
	"github.com/an4eetos/decision-room/internal/checkin/usecase"
	comport "github.com/an4eetos/decision-room/internal/commitments/port"
	comusecase "github.com/an4eetos/decision-room/internal/commitments/usecase"
	"github.com/an4eetos/decision-room/internal/infra/config"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
	memusecase "github.com/an4eetos/decision-room/internal/memory/usecase"
)

var Module = fx.Module("checkin",
	fx.Provide(
		provideLocation,
		provideRepository,
		provideNotifier,
		provideGenerate,
		provideScheduler,
		// Stale nudges fire from the commitments sweep at the moment of the
		// transition, without commitments knowing check-ins exist.
		fx.Annotate(
			func(s *usecase.Scheduler) comport.StaleListener { return s },
			fx.ResultTags(`group:"stale_listeners"`),
		),
	),
	fx.Invoke(startScheduler),
)

// provideLocation fails startup on an unknown timezone rather than quietly
// falling back to the server's, which would fire every check-in at the wrong
// hour with nothing to show why.
func provideLocation(cfg config.Config) (*time.Location, error) {
	if cfg.CheckinTimezone == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(cfg.CheckinTimezone)
	if err != nil {
		return nil, fmt.Errorf("CHECKIN_TZ=%q: %w", cfg.CheckinTimezone, err)
	}
	return loc, nil
}

func provideRepository(pool *pgxpool.Pool) port.Repository {
	return chkpostgres.NewRepository(pool)
}

func provideNotifier(cfg config.Config) *usecase.Notifier {
	return usecase.NewNotifier(cfg.CheckinNotifyCmd)
}

func provideGenerate(
	consult *memusecase.Consult,
	chat *memusecase.Chat,
	commitments *comusecase.Manage,
	repo port.Repository,
	notifier *usecase.Notifier,
	loc *time.Location,
) *usecase.Generate {
	return usecase.NewGenerate(consult, chat, commitments, repo, notifier, loc)
}

func provideScheduler(
	gen *usecase.Generate,
	chats memport.ChatRepository,
	loc *time.Location,
	cfg config.Config,
) (*usecase.Scheduler, error) {
	slots, err := service.ParseSlots(cfg.CheckinSlots)
	if err != nil {
		return nil, err
	}
	return usecase.NewScheduler(gen, chats, usecase.SchedulerConfig{
		Enabled:   cfg.CheckinEnabled,
		Slots:     slots,
		Location:  loc,
		Grace:     cfg.CheckinGrace,
		IdleAfter: cfg.CheckinIdleAfter,
	}), nil
}

func startScheduler(lc fx.Lifecycle, s *usecase.Scheduler) {
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
