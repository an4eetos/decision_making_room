// Package usecase signs the owner in and keeps track of their devices.
package usecase

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/auth/domain"
	"github.com/an4eetos/decision-room/internal/auth/port"
)

var (
	ErrWrongPassword   = errors.New("wrong password")
	ErrSetupRequired   = errors.New("no password set yet: open /setup")
	ErrSetupClosed     = errors.New("setup is closed: a password is already set")
	ErrWrongSetupCode  = errors.New("wrong setup code")
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrPasswordOff     = errors.New("password sign-in is turned off")
)

// hashSlots bounds concurrent argon2 runs. Each takes 64 MiB, so a burst of
// parallel logins would otherwise be a way to run a small host out of memory
// before the rate limiter has counted a single failure.
const hashSlots = 2

// Service owns the password and the sessions.
type Service struct {
	repo  port.Repository
	now   func() time.Time
	slots chan struct{}
	// password is false when PASSWORD_LOGIN_ENABLED=false: OIDC is then the
	// only way in, and there is no setup code to issue.
	password bool

	// setupCode is issued at boot when no password exists, and only shown in
	// the server log: whoever can read the log owns the deployment. Without
	// it, a freshly deployed room would belong to whoever found the URL first.
	mu        sync.Mutex
	setupCode string
}

func NewService(repo port.Repository, passwordLogin bool) *Service {
	return &Service{
		repo:     repo,
		now:      time.Now,
		slots:    make(chan struct{}, hashSlots),
		password: passwordLogin,
	}
}

// PasswordLogin reports whether the password is a way in.
func (s *Service) PasswordLogin() bool { return s.password }

// Bootstrap runs once at startup. With ROOM_PASSWORD set, the variable is the
// password: it creates the owner on first boot and replaces a different
// stored password later, signing every device out, which makes the variable
// the way back in after a forgotten password. Without it and without an
// owner, it issues a setup code and logs it.
func (s *Service) Bootstrap(ctx context.Context, envPassword string) error {
	if n, err := s.repo.DeleteExpiredSessions(ctx, s.now()); err != nil {
		return err
	} else if n > 0 {
		log.Printf("auth: removed %d expired session(s)", n)
	}

	if !s.password {
		if envPassword != "" {
			log.Printf("auth: ROOM_PASSWORD is ignored: PASSWORD_LOGIN_ENABLED=false")
		}
		return nil
	}

	stored, err := s.repo.OwnerHash(ctx)
	if err != nil && !errors.Is(err, port.ErrNoOwner) {
		return err
	}
	hasOwner := err == nil

	if envPassword != "" {
		if err := domain.ValidatePassword(envPassword); err != nil {
			return fmt.Errorf("ROOM_PASSWORD: %w", err)
		}
		if hasOwner {
			ok, err := s.verify(ctx, envPassword, stored)
			if err != nil {
				return err
			}
			if ok {
				return nil
			}
		}
		hash, err := s.hash(ctx, envPassword)
		if err != nil {
			return err
		}
		if hasOwner {
			if err := s.repo.ReplaceOwner(ctx, hash); err != nil {
				return err
			}
			log.Printf("auth: password replaced from ROOM_PASSWORD; every device has been signed out")
			return nil
		}
		if err := s.repo.CreateOwner(ctx, hash); err != nil {
			return err
		}
		log.Printf("auth: password set from ROOM_PASSWORD")
		return nil
	}

	if hasOwner {
		return nil
	}
	code, err := domain.NewSetupCode()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.setupCode = code
	s.mu.Unlock()
	log.Printf("auth: no password set. Open /setup and enter the setup code %s (or set ROOM_PASSWORD and restart)", code)
	return nil
}

// NeedsSetup reports whether the room has no password yet and is waiting
// for one. Always false with the password turned off.
func (s *Service) NeedsSetup(ctx context.Context) (bool, error) {
	if !s.password {
		return false, nil
	}
	_, err := s.repo.OwnerHash(ctx)
	if errors.Is(err, port.ErrNoOwner) {
		return true, nil
	}
	return false, err
}

// Setup sets the first password with the code from the log and signs the
// device in.
func (s *Service) Setup(ctx context.Context, code, password, client string) (string, domain.Session, error) {
	if !s.password {
		return "", domain.Session{}, ErrSetupClosed
	}
	if err := domain.ValidatePassword(password); err != nil {
		return "", domain.Session{}, err
	}

	s.mu.Lock()
	issued := s.setupCode
	s.mu.Unlock()
	if issued == "" {
		return "", domain.Session{}, ErrSetupClosed
	}
	if !domain.SetupCodeMatches(code, issued) {
		return "", domain.Session{}, ErrWrongSetupCode
	}

	hash, err := s.hash(ctx, password)
	if err != nil {
		return "", domain.Session{}, err
	}
	if err := s.repo.CreateOwner(ctx, hash); err != nil {
		if errors.Is(err, port.ErrOwnerExists) {
			return "", domain.Session{}, ErrSetupClosed
		}
		return "", domain.Session{}, err
	}

	s.mu.Lock()
	s.setupCode = ""
	s.mu.Unlock()
	log.Printf("auth: password set through /setup; the setup code is spent")

	return s.IssueSession(ctx, client)
}

// Login checks the password and opens a session for the named device.
func (s *Service) Login(ctx context.Context, password, client string) (string, domain.Session, error) {
	if !s.password {
		return "", domain.Session{}, ErrPasswordOff
	}
	stored, err := s.repo.OwnerHash(ctx)
	if errors.Is(err, port.ErrNoOwner) {
		return "", domain.Session{}, ErrSetupRequired
	}
	if err != nil {
		return "", domain.Session{}, err
	}

	ok, err := s.verify(ctx, password, stored)
	if err != nil {
		return "", domain.Session{}, err
	}
	if !ok {
		return "", domain.Session{}, ErrWrongPassword
	}

	// Housekeeping rides on the rare event, not on every request.
	if _, err := s.repo.DeleteExpiredSessions(ctx, s.now()); err != nil {
		log.Printf("auth: %v", err)
	}
	return s.IssueSession(ctx, client)
}

// Authenticate resolves a bearer token to its session, sliding the expiry
// forward at most once per TouchEvery.
func (s *Service) Authenticate(ctx context.Context, token string) (domain.Session, error) {
	if token == "" {
		return domain.Session{}, ErrUnauthenticated
	}
	now := s.now()
	sess, err := s.repo.SessionByTokenHash(ctx, domain.HashToken(token), now)
	if errors.Is(err, port.ErrSessionNotFound) {
		return domain.Session{}, ErrUnauthenticated
	}
	if err != nil {
		return domain.Session{}, err
	}

	if now.Sub(sess.LastSeenAt) >= domain.TouchEvery {
		expires := now.Add(domain.SessionTTL)
		// A failed touch costs a few minutes of expiry, not the request.
		if err := s.repo.TouchSession(ctx, sess.ID, now, expires); err != nil {
			log.Printf("auth: %v", err)
		} else {
			sess.LastSeenAt, sess.ExpiresAt = now, expires
		}
	}
	return sess, nil
}

// Sessions lists the signed-in devices.
func (s *Service) Sessions(ctx context.Context) ([]domain.Session, error) {
	return s.repo.ListSessions(ctx, s.now())
}

// Revoke signs one device out.
func (s *Service) Revoke(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteSession(ctx, id)
}

// IssueSession opens a session for a device whose owner has already been
// proven, by password or by an identity provider. It checks nothing itself.
func (s *Service) IssueSession(ctx context.Context, client string) (string, domain.Session, error) {
	token, hash, err := domain.NewToken()
	if err != nil {
		return "", domain.Session{}, err
	}
	now := s.now()
	sess, err := s.repo.CreateSession(ctx, domain.Session{
		TokenHash: hash,
		Client:    domain.CleanClient(client),
		CreatedAt: now,
		ExpiresAt: now.Add(domain.SessionTTL),
	})
	if err != nil {
		return "", domain.Session{}, err
	}
	return token, sess, nil
}

func (s *Service) hash(ctx context.Context, password string) (string, error) {
	if err := s.acquire(ctx); err != nil {
		return "", err
	}
	defer s.release()
	return domain.HashPassword(password)
}

func (s *Service) verify(ctx context.Context, password, stored string) (bool, error) {
	if err := s.acquire(ctx); err != nil {
		return false, err
	}
	defer s.release()
	return domain.VerifyPassword(password, stored)
}

func (s *Service) acquire(ctx context.Context) error {
	select {
	case s.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) release() { <-s.slots }
