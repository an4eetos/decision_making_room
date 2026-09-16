package domain

import "strings"

// Tier is how much work a single question is worth. The cost of an answer varies
// by more than an order of magnitude across these, and most questions do not
// deserve the expensive one.
type Tier string

const (
	// TierQuick answers from a handful of memories with no tool calls. For
	// "what's next" — a few seconds, a short answer.
	TierQuick Tier = "quick"
	// TierStandard is the default: full hybrid retrieval and one optional round
	// of recall if the pre-loaded context misses something.
	TierStandard Tier = "standard"
	// TierDeep widens the candidate pool and lets the model keep digging.
	TierDeep Tier = "deep"
)

// TierPolicy is the resolved set of knobs for one request. It is passed by value
// so nothing can hold a stale copy, and so a test can construct one directly.
type TierPolicy struct {
	Tier Tier

	// TopK is how many memories reach the prompt.
	TopK int
	// CandidateLimit is the pool each search arm returns before fusion.
	CandidateLimit int
	// IncludeRecent is how many recent entries are merged in regardless of
	// relevance, to keep "what did I do this week" working.
	IncludeRecent int
	// MaxBodyRunes caps a memory body in the prompt.
	MaxBodyRunes int

	// MaxGenerals is how many planning lenses the answer is written through.
	// More lenses means a longer, structured answer, so it rises with depth.
	MaxGenerals int

	// MaxToolRounds is how many rounds the model may call tools in. Zero means
	// single-shot with no agent loop at all. The loop always gets one further
	// round with tools withdrawn, so it can answer rather than stopping mid-dig.
	MaxToolRounds int

	// AnswerBudget is appended to the system prompt. Empty means no instruction.
	AnswerBudget string
}

func (p TierPolicy) UsesTools() bool { return p.MaxToolRounds > 0 }

// PolicyFor returns the knobs for a tier, falling back to standard for anything
// unrecognised.
func PolicyFor(t Tier) TierPolicy {
	switch t {
	case TierQuick:
		return TierPolicy{
			Tier:           TierQuick,
			TopK:           3,
			CandidateLimit: 20,
			IncludeRecent:  0,
			MaxBodyRunes:   900,
			MaxGenerals:    1,
			MaxToolRounds:  0,
			AnswerBudget:   "Answer in under 120 words. Give one recommendation, not a survey.",
		}
	case TierDeep:
		return TierPolicy{
			Tier:           TierDeep,
			TopK:           12,
			CandidateLimit: 60,
			IncludeRecent:  5,
			MaxBodyRunes:   2000,
			MaxGenerals:    3,
			MaxToolRounds:  3,
			AnswerBudget:   "Take the space you need. Show the reasoning that matters and name what you are unsure about.",
		}
	default:
		return TierPolicy{
			Tier:           TierStandard,
			TopK:           8,
			CandidateLimit: 30,
			IncludeRecent:  3,
			MaxBodyRunes:   2000,
			MaxGenerals:    2,
			MaxToolRounds:  1,
		}
	}
}

// ParseTier normalises user input. An empty or unknown value is not an error —
// it falls back to the given default, because a typo in a query parameter should
// not fail a question.
func ParseTier(s string, fallback Tier) Tier {
	switch Tier(strings.ToLower(strings.TrimSpace(s))) {
	case TierQuick:
		return TierQuick
	case TierStandard:
		return TierStandard
	case TierDeep:
		return TierDeep
	default:
		return fallback
	}
}

// Rank orders tiers by cost, for capping against a configured maximum.
func (t Tier) Rank() int {
	switch t {
	case TierQuick:
		return 0
	case TierDeep:
		return 2
	default:
		return 1
	}
}

// CapTo lowers a tier to a ceiling. A deployment without a tool-capable model,
// or one that wants to bound cost, sets the ceiling once rather than having
// every caller check.
func (t Tier) CapTo(max Tier) Tier {
	if t.Rank() > max.Rank() {
		return max
	}
	return t
}
