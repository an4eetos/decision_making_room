package usecase

import (
	"context"
	"fmt"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
	"github.com/an4eetos/decision-room/internal/memory/service"
)

type IngestInput struct {
	Kind     domain.MemoryKind
	Title    string
	Body     string
	Tags     []string
	Metadata map[string]string
}

type IngestResult struct {
	IDs    []string `json:"ids"`
	Chunks int      `json:"chunks"`
}

type Ingest struct {
	repo     port.MemoryRepository
	embedder port.Embedder
}

func NewIngest(repo port.MemoryRepository, embedder port.Embedder) *Ingest {
	return &Ingest{repo: repo, embedder: embedder}
}

func (u *Ingest) Execute(ctx context.Context, input IngestInput) (IngestResult, error) {
	if input.Body == "" {
		return IngestResult{}, fmt.Errorf("body is required")
	}
	if input.Kind == "" {
		return IngestResult{}, fmt.Errorf("kind is required")
	}

	chunks := service.ChunkContent(input.Body, 0)
	if len(chunks) == 0 {
		return IngestResult{}, fmt.Errorf("no content to ingest")
	}

	var ids []string
	for i, chunk := range chunks {
		title := input.Title
		if len(chunks) > 1 {
			title = fmt.Sprintf("%s (part %d/%d)", input.Title, i+1, len(chunks))
		}

		// The chunk text already carries its heading breadcrumb, so the embedded
		// text is title plus breadcrumb plus body — the same string that becomes
		// the stored body and therefore the full-text vector.
		embedText := chunk.Text
		if title != "" {
			embedText = title + "\n\n" + chunk.Text
		}

		embedding, err := u.embedder.Embed(ctx, embedText)
		if err != nil {
			return IngestResult{}, fmt.Errorf("embed chunk %d: %w", i+1, err)
		}

		metadata := map[string]string{
			"chunk_index":    fmt.Sprintf("%d", i+1),
			"chunk_total":    fmt.Sprintf("%d", len(chunks)),
			"ingest_version": fmt.Sprintf("%d", service.IngestVersion),
		}
		if chunk.Breadcrumb != "" {
			metadata["breadcrumb"] = chunk.Breadcrumb
		}
		for key, value := range input.Metadata {
			metadata[key] = value
		}

		entry := domain.MemoryEntry{
			Kind:      input.Kind,
			Title:     title,
			Body:      chunk.Text,
			Tags:      input.Tags,
			Embedding: embedding,
			Metadata:  metadata,

			// Recording which model produced this vector is what makes switching
			// providers detectable. Gemini is pinned to 768 dimensions to match
			// nomic-embed-text, so a mixed table compares cleanly and means
			// nothing — Postgres will never complain.
			EmbeddingModel: u.embedder.ModelID(),
			EmbeddingDim:   len(embedding),
		}

		if err := u.repo.Save(ctx, entry); err != nil {
			return IngestResult{}, fmt.Errorf("save chunk %d: %w", i+1, err)
		}

		ids = append(ids, entry.ID.String())
	}

	return IngestResult{IDs: ids, Chunks: len(chunks)}, nil
}
