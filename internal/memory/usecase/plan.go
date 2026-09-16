package usecase

import (
	"strings"
	"time"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

// ConsultPlan is everything one request needs, resolved once at the top of
// Consult.Execute and then passed by value.
//
// This exists because the alternative is a parameter per knob — which is how the
// Consult constructor reached nine arguments — or fields on the Consult struct,
// which is worse: the tool-round limit used to be frozen at dependency-injection
// time, so it could not vary per request no matter what the caller asked for.
type ConsultPlan struct {
	Question string
	History  []port.Message
	Tier     domain.TierPolicy

	// Now is injected so recency scoring is testable rather than reading the
	// wall clock three layers down.
	Now time.Time
}

// ResolvePlan turns request input into a plan, capping the tier at whatever the
// deployment allows.
func ResolvePlan(input ConsultInput, defaultTier, maxTier domain.Tier) ConsultPlan {
	tier := domain.ParseTier(input.Tier, defaultTier).CapTo(maxTier)
	policy := domain.PolicyFor(tier)

	// An explicit TopK from the caller still wins; the tier only supplies the
	// default.
	if input.TopK > 0 {
		policy.TopK = input.TopK
	}

	return ConsultPlan{
		Question: strings.TrimSpace(input.Question),
		History:  input.History,
		Tier:     policy,
		Now:      time.Now().UTC(),
	}
}
