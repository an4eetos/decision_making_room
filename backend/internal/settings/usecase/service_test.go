package usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/an4eetos/decision-room/internal/checkin/service"
	"github.com/an4eetos/decision-room/internal/infra/config"
	"github.com/an4eetos/decision-room/internal/settings/domain"
	"github.com/an4eetos/decision-room/internal/settings/port"
)

const settingsKey = "0123456789abcdef0123456789abcdef-test"

type fakeRepo struct {
	mu   sync.Mutex
	rows map[string]port.Stored
}

func (f *fakeRepo) All(context.Context) ([]port.Stored, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []port.Stored
	for _, r := range f.rows {
		out = append(out, r)
	}
	return out, nil
}

func (f *fakeRepo) Save(_ context.Context, upserts []port.Stored, deletes []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range upserts {
		f.rows[u.Key] = u
	}
	for _, k := range deletes {
		delete(f.rows, k)
	}
	return nil
}

var slotsValidator = map[string]Validator{
	"CHECKIN_SLOTS": func(v string) error { _, err := service.ParseSlots(v); return err },
}

// newService builds over the real environment, so these tests use
// t.Setenv and do not run in parallel.
func newService(t *testing.T, withKey bool, rows map[string]port.Stored) (*Service, *fakeRepo, *config.Resolver, *[]config.Config) {
	t.Helper()
	for _, k := range []string{"LLM_PROVIDER", "EMBED_PROVIDER", "GEMINI_API_KEY", "ANTHROPIC_API_KEY",
		"SORTIE_MINUTES", "CHECKIN_SLOTS", "CHECKIN_TZ", "DEFAULT_TIER"} {
		t.Setenv(k, "")
	}
	boot, err := config.Build(func(k string) (string, bool) { return "", false })
	if err != nil {
		t.Fatal(err)
	}
	resolver := config.NewResolver(boot, domain.Exposed)

	var cipher *domain.Cipher
	if withKey {
		cipher, _ = domain.NewCipher(settingsKey)
	}
	if rows == nil {
		rows = map[string]port.Stored{}
	}
	repo := &fakeRepo{rows: rows}
	probed := &[]config.Config{}
	probe := func(_ context.Context, cfg config.Config) TestResult {
		*probed = append(*probed, cfg)
		return TestResult{Chat: Check{OK: true}}
	}
	svc := NewService(repo, resolver, cipher, slotsValidator, probe)
	if err := svc.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return svc, repo, resolver, probed
}

func ptr(s string) *string { return &s }

func item(v View, key string) Item {
	for _, it := range v.Items {
		if it.Key == key {
			return it
		}
	}
	return Item{}
}

func TestUpdateSealsSecretsAndAppliesAtOnce(t *testing.T) {
	svc, repo, resolver, _ := newService(t, true, nil)
	ctx := context.Background()

	if p := svc.View().ChatProblem; !strings.Contains(p, "Gemini") {
		t.Fatalf("expected a missing-key problem, got %q", p)
	}

	view, err := svc.Update(ctx, map[string]*string{
		"GEMINI_API_KEY": ptr("AIzaSyD-secret-0000-1234"),
		"DEFAULT_TIER":   ptr("deep"),
	})
	if err != nil {
		t.Fatal(err)
	}

	row := repo.rows["GEMINI_API_KEY"]
	if !row.Secret || !strings.HasPrefix(row.Value, "v1:") || strings.Contains(row.Value, "secret") {
		t.Fatalf("stored row %+v", row)
	}
	if got := resolver.Current().GeminiAPIKey; got != "AIzaSyD-secret-0000-1234" {
		t.Fatalf("not applied: %q", got)
	}
	if it := item(view, "GEMINI_API_KEY"); it.Value != "••••1234" || it.Source != "stored" {
		t.Fatalf("view %+v", it)
	}
	if view.ChatProblem != "" {
		t.Fatalf("problem left after the key was set: %q", view.ChatProblem)
	}

	// Each value passes its own check, but the whole does not build
	// (sorties under five minutes): nothing of it is saved.
	if _, err := svc.Update(ctx, map[string]*string{"DEFAULT_TIER": ptr("quick"), "SORTIE_MINUTES": ptr("2")}); !errors.Is(err, domain.ErrInvalidValue) {
		t.Fatalf("half-valid update: %v", err)
	}
	if resolver.Current().DefaultTier != "deep" || repo.rows["DEFAULT_TIER"].Value != "deep" {
		t.Fatal("half-valid update left a trace")
	}

	// null goes back to the default.
	if _, err := svc.Update(ctx, map[string]*string{"DEFAULT_TIER": nil}); err != nil {
		t.Fatal(err)
	}
	if _, ok := repo.rows["DEFAULT_TIER"]; ok || resolver.Current().DefaultTier != "standard" {
		t.Fatal("reset did not remove the row")
	}
}

func TestUpdateRefusals(t *testing.T) {
	svc, repo, resolver, _ := newService(t, false, nil)
	ctx := context.Background()
	t.Setenv("SORTIE_MINUTES", "40")

	cases := []struct {
		name    string
		changes map[string]*string
		want    error
	}{
		{"unknown key", map[string]*string{"DATABASE_URL": ptr("x")}, ErrUnknownKey},
		{"locked by env", map[string]*string{"SORTIE_MINUTES": ptr("60")}, ErrLocked},
		{"secret without SETTINGS_KEY", map[string]*string{"GEMINI_API_KEY": ptr("AIza123456789")}, domain.ErrNoSettingsKey},
		{"bad choice", map[string]*string{"LLM_PROVIDER": ptr("openai")}, domain.ErrInvalidValue},
		{"bad slots", map[string]*string{"CHECKIN_SLOTS": ptr("morning 8am")}, domain.ErrInvalidValue},
		{"bad timezone", map[string]*string{"CHECKIN_TZ": ptr("Nowhere/City")}, domain.ErrInvalidValue},
	}
	for _, tc := range cases {
		if _, err := svc.Update(ctx, tc.changes); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if len(repo.rows) != 0 || resolver.Current().DefaultTier != "standard" {
		t.Fatalf("a refused update left traces: %v", repo.rows)
	}
}

func TestLoadSkipsWhatItCannotUse(t *testing.T) {
	other, _ := domain.NewCipher(settingsKey + "-old")
	sealedElsewhere, _ := other.Seal("GEMINI_API_KEY", "AIza-old-key-9999")
	rows := map[string]port.Stored{
		"GEMINI_API_KEY": {Key: "GEMINI_API_KEY", Value: sealedElsewhere, Secret: true},
		"SORTIE_MINUTES": {Key: "SORTIE_MINUTES", Value: "1"}, // below the 5-minute floor
		"DEFAULT_TIER":   {Key: "DEFAULT_TIER", Value: "deep"},
		"DATABASE_URL":   {Key: "DATABASE_URL", Value: "postgres://evil"},
	}
	svc, _, resolver, _ := newService(t, true, rows)

	cur := resolver.Current()
	if cur.DefaultTier != "deep" {
		t.Fatal("a good row was dropped with the bad ones")
	}
	if cur.GeminiAPIKey != "" || cur.SortieMinutes != 25 || cur.DatabaseURL == "postgres://evil" {
		t.Fatalf("unusable rows applied: %+v", cur)
	}
	view := svc.View()
	if p := item(view, "GEMINI_API_KEY").Problem; !strings.Contains(p, "different SETTINGS_KEY") {
		t.Fatalf("GEMINI_API_KEY problem %q", p)
	}
	if item(view, "SORTIE_MINUTES").Problem == "" {
		t.Fatal("invalid stored SORTIE_MINUTES not reported")
	}

	// Overwriting the broken secret clears its problem.
	if _, err := svc.Update(context.Background(), map[string]*string{"GEMINI_API_KEY": ptr("AIza-new-key-0000")}); err != nil {
		t.Fatal(err)
	}
	if p := item(svc.View(), "GEMINI_API_KEY").Problem; p != "" {
		t.Fatalf("problem kept: %q", p)
	}
}

func TestTestDoesNotSave(t *testing.T) {
	svc, repo, resolver, probed := newService(t, false, nil)

	// A secret can be tried even before it could be saved.
	res, err := svc.Test(context.Background(), map[string]*string{
		"LLM_PROVIDER":      ptr("anthropic"),
		"ANTHROPIC_API_KEY": ptr("sk-ant-try-me"),
	})
	if err != nil || !res.Chat.OK {
		t.Fatalf("Test: %+v %v", res, err)
	}
	if len(*probed) != 1 || (*probed)[0].AnthropicAPIKey != "sk-ant-try-me" || (*probed)[0].LLMProvider != "anthropic" {
		t.Fatalf("probe saw %+v", *probed)
	}
	if len(repo.rows) != 0 || resolver.Current().LLMProvider != "gemini" {
		t.Fatal("Test changed something")
	}
}

func TestTimezoneWaitsForRestart(t *testing.T) {
	svc, _, resolver, _ := newService(t, true, nil)
	resolver.Settle()

	view, err := svc.Update(context.Background(), map[string]*string{"CHECKIN_TZ": ptr("Asia/Almaty")})
	if err != nil {
		t.Fatal(err)
	}
	if it := item(view, "CHECKIN_TZ"); !it.Restart || !it.PendingRestart {
		t.Fatalf("CHECKIN_TZ %+v", it)
	}
	if it := item(view, "DEFAULT_TIER"); it.PendingRestart {
		t.Fatal("a live setting reported as pending restart")
	}
}
