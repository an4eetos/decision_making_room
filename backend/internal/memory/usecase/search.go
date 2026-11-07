package usecase

import (
	"context"
	"strings"

	"github.com/an4eetos/decision-room/internal/memory/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

type SearchInput struct {
	Query string
	Kind  *domain.MemoryKind
	Tags  []string
	Limit int
}

type Search struct {
	repo      port.MemoryRepository
	retriever *Retrieve
}

func NewSearch(repo port.MemoryRepository, retriever *Retrieve) *Search {
	return &Search{repo: repo, retriever: retriever}
}

func (u *Search) Execute(ctx context.Context, input SearchInput) ([]domain.MemoryEntry, error) {
	limit := input.Limit
	if limit <= 0 {
		limit = 20
	}

	filter := port.SearchFilter{Kind: input.Kind, Tags: input.Tags}

	query := strings.TrimSpace(input.Query)
	if query == "" {
		return u.repo.ListRecent(ctx, limit, filter)
	}

	return u.retriever.Execute(ctx, RetrieveInput{
		Query:  query,
		Filter: filter,
		TopK:   limit,
	})
}
