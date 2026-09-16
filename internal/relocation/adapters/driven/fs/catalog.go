// Package fs loads the relocation knowledge base from YAML.
package fs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/an4eetos/decision-room/internal/relocation/domain"
)

// Load reads the embedded catalogue, then overlays a directory from disk if one
// is configured. An overlay entry with an existing id replaces it; a new id is
// appended. That lets someone correct a shipped item without forking the repo.
//
// Validation is strict and the error is fatal at startup. A silently dropped
// catalogue entry is a checklist that is quietly missing the one thing you
// needed, which is the failure mode this whole feature exists to prevent.
func Load(builtinItems, builtinPitfalls fs.FS, overlayDir string) (domain.Catalog, error) {
	items, err := loadItems(builtinItems, "builtin")
	if err != nil {
		return domain.Catalog{}, err
	}
	pitfalls, err := loadPitfalls(builtinPitfalls, "builtin")
	if err != nil {
		return domain.Catalog{}, err
	}

	if overlayDir != "" {
		if err := applyOverlay(overlayDir, &items, &pitfalls); err != nil {
			return domain.Catalog{}, err
		}
	}

	catalog := domain.Catalog{Items: items, Pitfalls: pitfalls}
	if err := validate(catalog); err != nil {
		return domain.Catalog{}, err
	}
	return catalog, nil
}

func applyOverlay(dir string, items *[]domain.CatalogItem, pitfalls *[]domain.CatalogPitfall) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("relocation overlay %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("relocation overlay %q is not a directory", dir)
	}

	if sub := filepath.Join(dir, "catalog"); dirExists(sub) {
		extra, err := loadItems(os.DirFS(sub), sub)
		if err != nil {
			return err
		}
		*items = mergeItems(*items, extra)
	}
	if sub := filepath.Join(dir, "pitfalls"); dirExists(sub) {
		extra, err := loadPitfalls(os.DirFS(sub), sub)
		if err != nil {
			return err
		}
		*pitfalls = mergePitfalls(*pitfalls, extra)
	}
	return nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func loadItems(fsys fs.FS, source string) ([]domain.CatalogItem, error) {
	var all []domain.CatalogItem
	err := eachYAML(fsys, source, func(name string, data []byte) error {
		var batch []domain.CatalogItem
		if err := yaml.Unmarshal(data, &batch); err != nil {
			return fmt.Errorf("%s/%s: %w", source, name, err)
		}
		all = append(all, batch...)
		return nil
	})
	return all, err
}

func loadPitfalls(fsys fs.FS, source string) ([]domain.CatalogPitfall, error) {
	var all []domain.CatalogPitfall
	err := eachYAML(fsys, source, func(name string, data []byte) error {
		var batch []domain.CatalogPitfall
		if err := yaml.Unmarshal(data, &batch); err != nil {
			return fmt.Errorf("%s/%s: %w", source, name, err)
		}
		all = append(all, batch...)
		return nil
	})
	return all, err
}

// eachYAML walks files in sorted order so the catalogue is deterministic: the
// numeric filename prefixes are what group the checklist by category.
func eachYAML(fsys fs.FS, source string, fn func(name string, data []byte) error) error {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return fmt.Errorf("read %s: %w", source, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		names = append(names, e.Name())
	}
	slices.Sort(names)

	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return fmt.Errorf("read %s/%s: %w", source, name, err)
		}
		if err := fn(name, data); err != nil {
			return err
		}
	}
	return nil
}

func mergeItems(base, overlay []domain.CatalogItem) []domain.CatalogItem {
	index := make(map[string]int, len(base))
	for i, item := range base {
		index[item.ID] = i
	}
	for _, item := range overlay {
		if i, ok := index[item.ID]; ok {
			base[i] = item
			continue
		}
		index[item.ID] = len(base)
		base = append(base, item)
	}
	return base
}

func mergePitfalls(base, overlay []domain.CatalogPitfall) []domain.CatalogPitfall {
	index := make(map[string]int, len(base))
	for i, p := range base {
		index[p.ID] = i
	}
	for _, p := range overlay {
		if i, ok := index[p.ID]; ok {
			base[i] = p
			continue
		}
		index[p.ID] = len(base)
		base = append(base, p)
	}
	return base
}
