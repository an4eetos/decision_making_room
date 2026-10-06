package usecase

import (
	"context"
	"errors"
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	gendomain "github.com/an4eetos/decision-room/internal/generals/domain"
	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
	modedomain "github.com/an4eetos/decision-room/internal/modes/domain"
)

// bagEmbedder hashes a text's terms into buckets, so texts sharing words point
// the same way. Enough to make cosine ranking meaningful without a model.
type bagEmbedder struct {
	calls atomic.Int32
	fail  bool
}

func (e *bagEmbedder) ModelID() string { return "bag-64" }

func (e *bagEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	e.calls.Add(1)
	if e.fail {
		return nil, errors.New("embedder down")
	}
	vec := make([]float32, 64)
	for term := range termSet(text) {
		h := fnv.New32a()
		h.Write([]byte(term))
		vec[h.Sum32()%64]++
	}
	return vec, nil
}

type memStore struct {
	mu   sync.Mutex
	rows map[string][]float32
}

func (s *memStore) Load(context.Context, string) (map[string][]float32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]float32, len(s.rows))
	for k, v := range s.rows {
		out[k] = v
	}
	return out, nil
}

func (s *memStore) Save(_ context.Context, hash, _ string, vec []float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rows == nil {
		s.rows = map[string][]float32{}
	}
	s.rows[hash] = vec
	return nil
}

func (s *memStore) Prune(context.Context, string, []string) error { return nil }

type fakeRegistry struct{ generals []gendomain.Lens }

func (r fakeRegistry) Get(id string) (gendomain.Lens, bool) {
	for _, g := range r.generals {
		if g.ID == id {
			return g, true
		}
	}
	return gendomain.Lens{}, false
}
func (r fakeRegistry) Generals() []gendomain.Lens         { return r.generals }
func (r fakeRegistry) Styles() []gendomain.Lens           { return nil }
func (r fakeRegistry) Resolve([]string) []gendomain.Lens  { return nil }
func (r fakeRegistry) Traps() []gendomain.Trap            { return nil }
func (r fakeRegistry) Trap(string) (gendomain.Trap, bool) { return gendomain.Trap{}, false }

var testLens = gendomain.Lens{
	ID:   "zhukov",
	Name: "Zhukov",
	Doctrine: `## Doctrine

Mass and return. Keep the daily slot and let the weight accumulate over months.

## Encircled

When the pocket closes around you, stop spreading effort. Pick the single thinnest point of the ring and break out there before supplies run dry.

## Where it broke

At the heights he attacked straight into prepared defences and paid heavily.`,
}

func doctrinePlan(question string, passages int) ConsultPlan {
	return ConsultPlan{
		Question: question,
		Tier:     domain.TierPolicy{DoctrinePassages: passages},
		Generals: []gendomain.Lens{testLens},
	}
}

func builtIndex(t *testing.T, embedder *bagEmbedder, store port.DoctrineVectorStore) *DoctrineIndex {
	t.Helper()
	x := NewDoctrineIndex(fakeRegistry{[]gendomain.Lens{testLens}}, embedder, store)
	if err := x.Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	return x
}

func TestDoctrineSelectsThePassageTheQuestionIsAbout(t *testing.T) {
	t.Parallel()
	x := builtIndex(t, &bagEmbedder{}, &memStore{})

	got := x.Select(context.Background(), doctrinePlan("I'm encircled, the pocket is closing and supplies are running dry", 1))
	if len(got["zhukov"]) != 1 || got["zhukov"][0].Section != "Encircled" {
		t.Fatalf("expected the Encircled passage, got %+v", got["zhukov"])
	}
}

func TestDoctrineBudgetFollowsTheTier(t *testing.T) {
	t.Parallel()
	x := builtIndex(t, &bagEmbedder{}, &memStore{})

	if got := x.Select(context.Background(), doctrinePlan("daily slot", 0)); got != nil {
		t.Fatalf("a tier with no doctrine budget should get none, got %+v", got)
	}
	got := x.Select(context.Background(), doctrinePlan("encircled pocket daily slot months", 2))
	if len(got["zhukov"]) != 2 {
		t.Fatalf("expected two passages, got %d", len(got["zhukov"]))
	}
	if got["zhukov"][0].Section == got["zhukov"][1].Section {
		t.Fatal("two passages should come from two sections when two are available")
	}
}

// Restarts must not re-embed the roster: on a free quota that is a real share
// of the day's budget spent on text that has not changed.
func TestDoctrineVectorsAreCachedAcrossBuilds(t *testing.T) {
	t.Parallel()
	store := &memStore{}

	first := &bagEmbedder{}
	builtIndex(t, first, store)
	if first.calls.Load() == 0 {
		t.Fatal("first build should embed")
	}

	second := &bagEmbedder{}
	builtIndex(t, second, store)
	if n := second.calls.Load(); n != 0 {
		t.Fatalf("second build embedded %d passages; everything was cached", n)
	}
}

// A failing embedder degrades which passage is picked, never whether the answer
// gets doctrine at all.
func TestDoctrineFallsBackToWordOverlap(t *testing.T) {
	t.Parallel()
	x := builtIndex(t, &bagEmbedder{fail: true}, nil)

	got := x.Select(context.Background(), doctrinePlan("the pocket is closing around me", 1))
	if len(got["zhukov"]) != 1 || got["zhukov"][0].Section != "Encircled" {
		t.Fatalf("expected the overlap fallback to find Encircled, got %+v", got["zhukov"])
	}

	// Zero shared words is an unambiguous "nothing here".
	if got := x.Select(context.Background(), doctrinePlan("xylophone", 1)); len(got["zhukov"]) != 0 {
		t.Fatalf("no overlap should mean no passage, got %+v", got["zhukov"])
	}
}

func TestModePreferenceBreaksTies(t *testing.T) {
	t.Parallel()
	x := builtIndex(t, &bagEmbedder{fail: true}, nil)

	plan := doctrinePlan("xylophone", 1)
	plan.Mode = modedomain.Mode{Generals: modedomain.Generals{Doctrine: []string{"where it broke"}}}
	got := x.Select(context.Background(), plan)
	if len(got["zhukov"]) != 1 || got["zhukov"][0].Section != "Where it broke" {
		t.Fatalf("the mode's section should win when the question matches nothing, got %+v", got["zhukov"])
	}
}

// The whole point of the memo: retrieval embeds the question, and doctrine
// selection embedding it again would be a second network call per answer.
func TestMemoEmbedderServesRepeatText(t *testing.T) {
	t.Parallel()
	inner := &bagEmbedder{}
	memo := NewMemoEmbedder(inner, 2)

	for range 3 {
		if _, err := memo.Embed(context.Background(), "same question"); err != nil {
			t.Fatal(err)
		}
	}
	if n := inner.calls.Load(); n != 1 {
		t.Fatalf("embedded %d times, want 1", n)
	}

	memo.Embed(context.Background(), "b")
	memo.Embed(context.Background(), "c") // evicts "same question"
	memo.Embed(context.Background(), "same question")
	if n := inner.calls.Load(); n != 4 {
		t.Fatalf("expected eviction past the size bound, got %d calls", n)
	}
}

func TestNilDoctrineIndexSelectsNothing(t *testing.T) {
	t.Parallel()
	var x *DoctrineIndex
	if got := x.Select(context.Background(), doctrinePlan("anything", 1)); got != nil {
		t.Fatalf("got %+v", got)
	}
}

func TestDoctrineRefsNameLensAndSection(t *testing.T) {
	t.Parallel()
	plan := doctrinePlan("q", 1)
	plan.Doctrine = map[string][]gendomain.Passage{"zhukov": {{Section: "Encircled"}}}
	if got := strings.Join(plan.DoctrineRefs(), ","); got != "zhukov: Encircled" {
		t.Fatalf("got %q", got)
	}
}
