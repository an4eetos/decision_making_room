package usecase

import (
	"context"
	"fmt"
	"log"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

// Reindex re-embeds rows whose vectors came from a different model.
//
// Switching embedding provider is the failure this exists for: both produce
// 768-dimensional vectors, so a mixed table compares without error and returns
// confident nonsense. Postgres cannot detect it and neither can the search code.
type Reindex struct {
	repo     port.MemoryRepository
	embedder port.Embedder
}

func NewReindex(repo port.MemoryRepository, embedder port.Embedder) *Reindex {
	return &Reindex{repo: repo, embedder: embedder}
}

type ReindexResult struct {
	Model     string `json:"model"`
	Reindexed int    `json:"reindexed"`
	Remaining int    `json:"remaining"`
	Failed    int    `json:"failed"`
}

// batchSize keeps a reindex resumable: each batch is committed as it goes, so an
// interrupted run has made real progress rather than none.
const batchSize = 50

// Execute re-embeds up to max stale rows. Zero means every stale row.
func (u *Reindex) Execute(ctx context.Context, max int) (ReindexResult, error) {
	model := u.embedder.ModelID()
	result := ReindexResult{Model: model}

	for {
		remaining := batchSize
		if max > 0 {
			if done := max - result.Reindexed; done < remaining {
				remaining = done
			}
		}
		if remaining <= 0 {
			break
		}

		stale, err := u.repo.ListStale(ctx, model, remaining)
		if err != nil {
			return result, fmt.Errorf("list stale: %w", err)
		}
		if len(stale) == 0 {
			break
		}

		for _, entry := range stale {
			text := entry.Body
			if entry.Title != "" {
				text = entry.Title + "\n\n" + entry.Body
			}

			embedding, err := u.embedder.Embed(ctx, text)
			if err != nil {
				// One bad row must not abandon the run: the rest are still worth
				// fixing, and the count tells you something went wrong.
				log.Printf("reindex: embed %s failed: %v", entry.ID, err)
				result.Failed++
				continue
			}

			if err := u.repo.UpdateEmbedding(ctx, entry.ID, embedding, model); err != nil {
				return result, fmt.Errorf("update %s: %w", entry.ID, err)
			}
			result.Reindexed++
		}

		// A batch that failed entirely would otherwise loop forever on the same
		// rows, since nothing was updated and ListStale returns them again.
		if result.Failed >= len(stale) {
			break
		}
	}

	counts, err := u.repo.CountByEmbeddingModel(ctx)
	if err == nil {
		for m, n := range counts {
			if m != model {
				result.Remaining += n
			}
		}
	}

	return result, nil
}

// WarnOnMixedEmbeddings logs at startup when the table holds vectors the current
// model cannot be compared against. Loud, because the symptom otherwise is
// "search got worse" with no error anywhere.
func WarnOnMixedEmbeddings(ctx context.Context, repo port.MemoryRepository, embedder port.Embedder) {
	counts, err := repo.CountByEmbeddingModel(ctx)
	if err != nil {
		return
	}

	current := embedder.ModelID()
	stale := 0
	for model, n := range counts {
		if model != current {
			stale += n
		}
	}
	if stale == 0 {
		return
	}

	log.Printf("WARNING: %d memories were embedded with a different model than %q and are "+
		"excluded from semantic search. Run: curl -X POST localhost:8080/api/admin/reindex",
		stale, current)
}
