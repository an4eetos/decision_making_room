// Package beast wires beast mode's board: timed orders for the crisis gear.
package beast

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	beastpostgres "github.com/an4eetos/decision-room/internal/beast/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/beast/port"
	"github.com/an4eetos/decision-room/internal/beast/usecase"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
	comusecase "github.com/an4eetos/decision-room/internal/commitments/usecase"
	memport "github.com/an4eetos/decision-room/internal/memory/port"
)

var Module = fx.Module("beast",
	fx.Provide(
		provideRepository,
		provideLoops,
		provideBoard,
		// The chat reads and writes the board through its own port, so memory
		// never imports this module.
		func(b *usecase.Board) memport.BeastBoard { return b },
	),
)

func provideRepository(pool *pgxpool.Pool) port.Repository {
	return beastpostgres.NewRepository(pool)
}

type loopsParams struct {
	fx.In

	Manage *comusecase.Manage `optional:"true"`
}

// provideLoops returns nil without commitments: the board then keeps time
// without seeing open loops.
func provideLoops(p loopsParams) port.Loops {
	if p.Manage == nil {
		return nil
	}
	return loops{manage: p.Manage}
}

func provideBoard(repo port.Repository, loops port.Loops) *usecase.Board {
	return usecase.NewBoard(repo, loops)
}

// loops reads the open-loop list for the board. Stale loops count: a loop gone
// quiet for weeks and due today is exactly the crisis.
type loops struct {
	manage *comusecase.Manage
}

func (l loops) DueBy(ctx context.Context, by time.Time) ([]port.Loop, error) {
	live, err := l.manage.List(ctx, []comdomain.Status{comdomain.StatusOpen, comdomain.StatusStale})
	if err != nil {
		return nil, err
	}
	var out []port.Loop
	for _, c := range live {
		if c.DueAt == nil || c.DueAt.After(by) {
			continue
		}
		out = append(out, port.Loop{ID: c.ID, Text: c.Text, DueAt: c.DueAt})
	}
	return out, nil
}

func (l loops) Done(ctx context.Context, id uuid.UUID) error {
	_, err := l.manage.SetStatus(ctx, id, comdomain.StatusDone)
	return err
}
