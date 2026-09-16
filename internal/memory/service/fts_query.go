package service

import (
	"strings"
	"unicode"
)

// FTSQuery is a natural-language question reduced to something Postgres
// full-text search can actually match.
//
// The naive approach — handing the whole question to websearch_to_tsquery —
// AND-joins every lexeme in it, so "what should I focus on today given the
// backlog" only matches a document containing all of those words. In practice
// that returns nothing for essentially any conversational question, which
// silently degrades hybrid search to vector-only.
type FTSQuery struct {
	Terms []string

	// English is OR-joined and stemmed. Simple is the same terms against the
	// 'simple' config, which neither stems nor drops stopwords — that is what
	// catches proper nouns, project codenames and transliterated words the
	// snowball stemmer mangles.
	English string
	Simple  string
}

const (
	minTermRunes = 3
	maxFTSTerms  = 12
)

// BuildFTSQuery extracts content words from a question. The bool is false when
// nothing usable is left, in which case the caller should skip the full-text arm
// rather than run a query that cannot match.
func BuildFTSQuery(question string) (FTSQuery, bool) {
	seen := make(map[string]struct{})
	var terms []string

	for _, field := range strings.FieldsFunc(strings.ToLower(question), isNotAlphanumeric) {
		// to_tsquery errors on malformed input rather than returning no rows, and
		// an error fails the whole retrieval. Everything reaching it is reduced to
		// [a-z0-9] above, so there is nothing left to escape.
		if len([]rune(field)) < minTermRunes {
			continue
		}
		if _, ok := stopwords[field]; ok {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		terms = append(terms, field)
		if len(terms) == maxFTSTerms {
			break
		}
	}

	if len(terms) == 0 {
		return FTSQuery{}, false
	}

	joined := strings.Join(terms, " | ")
	return FTSQuery{Terms: terms, English: joined, Simple: joined}, true
}

func isNotAlphanumeric(r rune) bool {
	return !unicode.IsLetter(r) && !unicode.IsDigit(r)
}

// stopwords covers ordinary English filler plus the question and modal words
// that conversational prompts are made of. Postgres' own 'english' dictionary
// drops most of the former, but these are removed before the query is built so
// they never inflate the term budget or the OR-join.
var stopwords = newStopwordSet(`
a about above after again against all also am an and any are aren as at
be because been before being below between both but by
can cannot could couldn
did didn do does doesn doing don down during
each few for from further
had hadn has hasn have haven having he her here hers herself him himself his how
i if in into is isn it its itself
just
let
me more most must my myself
no nor not now
of off on once only or other ought our ours ourselves out over own
same shan she should shouldn so some such
than that the their theirs them themselves then there these they this those through to too
under until up
very
want was wasn we were weren what when where which while who whom why will with won would wouldn
you your yours yourself yourselves
get got give given going gonna
like make making need needs
one two three
thing things stuff
please thanks thank
`)

func newStopwordSet(list string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, w := range strings.Fields(list) {
		set[w] = struct{}{}
	}
	return set
}
