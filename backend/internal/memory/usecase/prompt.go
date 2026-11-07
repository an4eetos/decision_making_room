package usecase

import (
	"strings"

	"github.com/an4eetos/decision-room/internal/memory/port"
)

// buildConsultMessages lays out one call: the system prompt, then what is
// always true about the person (about me, and the campaign map when there is
// one) with what retrieval found, then the conversation, then the question.
func buildConsultMessages(
	systemPrompt, aboutMe, situation string,
	history []port.Message,
	retrievedContext, question string,
) []port.Message {
	messages := []port.Message{
		{Role: "system", Content: systemPrompt},
	}

	contextContent := buildContextBlock(aboutMe, situation, retrievedContext)
	if contextContent != "" {
		messages = append(messages, port.Message{Role: "user", Content: contextContent})
	}

	messages = append(messages, history...)
	messages = append(messages, port.Message{Role: "user", Content: question})

	return messages
}

// buildContextBlock puts the map between about-me and the retrieved
// memories: it is the person's own record of what they are working toward,
// so it travels as their context, never as an instruction. Empty when the
// map is empty, which leaves the block exactly as it was before v2.
func buildContextBlock(aboutMe, situation, retrievedContext string) string {
	var b strings.Builder

	if strings.TrimSpace(aboutMe) != "" {
		b.WriteString("About me (always apply):\n\n")
		b.WriteString(strings.TrimSpace(aboutMe))
		b.WriteString("\n\n")
	}

	if strings.TrimSpace(situation) != "" {
		b.WriteString("The campaign map (what I am working toward, as I keep it):\n\n")
		b.WriteString(strings.TrimSpace(situation))
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
