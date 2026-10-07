package service

import (
	"regexp"
	"strings"
)

// InterrogationIntel is what an interrogation turn already says in a fixed
// shape, read without a model call: the unknowns it named, the probe it ordered,
// and on the closing turn, the orders it gave.
type InterrogationIntel struct {
	// Unknowns come from "Still dark", or "Accepted dark" on the closing turn.
	Unknowns []string
	// Probe is the one thing to check against reality within 48 hours. It
	// becomes a recon order.
	Probe string
	// Orders are the closing turn's orders, without the general's name.
	Orders []string
}

// maxInterrogationUnknowns bounds the fog one turn adds. An interrogation names
// several unknowns every turn, most of them the same ones again.
const maxInterrogationUnknowns = 4

// sectionHeading matches a bold or markdown section heading the interrogation
// mode uses, with anything after it on the same line.
var sectionHeading = regexp.MustCompile(
	`(?i)^\s*(?:#{1,4}\s*)?\*\*(on the record|still dark|caught|questions|position|killed|orders|accepted dark)\*\*\s*:?\s*(.*)$`)

// attributed matches "**Name:** text" and "**Name**: text".
var attributed = regexp.MustCompile(`^\*\*([^*]+?):\*\*\s*(.*)$|^\*\*([^*]+?)\*\*:\s*(.*)$`)

// ParseInterrogation reads an interrogation answer. Anything that does not
// parse yields nothing rather than guesses.
func ParseInterrogation(answer string) InterrogationIntel {
	sections := map[string][]string{}
	current := ""
	for _, line := range strings.Split(answer, "\n") {
		if m := sectionHeading.FindStringSubmatch(line); m != nil {
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

	var intel InterrogationIntel

	for _, key := range []string{"still dark", "accepted dark"} {
		for _, line := range bullets(sections[key]) {
			if strings.Contains(strings.ToLower(line), "ready to take a position") {
				continue
			}
			intel.Unknowns = append(intel.Unknowns, line)
			if len(intel.Unknowns) == maxInterrogationUnknowns {
				break
			}
		}
	}

	for _, line := range bullets(sections["questions"]) {
		if name, text, ok := split(line); ok && strings.EqualFold(name, "probe") {
			intel.Probe = text
		}
	}

	for _, line := range bullets(sections["orders"]) {
		if _, text, ok := split(line); ok {
			line = text
		}
		if line != "" {
			intel.Orders = append(intel.Orders, line)
		}
	}

	return intel
}

// listMarker is a bullet or a number followed by a space. The space matters:
// "**Probe:**" starts with asterisks that are bold, not a bullet.
var listMarker = regexp.MustCompile(`^\s*(?:[-*•]|\d{1,3}\.)\s+`)

// bullets returns the non-empty lines of a section with list markers removed.
func bullets(lines []string) []string {
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(listMarker.ReplaceAllString(line, ""))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func split(line string) (name, text string, ok bool) {
	m := attributed.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	if m[1] != "" {
		return strings.TrimSpace(m[1]), strings.TrimSpace(m[2]), true
	}
	return strings.TrimSpace(m[3]), strings.TrimSpace(m[4]), true
}

// Overlap counts the meaningful words two texts share. It links a probe to the
// unknown it is most plainly aimed at, with no model call.
//
// Words are compared by their first five letters, a crude stem that is enough
// for "reside" to meet "residency" and "notify" to meet "notified".
func Overlap(a, b string) int {
	words := func(s string) map[string]bool {
		out := map[string]bool{}
		for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127)
		}) {
			if len([]rune(w)) <= 3 || stopWords[w] {
				continue
			}
			if r := []rune(w); len(r) > 5 {
				w = string(r[:5])
			}
			out[w] = true
		}
		return out
	}
	wa, wb := words(a), words(b)
	n := 0
	for w := range wa {
		if wb[w] {
			n++
		}
	}
	return n
}

var stopWords = map[string]bool{
	"what": true, "when": true, "which": true, "with": true, "your": true, "that": true,
	"this": true, "there": true, "their": true, "from": true, "have": true, "will": true,
	"would": true, "could": true, "about": true, "into": true, "next": true, "hours": true,
	"exact": true, "exactly": true, "check": true, "write": true, "down": true,
}
