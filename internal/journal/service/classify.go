package service

import (
	"path/filepath"
	"strings"

	"github.com/an4eetos/decision-room/internal/memory/domain"
)

var journalExtensions = map[string]struct{}{
	".md":       {},
	".txt":      {},
	".markdown": {},
}

func IsJournalFile(name string) bool {
	_, ok := journalExtensions[strings.ToLower(filepath.Ext(name))]
	return ok
}

func ClassifyRelativePath(rel string) (domain.MemoryKind, []string) {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	if len(parts) > 1 {
		switch parts[0] {
		case "workouts":
			return domain.KindDailyLog, []string{"workout"}
		case "books":
			return domain.KindNote, []string{"book"}
		case "daily":
			return domain.KindDailyLog, []string{"daily"}
		}
	}

	return domain.KindNote, nil
}

func TitleFromPath(rel string) string {
	base := filepath.Base(rel)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
