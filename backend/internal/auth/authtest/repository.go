// Package authtest holds an in-memory auth repository for tests.
package authtest

import (
	"bytes"
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/auth/domain"
	"github.com/an4eetos/decision-room/internal/auth/port"
)

// Repository mirrors the Postgres adapter's behaviour in memory.
type Repository struct {
	mu       sync.Mutex
	Owner    string
	Sessions map[uuid.UUID]domain.Session
	Touches  int
}

func NewRepository() *Repository {
	return &Repository{Sessions: make(map[uuid.UUID]domain.Session)}
}

func (r *Repository) OwnerHash(context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Owner == "" {
		return "", port.ErrNoOwner
	}
	return r.Owner, nil
}

func (r *Repository) CreateOwner(_ context.Context, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Owner != "" {
		return port.ErrOwnerExists
	}
	r.Owner = hash
	return nil
}

func (r *Repository) ReplaceOwner(_ context.Context, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Owner = hash
	r.Sessions = make(map[uuid.UUID]domain.Session)
	return nil
}

func (r *Repository) CreateSession(_ context.Context, s domain.Session) (domain.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s.ID = uuid.New()
	s.LastSeenAt = s.CreatedAt
	r.Sessions[s.ID] = s
	return s, nil
}

func (r *Repository) SessionByTokenHash(_ context.Context, hash []byte, now time.Time) (domain.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range r.Sessions {
		if bytes.Equal(s.TokenHash, hash) && s.ExpiresAt.After(now) {
			return s, nil
		}
	}
	return domain.Session{}, port.ErrSessionNotFound
}

func (r *Repository) TouchSession(_ context.Context, id uuid.UUID, seenAt, expiresAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.Sessions[id]
	if !ok {
		return nil
	}
	s.LastSeenAt, s.ExpiresAt = seenAt, expiresAt
	r.Sessions[id] = s
	r.Touches++
	return nil
}

func (r *Repository) ListSessions(_ context.Context, now time.Time) ([]domain.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.Session
	for _, s := range r.Sessions {
		if s.ExpiresAt.After(now) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeenAt.After(out[j].LastSeenAt) })
	return out, nil
}

func (r *Repository) DeleteSession(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.Sessions[id]; !ok {
		return port.ErrSessionNotFound
	}
	delete(r.Sessions, id)
	return nil
}

func (r *Repository) DeleteExpiredSessions(_ context.Context, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for id, s := range r.Sessions {
		if !s.ExpiresAt.After(now) {
			delete(r.Sessions, id)
			n++
		}
	}
	return n, nil
}

// IdentityProvider is a scripted OpenID Connect provider.
type IdentityProvider struct {
	Identity port.Identity
	Err      error

	mu        sync.Mutex
	Verifiers map[string]string // state → verifier given to AuthCodeURL
	Nonces    map[string]string // state → nonce
	// LastExchange records what the room sent to Exchange.
	LastVerifier, LastNonce string
}

func (p *IdentityProvider) AuthCodeURL(_ context.Context, state, nonce, verifier string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Verifiers == nil {
		p.Verifiers, p.Nonces = map[string]string{}, map[string]string{}
	}
	p.Verifiers[state], p.Nonces[state] = verifier, nonce
	return "https://idp.example/authorize?state=" + state, nil
}

func (p *IdentityProvider) Exchange(_ context.Context, _, verifier, nonce string) (port.Identity, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.LastVerifier, p.LastNonce = verifier, nonce
	return p.Identity, p.Err
}
