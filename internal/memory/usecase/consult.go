package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

type ConsultInput struct {
	Question string
	TopK     int
	History  []port.Message
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
}

type Consult struct {
	agent          *AgentConsult
	repo           port.MemoryRepository
	retriever      *Retrieve
	llm            port.LLM
	initialContext port.InitialContextReader
	defaultTopK    int
	agenticEnabled bool
}

func NewConsult(
	repo port.MemoryRepository,
	retriever *Retrieve,
	llm port.LLM,
	toolLLM port.ToolLLM,
	initialContext port.InitialContextReader,
	tools *MemoryToolExecutor,
	defaultTopK int,
	agenticEnabled bool,
	agentMaxRounds int,
) *Consult {
	c := &Consult{
		repo:           repo,
		retriever:      retriever,
		llm:            llm,
		initialContext: initialContext,
		defaultTopK:    defaultTopK,
		agenticEnabled: agenticEnabled,
	}
	if agenticEnabled && toolLLM != nil && tools != nil {
		c.agent = NewAgentConsult(toolLLM, tools, initialContext, agentMaxRounds)
	}
	return c
}

const systemPrompt = `You are a personal advisor with access to the user's stored decisions, plans, and notes.
Use the retrieved context to answer. If information is missing, say so clearly.
Prefer recent decisions over older ones when they conflict.
Be concise and actionable for daily check-ins.`

func (u *Consult) Execute(ctx context.Context, input ConsultInput) (ConsultResult, error) {
	question := strings.TrimSpace(input.Question)
	if question == "" {
		return ConsultResult{}, fmt.Errorf("question is required")
	}

	if u.agent != nil {
		prefetch, err := u.prefetchContext(ctx, input, question)
		if err != nil {
			return ConsultResult{}, err
		}
		return u.agent.Execute(ctx, input, prefetch)
	}

	return u.executeSingleShot(ctx, input, question)
}

func (u *Consult) prefetchContext(ctx context.Context, input ConsultInput, question string) ([]domain.MemoryEntry, error) {
	if skipRetrieval(question) {
		return nil, nil
	}

	query := question
	if lastUser := lastUserMessage(input.History); lastUser != "" {
		query = question + " " + lastUser
	}

	topK := input.TopK
	if topK <= 0 {
		topK = u.defaultTopK
	}

	retrieved, err := u.retriever.Execute(ctx, RetrieveInput{
		Query:  query,
		TopK:   topK,
		Filter: port.SearchFilter{},
	})
	if err != nil {
		return nil, fmt.Errorf("prefetch retrieve: %w", err)
	}

	recent, err := u.repo.ListRecent(ctx, 5, port.SearchFilter{})
	if err != nil {
		return nil, fmt.Errorf("prefetch recent: %w", err)
	}

	return mergeEntries(retrieved, recent), nil
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

func (u *Consult) executeSingleShot(ctx context.Context, input ConsultInput, question string) (ConsultResult, error) {
	topK := input.TopK
	if topK <= 0 {
		topK = u.defaultTopK
	}

	retrieved, err := u.retriever.Execute(ctx, RetrieveInput{
		Query:  retrievalQuery(input, question),
		TopK:   topK,
		Filter: port.SearchFilter{},
	})
	if err != nil {
		return ConsultResult{}, fmt.Errorf("retrieve context: %w", err)
	}

	recent, err := u.repo.ListRecent(ctx, 5, port.SearchFilter{})
	if err != nil {
		return ConsultResult{}, fmt.Errorf("list recent: %w", err)
	}

	contextEntries := mergeEntries(retrieved, recent)
	contextBlock := formatContext(contextEntries)

	aboutMe, err := u.initialContext.Read()
	if err != nil {
		return ConsultResult{}, fmt.Errorf("read about me: %w", err)
	}

	messages := buildConsultMessages(systemPrompt, aboutMe, input.History, contextBlock, question)

	answer, err := u.llm.Chat(ctx, messages)
	if err != nil {
		return ConsultResult{}, fmt.Errorf("llm chat: %w", err)
	}

	sources := make([]ConsultSource, 0, len(retrieved))
	for _, e := range retrieved {
		sources = append(sources, ConsultSource{
			ID:    e.ID.String(),
			Kind:  e.Kind,
			Title: e.Title,
			Score: e.Score,
		})
	}

	return ConsultResult{Answer: answer, Sources: sources}, nil
}

func mergeEntries(primary, recent []domain.MemoryEntry) []domain.MemoryEntry {
	seen := make(map[uuid.UUID]struct{})
	var merged []domain.MemoryEntry

	for _, e := range primary {
		if _, ok := seen[e.ID]; ok {
			continue
		}
		seen[e.ID] = struct{}{}
		merged = append(merged, e)
	}

	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)
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

func retrievalQuery(input ConsultInput, question string) string {
	query := question
	if lastUser := lastUserMessage(input.History); lastUser != "" {
		query = question + " " + lastUser
	}
	return query
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

func formatContext(entries []domain.MemoryEntry) string {
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
		fmt.Fprintf(&b, "[%s | %s] Title: %s\nBody: %s\n\n", date, e.Kind, title, truncateRunes(e.Body, maxEntryBodyRunes))
	}
	return strings.TrimSpace(b.String())
}
