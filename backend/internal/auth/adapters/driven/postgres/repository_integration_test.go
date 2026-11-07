package postgres_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/an4eetos/decision-room/internal/auth/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/auth/domain"
	"github.com/an4eetos/decision-room/internal/auth/port"
	infrapg "github.com/an4eetos/decision-room/internal/infra/postgres"
)

// startPostgres runs the real embedded migrations, so 014_auth's constraints
// are what is tested.
func startPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if exec.Command("docker", "info").Run() != nil {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "pgvector/pgvector:pg16",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "decision_room",
				"POSTGRES_USER":     "room",
				"POSTGRES_PASSWORD": "room",
			},
			WaitingFor: wait.ForListeningPort("5432/tcp"),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mapped, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := infrapg.NewPool(ctx, "postgres://room:room@"+host+":"+mapped.Port()+"/decision_room?sslmode=disable")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := infrapg.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func TestAuthRepositoryRoundTrip(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	repo := postgres.NewRepository(pool)

	if _, err := repo.OwnerHash(ctx); !errors.Is(err, port.ErrNoOwner) {
		t.Fatalf("empty owner: %v", err)
	}
	if err := repo.CreateOwner(ctx, "not-a-hash"); err == nil {
		t.Fatal("check constraint let a non-argon2id hash in")
	}

	hash, _ := domain.HashPassword("a long enough password")
	if err := repo.CreateOwner(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateOwner(ctx, hash); !errors.Is(err, port.ErrOwnerExists) {
		t.Fatalf("second owner: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	_, tokenHash, _ := domain.NewToken()
	sess, err := repo.CreateSession(ctx, domain.Session{
		TokenHash: tokenHash, Client: "laptop", CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.SessionByTokenHash(ctx, tokenHash, now)
	if err != nil || got.ID != sess.ID || got.Client != "laptop" {
		t.Fatalf("lookup: %+v %v", got, err)
	}
	if _, err := repo.SessionByTokenHash(ctx, tokenHash, now.Add(2*time.Hour)); !errors.Is(err, port.ErrSessionNotFound) {
		t.Fatalf("expired lookup: %v", err)
	}

	if err := repo.TouchSession(ctx, sess.ID, now.Add(time.Minute), now.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SessionByTokenHash(ctx, tokenHash, now.Add(2*time.Hour)); err != nil {
		t.Fatalf("touch did not slide expiry: %v", err)
	}

	list, err := repo.ListSessions(ctx, now)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %d %v", len(list), err)
	}

	// A new password signs every device out.
	newHash, _ := domain.HashPassword("another long password")
	if err := repo.ReplaceOwner(ctx, newHash); err != nil {
		t.Fatal(err)
	}
	if stored, _ := repo.OwnerHash(ctx); stored != newHash {
		t.Fatal("owner not replaced")
	}
	if list, _ := repo.ListSessions(ctx, now); len(list) != 0 {
		t.Fatalf("%d sessions survived a password change", len(list))
	}
	if err := repo.DeleteSession(ctx, sess.ID); !errors.Is(err, port.ErrSessionNotFound) {
		t.Fatalf("delete missing: %v", err)
	}

	_, expiredHash, _ := domain.NewToken()
	if _, err := repo.CreateSession(ctx, domain.Session{
		TokenHash: expiredHash, CreatedAt: now.Add(-2 * time.Hour), ExpiresAt: now.Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.DeleteExpiredSessions(ctx, now); err != nil || n != 1 {
		t.Fatalf("expired sweep: n=%d err=%v", n, err)
	}
}
