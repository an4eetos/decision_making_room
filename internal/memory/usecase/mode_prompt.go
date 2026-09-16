package usecase

import (
	"fmt"
	"strings"

	gendomain "github.com/an4eetos/decision-room/internal/generals/domain"
	modedomain "github.com/an4eetos/decision-room/internal/modes/domain"
)

// modePrompt renders a mode's instructions. The open mode contributes only its
// system text and no template, which is the point of having it.
func modePrompt(mode modedomain.Mode) string {
	if strings.TrimSpace(mode.SystemPrompt) == "" {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "This is a %s.\n\n", strings.ToLower(mode.Name))
	b.WriteString(strings.TrimSpace(mode.SystemPrompt))

	if out := strings.TrimSpace(mode.OutputPrompt); out != "" {
		b.WriteString("\n\nStructure the answer like this:\n\n")
		b.WriteString(out)
	}

	return b.String()
}

// stylesPrompt names the working styles a mode leans on. Styles describe how to
// execute rather than what to decide, so they colour the advice instead of being
// argued between the way generals are.
func stylesPrompt(styles []gendomain.Lens) string {
	if len(styles) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("Lean on these working styles when suggesting how to act:\n\n")
	for _, s := range styles {
		fmt.Fprintf(&b, "%s — %s\n", s.Name, s.Job)
	}
	return strings.TrimRight(b.String(), "\n")
}
