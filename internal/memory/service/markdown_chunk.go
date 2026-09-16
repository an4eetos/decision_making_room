package service

import (
	"strings"
	"unicode/utf8"
)

var markdownHeaderPrefixes = []string{"### ", "## ", "# "}

// ChunkContent splits text using markdown sections when possible, otherwise word chunks.
func ChunkContent(text string, maxRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxRunes <= 0 {
		maxRunes = defaultChunkSize
	}

	if chunks := chunkMarkdown(text, maxRunes); len(chunks) > 0 {
		return chunks
	}

	return ChunkText(text, maxRunes)
}

func chunkMarkdown(text string, maxRunes int) []string {
	if !strings.Contains(text, "#") {
		return nil
	}

	lines := strings.Split(text, "\n")
	var sections []string
	var current strings.Builder

	flush := func() {
		if current.Len() == 0 {
			return
		}
		sections = append(sections, strings.TrimSpace(current.String()))
		current.Reset()
	}

	for _, line := range lines {
		if isMarkdownHeader(line) && current.Len() > 0 {
			flush()
		}
		if current.Len() > 0 {
			current.WriteByte('\n')
		}
		current.WriteString(line)
	}
	flush()

	if len(sections) <= 1 {
		return nil
	}

	var chunks []string
	for _, section := range sections {
		if section == "" {
			continue
		}
		if utf8.RuneCountInString(section) <= maxRunes {
			chunks = append(chunks, section)
			continue
		}
		chunks = append(chunks, ChunkText(section, maxRunes)...)
	}

	if len(chunks) == 0 {
		return nil
	}

	return chunks
}

func isMarkdownHeader(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	for _, prefix := range markdownHeaderPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}
