package service

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const initialContextSeparator = "\n\n---\n\n"

// LoadInitialContext reads about-me plus all markdown files under contextDir.
// maxRunes 0 means no limit.
func LoadInitialContext(aboutMeFile, contextDir string, maxRunes int) (string, error) {
	var parts []string

	if aboutMeFile != "" {
		content, err := readMarkdownFile(aboutMeFile)
		if err != nil {
			return "", err
		}
		if content != "" {
			parts = append(parts, content)
		}
	}

	dirParts, err := loadContextDir(contextDir)
	if err != nil {
		return "", err
	}
	parts = append(parts, dirParts...)

	combined := strings.Join(parts, initialContextSeparator)
	if maxRunes > 0 {
		combined = truncateRunes(combined, maxRunes)
	}

	return combined, nil
}

func loadContextDir(contextDir string) ([]string, error) {
	if strings.TrimSpace(contextDir) == "" {
		return nil, nil
	}

	info, err := os.Stat(contextDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat context dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("context dir is not a directory: %s", contextDir)
	}

	var paths []string
	err = filepath.WalkDir(contextDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !IsMarkdownFile(d.Name()) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk context dir: %w", err)
	}

	sort.Strings(paths)

	var parts []string
	for _, path := range paths {
		content, err := readMarkdownFile(path)
		if err != nil {
			return nil, err
		}
		if content != "" {
			parts = append(parts, content)
		}
	}

	return parts, nil
}

func readMarkdownFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return strings.TrimSpace(string(content)), nil
}

func IsMarkdownFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".txt", ".markdown":
		return true
	default:
		return false
	}
}

func truncateRunes(text string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(text) <= maxRunes {
		return text
	}

	runes := []rune(text)
	return string(runes[:maxRunes])
}
