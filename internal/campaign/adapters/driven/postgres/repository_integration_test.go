package postgres_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/an4eetos/decision-room/internal/campaign/adapters/driven/postgres"
	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	compostgres "github.com/an4eetos/decision-room/internal/commitments/adapters/driven/postgres"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
	infrapg "github.com/an4eetos/decision-room/internal/infra/postgres"
)

// startPostgres runs the real embedded migrations, the same path the binary
// takes at startup, so the campaign schema's checks are what is tested. With
// TEST_DATABASE_URL set it uses that database instead of a container.
func startPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		pool, err := infrapg.NewPool(ctx, url)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		t.Cleanup(pool.Close)
		if err := infrapg.Migrate(ctx, pool); err != nil {
			t.Fatalf("migrate: %v", err)
		}
		return pool
	}
	if exec.Command("docker", "info").Run() != nil {
		t.Skip("docker not available")
	}

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

func TestCampaignRepositoryRoundTrip(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	repo := postgres.NewRepository(pool)

	work, err := repo.CreateFront(ctx, "Work")
	if err != nil {
		t.Fatalf("create front: %v", err)
	}
	if _, err := repo.CreateFront(ctx, "work"); !errors.Is(err, port.ErrDuplicate) {
		t.Fatalf("an active front name is unique, case-insensitively: %v", err)
	}
	life, _ := repo.CreateFront(ctx, "Life")
	if life.Position <= work.Position {
		t.Fatal("a new front goes after the existing ones")
	}

	objective, err := repo.CreateItem(ctx, domain.Item{
		Type: domain.TypeObjective, FrontID: &work.ID, Text: "Ship the beta",
		Status: domain.StatusActive, Source: domain.SourceManual, Confidence: 1, Fingerprint: "beta",
	})
	if err != nil {
		t.Fatalf("create objective: %v", err)
	}

	obstacle, err := repo.CreateItem(ctx, domain.Item{
		Type: domain.TypeObstacle, FrontID: &work.ID, ObjectiveID: &objective.ID, Text: "Legal review pending",
		Status: domain.StatusProposed, Kind: domain.KindWaiting, Strength: 2,
		Source: domain.SourceChat, Confidence: 0.8, Fingerprint: "legal",
	})
	if err != nil {
		t.Fatalf("create obstacle: %v", err)
	}
	if obstacle.Kind != domain.KindWaiting || obstacle.Strength != 2 || obstacle.StrengthConfirmed {
		t.Fatalf("obstacle fields did not round-trip: %+v", obstacle)
	}

	// The same blocker said twice is one live row.
	if _, err := repo.CreateItem(ctx, domain.Item{
		Type: domain.TypeObstacle, Text: "legal review pending", Status: domain.StatusProposed,
		Source: domain.SourceChat, Confidence: 0.8, Fingerprint: "legal",
	}); !errors.Is(err, port.ErrDuplicate) {
		t.Fatalf("want a duplicate, got %v", err)
	}

	// The schema refuses what the domain forbids, as a second line.
	if _, err := repo.CreateItem(ctx, domain.Item{
		Type: domain.TypeUnknown, Text: "Withdrawn fog?", Status: domain.StatusWithdrawn,
		Source: domain.SourceManual, Confidence: 1, Fingerprint: "w",
	}); err == nil {
		t.Fatal("only an objective can be withdrawn")
	}
	if _, err := repo.CreateItem(ctx, domain.Item{
		Type: domain.TypeObjective, Text: "Strong objective", Status: domain.StatusActive, Strength: 3,
		Source: domain.SourceManual, Confidence: 1, Fingerprint: "s",
	}); err == nil {
		t.Fatal("only an obstacle has a strength")
	}

	obstacle.Status, obstacle.StrengthConfirmed = domain.StatusResolved, true
	cleared, err := repo.UpdateItem(ctx, obstacle)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if cleared.ResolvedAt == nil || !cleared.StrengthConfirmed {
		t.Fatalf("resolving should stamp resolved_at: %+v", cleared)
	}
	cleared.Status = domain.StatusActive
	reopened, _ := repo.UpdateItem(ctx, cleared)
	if reopened.ResolvedAt != nil {
		t.Fatal("reopening should clear resolved_at")
	}

	active, err := repo.ListItems(ctx, []domain.Status{domain.StatusActive}, 10)
	if err != nil || len(active) != 2 || active[0].Type != domain.TypeObjective {
		t.Fatalf("objectives list before opposition: %+v %v", active, err)
	}

	// An order aimed at the objective is found by its target, and loses the
	// aim — not the order — when the objective is deleted out from under it.
	orders := compostgres.NewRepository(pool)
	order, err := orders.Create(ctx, comdomain.Commitment{
		Text: "Email legal", Status: comdomain.StatusOpen, Source: comdomain.SourceManual,
		Confidence: 1, Fingerprint: "email-legal", TargetID: &objective.ID,
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if order.Kind != comdomain.KindOrder {
		t.Fatalf("an order without a kind is a plain order: %q", order.Kind)
	}
	aimed, err := orders.ListByTargets(ctx, []uuid.UUID{objective.ID})
	if err != nil || len(aimed) != 1 || aimed[0].ID != order.ID {
		t.Fatalf("orders by target: %+v %v", aimed, err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM campaign_items WHERE id = $1`, objective.ID); err != nil {
		t.Fatalf("delete objective: %v", err)
	}
	kept, err := orders.Get(ctx, order.ID)
	if err != nil || kept.TargetID != nil {
		t.Fatalf("the order should survive with its aim cleared: %+v %v", kept, err)
	}
}

func TestSnapshotsRoundTrip(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	repo := postgres.NewRepository(pool)

	if _, err := repo.GetSnapshot(ctx, "2026-01-01"); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("a day with no snapshot is not found: %v", err)
	}
	if err := repo.SaveSnapshot(ctx, domain.Snapshot{Day: "not a day"}); err == nil {
		t.Fatal("a bad day is refused")
	}

	front := uuid.New()
	theatre := domain.Theatre{Fronts: []domain.TheatreFront{{
		ID: front, Name: "Work", Line: 0.5,
		Rows: []domain.TheatreRow{{
			Objective: &domain.TheatrePost{ID: uuid.New(), Text: "Ship", Status: domain.StatusActive},
			Progress:  0.5, Obstacles: []domain.TheatrePost{}, Unknowns: []domain.TheatrePost{},
			Orders: domain.TheatreOrders{Open: 1},
		}},
	}}, Totals: domain.TheatreTotals{Objectives: 1, OrdersOpen: 1}}

	first := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	for _, day := range []string{"2026-01-02", "2026-01-03", "2026-01-01"} {
		if err := repo.SaveSnapshot(ctx, domain.Snapshot{Day: day, TakenAt: first, Theatre: theatre}); err != nil {
			t.Fatalf("save %s: %v", day, err)
		}
	}
	// The same day written again replaces the first write.
	theatre.Fronts[0].Line = 0.75
	later := first.Add(8 * time.Hour)
	if err := repo.SaveSnapshot(ctx, domain.Snapshot{Day: "2026-01-02", TakenAt: later, Theatre: theatre}); err != nil {
		t.Fatalf("resave: %v", err)
	}

	got, err := repo.GetSnapshot(ctx, "2026-01-02")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Day != "2026-01-02" || got.Theatre.Day != "2026-01-02" || !got.TakenAt.Equal(later) {
		t.Fatalf("snapshot header did not round-trip: %+v", got)
	}
	if len(got.Theatre.Fronts) != 1 || got.Theatre.Fronts[0].ID != front || got.Theatre.Fronts[0].Line != 0.75 {
		t.Fatalf("theatre did not round-trip: %+v", got.Theatre)
	}
	if row := got.Theatre.Fronts[0].Rows[0]; row.Objective == nil || row.Objective.Text != "Ship" || row.Orders.Open != 1 {
		t.Fatalf("row did not round-trip: %+v", row)
	}

	all, err := repo.ListSnapshots(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 || all[0].Day != "2026-01-01" || all[2].Day != "2026-01-03" {
		t.Fatalf("snapshots list oldest first: %+v", all)
	}
	// The limit keeps the most recent days, still oldest first.
	recent, _ := repo.ListSnapshots(ctx, 2)
	if len(recent) != 2 || recent[0].Day != "2026-01-02" || recent[1].Day != "2026-01-03" {
		t.Fatalf("limited list: %+v", recent)
	}
}

func TestAppraisalsRoundTrip(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	repo := postgres.NewRepository(pool)

	if _, err := repo.GetAppraisal(ctx, uuid.New()); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("an unknown appraisal is not found: %v", err)
	}
	if _, err := repo.CreateAppraisal(ctx, domain.Appraisal{Body: "  "}); err == nil {
		t.Fatal("an empty appraisal is refused by the schema")
	}

	front, err := repo.CreateFront(ctx, "Work")
	if err != nil {
		t.Fatal(err)
	}
	whole, err := repo.CreateAppraisal(ctx, domain.Appraisal{Tier: "standard", Generals: []string{"kutuzov", "manstein"}, Body: "**Point of main effort**\nShip."})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if whole.FrontID != nil || len(whole.Generals) != 2 || whole.CreatedAt.IsZero() {
		t.Fatalf("appraisal did not round-trip: %+v", whole)
	}
	onFront, err := repo.CreateAppraisal(ctx, domain.Appraisal{FrontID: &front.ID, Tier: "deep", Body: "Hold."})
	if err != nil {
		t.Fatalf("create on a front: %v", err)
	}
	if onFront.FrontID == nil || *onFront.FrontID != front.ID || len(onFront.Generals) != 0 {
		t.Fatalf("front appraisal: %+v", onFront)
	}

	list, err := repo.ListAppraisals(ctx, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 || list[0].ID != onFront.ID {
		t.Fatalf("latest first: %+v", list)
	}
	if one, _ := repo.ListAppraisals(ctx, 1); len(one) != 1 {
		t.Fatalf("limit: %+v", one)
	}

	session := uuid.New()
	if err := repo.AttachAppraisalSession(ctx, whole.ID, session); err == nil {
		t.Fatal("a session that does not exist is refused by the foreign key")
	}
	if err := repo.AttachAppraisalSession(ctx, uuid.New(), session); !errors.Is(err, port.ErrNotFound) {
		t.Fatalf("attaching to an unknown appraisal: %v", err)
	}
	var created uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO chat_sessions (title) VALUES ('Staff appraisal') RETURNING id`).Scan(&created); err != nil {
		t.Fatal(err)
	}
	if err := repo.AttachAppraisalSession(ctx, whole.ID, created); err != nil {
		t.Fatalf("attach: %v", err)
	}
	got, _ := repo.GetAppraisal(ctx, whole.ID)
	if got.SessionID == nil || *got.SessionID != created {
		t.Fatalf("the session should be attached: %+v", got)
	}
}
