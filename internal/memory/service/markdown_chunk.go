package service

import (
	"strings"
	"unicode/utf8"
)

// IngestVersion changes whenever chunking changes in a way that makes existing
// chunks stale. The journal watcher compares it alongside the content hash, so
// an unchanged file is re-ingested when the chunker improves — without it, a fix
// here is invisible on every file you have already saved, and it looks like the
// fix did not work.
const IngestVersion = 2

// defaultOverlapRunes carries a little of the previous chunk into the next one,
// so a sentence split across the boundary is still retrievable from both sides.
const defaultOverlapRunes = 200

// Chunk is a piece of a document plus where it came from.
type Chunk struct {
	// Text is the chunk body, with the breadcrumb prefixed. It is prefixed
	// rather than kept separate so the heading path lands in both the embedding
	// and the generated full-text vector — a chunk that says "transactions
	// mattered more than scale" is far more findable when it also carries
	// "Database choice > Why".
	Text string
	// Breadcrumb is the heading path, for display.
	Breadcrumb string
	// Headings is that path unjoined.
	Headings []string
	Index    int
	Total    int
}

// ChunkContent splits text on markdown headings when it can, falling back to
// word chunks.
func ChunkContent(text string, maxRunes int) []Chunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxRunes <= 0 {
		maxRunes = DefaultChunkSize
	}

	chunks := chunkMarkdown(text, maxRunes)
	if len(chunks) == 0 {
		chunks = plainChunks(ChunkText(text, maxRunes))
	}

	for i := range chunks {
		chunks[i].Index = i
		chunks[i].Total = len(chunks)
	}
	return chunks
}

func plainChunks(parts []string) []Chunk {
	out := make([]Chunk, 0, len(parts))
	for _, p := range parts {
		out = append(out, Chunk{Text: p})
	}
	return out
}

type section struct {
	headings []string
	body     string
}

func chunkMarkdown(text string, maxRunes int) []Chunk {
	sections := splitSections(text)
	// A document with a single section has no structure to exploit, so word
	// chunking is the honest fallback. The previous version returned nil for
	// exactly one section, which silently sent every single-heading document
	// down the blind path — including most daily notes.
	if len(sections) == 0 {
		return nil
	}
	if len(sections) == 1 && utf8.RuneCountInString(sections[0].body) <= maxRunes {
		return []Chunk{newChunk(sections[0], sections[0].body)}
	}

	var chunks []Chunk
	for _, s := range sections {
		body := strings.TrimSpace(s.body)
		if body == "" {
			continue
		}

		if utf8.RuneCountInString(body) <= maxRunes {
			chunks = append(chunks, newChunk(s, body))
			continue
		}

		// An over-long section is split with overlap so a point made across the
		// boundary survives in both halves.
		parts := ChunkText(body, maxRunes)
		for i, part := range parts {
			if i > 0 {
				part = overlapFrom(parts[i-1], defaultOverlapRunes) + " " + part
			}
			chunks = append(chunks, newChunk(s, part))
		}
	}

	return chunks
}

func newChunk(s section, body string) Chunk {
	// Skip levels that were never present. A document starting at "##" with no
	// "#" above it has a gap in the stack, and joining blindly yields " > Notes".
	present := make([]string, 0, len(s.headings))
	for _, h := range s.headings {
		if h != "" {
			present = append(present, h)
		}
	}

	breadcrumb := strings.Join(present, " > ")
	text := body
	if breadcrumb != "" {
		text = "> " + breadcrumb + "\n\n" + body
	}
	return Chunk{
		Text:       text,
		Breadcrumb: breadcrumb,
		Headings:   present,
	}
}

// overlapFrom takes the tail of the previous chunk, snapped to a sentence
// boundary where there is one nearby, so the carried text reads as a sentence
// rather than starting mid-clause.
func overlapFrom(previous string, runes int) string {
	r := []rune(previous)
	if len(r) <= runes {
		return previous
	}

	tail := string(r[len(r)-runes:])

	// Snapping forward to a sentence start is a nicety. If the only boundary is
	// near the end of the tail, snapping would discard nearly all the overlap —
	// including, in the common case, the sentence the split was meant to
	// preserve. Keep the raw tail rather than lose it.
	for _, sep := range []string{". ", "? ", "! ", "\n"} {
		i := strings.Index(tail, sep)
		if i < 0 {
			continue
		}
		snapped := strings.TrimSpace(tail[i+len(sep):])
		if utf8.RuneCountInString(snapped)*2 >= runes {
			return snapped
		}
	}
	return strings.TrimSpace(tail)
}

// splitSections walks the document maintaining a heading stack, so each section
// knows its full path rather than only its immediate heading.
func splitSections(text string) []section {
	var (
		sections []section
		stack    []string
		current  strings.Builder
	)

	flush := func() {
		body := strings.TrimSpace(current.String())
		current.Reset()
		if body == "" {
			return
		}
		sections = append(sections, section{
			headings: append([]string(nil), stack...),
			body:     body,
		})
	}

	for _, line := range strings.Split(text, "\n") {
		level, title, ok := headerOf(line)
		if !ok {
			if current.Len() > 0 {
				current.WriteByte('\n')
			}
			current.WriteString(line)
			continue
		}

		flush()

		// Pop to the parent level, then push. This is what gives a level-three
		// heading its level-one and level-two ancestors.
		if level-1 < len(stack) {
			stack = stack[:level-1]
		}
		for len(stack) < level-1 {
			stack = append(stack, "")
		}
		stack = append(stack, title)
	}
	flush()

	return sections
}

// headerOf recognises headings at any level. The previous version listed only
// "# ", "## " and "### " as prefixes, so a level-four heading was not a split
// point and its content was glued onto its parent section.
func headerOf(line string) (level int, title string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return 0, "", false
	}

	hashes := 0
	for _, r := range trimmed {
		if r != '#' {
			break
		}
		hashes++
	}
	if hashes == 0 || hashes > 6 {
		return 0, "", false
	}

	rest := trimmed[hashes:]
	// A heading needs a space after the hashes; "#hashtag" is not one.
	if !strings.HasPrefix(rest, " ") {
		return 0, "", false
	}

	title = strings.TrimSpace(rest)
	if title == "" {
		return 0, "", false
	}
	return hashes, title, true
}
