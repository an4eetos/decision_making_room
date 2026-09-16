package fs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/an4eetos/decision-room/internal/generals/domain"
)

// overlay merges a directory from disk over the embedded roster. It expects
// generals/ and styles/ subdirectories; either may be absent.
func overlay(dir string, generals, styles *[]domain.Lens) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("generals overlay %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("generals overlay %q is not a directory", dir)
	}

	for _, target := range []struct {
		sub  string
		kind domain.Kind
		into *[]domain.Lens
	}{
		{"generals", domain.KindGeneral, generals},
		{"styles", domain.KindStyle, styles},
	} {
		path := filepath.Join(dir, target.sub)
		if stat, err := os.Stat(path); err != nil || !stat.IsDir() {
			continue
		}
		extra, err := loadDir(os.DirFS(path), target.kind, path)
		if err != nil {
			return err
		}
		*target.into = merge(*target.into, extra)
	}

	return nil
}

func merge(base, extra []domain.Lens) []domain.Lens {
	index := make(map[string]int, len(base))
	for i, lens := range base {
		index[lens.ID] = i
	}
	for _, lens := range extra {
		if i, ok := index[lens.ID]; ok {
			base[i] = lens
			continue
		}
		index[lens.ID] = len(base)
		base = append(base, lens)
	}
	return base
}
