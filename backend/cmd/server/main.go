package main

import (
	// Embedded zoneinfo, so CHECKIN_TZ resolves inside the distroless image,
	// which ships no tz database.
	_ "time/tzdata"

	"go.uber.org/fx"

	"github.com/an4eetos/decision-room/internal/app"
)

func main() {
	fx.New(app.Module).Run()
}
