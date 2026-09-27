package usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
	"github.com/an4eetos/decision-room/internal/memory/service"
)

// Deep holds the extra passes the deepest tier can afford: breaking a question
// into sub-queries, and re-ranking the merged candidates with the model.
//
// Both are optional improvements and both degrade silently. The rule throughout
// is that an extra pass must never be able to make an answer worse than not
// running it — so every failure path returns what the cheaper pipeline would
// have produced.
type Deep struct {
	llm port.LLM
}

func NewDeep(llm port.LLM) *Deep {
	return &Deep{llm: llm}
}

const decomposePrompt = `Break a question into up to 3 short search queries for a personal notes database.

Each query targets a different facet of what the person is asking. They are
keyword searches, not questions. Do not restate the original.

Return ONLY the queries, one per line, no numbering and no commentary. If the
question is already a single clear search, return nothing.`

// SubQueries returns additional search queries for a question, or nil.
func (d *Deep) SubQueries(ctx context.Context, question string) []string {
	if d == nil || d.llm == nil || strings.TrimSpace(question) == "" {
		return nil
	}

	answer, err := d.llm.Chat(ctx, []port.Message{
		{Role: "system", Content: decomposePrompt},
		{Role: "user", Content: question},
	})
	if err != nil {
		log.Printf("deep: decomposition failed, searching the question as asked: %v", err)
		return nil
	}

	return service.ParseSubQueries(answer, question)
}

const rerankPrompt = `Rank numbered notes by how useful each is for answering a question.

Return ONLY the numbers, most useful first, separated by spaces. No commentary.
Include every number you consider relevant; omit ones that are not.`

// Rerank reorders candidates by usefulness to the question. On any failure it
// returns the input untouched, which is the heuristic ordering.
func (d *Deep) Rerank(ctx context.Context, question string, candidates []service.ScoredCandidate, topK int) []service.ScoredCandidate {
	if d == nil || d.llm == nil || len(candidates) < 2 {
		return candidates
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Question: %s\n\nNotes:\n", question)
	for i, c := range candidates {
		title := c.Entry.Title
		if title == "" {
			title = "(untitled)"
		}
		// A preview, not the body: the whole point is to stay cheap enough to be
		// worth running.
		fmt.Fprintf(&b, "%d. [%s] %s — %s\n",
			i+1, c.Entry.Kind, title, truncateRunes(flatten(c.Entry.Body), rerankPreviewRunes))
	}

	answer, err := d.llm.Chat(ctx, []port.Message{
		{Role: "system", Content: rerankPrompt},
		{Role: "user", Content: b.String()},
	})
	if err != nil {
		log.Printf("deep: rerank failed, keeping heuristic order: %v", err)
		return candidates
	}

	order := service.ParseRerankOrder(answer, len(candidates))
	if len(order) == 0 {
		log.Printf("deep: rerank returned nothing usable, keeping heuristic order")
		return candidates
	}

	return service.ApplyRerankOrder(candidates, order)
}

// rerankPreviewRunes keeps the rerank prompt affordable: 60 candidates at this
// size is a few thousand tokens.
const rerankPreviewRunes = 200

// flatten collapses newlines so each candidate stays on one numbered line.
func flatten(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// DeepEntries is what a deep retrieval produced, kept separate so the caller can
// tell whether the extra passes actually ran.
type DeepEntries struct {
	Entries    []domain.MemoryEntry
	SubQueries []string
	Reranked   bool
}
