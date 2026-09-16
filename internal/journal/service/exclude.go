package service

import (
	"path/filepath"
	"strings"
)

// IsExcludedFromJournalSync reports files injected as initial context, not RAG chunks.
func IsExcludedFromJournalSync(relPath string) bool {
	rel := filepath.ToSlash(relPath)
	name := strings.ToLower(filepath.Base(rel))

	if name == "about-me.md" {
		return true
	}
	if strings.HasPrefix(rel, "context/") || rel == "context" {
		return true
	}

	return false
}
