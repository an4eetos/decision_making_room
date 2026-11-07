// Package auth wires the room's password and sessions.
package auth

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	authoidc "github.com/an4eetos/decision-room/internal/auth/adapters/driven/oidc"
	authpostgres "github.com/an4eetos/decision-room/internal/auth/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/auth/port"
	"github.com/an4eetos/decision-room/internal/auth/usecase"
	"github.com/an4eetos/decision-room/internal/infra/config"
	httpserver "github.com/an4eetos/decision-room/internal/infra/http"
)

// bootstrapTimeout covers the expired-session sweep and one argon2 hash.
const bootstrapTimeout = 30 * time.Second

var Module = fx.Module("auth",
	fx.Provide(
		provideRepository,
		provideService,
		provideOIDC,
		func(s *usecase.Service) httpserver.Authenticator { return authenticator{svc: s} },
	),
	// An invoke, not a start hook: it finishes before the server starts
	// listening, so there is never a moment with no password and no code.
	fx.Invoke(bootstrap),
)

func provideRepository(pool *pgxpool.Pool) port.Repository {
	return authpostgres.NewRepository(pool)
}

func provideService(repo port.Repository, cfg config.Config) *usecase.Service {
	return usecase.NewService(repo, cfg.PasswordLogin)
}

// OIDCCallbackPath is registered with the provider under PUBLIC_URL.
const OIDCCallbackPath = "/auth/oidc/callback"

// provideOIDC returns nil when OIDC_ISSUER is unset; the routes then answer
// 404 and the login page shows only the password.
func provideOIDC(cfg config.Config, svc *usecase.Service) *usecase.OIDC {
	if cfg.OIDCIssuer == "" {
		return nil
	}
	idp := authoidc.NewProvider(cfg.OIDCIssuer, cfg.OIDCClientID, cfg.OIDCClientSecret, cfg.PublicURL+OIDCCallbackPath)
	log.Printf("auth: %s sign-in on; register %s%s as the redirect URI", cfg.OIDCName, cfg.PublicURL, OIDCCallbackPath)
	return usecase.NewOIDC(svc, idp, cfg.OIDCAllowedEmails, cfg.OIDCAppRedirects)
}

func bootstrap(cfg config.Config, svc *usecase.Service) error {
	ctx, cancel := context.WithTimeout(context.Background(), bootstrapTimeout)
	defer cancel()
	return svc.Bootstrap(ctx, cfg.RoomPassword)
}

// authenticator adapts the service to the middleware, which knows sessions
// only by id.
type authenticator struct {
	svc *usecase.Service
}

func (a authenticator) Authenticate(ctx context.Context, token string) (string, error) {
	sess, err := a.svc.Authenticate(ctx, token)
	if errors.Is(err, usecase.ErrUnauthenticated) {
		return "", httpserver.ErrUnauthenticated
	}
	if err != nil {
		return "", err
	}
	return sess.ID.String(), nil
}
