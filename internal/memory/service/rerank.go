package service

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
)

const (
	defaultMMRLambda       = 0.7
	defaultRecencyHalfLife = 30 * 24 * time.Hour
	defaultTopK            = 8
)

// ScoredCandidate is a memory entry with its retrieval scores.
type ScoredCandidate struct {
	Entry       domain.MemoryEntry
	VectorScore float64
	TextScore   float64
	RRFScore    float64
	FinalScore  float64
}

// Weights control what "best" means. They must stay roughly commensurate: RRF
// scores are tiny (two lists cap at 2/61 ≈ 0.033) while a raw recency term is
// order 0.1, so relevance is normalised to [0,1] before any of this is applied.
// Without that normalisation recency outweighs relevance by roughly 5:1 and MMR
// — which compares relevance against a Jaccard similarity of order 0.3 — ends up
// selecting almost purely for diversity.
type Weights struct {
	Relevance       float64
	Recency         float64
	Kind            float64
	RecencyHalfLife time.Duration
}

func DefaultWeights() Weights {
	return Weights{
		Relevance:       0.75,
		Recency:         0.15,
		Kind:            0.10,
		RecencyHalfLife: defaultRecencyHalfLife,
	}
}

// Bias nudges results towards the kinds a particular question cares about. It is
// a boost and never a filter: on a single-user corpus of a few thousand rows,
// filtering by kind throws away good hits for no gain.
type Bias struct {
	Kinds     []string
	KindBoost float64
}

type RerankOptions struct {
	TopK    int
	Weights Weights
	Bias    Bias
	Now     time.Time
}

func (o RerankOptions) withDefaults() RerankOptions {
	if o.TopK <= 0 {
		o.TopK = defaultTopK
	}
	if o.Weights == (Weights{}) {
		o.Weights = DefaultWeights()
	}
	if o.Weights.RecencyHalfLife <= 0 {
		o.Weights.RecencyHalfLife = defaultRecencyHalfLife
	}
	if o.Now.IsZero() {
		o.Now = time.Now().UTC()
	}
	return o
}

// RerankCandidates scores candidates on relevance, recency and kind, then picks
// topK with MMR so the result is not five chunks of the same document.
func RerankCandidates(candidates []ScoredCandidate, opts RerankOptions) []domain.MemoryEntry {
	if len(candidates) == 0 {
		return nil
	}
	opts = opts.withDefaults()

	maxRRF := maxRRFScore(candidates)

	scored := make([]ScoredCandidate, len(candidates))
	for i, c := range candidates {
		relevance := normalize(c.RRFScore, maxRRF)
		recency := recencyScore(c.Entry.CreatedAt, opts.Now, opts.Weights.RecencyHalfLife)
		kind := kindScore(c.Entry.Kind, opts.Bias)

		c.FinalScore = opts.Weights.Relevance*relevance +
			opts.Weights.Recency*recency +
			opts.Weights.Kind*kind
		scored[i] = c
	}

	// Order by FinalScore before MMR. mmrSelect returns early when there are no
	// more candidates than slots, and without this sort that early return would
	// hand back the caller's RRF ordering with the recency and kind terms
	// computed but silently discarded — which is most queries.
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].FinalScore != scored[j].FinalScore {
			return scored[i].FinalScore > scored[j].FinalScore
		}
		return scored[i].Entry.ID.String() < scored[j].Entry.ID.String()
	})

	selected := mmrSelect(scored, opts.TopK)
	result := make([]domain.MemoryEntry, len(selected))
	for i, c := range selected {
		entry := c.Entry
		entry.Score = c.FinalScore
		result[i] = entry
	}
	return result
}

func maxRRFScore(candidates []ScoredCandidate) float64 {
	max := 0.0
	for _, c := range candidates {
		if c.RRFScore > max {
			max = c.RRFScore
		}
	}
	return max
}

// normalize scales a raw RRF score onto [0,1] against the best candidate in this
// result set.
//
// Deliberately not min-max: RRF is a ratio scale anchored at zero (a document in
// neither result list scores 0), so dividing by the max preserves the actual
// proportions. Min-max would stretch whatever gap happens to exist across the
// full range, which on a two-candidate set turns a 0.001 difference into the
// difference between 0 and 1 and drowns out every other term.
func normalize(value, max float64) float64 {
	if max <= 0 {
		return 0
	}
	return value / max
}

// recencyScore decays from 1 to 0 with a true half-life: an entry exactly one
// half-life old scores 0.5.
func recencyScore(createdAt, now time.Time, halfLife time.Duration) float64 {
	if createdAt.IsZero() {
		return 0
	}
	age := now.Sub(createdAt)
	if age < 0 {
		age = 0
	}
	return math.Exp(-float64(age) * math.Ln2 / float64(halfLife))
}

func kindScore(kind domain.MemoryKind, bias Bias) float64 {
	if bias.KindBoost <= 0 || len(bias.Kinds) == 0 {
		return 0
	}
	for _, want := range bias.Kinds {
		if string(kind) == want {
			return bias.KindBoost
		}
	}
	return 0
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
			maxSim := 0.0
			for _, picked := range selected {
				if sim := textSimilarity(cand.Entry, picked.Entry); sim > maxSim {
					maxSim = sim
				}
			}
			mmr := defaultMMRLambda*cand.FinalScore - (1-defaultMMRLambda)*maxSim
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
