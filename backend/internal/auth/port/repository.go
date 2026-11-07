// Package port declares what the auth use cases need from storage.
package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/auth/domain"
)

var (
	ErrNoOwner         = errors.New("no owner password set")
	ErrOwnerExists     = errors.New("owner password already set")
	ErrSessionNotFound = errors.New("session not found")
)

type Repository interface {
	// OwnerHash returns the stored password hash, or ErrNoOwner.
	OwnerHash(ctx context.Context) (string, error)
	// CreateOwner stores the first password. ErrOwnerExists when one is
	// already there, so two setups racing cannot both win.
	CreateOwner(ctx context.Context, hash string) error
	// ReplaceOwner sets a new password and signs every device out, in one
	// transaction.
	ReplaceOwner(ctx context.Context, hash string) error

	CreateSession(ctx context.Context, s domain.Session) (domain.Session, error)
	// SessionByTokenHash returns an unexpired session, or ErrSessionNotFound.
	SessionByTokenHash(ctx context.Context, hash []byte, now time.Time) (domain.Session, error)
	// TouchSession records use and slides the expiry forward.
	TouchSession(ctx context.Context, id uuid.UUID, seenAt, expiresAt time.Time) error
	// ListSessions returns unexpired sessions, most recently used first.
	ListSessions(ctx context.Context, now time.Time) ([]domain.Session, error)
	// DeleteSession revokes one session, or returns ErrSessionNotFound.
	DeleteSession(ctx context.Context, id uuid.UUID) error
	// DeleteExpiredSessions removes sessions past their expiry.
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error)
}
