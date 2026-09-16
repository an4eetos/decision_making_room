package usecase

import (
	"strings"
	"time"

	gendomain "github.com/an4eetos/decision-room/internal/generals/domain"
	genport "github.com/an4eetos/decision-room/internal/generals/port"
	genservice "github.com/an4eetos/decision-room/internal/generals/service"
	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
	"github.com/an4eetos/decision-room/internal/memory/service"
	modedomain "github.com/an4eetos/decision-room/internal/modes/domain"
	modeservice "github.com/an4eetos/decision-room/internal/modes/service"
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

	// Mode is the conversation shape, and ModeMethod is how it was arrived at:
	// explicit, sticky, keyword or default. Both surface in the interface so a
	// wrong detection is one click to fix rather than a silently odd answer.
	Mode       modedomain.Mode
	ModeMethod string
	// Styles are the working styles this mode leans on.
	Styles []gendomain.Lens

	// Now is injected so recency scoring is testable rather than reading the
	// wall clock three layers down.
	Now time.Time
}

// PlanResolver composes tier policy with lens selection. It is a struct rather
// than a function because selection needs the roster.
type PlanResolver struct {
	registry    genport.Registry
	detector    *modeservice.Detector
	defaultTier domain.Tier
	maxTier     domain.Tier
}

func NewPlanResolver(
	registry genport.Registry,
	detector *modeservice.Detector,
	defaultTier, maxTier domain.Tier,
) *PlanResolver {
	return &PlanResolver{
		registry:    registry,
		detector:    detector,
		defaultTier: defaultTier,
		maxTier:     maxTier,
	}
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

	var defaults []string
	if r.detector != nil {
		detection := r.detector.Detect(modeservice.Input{
			Question:    plan.Question,
			Explicit:    input.ModeID,
			SessionMode: input.SessionMode,
			Locked:      input.ModeLocked,
			TurnIndex:   input.TurnIndex,
		})
		plan.Mode = detection.Mode
		plan.ModeMethod = string(detection.Method)

		// The mode supplies preferred lenses and can raise or lower how many an
		// answer is written through, within what the tier allows.
		defaults = detection.Mode.Generals.Default
		if m := detection.Mode.Generals.Max; m > 0 && m < policy.MaxGenerals {
			policy.MaxGenerals = m
			plan.Tier = policy
		}

		// Per-mode retrieval bias: a debrief wants recency to dominate, a
		// pre-mortem wants it nearly ignored.
		if k := detection.Mode.Retrieval.TopK; k > 0 && input.TopK == 0 {
			policy.TopK = k
			plan.Tier = policy
		}
	}

	if r.registry != nil {
		selection := genservice.Select(r.registry, genservice.SelectInput{
			Question:     plan.Question,
			Explicit:     input.GeneralIDs,
			Defaults:     defaults,
			RecentlyUsed: input.RecentGenerals,
			Max:          plan.Tier.MaxGenerals,
		})
		plan.Generals = selection.Lenses
		plan.GeneralsMethod = selection.Method

		plan.Styles = r.registry.Resolve(plan.Mode.Styles)
	}

	return plan
}

// RetrievalBias turns the mode's preferences into reranking inputs. Kinds are
// boosted, never filtered.
func (p ConsultPlan) RetrievalBias() service.Bias {
	return service.Bias{
		Kinds:     p.Mode.Retrieval.Kinds,
		KindBoost: p.Mode.Retrieval.KindBoost,
	}
}

// RerankWeights applies the mode's recency preference over the defaults.
func (p ConsultPlan) RerankWeights() service.Weights {
	w := service.DefaultWeights()
	if rw := p.Mode.Retrieval.RecencyWeight; rw > 0 {
		// Relevance absorbs the change so the three weights still sum to one and
		// scores stay comparable between modes.
		delta := rw - w.Recency
		w.Recency = rw
		w.Relevance -= delta
		if w.Relevance < 0 {
			w.Relevance = 0
		}
	}
	return w
}

// GeneralIDs lists the chosen lens ids, for storing alongside the answer.
func (p ConsultPlan) GeneralIDs() []string {
	ids := make([]string, 0, len(p.Generals))
	for _, lens := range p.Generals {
		ids = append(ids, lens.ID)
	}
	return ids
}
