package domain

import (
	"time"

	"github.com/google/uuid"
)

type MemoryKind string

const (
	KindDecision MemoryKind = "decision"
	KindPlan     MemoryKind = "plan"
	KindNote     MemoryKind = "note"
	KindDailyLog MemoryKind = "daily_log"
)

func ParseMemoryKind(s string) (MemoryKind, bool) {
	switch MemoryKind(s) {
	case KindDecision, KindPlan, KindNote, KindDailyLog:
		return MemoryKind(s), true
	default:
		return "", false
	}
}

type MemoryEntry struct {
	ID        uuid.UUID         `json:"id"`
	Kind      MemoryKind        `json:"kind"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Tags      []string          `json:"tags"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Embedding []float32         `json:"-"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	Score     float64           `json:"score,omitempty"`
}
