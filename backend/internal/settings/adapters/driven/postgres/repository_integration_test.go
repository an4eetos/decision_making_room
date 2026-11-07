package postgres_test

import (
	"context"
	"os/exec"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	infrapg "github.com/an4eetos/decision-room/internal/infra/postgres"
	"github.com/an4eetos/decision-room/internal/settings/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/settings/port"
)

// startPostgres runs the real embedded migrations, so 015_settings's constraints
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

func TestSettingsRepositoryRoundTrip(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	repo := postgres.NewRepository(pool)

	if err := repo.Save(ctx, []port.Stored{
		{Key: "DEFAULT_TIER", Value: "deep"},
		{Key: "GEMINI_API_KEY", Value: "v1:c2VhbGVk", Secret: true},
	}, nil); err != nil {
		t.Fatal(err)
	}

	// An unsealed secret violates the check, and the batch it was in is
	// rolled back whole.
	if err := repo.Save(ctx, []port.Stored{
		{Key: "DEFAULT_TIER", Value: "quick"},
		{Key: "ANTHROPIC_API_KEY", Value: "sk-plaintext", Secret: true},
	}, nil); err == nil {
		t.Fatal("plaintext secret stored")
	}

	rows, err := repo.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]port.Stored{}
	for _, r := range rows {
		got[r.Key] = r
	}
	if len(got) != 2 || got["DEFAULT_TIER"].Value != "deep" || !got["GEMINI_API_KEY"].Secret {
		t.Fatalf("rows %+v", got)
	}

	// Upsert one, delete the other, in one call.
	if err := repo.Save(ctx, []port.Stored{{Key: "DEFAULT_TIER", Value: "standard"}}, []string{"GEMINI_API_KEY"}); err != nil {
		t.Fatal(err)
	}
	rows, _ = repo.All(ctx)
	if len(rows) != 1 || rows[0].Value != "standard" || !rows[0].UpdatedAt.After(got["DEFAULT_TIER"].UpdatedAt) {
		t.Fatalf("after upsert and delete: %+v", rows)
	}

	if err := repo.Save(ctx, []port.Stored{{Key: "lower_case", Value: "x"}}, nil); err == nil {
		t.Fatal("key check let a malformed key in")
	}
}
