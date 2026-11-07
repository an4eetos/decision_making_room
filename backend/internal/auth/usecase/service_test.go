package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/an4eetos/decision-room/internal/auth/authtest"
	"github.com/an4eetos/decision-room/internal/auth/domain"
)

const goodPassword = "a long enough password"

func newService(t *testing.T, password bool) (*Service, *authtest.Repository, *time.Time) {
	t.Helper()
	repo := authtest.NewRepository()
	svc := NewService(repo, password)
	clock := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return clock }
	return svc, repo, &clock
}

func TestBootstrapIssuesSetupCodeWithoutPassword(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, repo, _ := newService(t, true)

	if err := svc.Bootstrap(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if needs, _ := svc.NeedsSetup(ctx); !needs {
		t.Fatal("NeedsSetup = false with no owner")
	}

	if _, _, err := svc.Setup(ctx, "WRONG-CODE-XXXX", goodPassword, "test"); !errors.Is(err, ErrWrongSetupCode) {
		t.Fatalf("wrong code: %v", err)
	}
	if _, _, err := svc.Setup(ctx, svc.setupCode, "short", "test"); !errors.Is(err, domain.ErrPasswordTooShort) {
		t.Fatalf("short password: %v", err)
	}

	token, sess, err := svc.Setup(ctx, svc.setupCode, goodPassword, "laptop")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if token == "" || sess.Client != "laptop" || repo.Owner == "" {
		t.Fatalf("setup did not sign in: token=%q sess=%+v", token, sess)
	}

	// The code is spent: a second setup cannot replace the password.
	if _, _, err := svc.Setup(ctx, "anything", goodPassword+"!", "x"); !errors.Is(err, ErrSetupClosed) {
		t.Fatalf("second setup: %v", err)
	}
	if needs, _ := svc.NeedsSetup(ctx); needs {
		t.Fatal("NeedsSetup = true after setup")
	}
}

func TestBootstrapFromEnvironment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, repo, _ := newService(t, true)

	if err := svc.Bootstrap(ctx, "short"); err == nil {
		t.Fatal("short ROOM_PASSWORD accepted")
	}

	if err := svc.Bootstrap(ctx, goodPassword); err != nil {
		t.Fatal(err)
	}
	if svc.setupCode != "" {
		t.Fatal("setup code issued although ROOM_PASSWORD set the owner")
	}
	if _, _, err := svc.Login(ctx, goodPassword, "phone"); err != nil {
		t.Fatalf("login with env password: %v", err)
	}

	// Same password on the next boot: nothing changes, devices stay in.
	if err := svc.Bootstrap(ctx, goodPassword); err != nil {
		t.Fatal(err)
	}
	if len(repo.Sessions) != 1 {
		t.Fatalf("unchanged password signed devices out: %d sessions", len(repo.Sessions))
	}

	// A different one replaces it and signs every device out.
	if err := svc.Bootstrap(ctx, goodPassword+" v2"); err != nil {
		t.Fatal(err)
	}
	if len(repo.Sessions) != 0 {
		t.Fatalf("replaced password left %d sessions", len(repo.Sessions))
	}
	if _, _, err := svc.Login(ctx, goodPassword, "phone"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("old password still works: %v", err)
	}
}

func TestLoginAndAuthenticate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, repo, clock := newService(t, true)

	if _, _, err := svc.Login(ctx, goodPassword, "x"); !errors.Is(err, ErrSetupRequired) {
		t.Fatalf("login before setup: %v", err)
	}
	if err := svc.Bootstrap(ctx, goodPassword); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Login(ctx, "not the password", "x"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong password: %v", err)
	}

	token, sess, err := svc.Login(ctx, goodPassword, "phone")
	if err != nil {
		t.Fatal(err)
	}
	if sess.ExpiresAt != clock.Add(domain.SessionTTL) {
		t.Fatalf("expiry %v", sess.ExpiresAt)
	}

	if _, err := svc.Authenticate(ctx, "bogus"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("bogus token: %v", err)
	}
	if _, err := svc.Authenticate(ctx, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("empty token: %v", err)
	}

	// Within TouchEvery a request only reads.
	*clock = clock.Add(time.Minute)
	if _, err := svc.Authenticate(ctx, token); err != nil {
		t.Fatal(err)
	}
	if repo.Touches != 0 {
		t.Fatalf("touched after a minute: %d", repo.Touches)
	}

	// After it, the expiry slides forward.
	*clock = clock.Add(domain.TouchEvery)
	got, err := svc.Authenticate(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if repo.Touches != 1 || got.ExpiresAt != clock.Add(domain.SessionTTL) {
		t.Fatalf("touches=%d expires=%v", repo.Touches, got.ExpiresAt)
	}

	// Revoked means gone.
	if err := svc.Revoke(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("revoked token: %v", err)
	}

	// And an idle session expires on its own.
	token, _, _ = svc.Login(ctx, goodPassword, "tablet")
	*clock = clock.Add(domain.SessionTTL + time.Second)
	if _, err := svc.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired token: %v", err)
	}
}

func TestPasswordTurnedOff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, repo, _ := newService(t, false)

	if err := svc.Bootstrap(ctx, goodPassword); err != nil {
		t.Fatal(err)
	}
	if repo.Owner != "" || svc.setupCode != "" {
		t.Fatal("password off, yet an owner or a setup code appeared")
	}
	if needs, _ := svc.NeedsSetup(ctx); needs {
		t.Fatal("password off must never ask for setup")
	}
	if _, _, err := svc.Login(ctx, goodPassword, "x"); !errors.Is(err, ErrPasswordOff) {
		t.Fatalf("login: %v", err)
	}
	if _, _, err := svc.Setup(ctx, "x", goodPassword, "x"); !errors.Is(err, ErrSetupClosed) {
		t.Fatalf("setup: %v", err)
	}
}
