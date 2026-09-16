package usecase

import (
	"strings"
	"time"

	gendomain "github.com/an4eetos/decision-room/internal/generals/domain"
	genport "github.com/an4eetos/decision-room/internal/generals/port"
	genservice "github.com/an4eetos/decision-room/internal/generals/service"
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

	// Generals are the lenses this answer is written through. Only their cards
	// reach the prompt, never the full doctrine.
	Generals []gendomain.Lens
	// GeneralsMethod is "explicit" or "auto", so the UI can say which.
	GeneralsMethod string

	// Now is injected so recency scoring is testable rather than reading the
	// wall clock three layers down.
	Now time.Time
}

// PlanResolver composes tier policy with lens selection. It is a struct rather
// than a function because selection needs the roster.
type PlanResolver struct {
	registry    genport.Registry
	defaultTier domain.Tier
	maxTier     domain.Tier
}

func NewPlanResolver(registry genport.Registry, defaultTier, maxTier domain.Tier) *PlanResolver {
	return &PlanResolver{registry: registry, defaultTier: defaultTier, maxTier: maxTier}
}

// Resolve turns request input into a plan: tier capped to what the deployment
// allows, and lenses either as picked or selected deterministically.
func (r *PlanResolver) Resolve(input ConsultInput) ConsultPlan {
	tier := domain.ParseTier(input.Tier, r.defaultTier).CapTo(r.maxTier)
	policy := domain.PolicyFor(tier)

	// An explicit TopK from the caller still wins; the tier only supplies the
	// default.
	if input.TopK > 0 {
		policy.TopK = input.TopK
	}

	plan := ConsultPlan{
		Question: strings.TrimSpace(input.Question),
		History:  input.History,
		Tier:     policy,
		Now:      time.Now().UTC(),
	}

	if r.registry != nil {
		selection := genservice.Select(r.registry, genservice.SelectInput{
			Question:     plan.Question,
			Explicit:     input.GeneralIDs,
			RecentlyUsed: input.RecentGenerals,
			Max:          policy.MaxGenerals,
		})
		plan.Generals = selection.Lenses
		plan.GeneralsMethod = selection.Method
	}

	return plan
}

// GeneralIDs lists the chosen lens ids, for storing alongside the answer.
func (p ConsultPlan) GeneralIDs() []string {
	ids := make([]string, 0, len(p.Generals))
	for _, lens := range p.Generals {
		ids = append(ids, lens.ID)
	}
	return ids
}
