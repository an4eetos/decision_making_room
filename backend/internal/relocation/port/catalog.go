package port

import "github.com/an4eetos/decision-room/internal/relocation/domain"

// CatalogReader supplies the loaded knowledge base. It is a port so the rules
// engine and the usecases never touch a filesystem.
type CatalogReader interface {
	Catalog() domain.Catalog
}
