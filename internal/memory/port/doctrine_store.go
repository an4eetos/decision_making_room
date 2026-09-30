package port

import "context"

// DoctrineVectorStore persists doctrine passage embeddings, keyed by a hash of
// the embedded text and the model that embedded it. Without it every restart
// re-embeds the whole roster, which on a free Gemini quota is a real share of
// the day's budget spent on text that has not changed.
type DoctrineVectorStore interface {
	// Load returns every stored vector for the model, by content hash.
	Load(ctx context.Context, model string) (map[string][]float32, error)
	Save(ctx context.Context, hash, model string, embedding []float32) error
	// Prune drops the model's vectors whose hash is not in keep, so an edited
	// passage does not leave its old vector behind forever.
	Prune(ctx context.Context, model string, keep []string) error
}
