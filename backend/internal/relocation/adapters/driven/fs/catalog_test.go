package fs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/an4eetos/decision-room/internal/relocation/adapters/driven/fs"
	"github.com/an4eetos/decision-room/internal/relocation/assets"
	"github.com/an4eetos/decision-room/internal/relocation/domain"
)

func load(t *testing.T, overlay string) domain.Catalog {
	t.Helper()
	catalog, err := fs.Load(assets.Catalog(), assets.Pitfalls(), overlay)
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	return catalog
}

// The shipped catalogue has to survive its own validator, or the binary will not
// start.
func TestBuiltinCatalogIsValid(t *testing.T) {
	t.Parallel()

	catalog := load(t, "")
	if len(catalog.Items) < 50 {
		t.Fatalf("expected a substantial catalogue, got %d items", len(catalog.Items))
	}
	if len(catalog.Pitfalls) < 10 {
		t.Fatalf("expected a substantial pitfall list, got %d", len(catalog.Pitfalls))
	}
}

func TestOverlayReplacesAndAppends(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	catalogDir := filepath.Join(dir, "catalog")
	if err := os.MkdirAll(catalogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yaml := `
- id: bath_towel
  category: bathroom
  name: Bath towel (mine)
  unit: towel
  anchor_usd: 3
  quantity: {base: 1}
- id: espresso_machine
  category: kitchen
  name: Espresso machine
  unit: unit
  anchor_usd: 300
  quantity: {base: 1}
`
	if err := os.WriteFile(filepath.Join(catalogDir, "mine.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	catalog := load(t, dir)

	var replaced, appended bool
	count := 0
	for _, item := range catalog.Items {
		if item.ID == "bath_towel" {
			count++
			replaced = item.Name == "Bath towel (mine)"
		}
		if item.ID == "espresso_machine" {
			appended = true
		}
	}

	if !replaced {
		t.Error("overlay did not replace the shipped bath_towel")
	}
	if count != 1 {
		t.Errorf("bath_towel appears %d times; a replacement must not duplicate", count)
	}
	if !appended {
		t.Error("overlay did not append the new item")
	}
}

func TestOverlayRejectsInvalidEntries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	catalogDir := filepath.Join(dir, "catalog")
	if err := os.MkdirAll(catalogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Unknown category, no quantity rule, impossible night window.
	bad := `
- id: mystery
  category: teleportation
  name: Mystery
  when: {min_nights: 90, max_nights: 10}
`
	if err := os.WriteFile(filepath.Join(catalogDir, "bad.yaml"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.Load(assets.Catalog(), assets.Pitfalls(), dir); err == nil {
		t.Fatal("expected invalid overlay to fail loading")
	}
}

// A misspelt comfort dimension would quietly drop the item from the score, which
// reads as "fine" rather than as a mistake.
func TestOverlayRejectsUnknownComfortRule(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	catalogDir := filepath.Join(dir, "catalog")
	if err := os.MkdirAll(catalogDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := `
- id: hammock
  category: bedding
  name: Hammock
  quantity: {base: 1}
  comfort: {dimension: vibes, weight: essential}
`
	if err := os.WriteFile(filepath.Join(catalogDir, "bad.yaml"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.Load(assets.Catalog(), assets.Pitfalls(), dir); err == nil {
		t.Fatal("expected an unknown comfort dimension and weight to fail loading")
	}
}

// The essentials are what make the comfort score mean anything. Losing their
// tag in an edit would let a stay with no towel read as comfortable.
func TestBuiltinEssentialsAreCritical(t *testing.T) {
	t.Parallel()

	catalog := load(t, "")
	want := map[string]bool{"bath_towel": true, "toothbrush": true, "own_pillow": true, "bed_sheets": true}
	for _, item := range catalog.Items {
		if !want[item.ID] {
			continue
		}
		delete(want, item.ID)
		if item.Comfort == nil || item.Comfort.Weight != domain.WeightCritical {
			t.Errorf("%s should carry a critical comfort weight, got %+v", item.ID, item.Comfort)
		}
	}
	for id := range want {
		t.Errorf("%s missing from the catalogue", id)
	}
}

func TestMissingOverlayDirIsAnError(t *testing.T) {
	t.Parallel()

	if _, err := fs.Load(assets.Catalog(), assets.Pitfalls(), filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("expected a missing overlay directory to fail loudly")
	}
}
