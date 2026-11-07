// Package port declares what the settings use cases need from storage.
package port

import (
	"context"
	"time"
)

// Stored is one row as kept: a secret's Value is sealed.
type Stored struct {
	Key       string
	Value     string
	Secret    bool
	UpdatedAt time.Time
}

type Repository interface {
	// All returns every stored setting.
	All(ctx context.Context) ([]Stored, error)
	// Save upserts and deletes in one transaction, so a PUT that changes the
	// provider and its key never lands half done.
	Save(ctx context.Context, upserts []Stored, deletes []string) error
}
