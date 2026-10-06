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

	// Generals are the lenses this answer is written through. Their cards reach
	// the prompt, plus the doctrine passages in Doctrine — never the full text.
	Generals []gendomain.Lens
	// Doctrine is, by lens id, the passages chosen for this question. Filled
	// after retrieval, once the tier is final; empty on a greeting or a tier
	// with no doctrine budget.
	Doctrine map[string][]gendomain.Passage
	// GeneralsMethod is "explicit", "auto" or "sticky", so the UI can say which.
	GeneralsMethod string

	// Mode is the conversation shape, and ModeMethod is how it was arrived at:
	// explicit, sticky, keyword or default. Both surface in the interface so a
	// wrong detection is one click to fix rather than a silently odd answer.
	Mode       modedomain.Mode
	ModeMethod string
	// Styles are the working styles this mode leans on.
	Styles []gendomain.Lens

	// Plain drops the output template and lenses; see ConsultInput.Plain.
	Plain bool

	// Conclude closes the mode: its "## Position" template replaces the output
	// template for this turn. See ConsultInput.Conclude.
	Conclude bool

	// Traps is the trap catalogue, and TrapHits the traps its signal phrases
	// found in the question. Hits are only looked for where a suggestion could
	// follow — never inside an interrogation, never on a plain check-in.
	Traps    []gendomain.Trap
	TrapHits []genservice.TrapHit

	// Progress is carried from the input; see ConsultInput.Progress.
	Progress *Progress

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
		Plain:    input.Plain,
		Conclude: input.Conclude,
		Progress: input.Progress,
	}

	var defaults []string
	// The conversation's roster stays seated unless the situation changes.
	seated := input.SeatedGenerals
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

		// A mode switch is the clearest sign the situation changed: the bar to
		// leave a mode is already set high, so it clearing earns a fresh pick of
		// generals too.
		// Unless the mode asks to keep them: an interrogation is run by the
		// generals who saw the trap.
		if input.SessionMode != "" && detection.Mode.ID != input.SessionMode && !detection.Mode.Generals.KeepSeated {
			seated = nil
		}

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

	if r.registry != nil && !plan.Plain {
		selection := genservice.Select(r.registry, genservice.SelectInput{
			Question:     plan.Question,
			Explicit:     input.GeneralIDs,
			Defaults:     defaults,
			RecentlyUsed: input.RecentGenerals,
			Seated:       seated,
			Max:          plan.Tier.MaxGenerals,
		})
		plan.Generals = selection.Lenses
		plan.GeneralsMethod = selection.Method

		plan.Styles = r.registry.Resolve(plan.Mode.Styles)

		plan.Traps = r.registry.Traps()
		if plan.Mode.IsInterrogation() {
			// Inside an interrogation the hits never become a suggestion; they
			// tell the interrogators which traps the conversation has shown, so
			// one is not misnamed after whatever the seated lenses carry.
			plan.TrapHits = genservice.DetectTraps(userText(plan.Question, plan.History), plan.Traps)
		} else {
			plan.TrapHits = genservice.DetectTraps(plan.Question, plan.Traps)
		}
	}

	return plan
}

// SpottedTraps lists the ids of the traps found in what the person wrote.
func (p ConsultPlan) SpottedTraps() []string {
	ids := make([]string, 0, len(p.TrapHits))
	for _, hit := range p.TrapHits {
		ids = append(ids, hit.ID)
	}
	return ids
}

// userText is everything the person has written in the conversation, newest
// last.
func userText(question string, history []port.Message) string {
	var b strings.Builder
	for _, m := range history {
		if m.Role == "user" {
			b.WriteString(m.Content)
			b.WriteString("\n")
		}
	}
	b.WriteString(question)
	return b.String()
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

// DoctrineRefs names the passages used, as "zhukov: Facing the unknown", so the
// interface can show what each lens actually argued from.
func (p ConsultPlan) DoctrineRefs() []string {
	var refs []string
	for _, lens := range p.Generals {
		for _, passage := range p.Doctrine[lens.ID] {
			section := passage.Section
			if section == "" {
				section = "doctrine"
			}
			refs = append(refs, lens.ID+": "+section)
		}
	}
	return refs
}
