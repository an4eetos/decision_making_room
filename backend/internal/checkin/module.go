package checkin

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	camusecase "github.com/an4eetos/decision-room/internal/campaign/usecase"
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
	sitreps *camusecase.Sitreps,
	repo port.Repository,
	notifier *usecase.Notifier,
	loc *time.Location,
) *usecase.Generate {
	// With a map to read, check-ins are sitreps drawn from it.
	return usecase.NewGenerate(consult, chat, commitments, repo, notifier, loc).WithSitreps(sitreps)
}

func provideScheduler(
	gen *usecase.Generate,
	chats memport.ChatRepository,
	loc *time.Location,
	cfg config.Config,
	resolver *config.Resolver,
) (*usecase.Scheduler, error) {
	slots, err := service.ParseSlots(cfg.CheckinSlots)
	if err != nil {
		return nil, err
	}
	// Slots are validated when they are saved, so a parse failure here means
	// the environment changed underneath; keep the last good list.
	var (
		mu       sync.Mutex
		lastRaw  = cfg.CheckinSlots
		lastGood = slots
	)
	live := func() (bool, []service.Slot) {
		cur := resolver.Current()
		mu.Lock()
		defer mu.Unlock()
		if cur.CheckinSlots != lastRaw {
			parsed, err := service.ParseSlots(cur.CheckinSlots)
			if err != nil {
				log.Printf("check-in: CHECKIN_SLOTS %q: %v; keeping the previous slots", cur.CheckinSlots, err)
			} else {
				lastGood = parsed
			}
			lastRaw = cur.CheckinSlots
		}
		return cur.CheckinEnabled, lastGood
	}
	return usecase.NewScheduler(gen, chats, usecase.SchedulerConfig{
		Enabled:   cfg.CheckinEnabled,
		Slots:     slots,
		Location:  loc,
		Grace:     cfg.CheckinGrace,
		IdleAfter: cfg.CheckinIdleAfter,
	}).WithLive(live), nil
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
