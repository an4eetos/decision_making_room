package postgres_test

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/an4eetos/decision-room/internal/beast/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/beast/domain"
	"github.com/an4eetos/decision-room/internal/beast/port"
	infrapg "github.com/an4eetos/decision-room/internal/infra/postgres"
)

// startPostgres runs the real embedded migrations, the same path the binary
// takes at startup, so the board's schema checks are what is tested.
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

func newSession(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), `INSERT INTO chat_sessions DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("create session: %v", err)
	}
	return id
}

func TestBeastRepositoryRoundTrip(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	repo := postgres.NewRepository(pool)
	session := newSession(t, pool)
	now := time.Now().UTC().Truncate(time.Microsecond)

	first, err := repo.Create(ctx, domain.Task{
		SessionID: session, Text: "Open the report", Minutes: 10,
		DeadlineAt: now.Add(10 * time.Minute), Status: domain.StatusOpen,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	second, err := repo.Create(ctx, domain.Task{
		SessionID: session, Text: "Email", Minutes: 20,
		DeadlineAt: now.Add(-time.Minute), Status: domain.StatusOpen,
	})
	if err != nil {
		t.Fatalf("create second: %v", err)
	}
	if first.Seq != 1 || second.Seq != 2 {
		t.Fatalf("numbers %d, %d; want 1, 2", first.Seq, second.Seq)
	}

	// Numbering is per conversation.
	other, err := repo.Create(ctx, domain.Task{
		SessionID: newSession(t, pool), Text: "Elsewhere", Minutes: 5,
		DeadlineAt: now, Status: domain.StatusOpen,
	})
	if err != nil || other.Seq != 1 {
		t.Fatalf("another session's first task: seq %d, %v", other.Seq, err)
	}

	// Only the late task re-arms, and only once at a given moment.
	if _, ok, err := repo.Rearm(ctx, first.ID, now, now.Add(5*time.Minute)); ok || err != nil {
		t.Fatalf("re-armed a task that was not late: %v, %v", ok, err)
	}
	rearmed, ok, err := repo.Rearm(ctx, second.ID, now, now.Add(10*time.Minute))
	if !ok || err != nil || rearmed.Expiries != 1 || !rearmed.DeadlineAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("rearm: %+v, %v, %v", rearmed, ok, err)
	}
	if _, ok, _ := repo.Rearm(ctx, second.ID, now, now.Add(10*time.Minute)); ok {
		t.Fatal("re-armed twice")
	}

	first.Status = domain.StatusDone
	done, err := repo.Update(ctx, first)
	if err != nil || done.ResolvedAt == nil {
		t.Fatalf("resolving should stamp resolved_at: %+v, %v", done, err)
	}

	tasks, err := repo.ListBySession(ctx, session)
	if err != nil || len(tasks) != 2 || tasks[0].ID != first.ID {
		t.Fatalf("list: %+v, %v", tasks, err)
	}

	if _, err := repo.Get(ctx, uuid.New()); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("missing task: %v", err)
	}

	// The schema refuses a clock it cannot mean.
	if _, err := repo.Create(ctx, domain.Task{
		SessionID: session, Text: "Forever", Minutes: 9999, DeadlineAt: now, Status: domain.StatusOpen,
	}); err == nil {
		t.Fatal("a 9999-minute clock should be rejected")
	}
}

// Two orders landing at once both get a number, and different ones.
func TestConcurrentCreatesGetDistinctNumbers(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	repo := postgres.NewRepository(pool)
	session := newSession(t, pool)

	var wg sync.WaitGroup
	seqs := make([]int, 3)
	errs := make([]error, 3)
	for i := range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := repo.Create(ctx, domain.Task{
				SessionID: session, Text: "order", Minutes: 5,
				DeadlineAt: time.Now(), Status: domain.StatusOpen,
			})
			seqs[i], errs[i] = task.Seq, err
		}()
	}
	wg.Wait()

	seen := map[int]bool{}
	for i := range 3 {
		if errs[i] != nil {
			t.Fatalf("create %d: %v", i, errs[i])
		}
		if seen[seqs[i]] {
			t.Fatalf("number %d handed out twice", seqs[i])
		}
		seen[seqs[i]] = true
	}
}
