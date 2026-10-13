package service

import (
	"regexp"
	"strings"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
)

// RedTeamIntel is what a red team turn already says in a fixed shape, read
// without a model call: the attacks it named, which are the enemy's own
// estimate of your opposition, and the fog it exploits.
type RedTeamIntel struct {
	Attacks []Attack
	// Cover is the unknowns the red team used, each as the question not yet
	// answered.
	Cover []string
}

// Attack is one line of "Where the plan breaks": what it hits and how, with
// the kind of stuck the red team gave it.
type Attack struct {
	Text string
	Kind domain.Kind
}

// Bounds on what one red team turn proposes. Three attacks are the template;
// a fourth is the model padding.
const (
	maxRedTeamAttacks = 3
	maxRedTeamCover   = 4
	// maxItemRunes is the campaign item limit; an attack longer than that is
	// cut at a sentence end so the proposal is still one thing.
	maxItemRunes = 200
)

// redTeamHeading matches the red team mode's section headings, bold or as
// markdown headings, with anything after on the same line.
var redTeamHeading = regexp.MustCompile(
	`(?i)^\s*(?:#{1,4}\s*)?\*\*(enemy commander|how i hold|where the plan breaks|your fog, my cover|i concede|to beat me)\*\*\s*:?\s*(.*)$`)

// kindTag is the "(waiting)" the template asks for at the end of an attack.
var kindTag = regexp.MustCompile(`\(\s*(undefined|waiting|fear|too_big|too big|unwanted)\s*\)\s*\.?\s*$`)

// ParseRedTeam reads a red team answer. Anything that does not parse yields
// nothing rather than guesses.
func ParseRedTeam(answer string) RedTeamIntel {
	sections := map[string][]string{}
	current := ""
	for _, line := range strings.Split(answer, "\n") {
		if m := redTeamHeading.FindStringSubmatch(line); m != nil {
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

	var intel RedTeamIntel
	for _, line := range bullets(sections["where the plan breaks"]) {
		attack := Attack{Text: line}
		if m := kindTag.FindStringSubmatch(line); m != nil {
			attack.Kind = domain.Kind(strings.ReplaceAll(strings.ToLower(m[1]), " ", "_"))
			attack.Text = strings.TrimSpace(line[:len(line)-len(m[0])])
		}
		attack.Text = strings.TrimSpace(strings.TrimSuffix(attack.Text, "."))
		attack.Text = oneThing(attack.Text)
		if attack.Text == "" {
			continue
		}
		intel.Attacks = append(intel.Attacks, attack)
		if len(intel.Attacks) == maxRedTeamAttacks {
			break
		}
	}

	for _, line := range bullets(sections["your fog, my cover"]) {
		// The template asks for the question first; what they would find if
		// they looked may follow it on the same line. A line with no question
		// in it is commentary, not fog.
		i := strings.Index(line, "?")
		if i < 0 {
			continue
		}
		question := strings.TrimSpace(line[:i+1])
		if question == "" || len([]rune(question)) > maxItemRunes {
			continue
		}
		intel.Cover = append(intel.Cover, question)
		if len(intel.Cover) == maxRedTeamCover {
			break
		}
	}
	return intel
}

// oneThing cuts an attack to the item limit at the last sentence end that
// fits, so the proposal reads as one position rather than a cut-off clause.
func oneThing(text string) string {
	r := []rune(text)
	if len(r) <= maxItemRunes {
		return text
	}
	head := string(r[:maxItemRunes])
	if i := strings.LastIndexAny(head, ".;:"); i > maxItemRunes/2 {
		return strings.TrimSpace(head[:i])
	}
	return strings.TrimSpace(string(r[:maxItemRunes-1])) + "…"
}
