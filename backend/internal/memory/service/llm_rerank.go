package service

import (
	"strconv"
	"strings"
)

// ParseRerankOrder reads a model's ranking of numbered candidates and returns
// the indices it chose, in order.
//
// Like decomposition, this never errors. A reranker that cannot be parsed must
// fall back to the heuristic ordering: it is an optional improvement, and it
// must never be able to make retrieval worse than not running it at all.
func ParseRerankOrder(answer string, candidateCount int) []int {
	seen := make(map[int]struct{}, candidateCount)
	order := make([]int, 0, candidateCount)

	for _, token := range strings.FieldsFunc(answer, func(r rune) bool {
		return r < '0' || r > '9'
	}) {
		n, err := strconv.Atoi(token)
		if err != nil {
			continue
		}
		// The prompt numbers candidates from one.
		idx := n - 1
		if idx < 0 || idx >= candidateCount {
			continue
		}
		if _, dup := seen[idx]; dup {
			continue
		}
		seen[idx] = struct{}{}
		order = append(order, idx)
	}

	return order
}

// ApplyRerankOrder reorders candidates by the given indices, appending anything
// the model left out so nothing is silently dropped. A model that returns five
// of twelve has expressed a preference about five, not a decision to discard
// seven.
func ApplyRerankOrder(candidates []ScoredCandidate, order []int) []ScoredCandidate {
	if len(order) == 0 {
		return candidates
	}

	out := make([]ScoredCandidate, 0, len(candidates))
	used := make(map[int]struct{}, len(order))

	for _, idx := range order {
		if idx < 0 || idx >= len(candidates) {
			continue
		}
		if _, dup := used[idx]; dup {
			continue
		}
		used[idx] = struct{}{}
		out = append(out, candidates[idx])
	}

	for i, c := range candidates {
		if _, taken := used[i]; !taken {
			out = append(out, c)
		}
	}

	return out
}
