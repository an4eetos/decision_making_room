package service

import (
	"regexp"
	"strings"
)

// PreMortemIntel is what a pre-mortem already says in a fixed shape, read
// without a model call: the failures it worked backwards from, and the early
// warning of each when the two lists are the same length.
type PreMortemIntel struct {
	Failures []Failure
}

// Failure is one way it failed, and the warning that would have shown it
// coming when the answer paired them.
type Failure struct {
	Text    string
	Warning string
}

const maxPreMortemFailures = 3

// preMortemHeading matches the pre-mortem's section headings, bold or as
// markdown headings, with anything after on the same line.
var preMortemHeading = regexp.MustCompile(
	`(?i)^\s*(?:#{1,4}\s*)?\*\*(how it failed|early warnings|the cheapest insurance)\*\*\s*:?\s*(.*)$`)

// ParsePreMortem reads a pre-mortem answer. The cheapest insurance is not a
// prediction. Warnings are attached only when there is one per failure;
// anything else is left off rather than guessed. A section that does not
// parse yields nothing.
func ParsePreMortem(answer string) PreMortemIntel {
	sections := sectionLines(answer, preMortemHeading)
	failures := cleanLines(sections["how it failed"])
	warnings := cleanLines(sections["early warnings"])
	if len(warnings) == len(failures) {
		for i := range failures {
			failures[i].Warning = warnings[i].Text
		}
	}
	if len(failures) > maxPreMortemFailures {
		failures = failures[:maxPreMortemFailures]
	}
	return PreMortemIntel{Failures: failures}
}

func sectionLines(answer string, heading *regexp.Regexp) map[string][]string {
	sections := map[string][]string{}
	current := ""
	for _, line := range strings.Split(answer, "\n") {
		if m := heading.FindStringSubmatch(line); m != nil {
			current = strings.ToLower(m[1])
			if rest := strings.TrimSpace(strings.TrimLeft(m[2], "—–- ")); rest != "" {
				sections[current] = append(sections[current], rest)
			}
			continue
		}
		if current != "" {
			sections[current] = append(sections[current], line)
		}
	}
	return sections
}

func cleanLines(lines []string) []Failure {
	var out []Failure
	for _, line := range bullets(lines) {
		text := oneThing(strings.TrimRight(strings.TrimSpace(line), "."))
		if text == "" {
			continue
		}
		out = append(out, Failure{Text: text})
	}
	return out
}
