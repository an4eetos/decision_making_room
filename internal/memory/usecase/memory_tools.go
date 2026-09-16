package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
	"github.com/an4eetos/decision-room/internal/memory/service"
)

const (
	maxToolResults = 8
	// A retrieved memory is shown to the model at full chunk length. This used to
	// be 400 while chunks are stored at 2000, so roughly 80% of every chunk was
	// embedded, indexed and stored, then silently discarded on the way into the
	// prompt.
	maxEntryBodyRunes  = service.DefaultChunkSize
	defaultRecallLimit = 6
)

type MemoryToolExecutor struct {
	retriever *Retrieve
	repo      port.MemoryRepository
}

func NewMemoryToolExecutor(retriever *Retrieve, repo port.MemoryRepository) *MemoryToolExecutor {
	return &MemoryToolExecutor{retriever: retriever, repo: repo}
}

type ToolExecutionResult struct {
	Content string
	Entries []domain.MemoryEntry
}

// Execute takes the tier policy so a recall made inside a deep answer searches
// as widely as the surrounding request, rather than at a fixed default.
func (e *MemoryToolExecutor) Execute(ctx context.Context, name string, args map[string]any, policy domain.TierPolicy) (ToolExecutionResult, error) {
	switch name {
	case "recall_memories":
		return e.recallMemories(ctx, args, policy)
	default:
		return ToolExecutionResult{}, fmt.Errorf("unknown tool: %s", name)
	}
}

func (e *MemoryToolExecutor) recallMemories(ctx context.Context, args map[string]any, policy domain.TierPolicy) (ToolExecutionResult, error) {
	query := strings.TrimSpace(stringArg(args, "query"))
	if query == "" {
		return ToolExecutionResult{}, fmt.Errorf("query is required")
	}

	limit := intArg(args, "limit", defaultRecallLimit)
	if limit > maxToolResults {
		limit = maxToolResults
	}

	filter, err := buildFilter(args)
	if err != nil {
		return ToolExecutionResult{}, err
	}

	entries, err := e.retriever.Execute(ctx, RetrieveInput{
		Query:          query,
		Filter:         filter,
		TopK:           limit,
		CandidateLimit: policy.CandidateLimit,
	})
	if err != nil {
		return ToolExecutionResult{}, err
	}

	includeRecent := boolArg(args, "include_recent", true) && policy.IncludeRecent > 0
	if includeRecent {
		recent, err := e.repo.ListRecent(ctx, policy.IncludeRecent, filter)
		if err != nil {
			return ToolExecutionResult{}, err
		}
		entries = mergeSourceEntries(entries, recent)
		if len(entries) > maxToolResults {
			entries = entries[:maxToolResults]
		}
	}

	return ToolExecutionResult{
		Content: formatToolEntries(entries, policy.MaxBodyRunes),
		Entries: entries,
	}, nil
}

func buildFilter(args map[string]any) (port.SearchFilter, error) {
	filter := port.SearchFilter{}

	if kindRaw := stringArg(args, "kind"); kindRaw != "" {
		kind, ok := domain.ParseMemoryKind(kindRaw)
		if !ok {
			return port.SearchFilter{}, fmt.Errorf("invalid kind: %s", kindRaw)
		}
		filter.Kind = &kind
	}

	if tagsRaw, ok := args["tags"]; ok {
		filter.Tags = parseTagsArg(tagsRaw)
	}

	return filter, nil
}

func parseTagsArg(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		var tags []string
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				tags = append(tags, strings.TrimSpace(s))
			}
		}
		return tags
	default:
		return nil
	}
}

func stringArg(args map[string]any, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func intArg(args map[string]any, key string, fallback int) int {
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	switch t := v.(type) {
	case float64:
		if t <= 0 {
			return fallback
		}
		return int(t)
	case int:
		if t <= 0 {
			return fallback
		}
		return t
	case json.Number:
		n, err := t.Int64()
		if err != nil || n <= 0 {
			return fallback
		}
		return int(n)
	default:
		return fallback
	}
}

func boolArg(args map[string]any, key string, fallback bool) bool {
	v, ok := args[key]
	if !ok || v == nil {
		return fallback
	}
	switch t := v.(type) {
	case bool:
		return t
	default:
		return fallback
	}
}

func formatToolEntries(entries []domain.MemoryEntry, maxBodyRunes int) string {
	if len(entries) == 0 {
		return "(no matching memories)"
	}

	var b strings.Builder
	for _, e := range entries {
		date := e.CreatedAt.Format("2006-01-02")
		title := e.Title
		if title == "" {
			title = "(untitled)"
		}
		score := ""
		if e.Score > 0 {
			score = fmt.Sprintf(" score=%.3f", e.Score)
		}
		body := truncateRunes(e.Body, maxBodyRunes)
		fmt.Fprintf(&b, "[%s | %s%s] Title: %s\nBody: %s\n\n", date, e.Kind, score, title, body)
	}
	return strings.TrimSpace(b.String())
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

func entriesToSources(entries []domain.MemoryEntry) []ConsultSource {
	seen := make(map[string]struct{})
	var sources []ConsultSource
	for _, e := range entries {
		id := e.ID.String()
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		sources = append(sources, ConsultSource{
			ID:    id,
			Kind:  e.Kind,
			Title: e.Title,
			Score: e.Score,
		})
	}
	return sources
}

func mergeSourceEntries(existing []domain.MemoryEntry, added []domain.MemoryEntry) []domain.MemoryEntry {
	seen := make(map[string]struct{})
	var merged []domain.MemoryEntry
	for _, e := range append(existing, added...) {
		id := e.ID.String()
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		merged = append(merged, e)
	}
	return merged
}
