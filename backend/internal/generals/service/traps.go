package service

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/an4eetos/decision-room/internal/generals/domain"
)

// TrapHit is one trap found in what someone wrote, with the sentence that gave
// it away so the suggestion can quote them rather than paraphrase.
type TrapHit struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Quote string `json:"quote,omitempty"`
}

// maxQuoteRunes keeps a quoted sentence short enough to sit in a banner.
const maxQuoteRunes = 160

// negators are words that, just before a signal, turn it around: "not
// embarrassed", "never ashamed". "No" is left out: "if she says no, I'll never
// get over it" is the trap, not its denial. Two tokens back is close enough to catch them
// and far enough not to swallow the sentence.
var negators = []string{"not", "never", "don't", "doesn't", "isn't", "wasn't", "aren't", "weren't", "without", "nobody", "hardly"}

// DetectTraps finds traps by their signal phrases. It costs no model call and
// always gives the same answer for the same text, which is what lets it decide
// whether to interrupt someone with a suggestion.
//
// Weak traps are reported only alongside a trap that is not weak: "try to" or
// "obviously" on their own are ordinary speech, and a room that suggests an
// interrogation over them would be ignored within a day.
func DetectTraps(text string, traps []domain.Trap) []TrapHit {
	lower := normalise(text)
	if strings.TrimSpace(lower) == "" {
		return nil
	}

	var (
		hits   []TrapHit
		strong bool
	)
	for _, trap := range traps {
		for _, signal := range trap.Signals {
			at, ok := findSignal(lower, signal)
			if !ok {
				continue
			}
			hits = append(hits, TrapHit{ID: trap.ID, Name: trap.Name, Quote: sentenceAt(text, lower, at)})
			if !trap.Weak {
				strong = true
			}
			break
		}
	}
	if !strong {
		return nil
	}
	return hits
}

// normalise lower-cases and straightens curly apostrophes, which phones insert
// and which would otherwise make "can’t" miss "can't".
func normalise(text string) string {
	return strings.ToLower(strings.NewReplacer("’", "'", "‘", "'").Replace(text))
}

// findSignal returns the byte offset of the first un-negated match. Phrases
// match as substrings; a single word must be a whole word, because prefix
// matching makes "ruin" fire on "ruins of Rome".
func findSignal(lower, signal string) (int, bool) {
	signal = normalise(strings.TrimSpace(signal))
	if signal == "" {
		return -1, false
	}
	phrase := strings.ContainsAny(signal, " '-")

	for from := 0; from < len(lower); {
		i := strings.Index(lower[from:], signal)
		if i < 0 {
			return -1, false
		}
		at := from + i
		end := at + len(signal)
		from = end

		if !phrase && (!boundaryBefore(lower, at) || !boundaryAfter(lower, end)) {
			continue
		}
		if negated(lower[:at]) {
			continue
		}
		return at, true
	}
	return -1, false
}

func boundaryBefore(s string, at int) bool {
	if at == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:at])
	return !isWordRune(r)
}

func boundaryAfter(s string, end int) bool {
	if end >= len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[end:])
	return !isWordRune(r)
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '\''
}

// negated looks at the two words before a match.
func negated(before string) bool {
	words := strings.FieldsFunc(before, func(r rune) bool { return !isWordRune(r) })
	if len(words) > 2 {
		words = words[len(words)-2:]
	}
	for _, w := range words {
		if slices.Contains(negators, w) {
			return true
		}
	}
	return false
}

// sentenceAt returns the sentence of the original text around a byte offset in
// its normalised form. Normalising swaps a three-byte apostrophe for a one-byte
// one, so offsets drift; the sentence is found in the normalised text and then
// cut from the original by rune position, which both share.
func sentenceAt(original, lower string, at int) string {
	start := strings.LastIndexAny(lower[:at], ".!?\n") + 1
	end := len(lower)
	if i := strings.IndexAny(lower[at:], ".!?\n"); i >= 0 {
		end = at + i + 1
	}

	runes := []rune(original)
	from := utf8.RuneCountInString(lower[:start])
	to := utf8.RuneCountInString(lower[:end])
	if to > len(runes) {
		to = len(runes)
	}
	quote := strings.TrimSpace(string(runes[from:to]))
	if utf8.RuneCountInString(quote) > maxQuoteRunes {
		quote = string([]rune(quote)[:maxQuoteRunes-1]) + "…"
	}
	return quote
}
