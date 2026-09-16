package service

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
)

func TestReciprocalRankFusion(t *testing.T) {
	t.Parallel()

	idA := uuid.New()
	idB := uuid.New()
	idC := uuid.New()

	vectorList := []RankedEntry{
		{ID: idA, Rank: 1},
		{ID: idB, Rank: 2},
	}
	textList := []RankedEntry{
		{ID: idB, Rank: 1},
		{ID: idC, Rank: 2},
	}

	scores := ReciprocalRankFusion(vectorList, textList)
	if scores[idB] <= scores[idA] || scores[idB] <= scores[idC] {
		t.Fatalf("expected idB to win RRF merge, scores=%v", scores)
	}
}

func TestRerankCandidatesPrefersRecent(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	oldID := uuid.New()
	newID := uuid.New()

	candidates := []ScoredCandidate{
		{
			Entry: domain.MemoryEntry{
				ID:        oldID,
				Title:     "Old plan",
				Body:      "focus on backend",
				CreatedAt: now.Add(-30 * 24 * time.Hour),
			},
			RRFScore: 0.5,
		},
		{
			Entry: domain.MemoryEntry{
				ID:        newID,
				Title:     "New plan",
				Body:      "focus on retrieval",
				CreatedAt: now.Add(-24 * time.Hour),
			},
			RRFScore: 0.5,
		},
	}

	result := RerankCandidates(candidates, RerankOptions{TopK: 1, Now: now})
	if len(result) != 1 || result[0].ID != newID {
		t.Fatalf("expected recent entry to win tie-break, got %+v", result)
	}
}

func TestMMRReducesDuplicateChunks(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	id1 := uuid.New()
	id2 := uuid.New()
	id3 := uuid.New()

	candidates := []ScoredCandidate{
		{
			Entry: domain.MemoryEntry{
				ID: id1, Title: "Workout", Body: "bench press 60kg three sets",
				CreatedAt: now,
			},
			RRFScore: 0.9,
		},
		{
			Entry: domain.MemoryEntry{
				ID: id2, Title: "Workout copy", Body: "bench press 60kg three sets morning",
				CreatedAt: now,
			},
			RRFScore: 0.85,
		},
		{
			Entry: domain.MemoryEntry{
				ID: id3, Title: "Books", Body: "atomic habits systems compound",
				CreatedAt: now,
			},
			RRFScore: 0.7,
		},
	}

	result := RerankCandidates(candidates, RerankOptions{TopK: 2, Now: now})
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].ID == result[1].ID {
		t.Fatal("expected diverse results")
	}
}

// The old scoring added a recency term of up to 0.15 to an RRF score that caps
// at 2/61 ≈ 0.033, so anything written today beat the single most relevant thing
// in the corpus. This asserts the inverse: relevance wins unless the gap is
// small.
func TestRelevanceOutweighsRecency(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	relevantID := uuid.New()
	freshID := uuid.New()

	candidates := []ScoredCandidate{
		{
			Entry: domain.MemoryEntry{
				ID:        relevantID,
				Title:     "Chose Postgres over DynamoDB",
				Body:      "transactional guarantees mattered more than scale",
				CreatedAt: now.Add(-21 * 24 * time.Hour),
			},
			RRFScore: 2.0 / 61.0, // top of both result lists
		},
		{
			Entry: domain.MemoryEntry{
				ID:        freshID,
				Title:     "Bought coffee",
				Body:      "the machine at the office is broken again",
				CreatedAt: now,
			},
			RRFScore: 1.0 / 90.0, // scraped in at the bottom of one list
		},
	}

	result := RerankCandidates(candidates, RerankOptions{TopK: 1, Now: now})
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0].ID != relevantID {
		t.Fatalf("a three-week-old exact match lost to a same-day tangent: got %q", result[0].Title)
	}
}

// Recency still has to break ties, or "what did I decide today" stops working.
func TestRecencyBreaksTiesOnEqualRelevance(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	oldID, newID := uuid.New(), uuid.New()

	candidates := []ScoredCandidate{
		{Entry: domain.MemoryEntry{ID: oldID, Title: "A", Body: "same relevance", CreatedAt: now.Add(-90 * 24 * time.Hour)}, RRFScore: 0.03},
		{Entry: domain.MemoryEntry{ID: newID, Title: "B", Body: "same relevance", CreatedAt: now}, RRFScore: 0.03},
	}

	result := RerankCandidates(candidates, RerankOptions{TopK: 1, Now: now})
	if result[0].ID != newID {
		t.Fatal("expected the newer of two equally relevant entries")
	}
}

// One half-life old must score exactly 0.5. The old code divided by the
// half-life directly, which made it a 1/e life — a decay roughly 30% faster than
// the constant's name claimed.
func TestRecencyScoreIsATrueHalfLife(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	halfLife := 30 * 24 * time.Hour

	got := recencyScore(now.Add(-halfLife), now, halfLife)
	if math.Abs(got-0.5) > 1e-9 {
		t.Fatalf("recencyScore at one half-life = %v, want 0.5", got)
	}
	if got := recencyScore(now, now, halfLife); math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("recencyScore at age zero = %v, want 1.0", got)
	}
}

func TestKindBiasBoostsWithoutFiltering(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	decisionID, noteID := uuid.New(), uuid.New()

	candidates := []ScoredCandidate{
		{Entry: domain.MemoryEntry{ID: noteID, Kind: domain.KindNote, Title: "N", Body: "aaa bbb", CreatedAt: now}, RRFScore: 0.030},
		{Entry: domain.MemoryEntry{ID: decisionID, Kind: domain.KindDecision, Title: "D", Body: "ccc ddd", CreatedAt: now}, RRFScore: 0.029},
	}

	opts := RerankOptions{TopK: 2, Now: now, Bias: Bias{Kinds: []string{"decision"}, KindBoost: 1.0}}
	result := RerankCandidates(candidates, opts)

	if result[0].ID != decisionID {
		t.Fatal("expected the biased kind to be promoted")
	}
	if len(result) != 2 {
		t.Fatalf("bias must boost, never filter: got %d results, want 2", len(result))
	}
}
