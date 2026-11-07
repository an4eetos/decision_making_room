package domain

import (
	"strings"
	"unicode/utf8"
)

// MaxPassageRunes bounds one doctrine passage. A passage is the unit that
// reaches a prompt, so this is the price of a lens's doctrine on a cheap tier:
// about 225 tokens. Long sections split at paragraph breaks to stay under it.
const MaxPassageRunes = 900

// Passage is one retrievable piece of a lens's doctrine: a section, or part of
// one when the section is long.
type Passage struct {
	LensID   string
	LensName string
	// Section is the "## " heading the passage sits under, empty for text
	// before the first heading.
	Section string
	Text    string
}

// EmbedText is what gets embedded. The lens name and section ride along so
// "Zhukov — Where it broke" is findable by a question about failure even when
// the paragraph itself never uses the word.
func (p Passage) EmbedText() string {
	if p.Section == "" {
		return p.LensName + "\n\n" + p.Text
	}
	return p.LensName + " — " + p.Section + "\n\n" + p.Text
}

// Passages splits the doctrine into sections, and long sections into groups of
// whole paragraphs. It never cuts inside a paragraph: half a thought is worse in
// a prompt than a passage that runs a little long.
func (l Lens) Passages() []Passage {
	var out []Passage
	for _, sec := range splitSections(l.Doctrine) {
		for _, text := range groupParagraphs(sec.body, MaxPassageRunes) {
			out = append(out, Passage{LensID: l.ID, LensName: l.Name, Section: sec.heading, Text: text})
		}
	}
	return out
}

type section struct {
	heading string
	body    string
}

func splitSections(doctrine string) []section {
	var (
		out     []section
		heading string
		body    strings.Builder
	)
	flush := func() {
		if text := strings.TrimSpace(body.String()); text != "" {
			out = append(out, section{heading: heading, body: text})
		}
		body.Reset()
	}

	for _, line := range strings.Split(doctrine, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			heading = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		body.WriteString(line)
		body.WriteString("\n")
	}
	flush()
	return out
}

// groupParagraphs packs consecutive paragraphs into chunks of at most maxRunes.
// A single paragraph longer than that stays whole.
func groupParagraphs(body string, maxRunes int) []string {
	var (
		out     []string
		current []string
		size    int
	)
	for _, para := range strings.Split(body, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		n := utf8.RuneCountInString(para)
		if len(current) > 0 && size+2+n > maxRunes {
			out = append(out, strings.Join(current, "\n\n"))
			current, size = nil, 0
		}
		if len(current) > 0 {
			size += 2
		}
		current = append(current, para)
		size += n
	}
	if len(current) > 0 {
		out = append(out, strings.Join(current, "\n\n"))
	}
	return out
}
