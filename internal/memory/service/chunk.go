package service

import (
	"strings"
	"unicode/utf8"
)

// DefaultChunkSize is the rune budget a stored chunk is built to. It is exported
// because the prompt-side body cap has to match it: capping lower means paying to
// embed and store text that is then thrown away at read time.
const DefaultChunkSize = 2000

func ChunkText(text string, maxRunes int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if maxRunes <= 0 {
		maxRunes = DefaultChunkSize
	}
	if utf8.RuneCountInString(text) <= maxRunes {
		return []string{text}
	}

	var chunks []string
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{text}
	}

	var current strings.Builder
	currentRunes := 0

	for _, word := range words {
		wordRunes := utf8.RuneCountInString(word)
		spaceRunes := 0
		if current.Len() > 0 {
			spaceRunes = 1
		}

		if currentRunes+spaceRunes+wordRunes > maxRunes && current.Len() > 0 {
			chunks = append(chunks, strings.TrimSpace(current.String()))
			current.Reset()
			currentRunes = 0
			spaceRunes = 0
		}

		if current.Len() > 0 {
			current.WriteByte(' ')
			currentRunes++
		}
		current.WriteString(word)
		currentRunes += wordRunes
	}

	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}

	return chunks
}

func ParseTags(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var tags []string
	for _, p := range parts {
		tag := strings.TrimSpace(p)
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}
