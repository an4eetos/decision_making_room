package service

import (
	"math"
	"slices"
	"strings"

	"github.com/an4eetos/decision-room/internal/relocation/domain"
)

// Comfort is not an average. Forty items sorted out of forty-two, where the two
// missing ones are the towel and the pillow, is a bad stay rather than a 95% one.
// So the scoring is weakest-link at both levels: a missing critical item caps its
// dimension, and the worst dimension dominates the overall score.
const (
	// capOneCritical and capManyCritical bound a dimension missing one, or two or
	// more, critical items, however much else in it is covered.
	capOneCritical  = 35
	capManyCritical = 15
	// importantPenalty is taken off on top of the lost coverage. Coverage alone
	// would let a big dimension absorb a missing kettle without noticing.
	importantPenalty = 8
	// floorWithoutCritical is as low as a dimension can go while every critical
	// item in it is covered. Only essentials make a stay miserable: no kettle is
	// rough, no towel is something else.
	floorWithoutCritical = 50
	// floorOnlyNice is the same idea one step down: missing a rain jacket must
	// not make a dimension the weakest link of the whole stay.
	floorOnlyNice = 80
	// minWeight is how much the worst dimension counts in the overall score. A
	// broken dimension cannot be bought back with excellence elsewhere: good
	// coffee does not make up for no sheets.
	minWeight = 0.75
	// radarSize is how many suggestions the radar returns.
	radarSize = 3
)

// ComfortRules indexes the catalogue's comfort metadata by catalogue id.
// Computing comfort from the live catalogue, rather than from a copy stored on
// each line, means retuning a weight in YAML applies to every plan at once.
func ComfortRules(c domain.Catalog) map[string]domain.ComfortRule {
	out := make(map[string]domain.ComfortRule, len(c.Items))
	for _, item := range c.Items {
		if item.Comfort != nil {
			out[item.ID] = *item.Comfort
		}
	}
	return out
}

// ComfortReport is the comfort reading for a plan, as it stands.
type ComfortReport struct {
	// Score is nil when nothing on the list carries a comfort weight. That is
	// "no reading", which is different from both 0 and 100.
	Score *int
	Label string
	// Arrival scores only what you need on the first night. Nil when no arrival
	// items apply, as for a hotel stay.
	Arrival      *int
	ArrivalLabel string
	Dimensions   []DimensionScore
	// Radar is the shortest path up: the items that raise the score most, in
	// the order to buy them, with the score after each one.
	Radar []RadarStep
}

type DimensionScore struct {
	Dimension domain.ComfortDimension
	Score     int
	// Missing lists what is pulling this dimension down, worst first.
	Missing []domain.Item
}

type RadarStep struct {
	Item       domain.Item
	Weight     domain.ComfortWeight
	Dimension  domain.ComfortDimension
	ScoreAfter int
}

// scored is an item that counts toward comfort, with its rule attached.
type scored struct {
	item    domain.Item
	rule    domain.ComfortRule
	covered bool
}

// Comfort scores a plan's items. Have and bought count as covered; needed is
// missing. Skipped is left out entirely, because skipping is a decision that the
// item does not apply here ("the flat has towels"), not a gap. Lines you added
// yourself carry no rule and are not scored.
func Comfort(items []domain.Item, rules map[string]domain.ComfortRule) ComfortReport {
	var all []scored
	for _, item := range items {
		if item.CatalogID == "" || item.Status == domain.ItemSkipped {
			continue
		}
		rule, ok := rules[item.CatalogID]
		if !ok {
			continue
		}
		all = append(all, scored{
			item:    item,
			rule:    rule,
			covered: item.Status == domain.ItemHave || item.Status == domain.ItemBought,
		})
	}

	var report ComfortReport
	dims, overall, ok := score(all)
	if !ok {
		return report
	}
	report.Score = &overall
	report.Label = ComfortLabel(overall)
	report.Dimensions = dims

	var arrival []scored
	for _, s := range all {
		if s.rule.Arrival {
			arrival = append(arrival, s)
		}
	}
	if _, a, ok := score(arrival); ok {
		report.Arrival = &a
		report.ArrivalLabel = ComfortLabel(a)
	}

	report.Radar = radar(all, overall)
	return report
}

// ComfortLabel puts a number into words, so 48 reads as a problem rather than
// as "about half".
func ComfortLabel(score int) string {
	switch {
	case score >= 85:
		return "comfortable"
	case score >= 65:
		return "livable"
	case score >= 40:
		return "rough"
	default:
		return "miserable"
	}
}

func score(all []scored) ([]DimensionScore, int, bool) {
	byDim := make(map[domain.ComfortDimension][]scored)
	for _, s := range all {
		byDim[s.rule.Dimension] = append(byDim[s.rule.Dimension], s)
	}
	if len(byDim) == 0 {
		return nil, 0, false
	}

	dims := make([]DimensionScore, 0, len(byDim))
	worst, sum := 100, 0
	for _, dim := range domain.ComfortDimensions {
		entries, ok := byDim[dim]
		if !ok {
			continue
		}
		d := scoreDimension(dim, entries)
		dims = append(dims, d)
		worst = min(worst, d.Score)
		sum += d.Score
	}

	mean := float64(sum) / float64(len(dims))
	overall := int(math.Round(minWeight*float64(worst) + (1-minWeight)*mean))
	return dims, overall, true
}

func scoreDimension(dim domain.ComfortDimension, entries []scored) DimensionScore {
	var total, covered float64
	var missingCritical, missingImportant int
	var missing []scored

	for _, s := range entries {
		points := s.rule.Weight.Points()
		total += points
		if s.covered {
			covered += points
			continue
		}
		missing = append(missing, s)
		switch s.rule.Weight {
		case domain.WeightCritical:
			missingCritical++
		case domain.WeightImportant:
			missingImportant++
		}
	}

	value := 100.0
	if total > 0 {
		value = covered / total * 100
	}
	value -= float64(importantPenalty * missingImportant)
	switch {
	case missingCritical == 0 && missingImportant == 0 && len(missing) > 0:
		value = math.Max(value, floorOnlyNice)
	case missingCritical == 0 && len(missing) > 0:
		value = math.Max(value, floorWithoutCritical)
	case missingCritical >= 2:
		value = math.Min(value, capManyCritical)
	case missingCritical == 1:
		value = math.Min(value, capOneCritical)
	}

	slices.SortStableFunc(missing, func(a, b scored) int {
		if d := b.rule.Weight.Points() - a.rule.Weight.Points(); d != 0 {
			return int(d)
		}
		return strings.Compare(a.item.Name, b.item.Name)
	})
	out := DimensionScore{Dimension: dim, Score: clamp(value)}
	for _, s := range missing {
		out.Missing = append(out.Missing, s.item)
	}
	return out
}

// radar picks greedily: the single item that raises the overall score most,
// then the next given that one is sorted, and so on. Greedy is what you want for
// a shopping order, because the second-best item on its own may be worthless
// until the first is fixed — a pillow does nothing for a bed with no sheets.
func radar(all []scored, current int) []RadarStep {
	state := slices.Clone(all)
	var steps []RadarStep

	for len(steps) < radarSize {
		best, bestScore := -1, current
		for i, s := range state {
			if s.covered {
				continue
			}
			state[i].covered = true
			_, after, _ := score(state)
			state[i].covered = false

			if after <= current {
				continue
			}
			// Ties go to the heavier item, then to the earlier catalogue line, so
			// the radar is stable between page loads.
			if best < 0 || after > bestScore ||
				(after == bestScore && s.rule.Weight.Points() > state[best].rule.Weight.Points()) {
				best, bestScore = i, after
			}
		}
		if best < 0 {
			break
		}
		state[best].covered = true
		current = bestScore
		steps = append(steps, RadarStep{
			Item:       state[best].item,
			Weight:     state[best].rule.Weight,
			Dimension:  state[best].rule.Dimension,
			ScoreAfter: bestScore,
		})
	}
	return steps
}

func clamp(v float64) int {
	return int(math.Round(math.Max(0, math.Min(100, v))))
}
