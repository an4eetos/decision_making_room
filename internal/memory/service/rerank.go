package service

import (
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
)

const (
	defaultMMRLambda   = 0.7
	recencyHalfLifeDays = 14.0
)

// ScoredCandidate is a memory entry with retrieval scores for reranking.
type ScoredCandidate struct {
	Entry       domain.MemoryEntry
	VectorScore float64
	TextScore   float64
	RRFScore    float64
	FinalScore  float64
}

// RerankCandidates applies recency boost and MMR diversity, returning topK entries.
func RerankCandidates(candidates []ScoredCandidate, topK int) []domain.MemoryEntry {
	if len(candidates) == 0 {
		return nil
	}
	if topK <= 0 {
		topK = 8
	}

	now := time.Now().UTC()
	scored := make([]ScoredCandidate, len(candidates))
	for i, c := range candidates {
		c.FinalScore = c.RRFScore + recencyBoost(c.Entry.CreatedAt, now)
		scored[i] = c
	}

	selected := mmrSelect(scored, topK)
	result := make([]domain.MemoryEntry, len(selected))
	for i, c := range selected {
		entry := c.Entry
		entry.Score = c.FinalScore
		result[i] = entry
	}
	return result
}

func recencyBoost(createdAt, now time.Time) float64 {
	if createdAt.IsZero() {
		return 0
	}
	ageDays := now.Sub(createdAt).Hours() / 24
	if ageDays < 0 {
		ageDays = 0
	}
	return 0.15 * math.Exp(-ageDays/recencyHalfLifeDays)
}

func mmrSelect(candidates []ScoredCandidate, topK int) []ScoredCandidate {
	if len(candidates) <= topK {
		return candidates
	}

	remaining := append([]ScoredCandidate(nil), candidates...)
	var selected []ScoredCandidate

	for len(selected) < topK && len(remaining) > 0 {
		bestIdx := 0
		bestScore := -math.MaxFloat64

		for i, cand := range remaining {
			relevance := cand.FinalScore
			maxSim := 0.0
			for _, picked := range selected {
				sim := textSimilarity(cand.Entry, picked.Entry)
				if sim > maxSim {
					maxSim = sim
				}
			}
			mmr := defaultMMRLambda*relevance - (1-defaultMMRLambda)*maxSim
			if mmr > bestScore {
				bestScore = mmr
				bestIdx = i
			}
		}

		selected = append(selected, remaining[bestIdx])
		remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)
	}

	return selected
}

func textSimilarity(a, b domain.MemoryEntry) float64 {
	setA := tokenSet(a.Title + " " + a.Body)
	setB := tokenSet(b.Title + " " + b.Body)
	if len(setA) == 0 || len(setB) == 0 {
		return 0
	}

	intersection := 0
	for token := range setA {
		if setB[token] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func tokenSet(text string) map[string]bool {
	words := strings.Fields(strings.ToLower(text))
	set := make(map[string]bool, len(words))
	for _, w := range words {
		if len(w) > 2 {
			set[w] = true
		}
	}
	return set
}

func ToRankedEntries(ids []uuid.UUID) []RankedEntry {
	ranked := make([]RankedEntry, len(ids))
	for i, id := range ids {
		ranked[i] = RankedEntry{ID: id, Rank: i + 1}
	}
	return ranked
}
