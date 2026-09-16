// Package fs loads the roster from markdown with YAML frontmatter.
package fs

import (
	"bytes"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/an4eetos/decision-room/internal/generals/domain"
)

// Load reads the embedded roster, then overlays a directory from disk. An entry
// with an existing id replaces it; a new id is appended. That lets someone
// rewrite a general, or add their own, without forking.
func Load(builtinGenerals, builtinStyles fs.FS, overlayDir string) (domain.Roster, error) {
	generals, err := loadDir(builtinGenerals, domain.KindGeneral, "builtin")
	if err != nil {
		return domain.Roster{}, err
	}
	styles, err := loadDir(builtinStyles, domain.KindStyle, "builtin")
	if err != nil {
		return domain.Roster{}, err
	}

	if overlayDir != "" {
		if err := overlay(overlayDir, &generals, &styles); err != nil {
			return domain.Roster{}, err
		}
	}

	roster := domain.Roster{Generals: generals, Styles: styles}
	if err := validate(roster); err != nil {
		return domain.Roster{}, err
	}
	return roster, nil
}

func loadDir(fsys fs.FS, kind domain.Kind, source string) ([]domain.Lens, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read %s %s: %w", source, kind, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			names = append(names, e.Name())
		}
	}
	// Sorted so the roster order is stable between runs and between machines.
	slices.Sort(names)

	lenses := make([]domain.Lens, 0, len(names))
	for _, name := range names {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("read %s/%s: %w", source, name, err)
		}
		lens, err := parse(data, kind, source+"/"+name)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", source, name, err)
		}
		lenses = append(lenses, lens)
	}
	return lenses, nil
}

var (
	frontmatterFence = []byte("---")
	// Editors on Windows and some macOS tools prepend a byte order mark, which
	// would otherwise make the frontmatter fence fail to match.
	utf8BOM = []byte{0xEF, 0xBB, 0xBF}
)

// parse splits YAML frontmatter from the markdown body. The body is the full
// doctrine: stored, never injected into a prompt by default.
func parse(data []byte, kind domain.Kind, source string) (domain.Lens, error) {
	trimmed := bytes.TrimLeft(bytes.TrimPrefix(data, utf8BOM), " \t\r\n")
	if !bytes.HasPrefix(trimmed, frontmatterFence) {
		return domain.Lens{}, fmt.Errorf("missing YAML frontmatter")
	}

	rest := trimmed[len(frontmatterFence):]
	end := bytes.Index(rest, append([]byte("\n"), frontmatterFence...))
	if end < 0 {
		return domain.Lens{}, fmt.Errorf("unterminated YAML frontmatter")
	}

	var lens domain.Lens
	if err := yaml.Unmarshal(rest[:end], &lens); err != nil {
		return domain.Lens{}, fmt.Errorf("parse frontmatter: %w", err)
	}

	body := rest[end+len(frontmatterFence)+1:]
	if nl := bytes.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:]
	}

	lens.Kind = kind
	lens.Doctrine = strings.TrimSpace(string(body))
	lens.Source = source
	return lens, nil
}
