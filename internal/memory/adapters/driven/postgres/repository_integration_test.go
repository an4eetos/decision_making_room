package postgres_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/an4eetos/decision-room/internal/memory/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

func TestRepositorySearchFullText(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	repo := postgres.NewRepository(pool)

	if err := insertMemory(ctx, repo, "Bench workout", "bench press 60kg for three sets", nil); err != nil {
		t.Fatalf("insert bench: %v", err)
	}
	if err := insertMemory(ctx, repo, "Reading list", "atomic habits and deep work", nil); err != nil {
		t.Fatalf("insert books: %v", err)
	}

	results, err := repo.SearchFullText(ctx, "60kg bench", 5, port.SearchFilter{})
	if err != nil {
		t.Fatalf("search full text: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected FTS results")
	}
	if !strings.Contains(results[0].Body, "60kg") {
		t.Fatalf("expected bench entry first, got %+v", results[0])
	}
}

func TestRepositoryHybridFindsExactAndSemanticCandidates(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	repo := postgres.NewRepository(pool)

	uniqueToken := "xyzzy-retrieval-token"
	if err := insertMemory(ctx, repo, "Workout", "standard gym session without special tokens", unitVector(1, 0)); err != nil {
		t.Fatalf("insert generic: %v", err)
	}
	if err := insertMemory(ctx, repo, "Bench day", "bench press "+uniqueToken+" at morning", unitVector(0.9, 0.1)); err != nil {
		t.Fatalf("insert bench: %v", err)
	}

	textResults, err := repo.SearchFullText(ctx, uniqueToken, 5, port.SearchFilter{})
	if err != nil {
		t.Fatalf("fts: %v", err)
	}
	if len(textResults) != 1 || !strings.Contains(textResults[0].Body, uniqueToken) {
		t.Fatalf("expected unique token match, got %+v", textResults)
	}

	vectorResults, err := repo.SearchSimilar(ctx, unitVector(0.89, 0.11), 5, port.SearchFilter{})
	if err != nil {
		t.Fatalf("vector: %v", err)
	}
	if len(vectorResults) == 0 || !strings.Contains(vectorResults[0].Body, uniqueToken) {
		t.Fatalf("expected vector match on bench entry, got %+v", vectorResults)
	}
}

func TestRepositorySaveAllowsNilTags(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	repo := postgres.NewRepository(pool)

	entry := domain.MemoryEntry{
		ID:        uuid.New(),
		Kind:      domain.KindNote,
		Title:     "Shaposhnikov",
		Body:      "General notes",
		CreatedAt: time.Now().UTC(),
	}

	if err := repo.Save(ctx, entry); err != nil {
		t.Fatalf("save with nil tags: %v", err)
	}
}

func startPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if !dockerAvailable() {
		t.Skip("docker not available")
	}

	ctx := context.Background()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "pgvector/pgvector:pg16",
			ExposedPorts: []string{"5432/tcp"},
			Env: map[string]string{
				"POSTGRES_DB":       "decision_room",
				"POSTGRES_USER":   "room",
				"POSTGRES_PASSWORD": "room",
			},
			WaitingFor: wait.ForListeningPort("5432/tcp"),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(ctx)
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "5432")
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}

	dsn := "postgres://room:room@" + host + ":" + port.Port() + "/decision_room?sslmode=disable"
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	t.Cleanup(func() { pool.Close() })

	runMigrations(t, dsn)
	return pool
}

func runMigrations(t *testing.T, dsn string) {
	t.Helper()
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}

	files, err := filepath.Glob(filepath.Join(root, "migrations", "*.sql"))
	if err != nil {
		t.Fatalf("glob migrations: %v", err)
	}
	sort.Strings(files)

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("migration pool: %v", err)
	}
	defer pool.Close()

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read migration %s: %v", file, err)
		}
		upSQL := extractGooseUp(string(content))
		if _, err := pool.Exec(ctx, upSQL); err != nil {
			t.Fatalf("apply migration %s: %v", filepath.Base(file), err)
		}
	}
}

func extractGooseUp(content string) string {
	const upMarker = "-- +goose Up"
	const downMarker = "-- +goose Down"
	start := strings.Index(content, upMarker)
	if start < 0 {
		return content
	}
	start += len(upMarker)
	end := strings.Index(content[start:], downMarker)
	if end < 0 {
		return strings.TrimSpace(content[start:])
	}
	return strings.TrimSpace(content[start : start+end])
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

func insertMemory(ctx context.Context, repo *postgres.Repository, title, body string, embedding []float32) error {
	return repo.Save(ctx, domain.MemoryEntry{
		ID:        uuid.New(),
		Kind:      domain.KindNote,
		Title:     title,
		Body:      body,
		Tags:      []string{"test"},
		Embedding: embedding,
		CreatedAt: time.Now().UTC(),
	})
}

func unitVector(a, b float64) []float32 {
	vec := make([]float32, 768)
	vec[0] = float32(a)
	vec[1] = float32(b)
	return vec
}

func dockerAvailable() bool {
	return exec.Command("docker", "info").Run() == nil
}
