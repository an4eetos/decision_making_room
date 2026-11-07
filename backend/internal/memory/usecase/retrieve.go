package usecase

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

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
	// Queries are extra searches run alongside Query and fused with it. Reciprocal
	// rank fusion is variadic, so widening from two lists to eight costs nothing
	// structurally.
	Queries []string

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

// Execute retrieves and selects topK entries.
func (u *Retrieve) Execute(ctx context.Context, input RetrieveInput) ([]domain.MemoryEntry, error) {
	scored, topK, rerankOpts, err := u.candidates(ctx, input)
	if err != nil || len(scored) == 0 {
		return nil, err
	}

	rerankOpts.TopK = topK
	return service.SelectCandidates(scored, rerankOpts), nil
}

// Candidates returns every scored candidate, sorted, without selecting or
// truncating — for callers that will rerank the list themselves.
func (u *Retrieve) Candidates(ctx context.Context, input RetrieveInput) ([]service.ScoredCandidate, error) {
	scored, _, _, err := u.candidates(ctx, input)
	return scored, err
}

func (u *Retrieve) candidates(ctx context.Context, input RetrieveInput) ([]service.ScoredCandidate, int, service.RerankOptions, error) {
	query := input.Query
	topK := input.TopK
	if topK <= 0 {
		topK = u.defaultTopK
	}

	candidateLimit := input.CandidateLimit
	if candidateLimit <= 0 {
		candidateLimit = u.candidateLimit
	}

	// Only compare against vectors from the current embedding model. Mixing two
	// models produces dimensionally valid, semantically meaningless distances
	// that nothing downstream can detect.
	filter := input.Filter
	filter.EmbeddingModel = u.embedder.ModelID()

	queries := append([]string{query}, input.Queries...)

	// Every arm of every query is independent, so they all run together. On the
	// deep tier that is four queries by two arms against a 60-row pool, which
	// sequentially would be most of the answer's latency.
	type armResult struct {
		vector []domain.MemoryEntry
		text   []domain.MemoryEntry
	}
	results := make([]armResult, len(queries))

	group, groupCtx := errgroup.WithContext(ctx)

	for i, q := range queries {
		i, q := i, q

		group.Go(func() error {
			embedding, err := u.embedder.Embed(groupCtx, q)
			if err != nil {
				return fmt.Errorf("embed query: %w", err)
			}
			hits, err := u.repo.SearchSimilar(groupCtx, embedding, candidateLimit, filter)
			if err != nil {
				return fmt.Errorf("vector search: %w", err)
			}
			results[i].vector = hits
			return nil
		})

		// A query with no content words left (a greeting, or punctuation) cannot
		// match anything, so its full-text arm is skipped rather than run.
		ftsQuery, ok := service.BuildFTSQuery(q)
		if !ok {
			continue
		}
		group.Go(func() error {
			hits, err := u.repo.SearchFullText(groupCtx, port.TextQuery{
				English: ftsQuery.English,
				Simple:  ftsQuery.Simple,
				Terms:   ftsQuery.Terms,
			}, candidateLimit, input.Filter)
			if err != nil {
				return fmt.Errorf("full text search: %w", err)
			}
			results[i].text = hits
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		return nil, 0, service.RerankOptions{}, err
	}

	var (
		vectorResults []domain.MemoryEntry
		textResults   []domain.MemoryEntry
		rankedLists   [][]service.RankedEntry
	)
	for _, r := range results {
		vectorResults = append(vectorResults, r.vector...)
		textResults = append(textResults, r.text...)
		if len(r.vector) > 0 {
			rankedLists = append(rankedLists, service.ToRankedEntries(extractIDs(r.vector)))
		}
		if len(r.text) > 0 {
			rankedLists = append(rankedLists, service.ToRankedEntries(extractIDs(r.text)))
		}
	}

	if len(vectorResults) == 0 && len(textResults) == 0 {
		return nil, topK, input.Rerank, nil
	}

	// One list per arm per query. A row found by several of them rises, which is
	// the whole reason to decompose a question in the first place.
	rrfScores := service.ReciprocalRankFusion(rankedLists...)
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

	return service.ScoreCandidates(candidates, input.Rerank), topK, input.Rerank, nil
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
