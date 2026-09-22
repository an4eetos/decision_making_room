package service

import (
	"strings"
	"unicode"
)

// MaxSubQueries caps how far a question is broken up. Past three the sub-queries
// start restating each other and the extra searches return the same rows.
const MaxSubQueries = 3

// ParseSubQueries reads a model's decomposition of a question: one query per
// line, numbering and bullets tolerated.
//
// It never returns an error. A decomposition that cannot be parsed is not worth
// failing a question over — the caller searches the original question, which is
// what it would have done anyway.
func ParseSubQueries(answer string, original string) []string {
	seen := map[string]struct{}{normalise(original): {}}
	out := make([]string, 0, MaxSubQueries)

	for _, line := range strings.Split(answer, "\n") {
		q := cleanSubQuery(line)
		if q == "" {
			continue
		}

		key := normalise(q)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		out = append(out, q)
		if len(out) == MaxSubQueries {
			break
		}
	}

	return out
}

// cleanSubQuery strips the list markup models add despite instructions, and
// rejects anything that is prose about the task rather than a query.
func cleanSubQuery(line string) string {
	q := strings.TrimSpace(line)
	if q == "" {
		return ""
	}

	// Leading "1." / "1)" / "-" / "*" / "•".
	q = strings.TrimLeft(q, "0123456789.)-*• \t")
	q = strings.TrimSpace(q)
	q = strings.Trim(q, `"'`)

	if q == "" {
		return ""
	}
	// Models like to prefix an explanation line; a query is short.
	if len([]rune(q)) < 3 || len([]rune(q)) > 200 {
		return ""
	}
	// Punctuation alone is not a query, and would search for nothing.
	if !hasAlphanumeric(q) {
		return ""
	}
	// Commentary rather than a query.
	lower := strings.ToLower(q)
	for _, prefix := range []string{"here are", "sure", "of course", "i'll", "these are", "sub-quer", "queries:"} {
		if strings.HasPrefix(lower, prefix) {
			return ""
		}
	}

	return q
}

func normalise(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func hasAlphanumeric(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
