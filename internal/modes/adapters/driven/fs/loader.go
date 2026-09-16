// Package fs loads conversation modes from markdown with YAML frontmatter.
package fs

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/an4eetos/decision-room/internal/modes/domain"
)

// DefaultModeID is the unstructured fallback.
const DefaultModeID = "open"

// Load reads the embedded modes, then overlays a directory from disk. A file
// whose id matches a shipped mode replaces it; a new id is appended.
func Load(builtin fs.FS, overlayDir string) (domain.Registry, error) {
	modes, err := loadDir(builtin, "builtin")
	if err != nil {
		return domain.Registry{}, err
	}

	if overlayDir != "" {
		info, statErr := os.Stat(overlayDir)
		if statErr != nil {
			return domain.Registry{}, fmt.Errorf("modes overlay %q: %w", overlayDir, statErr)
		}
		if !info.IsDir() {
			return domain.Registry{}, fmt.Errorf("modes overlay %q is not a directory", overlayDir)
		}
		extra, err := loadDir(os.DirFS(overlayDir), filepath.Clean(overlayDir))
		if err != nil {
			return domain.Registry{}, err
		}
		modes = merge(modes, extra)
	}

	registry := domain.Registry{Modes: modes}
	if err := validate(registry); err != nil {
		return domain.Registry{}, err
	}
	return registry, nil
}

func loadDir(fsys fs.FS, source string) ([]domain.Mode, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read %s modes: %w", source, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names)

	modes := make([]domain.Mode, 0, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read %s/%s: %w", source, name, err)
		}
		mode, err := parse(data, source+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", source, name, err)
		}
		modes = append(modes, mode)
	}
	return modes, nil
}

var (
	fence   = []byte("---")
	utf8BOM = []byte{0xEF, 0xBB, 0xBF}
)

func parse(data []byte, source string) (domain.Mode, error) {
	trimmed := bytes.TrimLeft(bytes.TrimPrefix(data, utf8BOM), " \t\r\n")
	if !bytes.HasPrefix(trimmed, fence) {
		return domain.Mode{}, fmt.Errorf("missing YAML frontmatter")
	}

	rest := trimmed[len(fence):]
	end := bytes.Index(rest, append([]byte("\n"), fence...))
	if end < 0 {
		return domain.Mode{}, fmt.Errorf("unterminated YAML frontmatter")
	}

	var mode domain.Mode
	if err := yaml.Unmarshal(rest[:end], &mode); err != nil {
		return domain.Mode{}, fmt.Errorf("parse frontmatter: %w", err)
	}

	body := rest[end+len(fence)+1:]
	if nl := bytes.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:]
	}

	mode.SystemPrompt = section(string(body), "## System")
	mode.OutputPrompt = section(string(body), "## Output")
	mode.Source = source
	return mode, nil
}

// section extracts the text under a heading, up to the next "## " heading.
func section(body, heading string) string {
	idx := strings.Index(body, heading)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(heading):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		rest = rest[:next]
	}
	return strings.TrimSpace(rest)
}

func merge(base, extra []domain.Mode) []domain.Mode {
	index := make(map[string]int, len(base))
	for i, m := range base {
		index[m.ID] = i
	}
	for _, m := range extra {
		if i, ok := index[m.ID]; ok {
			base[i] = m
			continue
		}
		index[m.ID] = len(base)
		base = append(base, m)
	}
	return base
}
