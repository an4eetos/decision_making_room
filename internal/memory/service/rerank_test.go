package service

import (
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

	result := RerankCandidates(candidates, 1)
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

	result := RerankCandidates(candidates, 2)
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].ID == result[1].ID {
		t.Fatal("expected diverse results")
	}
}
