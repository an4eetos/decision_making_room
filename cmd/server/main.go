package main

import (
	"go.uber.org/fx"

	"github.com/an4eetos/decision-room/internal/app"
)

func main() {
	fx.New(app.Module).Run()
}
