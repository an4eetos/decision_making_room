package fs

import "github.com/an4eetos/decision-room/internal/relocation/domain"

// CatalogReader holds the catalogue loaded once at startup. It is immutable
// after construction, so no locking is needed on the read path.
type CatalogReader struct {
	catalog domain.Catalog
}

func NewCatalogReader(c domain.Catalog) *CatalogReader { return &CatalogReader{catalog: c} }

func (r *CatalogReader) Catalog() domain.Catalog { return r.catalog }
