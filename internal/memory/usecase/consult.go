package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	genport "github.com/an4eetos/decision-room/internal/generals/port"
	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
	"github.com/an4eetos/decision-room/internal/memory/service"
	modeservice "github.com/an4eetos/decision-room/internal/modes/service"
)

type ConsultInput struct {
	Question string
	TopK     int
	History  []port.Message
	// Tier is "quick", "standard" or "deep". Empty uses the configured default;
	// an unrecognised value falls back rather than failing the request.
	Tier string
	// GeneralIDs is the user's own pick, up to the tier's maximum. Empty means
	// auto-select.
	GeneralIDs []string
	// RecentGenerals are the lenses used in the last couple of turns; they are
	// demoted so one lens does not answer everything.
	RecentGenerals []string

	// ModeID is an explicit mode for this turn. SessionMode and ModeLocked are
	// the conversation's current mode and whether the user set it by hand;
	// TurnIndex gates stickiness so the first turn is always detected fresh.
	ModeID      string
	SessionMode string
	ModeLocked  bool
	TurnIndex   int
}

type ConsultSource struct {
	ID    string            `json:"id"`
	Kind  domain.MemoryKind `json:"kind"`
	Title string            `json:"title"`
	Score float64           `json:"score"`
}

type ConsultResult struct {
	Answer  string          `json:"answer"`
	Sources []ConsultSource `json:"sources"`
	// Tier is what actually ran, which may be lower than requested if the
	// deployment caps it, or higher if an empty retrieval forced an escalation.
	Tier string `json:"tier"`
	// Generals are the ids the answer was written through, and Method is whether
	// they were picked by the user or selected automatically.
	Generals       []string `json:"generals"`
	GeneralsMethod string   `json:"generals_method,omitempty"`
	// Mode is the conversation shape that produced this answer, ModeName is its
	// display name, and ModeMethod is how it was arrived at.
	Mode       string `json:"mode,omitempty"`
	ModeName   string `json:"mode_name,omitempty"`
	ModeMethod string `json:"mode_method,omitempty"`
}

type Consult struct {
	agent          *AgentConsult
	repo           port.MemoryRepository
	retriever      *Retrieve
	llm            port.LLM
	initialContext port.InitialContextReader
	resolver       *PlanResolver
}

func NewConsult(
	repo port.MemoryRepository,
	retriever *Retrieve,
	llm port.LLM,
	toolLLM port.ToolLLM,
	initialContext port.InitialContextReader,
	tools *MemoryToolExecutor,
	registry genport.Registry,
	detector *modeservice.Detector,
	defaultTier, maxTier domain.Tier,
) *Consult {
	// Without a tool-capable model there is no agent, so no tier can use tools
	// however high the ceiling is set.
	var agent *AgentConsult
	if toolLLM != nil && tools != nil {
		agent = NewAgentConsult(toolLLM, tools, initialContext)
	} else {
		maxTier = domain.TierQuick
	}

	return &Consult{
		agent:          agent,
		repo:           repo,
		retriever:      retriever,
		llm:            llm,
		initialContext: initialContext,
		resolver:       NewPlanResolver(registry, detector, defaultTier, maxTier),
	}
}

const systemPrompt = `You are a personal advisor with access to the user's stored decisions, plans, and notes.
Use the retrieved context to answer. If information is missing, say so clearly.
Prefer recent decisions over older ones when they conflict.
Be concise and actionable for daily check-ins.`

func (u *Consult) Execute(ctx context.Context, input ConsultInput) (ConsultResult, error) {
	plan := u.resolver.Resolve(input)
	if plan.Question == "" {
		return ConsultResult{}, fmt.Errorf("question is required")
	}

	context, err := u.gatherContext(ctx, plan)
	if err != nil {
		return ConsultResult{}, err
	}

	// A confidently wrong answer built on nothing is the worst output this can
	// produce, so an empty retrieval on the cheapest tier buys one more attempt
	// with the standard pipeline rather than answering blind.
	if len(context) == 0 && plan.Tier.Tier == domain.TierQuick && u.resolver.maxTier.Rank() > domain.TierQuick.Rank() {
		escalated := domain.PolicyFor(domain.TierStandard)
		// Keep the lens count the escalation implies, but not more lenses than
		// were actually selected.
		if escalated.MaxGenerals > len(plan.Generals) {
			escalated.MaxGenerals = len(plan.Generals)
		}
		plan.Tier = escalated
		if context, err = u.gatherContext(ctx, plan); err != nil {
			return ConsultResult{}, err
		}
	}

	if plan.Tier.UsesTools() && u.agent != nil {
		return u.agent.Execute(ctx, plan, context)
	}
	return u.executeSingleShot(ctx, plan, context)
}

// gatherContext runs retrieval once for every tier. Single-shot and agent paths
// used to each do their own, with slightly different parameters and different
// ideas of what counted as a source.
func (u *Consult) gatherContext(ctx context.Context, plan ConsultPlan) ([]domain.MemoryEntry, error) {
	if skipRetrieval(plan.Question) {
		return nil, nil
	}

	retrieved, err := u.retriever.Execute(ctx, RetrieveInput{
		Query:          retrievalQuery(plan),
		TopK:           plan.Tier.TopK,
		CandidateLimit: plan.Tier.CandidateLimit,
		Filter:         port.SearchFilter{},
		Rerank: service.RerankOptions{
			Now:     plan.Now,
			Weights: plan.RerankWeights(),
			Bias:    plan.RetrievalBias(),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("retrieve context: %w", err)
	}

	if plan.Tier.IncludeRecent <= 0 {
		return retrieved, nil
	}

	recent, err := u.repo.ListRecent(ctx, plan.Tier.IncludeRecent, port.SearchFilter{})
	if err != nil {
		return nil, fmt.Errorf("list recent: %w", err)
	}

	return mergeEntries(retrieved, recent, plan.Now), nil
}

func skipRetrieval(question string) bool {
	q := strings.ToLower(strings.TrimSpace(question))
	if q == "" {
		return true
	}

	switch q {
	case "hi", "hello", "hey", "thanks", "thank you", "ok", "okay", "yes", "no":
		return true
	}

	if strings.HasPrefix(q, "hi ") || strings.HasPrefix(q, "hello ") || strings.HasPrefix(q, "hey ") {
		return true
	}

	return false
}

func (u *Consult) executeSingleShot(ctx context.Context, plan ConsultPlan, entries []domain.MemoryEntry) (ConsultResult, error) {
	aboutMe, err := u.initialContext.Read()
	if err != nil {
		return ConsultResult{}, fmt.Errorf("read about me: %w", err)
	}

	messages := buildConsultMessages(
		buildSystemPrompt(systemPrompt, plan),
		aboutMe, plan.History,
		formatContext(entries, plan.Tier.MaxBodyRunes),
		plan.Question,
	)

	answer, err := u.llm.Chat(ctx, messages)
	if err != nil {
		return ConsultResult{}, fmt.Errorf("llm chat: %w", err)
	}

	return ConsultResult{
		Answer:         answer,
		Sources:        entriesToSources(entries),
		Tier:           string(plan.Tier.Tier),
		Generals:       plan.GeneralIDs(),
		GeneralsMethod: plan.GeneralsMethod,
		Mode:           plan.Mode.ID,
		ModeName:       plan.Mode.Name,
		ModeMethod:     plan.ModeMethod,
	}, nil
}

func mergeEntries(primary, recent []domain.MemoryEntry, now time.Time) []domain.MemoryEntry {
	seen := make(map[uuid.UUID]struct{})
	var merged []domain.MemoryEntry

	for _, e := range primary {
		if _, ok := seen[e.ID]; ok {
			continue
		}
		seen[e.ID] = struct{}{}
		merged = append(merged, e)
	}

	cutoff := now.Add(-7 * 24 * time.Hour)
	for _, e := range recent {
		if e.CreatedAt.Before(cutoff) {
			continue
		}
		if _, ok := seen[e.ID]; ok {
			continue
		}
		seen[e.ID] = struct{}{}
		merged = append(merged, e)
	}

	return merged
}

// retrievalQuery widens the search with the previous user turn, so a follow-up
// like "and the other one?" still retrieves against its actual subject.
func retrievalQuery(plan ConsultPlan) string {
	if lastUser := lastUserMessage(plan.History); lastUser != "" {
		return plan.Question + " " + lastUser
	}
	return plan.Question
}

// buildSystemPrompt assembles the base prompt, the lens instructions and the
// tier's length budget into one system message.
func buildSystemPrompt(base string, plan ConsultPlan) string {
	parts := []string{base}
	// Mode before lenses: the mode decides the shape of the answer, the lenses
	// decide the argument inside it.
	if mode := modePrompt(plan.Mode); mode != "" {
		parts = append(parts, mode)
	}
	if styles := stylesPrompt(plan.Styles); styles != "" {
		parts = append(parts, styles)
	}
	// The mode decides the shape of the answer; the lenses decide the argument
	// inside it. Passing that along stops the two imposing rival templates.
	structured := strings.TrimSpace(plan.Mode.OutputPrompt) != ""
	if lenses := generalsPrompt(plan.Generals, structured); lenses != "" {
		parts = append(parts, lenses)
	}
	if budget := strings.TrimSpace(plan.Tier.AnswerBudget); budget != "" {
		parts = append(parts, budget)
	}
	return strings.Join(parts, "\n\n")
}

func lastUserMessage(history []port.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" && strings.TrimSpace(history[i].Content) != "" {
			content := strings.TrimSpace(history[i].Content)
			if strings.HasPrefix(content, "Earlier conversation summary:") {
				continue
			}
			return content
		}
	}
	return ""
}

func formatContext(entries []domain.MemoryEntry, maxBodyRunes int) string {
	if len(entries) == 0 {
		return "(no stored memories yet)"
	}

	var b strings.Builder
	for _, e := range entries {
		date := e.CreatedAt.Format("2006-01-02")
		title := e.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&b, "[%s | %s] Title: %s\nBody: %s\n\n", date, e.Kind, title, truncateRunes(e.Body, maxBodyRunes))
	}
	return strings.TrimSpace(b.String())
}
