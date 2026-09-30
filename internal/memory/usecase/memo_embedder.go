package usecase

import (
	"context"
	"sync"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

// MemoEmbedder remembers the last few texts it embedded. Retrieval and doctrine
// selection both embed the same question; without this the second one is a
// duplicate network call made a few milliseconds after the first.
//
// It is a small FIFO, not an LRU: the only reuse that matters is within one
// request, so recency of access buys nothing over recency of insertion.
type MemoEmbedder struct {
	inner port.Embedder
	size  int

	mu    sync.Mutex
	cache map[string][]float32
	order []string
}

func NewMemoEmbedder(inner port.Embedder, size int) *MemoEmbedder {
	if size <= 0 {
		size = 32
	}
	return &MemoEmbedder{inner: inner, size: size, cache: make(map[string][]float32, size)}
}

func (m *MemoEmbedder) ModelID() string { return m.inner.ModelID() }

func (m *MemoEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	m.mu.Lock()
	if vec, ok := m.cache[text]; ok {
		m.mu.Unlock()
		return vec, nil
	}
	m.mu.Unlock()

	// Not held across the call: two concurrent misses on one text both embed,
	// which is cheaper than serialising every embedding behind one lock.
	vec, err := m.inner.Embed(ctx, text)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.cache[text]; !ok {
		if len(m.order) >= m.size {
			delete(m.cache, m.order[0])
			m.order = m.order[1:]
		}
		m.cache[text] = vec
		m.order = append(m.order, text)
	}
	return vec, nil
}
