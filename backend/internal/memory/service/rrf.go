package service

import (
	"sort"

	"github.com/google/uuid"
)

const defaultRRFK = 60

// RankedEntry holds a memory ID with its rank in a result list (1-based).
type RankedEntry struct {
	ID   uuid.UUID
	Rank int
}

// ReciprocalRankFusion merges ranked lists using RRF. Higher score is better.
func ReciprocalRankFusion(lists ...[]RankedEntry) map[uuid.UUID]float64 {
	scores := make(map[uuid.UUID]float64)
	for _, list := range lists {
		for _, item := range list {
			if item.Rank <= 0 {
				continue
			}
			scores[item.ID] += 1.0 / (float64(defaultRRFK) + float64(item.Rank))
		}
	}
	return scores
}

// SortByScore returns IDs sorted by score descending.
func SortByScore(scores map[uuid.UUID]float64) []uuid.UUID {
	type pair struct {
		id    uuid.UUID
		score float64
	}
	pairs := make([]pair, 0, len(scores))
	for id, score := range scores {
		pairs = append(pairs, pair{id: id, score: score})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].score == pairs[j].score {
			return pairs[i].id.String() < pairs[j].id.String()
		}
		return pairs[i].score > pairs[j].score
	})

	ids := make([]uuid.UUID, len(pairs))
	for i, p := range pairs {
		ids[i] = p.id
	}
	return ids
}
