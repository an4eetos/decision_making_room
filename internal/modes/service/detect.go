// Package service decides which mode a question belongs to.
package service

import (
	"sort"
	"strings"
	"unicode"

	"github.com/an4eetos/decision-room/internal/modes/domain"
	"github.com/an4eetos/decision-room/internal/modes/port"
)

// Method records how a mode was arrived at, so the interface can show it and the
// user can correct it.
type Method string

const (
	// MethodExplicit means the user picked it.
	MethodExplicit Method = "explicit"
	// MethodSticky means the conversation was already in this mode.
	MethodSticky Method = "sticky"
	// MethodKeyword means the question matched trigger phrases.
	MethodKeyword Method = "keyword"
	// MethodDefault means nothing matched.
	MethodDefault Method = "default"
)

type Detection struct {
	Mode       domain.Mode
	Method     Method
	Confidence float64
}

type Input struct {
	Question string
	// Explicit is a mode the user chose for this turn.
	Explicit string
	// SessionMode is the mode the conversation is already in, and Locked is
	// whether the user set it by hand rather than it being detected.
	SessionMode string
	Locked      bool
	// TurnIndex is how many turns have happened; stickiness only applies after
	// the first.
	TurnIndex int
}

const (
	// keywordHit is the score for one matched trigger phrase.
	keywordHit = 1.0
	// leadBonus rewards a match near the start of the question, where intent is
	// usually stated.
	leadBonus = 0.3
	leadChars = 40
	// acceptScore and acceptRatio are how sure the keyword scorer must be. A
	// mode has to both clear the bar and clearly beat the runner-up.
	acceptScore = 1.2
	acceptRatio = 1.4
	// challengeScore is what a competing mode must reach to override the mode a
	// conversation is already in. Set well above acceptScore on purpose:
	// switching mode mid-conversation is more disruptive than being in a
	// slightly wrong one, because it changes the shape of the answer underneath
	// the user.
	challengeScore = 2.2
)

type Detector struct {
	registry port.Registry
}

func NewDetector(registry port.Registry) *Detector {
	return &Detector{registry: registry}
}

// Detect runs the cascade: an explicit pick, then the mode the conversation is
// already in, then keyword scoring, then the open default.
func (d *Detector) Detect(in Input) Detection {
	fallback := d.registry.Default()

	if in.Explicit != "" {
		if mode, ok := d.registry.Get(in.Explicit); ok {
			return Detection{Mode: mode, Method: MethodExplicit, Confidence: 1}
		}
	}

	scores := d.score(in.Question)
	best, runnerUp := top2(scores)

	// A mode the user locked by hand is never overridden by detection.
	if in.Locked && in.SessionMode != "" {
		if mode, ok := d.registry.Get(in.SessionMode); ok {
			return Detection{Mode: mode, Method: MethodExplicit, Confidence: 1}
		}
	}

	if in.TurnIndex > 0 && in.SessionMode != "" && in.SessionMode != fallback.ID {
		if mode, ok := d.registry.Get(in.SessionMode); ok {
			// Stay unless a different mode makes a strong case.
			if best.id == "" || best.id == mode.ID || best.score < challengeScore {
				return Detection{Mode: mode, Method: MethodSticky, Confidence: 0.6}
			}
		}
	}

	if best.id != "" && best.score >= acceptScore && best.score >= runnerUp.score*acceptRatio {
		if mode, ok := d.registry.Get(best.id); ok {
			return Detection{Mode: mode, Method: MethodKeyword, Confidence: confidence(best.score)}
		}
	}

	return Detection{Mode: fallback, Method: MethodDefault, Confidence: 0.2}
}

type scored struct {
	id    string
	score float64
}

func (d *Detector) score(question string) []scored {
	lower := strings.ToLower(question)
	tokens := tokenize(lower)

	out := make([]scored, 0, len(d.registry.List()))
	for _, mode := range d.registry.List() {
		weight := mode.Triggers.Weight
		if weight <= 0 {
			// Weight zero means the mode opts out of keyword detection, which is
			// what the open default does.
			continue
		}

		total := 0.0
		for _, phrase := range mode.Triggers.Keywords {
			at, ok := matchAt(lower, tokens, phrase)
			if !ok {
				continue
			}
			hit := keywordHit
			if at >= 0 && at < leadChars {
				hit += leadBonus
			}
			total += hit
		}
		if total > 0 {
			out = append(out, scored{id: mode.ID, score: total * weight})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		// Stable tie-break so the same question always detects the same mode.
		return out[i].id < out[j].id
	})
	return out
}

func top2(scores []scored) (best, runnerUp scored) {
	if len(scores) > 0 {
		best = scores[0]
	}
	if len(scores) > 1 {
		runnerUp = scores[1]
	}
	return best, runnerUp
}

// matchAt reports where a phrase matched, so a match near the start of the
// question can be weighted higher. Multi-word phrases match as substrings;
// single words match a whole token, not a prefix — mode triggers are specific
// phrases and prefix matching makes them fire far too readily.
func matchAt(lower string, tokens []string, phrase string) (int, bool) {
	phrase = strings.ToLower(strings.TrimSpace(phrase))
	if phrase == "" {
		return -1, false
	}

	if strings.ContainsAny(phrase, " '-") {
		if i := strings.Index(lower, phrase); i >= 0 {
			return i, true
		}
		return -1, false
	}

	for _, token := range tokens {
		if token == phrase {
			return strings.Index(lower, phrase), true
		}
	}
	return -1, false
}

func tokenize(lower string) []string {
	return strings.FieldsFunc(lower, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// confidence maps a raw score onto [0,1] for display. Two clean hits is already
// as sure as this method gets.
func confidence(score float64) float64 {
	c := score / (2 * (keywordHit + leadBonus))
	if c > 1 {
		return 1
	}
	return c
}
