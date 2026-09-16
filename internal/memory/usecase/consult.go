package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
	"github.com/an4eetos/decision-room/internal/memory/service"
)

type ConsultInput struct {
	Question string
	TopK     int
	History  []port.Message
	// Tier is "quick", "standard" or "deep". Empty uses the configured default;
	// an unrecognised value falls back rather than failing the request.
	Tier string
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
}

type Consult struct {
	agent          *AgentConsult
	repo           port.MemoryRepository
	retriever      *Retrieve
	llm            port.LLM
	initialContext port.InitialContextReader
	defaultTier    domain.Tier
	maxTier        domain.Tier
}

func NewConsult(
	repo port.MemoryRepository,
	retriever *Retrieve,
	llm port.LLM,
	toolLLM port.ToolLLM,
	initialContext port.InitialContextReader,
	tools *MemoryToolExecutor,
	defaultTier, maxTier domain.Tier,
) *Consult {
	c := &Consult{
		repo:           repo,
		retriever:      retriever,
		llm:            llm,
		initialContext: initialContext,
		defaultTier:    defaultTier,
		maxTier:        maxTier,
	}
	// Without a tool-capable model there is no agent, so no tier can use tools
	// however high the ceiling is set.
	if toolLLM != nil && tools != nil {
		c.agent = NewAgentConsult(toolLLM, tools, initialContext)
	} else {
		c.maxTier = domain.TierQuick
	}
	return c
}

const systemPrompt = `You are a personal advisor with access to the user's stored decisions, plans, and notes.
Use the retrieved context to answer. If information is missing, say so clearly.
Prefer recent decisions over older ones when they conflict.
Be concise and actionable for daily check-ins.`

func (u *Consult) Execute(ctx context.Context, input ConsultInput) (ConsultResult, error) {
	plan := ResolvePlan(input, u.defaultTier, u.maxTier)
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
	if len(context) == 0 && plan.Tier.Tier == domain.TierQuick && u.maxTier.Rank() > domain.TierQuick.Rank() {
		plan.Tier = domain.PolicyFor(domain.TierStandard)
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
		Rerank:         service.RerankOptions{Now: plan.Now},
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
		withBudget(systemPrompt, plan.Tier.AnswerBudget),
		aboutMe, plan.History,
		formatContext(entries, plan.Tier.MaxBodyRunes),
		plan.Question,
	)

	answer, err := u.llm.Chat(ctx, messages)
	if err != nil {
		return ConsultResult{}, fmt.Errorf("llm chat: %w", err)
	}

	return ConsultResult{
		Answer:  answer,
		Sources: entriesToSources(entries),
		Tier:    string(plan.Tier.Tier),
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

// withBudget appends the tier's length instruction to a system prompt.
func withBudget(prompt, budget string) string {
	if strings.TrimSpace(budget) == "" {
		return prompt
	}
	return prompt + "\n\n" + budget
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
