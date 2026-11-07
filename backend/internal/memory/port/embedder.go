package port

import "context"

type Embedder interface {
	// ModelID identifies the embedding model, stored alongside every vector so
	// a provider switch is detectable rather than silently producing
	// dimensionally valid, semantically meaningless comparisons.
	ModelID() string

	Embed(ctx context.Context, text string) ([]float32, error)
}
