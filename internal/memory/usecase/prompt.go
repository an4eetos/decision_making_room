package usecase

import (
	"strings"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

func buildConsultMessages(
	systemPrompt, aboutMe string,
	history []port.Message,
	retrievedContext, question string,
) []port.Message {
	messages := []port.Message{
		{Role: "system", Content: systemPrompt},
	}

	contextContent := buildContextBlock(aboutMe, retrievedContext)
	if contextContent != "" {
		messages = append(messages, port.Message{Role: "user", Content: contextContent})
	}

	messages = append(messages, history...)
	messages = append(messages, port.Message{Role: "user", Content: question})

	return messages
}

func buildContextBlock(aboutMe, retrievedContext string) string {
	var b strings.Builder

	if strings.TrimSpace(aboutMe) != "" {
		b.WriteString("About me (always apply):\n\n")
		b.WriteString(strings.TrimSpace(aboutMe))
		b.WriteString("\n\n")
	}

	if strings.TrimSpace(retrievedContext) == "" {
		b.WriteString("Retrieved context:\n\n(no stored memories yet)")
	} else {
		b.WriteString("Retrieved context:\n\n")
		b.WriteString(strings.TrimSpace(retrievedContext))
	}

	return strings.TrimSpace(b.String())
}
