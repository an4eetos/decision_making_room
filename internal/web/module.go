package web

import (
	"go.uber.org/fx"

	"github.com/an4eetos/decision-room/internal/infra/config"
	httpserver "github.com/an4eetos/decision-room/internal/infra/http"
	"github.com/an4eetos/decision-room/internal/memory/usecase"
	relport "github.com/an4eetos/decision-room/internal/relocation/port"
	relusecase "github.com/an4eetos/decision-room/internal/relocation/usecase"
	"github.com/an4eetos/decision-room/internal/web/api"
	"github.com/an4eetos/decision-room/internal/web/relocationapi"
	"github.com/an4eetos/decision-room/internal/web/ui"
)

// Module provides the JSON API and the browser UI as route registrars. Both join
// the "routes" value group, so adding a module's routes never means touching the
// server constructor.
var Module = fx.Module("web",
	fx.Provide(
		asRoute(provideAPIHandler),
		asRoute(provideUIHandler),
		asRoute(provideRelocationHandler),
	),
)

func asRoute(constructor any) any {
	return fx.Annotate(
		constructor,
		fx.As(new(httpserver.RouteRegistrar)),
		fx.ResultTags(`group:"routes"`),
	)
}

func provideAPIHandler(
	ingest *usecase.Ingest,
	search *usecase.Search,
	consult *usecase.Consult,
	chat *usecase.Chat,
) *api.Handler {
	return api.NewHandler(ingest, search, consult, chat)
}

func provideUIHandler(cfg config.Config) (*ui.Handler, error) {
	return ui.NewHandler(cfg.WebRoot)
}

func provideRelocationHandler(
	plans relport.PlanRepository,
	prices relport.PriceRepository,
	build *relusecase.Build,
) *relocationapi.Handler {
	return relocationapi.NewHandler(plans, prices, build)
}
