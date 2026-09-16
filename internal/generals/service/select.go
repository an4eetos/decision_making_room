// Package service picks which lenses apply to a question.
//
// Deliberately deterministic: no model call. An LLM router here would add
// hundreds of milliseconds to every turn to make a choice keyword routing gets
// right most of the time, that the user can override in one click, and that is
// visible in the UI either way.
package service

import (
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/an4eetos/decision-room/internal/generals/domain"
	"github.com/an4eetos/decision-room/internal/generals/port"
)

const (
	// seedScore is what a mode's default lenses start with.
	seedScore = 1.0
	// keywordHit is added per distinct matched routing phrase, capped so a lens
	// with a long keyword list cannot win on volume alone.
	keywordHit     = 0.6
	maxKeywordGain = 1.2
	// familyAffinity nudges toward lenses suited to the kind of question.
	familyAffinity = 0.3
	// recentPenalty breaks ties toward variety. Deliberately smaller than one
	// keyword hit: a lens with real evidence behind it should still win even if
	// it answered the last question, so this decides between lenses that scored
	// the same rather than forcing rotation.
	recentPenalty = 0.25
	// minScore is the floor below which a lens is not worth including. It sits
	// just under a single keyword hit on purpose: one piece of real evidence
	// from the question should qualify a lens, while a lens with nothing but a
	// family nudge (0.3) should not.
	minScore = 0.5
)

type SelectInput struct {
	Question string
	// Explicit is the user's own pick. Non-empty short-circuits everything.
	Explicit []string
	// Defaults seed the scoring — a mode's preferred lenses, once modes exist.
	Defaults []string
	// Families the current question suits.
	Families []domain.Family
	// RecentlyUsed are lenses from the last couple of turns.
	RecentlyUsed []string
	// Max caps how many come back. Zero means one.
	Max int
}

// Selection records what was chosen and how, so the UI can say so rather than
// silently applying a lens the user did not ask for.
type Selection struct {
	Lenses []domain.Lens
	// Method is "explicit" or "auto".
	Method string
}

func Select(registry port.Registry, in SelectInput) Selection {
	max := in.Max
	if max <= 0 {
		max = 1
	}

	if len(in.Explicit) > 0 {
		lenses := registry.Resolve(in.Explicit)
		if len(lenses) > max {
			lenses = lenses[:max]
		}
		// Only fall through to auto-selection if every id was unknown; a stale id
		// should not silently swap in something the user did not choose.
		if len(lenses) > 0 {
			return Selection{Lenses: lenses, Method: "explicit"}
		}
	}

	tokens := tokenize(in.Question)
	scores := make(map[string]float64)

	for _, id := range in.Defaults {
		scores[id] = seedScore
	}

	for _, lens := range registry.Generals() {
		gain := 0.0
		for _, phrase := range lens.Routes.Keywords {
			if matches(tokens, in.Question, phrase) {
				gain += keywordHit
			}
		}
		if gain > maxKeywordGain {
			gain = maxKeywordGain
		}
		if gain > 0 {
			scores[lens.ID] += gain
		}

		if len(in.Families) > 0 && slices.Contains(in.Families, lens.Family) {
			scores[lens.ID] += familyAffinity
		}

		if slices.Contains(in.RecentlyUsed, lens.ID) {
			scores[lens.ID] -= recentPenalty
		}
	}

	type scored struct {
		lens  domain.Lens
		score float64
	}
	ranked := make([]scored, 0, len(scores))
	for id, score := range scores {
		lens, ok := registry.Get(id)
		if !ok || lens.Kind != domain.KindGeneral {
			continue
		}
		ranked = append(ranked, scored{lens: lens, score: score})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		// Stable tie-break so the same question gives the same answer twice.
		return ranked[i].lens.ID < ranked[j].lens.ID
	})

	out := make([]domain.Lens, 0, max)
	for _, r := range ranked {
		if len(out) == max || r.score < minScore {
			break
		}
		out = append(out, r.lens)
	}

	// Never return nothing. A question with no keyword hits and no defaults still
	// deserves a lens, and silently answering with none is a different product.
	if len(out) == 0 {
		if fallback, ok := registry.Get(defaultLens); ok {
			out = append(out, fallback)
		} else if generals := registry.Generals(); len(generals) > 0 {
			out = append(out, generals[0])
		}
	}

	// Asking for depth buys the argument between lenses, so fill the remaining
	// slots even when only one lens cleared the floor. Filling from a
	// complementary family is the point: two scouting lenses agree with each
	// other and produce nothing a single lens would not have said.
	if len(out) < max {
		out = backfill(registry, out, scores, max)
	}

	return Selection{Lenses: out, Method: "auto"}
}

// complements maps a family to the families that argue most usefully against it.
// This is what makes a multi-lens answer adversarial by construction rather than
// three voices nodding along.
var complements = map[domain.Family][]domain.Family{
	domain.FamilyScouting:      {domain.FamilyContact, domain.FamilyPreservation},
	domain.FamilyContact:       {domain.FamilyPreservation, domain.FamilyScouting},
	domain.FamilyEndurance:     {domain.FamilyPreservation, domain.FamilyAdaptation},
	domain.FamilyAdaptation:    {domain.FamilyEndurance, domain.FamilyConcentration},
	domain.FamilySystems:       {domain.FamilyContact, domain.FamilyAdaptation},
	domain.FamilyConcentration: {domain.FamilyScouting, domain.FamilyPreservation},
	domain.FamilyPreservation:  {domain.FamilyContact, domain.FamilyConcentration},
}

func backfill(registry port.Registry, chosen []domain.Lens, scores map[string]float64, max int) []domain.Lens {
	taken := make(map[string]bool, len(chosen))
	families := make(map[domain.Family]bool, len(chosen))
	for _, lens := range chosen {
		taken[lens.ID] = true
		families[lens.Family] = true
	}

	// Try each chosen lens's complementary families in order, then any family not
	// yet represented, so the result is diverse even for an unusual roster.
	var wanted []domain.Family
	for _, lens := range chosen {
		wanted = append(wanted, complements[lens.Family]...)
	}
	for _, family := range allFamilies {
		wanted = append(wanted, family)
	}

	for _, family := range wanted {
		if len(chosen) == max {
			break
		}
		if families[family] {
			continue
		}
		if lens, ok := bestIn(registry, family, taken, scores); ok {
			chosen = append(chosen, lens)
			taken[lens.ID] = true
			families[family] = true
		}
	}

	return chosen
}

var allFamilies = []domain.Family{
	domain.FamilyContact, domain.FamilyScouting, domain.FamilyEndurance,
	domain.FamilyAdaptation, domain.FamilySystems, domain.FamilyConcentration,
	domain.FamilyPreservation,
}

// bestIn picks the highest-scoring unused lens in a family, breaking ties on id
// so the same question always produces the same roster.
func bestIn(registry port.Registry, family domain.Family, taken map[string]bool, scores map[string]float64) (domain.Lens, bool) {
	var best domain.Lens
	found := false

	for _, lens := range registry.Generals() {
		if lens.Family != family || taken[lens.ID] {
			continue
		}
		if !found ||
			scores[lens.ID] > scores[best.ID] ||
			(scores[lens.ID] == scores[best.ID] && lens.ID < best.ID) {
			best, found = lens, true
		}
	}

	return best, found
}

// defaultLens is the lens for a question that routes nowhere: choosing what to
// do is the most common unstated question.
const defaultLens = "sun_tzu"

// matches handles both single words and multi-word phrases. Single words match
// on a token prefix so "priorit" catches "priority", "prioritise" and
// "prioritizing" from one entry.
func matches(tokens []string, question, phrase string) bool {
	phrase = strings.ToLower(strings.TrimSpace(phrase))
	if phrase == "" {
		return false
	}

	if strings.ContainsAny(phrase, " '") {
		return strings.Contains(strings.ToLower(question), phrase)
	}

	for _, token := range tokens {
		if strings.HasPrefix(token, phrase) {
			return true
		}
	}
	return false
}

func tokenize(question string) []string {
	return strings.FieldsFunc(strings.ToLower(question), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}
