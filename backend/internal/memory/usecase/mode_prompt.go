package usecase

import (
	"fmt"
	"strings"

	gendomain "github.com/an4eetos/decision-room/internal/generals/domain"
	modedomain "github.com/an4eetos/decision-room/internal/modes/domain"
)

// modePrompt renders a mode's instructions. The open mode contributes only its
// system text and no template, which is the point of having it. On a closing
// turn the mode's position template replaces its output template.
func modePrompt(mode modedomain.Mode, conclude bool) string {
	if strings.TrimSpace(mode.SystemPrompt) == "" {
		return ""
	}

	var b strings.Builder
	name := strings.ToLower(mode.Name)
	article := "a"
	if strings.ContainsAny(name[:1], "aeiou") {
		article = "an"
	}
	fmt.Fprintf(&b, "This is %s %s.\n\n", article, name)
	b.WriteString(strings.TrimSpace(mode.SystemPrompt))

	out := strings.TrimSpace(mode.OutputPrompt)
	if pos := strings.TrimSpace(mode.ConcludePrompt); conclude && pos != "" {
		b.WriteString("\n\nThis turn closes it. Do not ask anything more. ")
		b.WriteString("Write the position the conversation so far has earned.")
		out = pos
	}
	if out != "" {
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
