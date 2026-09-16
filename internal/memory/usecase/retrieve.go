package usecase

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
	"github.com/an4eetos/decision-room/internal/memory/service"
)

type RetrieveInput struct {
	Query  string
	Filter port.SearchFilter
	TopK   int
	// CandidateLimit overrides the configured pool size per request, so a deep
	// question can widen the net without changing the deployment default.
	CandidateLimit int

	// Rerank is optional; the zero value uses the default weights and clock.
	// Callers that care about how relevance, recency and kind trade off — or
	// that need a fixed clock in a test — set it.
	Rerank service.RerankOptions
}

type Retrieve struct {
	repo           port.MemoryRepository
	embedder       port.Embedder
	candidateLimit int
	defaultTopK    int
}

func NewRetrieve(repo port.MemoryRepository, embedder port.Embedder, candidateLimit, defaultTopK int) *Retrieve {
	return &Retrieve{
		repo:           repo,
		embedder:       embedder,
		candidateLimit: candidateLimit,
		defaultTopK:    defaultTopK,
	}
}

func (u *Retrieve) Execute(ctx context.Context, input RetrieveInput) ([]domain.MemoryEntry, error) {
	query := input.Query
	topK := input.TopK
	if topK <= 0 {
		topK = u.defaultTopK
	}

	candidateLimit := input.CandidateLimit
	if candidateLimit <= 0 {
		candidateLimit = u.candidateLimit
	}

	embedding, err := u.embedder.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	vectorResults, err := u.repo.SearchSimilar(ctx, embedding, candidateLimit, input.Filter)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	// A question with no content words left (a greeting, or punctuation) cannot
	// match anything, so the full-text arm is skipped rather than run.
	var textResults []domain.MemoryEntry
	if ftsQuery, ok := service.BuildFTSQuery(query); ok {
		textResults, err = u.repo.SearchFullText(ctx, port.TextQuery{
			English: ftsQuery.English,
			Simple:  ftsQuery.Simple,
			Terms:   ftsQuery.Terms,
		}, candidateLimit, input.Filter)
		if err != nil {
			return nil, fmt.Errorf("full text search: %w", err)
		}
	}

	if len(vectorResults) == 0 && len(textResults) == 0 {
		return nil, nil
	}

	vectorRanked := service.ToRankedEntries(extractIDs(vectorResults))
	textRanked := service.ToRankedEntries(extractIDs(textResults))
	rrfScores := service.ReciprocalRankFusion(vectorRanked, textRanked)
	sortedIDs := service.SortByScore(rrfScores)

	byID, vectorScores, textScores := mergeSearchResults(vectorResults, textResults)
	candidates := make([]service.ScoredCandidate, 0, len(sortedIDs))
	for _, id := range sortedIDs {
		entry, ok := byID[id]
		if !ok {
			continue
		}
		candidates = append(candidates, service.ScoredCandidate{
			Entry:       entry,
			VectorScore: vectorScores[id],
			TextScore:   textScores[id],
			RRFScore:    rrfScores[id],
		})
	}

	rerank := input.Rerank
	rerank.TopK = topK
	return service.RerankCandidates(candidates, rerank), nil
}

func extractIDs(entries []domain.MemoryEntry) []uuid.UUID {
	ids := make([]uuid.UUID, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	return ids
}

func mergeSearchResults(
	vectorResults, textResults []domain.MemoryEntry,
) (map[uuid.UUID]domain.MemoryEntry, map[uuid.UUID]float64, map[uuid.UUID]float64) {
	byID := make(map[uuid.UUID]domain.MemoryEntry)
	vectorScores := make(map[uuid.UUID]float64)
	textScores := make(map[uuid.UUID]float64)

	for _, e := range vectorResults {
		byID[e.ID] = e
		vectorScores[e.ID] = e.Score
	}
	for _, e := range textResults {
		if existing, ok := byID[e.ID]; ok {
			e.Metadata = existing.Metadata
			e.Tags = existing.Tags
		}
		byID[e.ID] = e
		textScores[e.ID] = e.Score
	}

	return byID, vectorScores, textScores
}
