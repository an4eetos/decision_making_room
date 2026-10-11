package usecase

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	gendomain "github.com/an4eetos/decision-room/internal/generals/domain"
	"github.com/an4eetos/decision-room/internal/memory/port"
)

// beastModeID is the shipped beast mode. The chat keys its board off it;
// everything else asks the mode itself.
const beastModeID = "beast"

// maxBeastOrders bounds one turn's orders. Three clocks is a fight; ten is a
// to-do list, and nobody under pressure reads a to-do list.
const maxBeastOrders = 3

// Order minutes are clamped to something a clock on screen can mean. Under a
// minute is a typo; over four hours is not a crisis any more.
const (
	minOrderMinutes = 1
	maxOrderMinutes = 240
)

// beastVoice is how the shouter talks. It is in Go rather than the mode file
// because it names the shouter, who is only known once the generals are seated.
const beastVoice = `How %[1]s shouts:
- Swear like a front commander who has run out of patience, in English with Russian mat mixed in: "fuck", "goddamn", "son of a bitch", "useless bastard", "сука", "блядь", "ёб твою мать", "твою мать", "хуйня", "сволочь", "трус", "паникёр". Mat comes in bursts, not on every word. Write Cyrillic in Cyrillic.
- Threaten the way Zhukov's staff remembered him: the tribunal, the penal battalion, "I'll have you shot", relieved of command, "you will answer for this". It is theatre aimed at a soldier who is failing a task, never a real-world threat.
- Patton's contempt for cowardice: hesitation is yellow, an excuse is an excuse, a retreat nobody ordered is desertion.
- Cut them off. Demand the exact number, the hour, the name. If they dodge an order, repeat it shorter and louder.
- Every insult is pinned to the behaviour in front of you: the excuse, the stalling, the missed clock, the abandoned task.`

// beastLimits outrank the voice. The person asked to be shouted at, not to be
// demeaned for who they are, and a crisis is exactly when someone may be in
// real trouble rather than just late.
const beastLimits = `Hard limits, which outrank everything above:
- No slurs about race, ethnicity, nationality, religion, sexuality, gender or disability, in any language. Contempt for the excuse, yes; contempt for who they are, never.
- No wishes or threats of real harm, nothing telling them to hurt themselves, nothing about their body or appearance.
- If the person mentions self-harm, a medical emergency, someone in danger, or distress beyond the deadline, drop the act completely for that turn: calm, plain, direct, and point them to real help. No swearing and no timed orders on that turn.`

// pickShouter is the first seated general in the mode's ranking, falling back
// to whoever is seated first. Go picks; the model only performs. A shouter
// chosen by the model drifts to whoever it finds most fun to write.
func pickShouter(lenses []gendomain.Lens, ranking []string) (gendomain.Lens, bool) {
	for _, id := range ranking {
		for _, lens := range lenses {
			if lens.ID == id {
				return lens, true
			}
		}
	}
	if len(lenses) > 0 {
		return lenses[0], true
	}
	return gendomain.Lens{}, false
}

// beastPrompt replaces the generals' exchange in beast mode. The exchange is
// deliberation, and deliberation is what the person pulled the alarm to stop.
func beastPrompt(lenses []gendomain.Lens, ranking []string, doctrine map[string][]gendomain.Passage, brief *port.BeastBrief, conclude bool) string {
	shouter, ok := pickShouter(lenses, ranking)
	if !ok {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s is the shouter and carries the profane voice. ", shouter.Name)
	if len(lenses) > 1 {
		b.WriteString("The other seated generals are just as aggressive, clipped and impatient, but they do not swear. ")
	}
	b.WriteString("\"Frames, not characters\" does not apply in this mode: the person asked for the performance. " +
		"Speak to them directly, in the second person, as a general barks at a subordinate who is late. " +
		"Invent the abuse, not history: no made-up quotes attributed to the real figure.\n\n")

	fmt.Fprintf(&b, beastVoice, shouter.Name)
	b.WriteString("\n\n")
	b.WriteString(beastLimits)
	b.WriteString("\n\n")

	b.WriteString("Seated:\n\n")
	b.WriteString(lensBlocks(lenses, doctrine))

	if brief != nil {
		if board := beastBoardLines(brief); board != "" {
			b.WriteString("\n\n")
			b.WriteString(board)
		}
		if event := beastEventLine(brief.Event); event != "" {
			b.WriteString("\n\n")
			b.WriteString(event)
		}
	}

	b.WriteString("\n\n")
	if conclude {
		b.WriteString("This turn stands them down. Write the debrief in the template above from the board as it is. " +
			"Use the Output names in place of the placeholders. No order lines.")
		return b.String()
	}
	b.WriteString(beastOrderSyntax)
	return b.String()
}

// beastOrderSyntax is the fixed shape the board reads. Like the interrogate
// flag, a line the model writes for the room and the code cuts out — except
// these become clocks rather than disappearing.
const beastOrderSyntax = "Replace the placeholders in the template with the generals' names. Write each order as its own line, exactly:\n" +
	"`[[order: MINUTES | the physical action]]`\n" +
	"Add `| loop N` — `[[order: MINUTES | the action | loop N]]` — only to the one order whose completion means " +
	"open loop N is finished outright: the final submit, the payment sent, the thing itself. Reporting that order done " +
	"closes the loop, so never put it on a step toward it (opening the page, finding the document, a first draft). " +
	"MINUTES is a whole number, the deadline you set. One to three orders per turn, never more, " +
	"and only when there is no open task already covering it — do not re-issue a task that is on the board.\n" +
	"Only when the person's own message says that task N on the board is finished, add the line `[[done: N]]`. " +
	"Never assume it from tone, and never for a task they did not name.\n" +
	"Never mention the syntax itself."

// beastBoardLines renders the board as the generals should read it: what is
// open and how much time is left, what is late and how often, and what was
// done or abandoned.
func beastBoardLines(brief *port.BeastBrief) string {
	var b strings.Builder
	if len(brief.Tasks) > 0 {
		b.WriteString("The board:\n")
		for _, t := range brief.Tasks {
			fmt.Fprintf(&b, "- Task %d: %q, %s\n", t.Number, t.Text, beastTaskState(t, brief.Now))
		}
	} else {
		b.WriteString("The board is empty. Put the first orders on it.\n")
	}

	if len(brief.Loops) > 0 {
		b.WriteString("\nOpen loops due today or overdue, the reason they hit the alarm:\n")
		for _, l := range brief.Loops {
			fmt.Fprintf(&b, "- Loop %d: %q, %s\n", l.Number, l.Text, loopDue(l.DueAt, brief.Now))
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func beastTaskState(t port.BeastTask, now time.Time) string {
	switch t.Status {
	case "done":
		return "done"
	case "abandoned":
		return "abandoned (cowardice)"
	}

	var state string
	if t.Overdue(now) {
		state = fmt.Sprintf("OVERDUE by %s", minutesText(now.Sub(t.DeadlineAt)))
	} else {
		state = fmt.Sprintf("%s left", minutesText(t.DeadlineAt.Sub(now)))
	}
	if t.Expiries > 0 {
		state += fmt.Sprintf(", deadline missed %d×", t.Expiries)
	}
	return state
}

func minutesText(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m < 1 {
		return "under a minute"
	}
	if m == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", m)
}

func loopDue(due *time.Time, now time.Time) string {
	if due == nil {
		return "no date"
	}
	// Loops are due at the end of their day, and the brief only carries ones
	// due by tonight, so one not yet passed is due today.
	if due.After(now) {
		return "due today"
	}
	return "overdue since " + due.In(time.Local).Format("2006-01-02")
}

func beastEventLine(event port.BeastEvent) string {
	names := make([]string, 0, len(event.Tasks))
	for _, t := range event.Tasks {
		names = append(names, fmt.Sprintf("Task %d %q", t.Number, t.Text))
	}
	list := strings.Join(names, "; ")

	switch event.Kind {
	case "expired":
		return "This turn: the clock ran out on " + list + " and they did not report. " +
			"Nobody wrote to you; you are erupting on your own. Open with fury about exactly this. " +
			"The board above already shows the new, shorter deadline; say it out loud and hold them to it. " +
			"Do not re-issue the same task as a new order."
	case "report":
		return "This turn: they report done on " + list + ". Acknowledge it in one hard line, then the next order."
	default:
		return ""
	}
}

var (
	// beastOrderFlag tolerates what models do to fixed syntax: stray spaces, a
	// "min" after the number, "loop" in any case. The action cannot contain a
	// bracket or a pipe, so a bracketed phrase in prose never becomes a clock.
	beastOrderFlag = regexp.MustCompile(`(?mi)^[ \t]*(?:[-*][ \t]*)?\[\[order:[ \t]*(\d{1,4})[ \t]*(?:m|min|mins|minutes)?[ \t]*\|[ \t]*([^|\]\n]+?)[ \t]*(?:\|[ \t]*loop[ \t]*(\d{1,3})[ \t]*)?\]\][ \t]*$`)
	beastDoneFlag  = regexp.MustCompile(`(?mi)^[ \t]*\[\[done:[ \t]*([\d, \t]+)\]\][ \t]*$`)
)

// parseBeastFlags turns each order line into a readable one and removes the
// done lines, returning what both said. The order stays in the stored answer
// as a line of text, so the transcript still reads as the generals gave it.
func parseBeastFlags(answer string) (string, []port.BeastOrder, []int) {
	var orders []port.BeastOrder
	clean := beastOrderFlag.ReplaceAllStringFunc(answer, func(line string) string {
		m := beastOrderFlag.FindStringSubmatch(line)
		minutes, _ := strconv.Atoi(m[1])
		minutes = max(minOrderMinutes, min(maxOrderMinutes, minutes))
		text := strings.TrimSpace(m[2])
		if len(orders) >= maxBeastOrders || text == "" {
			return ""
		}
		loop, _ := strconv.Atoi(m[3])
		orders = append(orders, port.BeastOrder{Minutes: minutes, Text: text, Loop: loop})
		return fmt.Sprintf("- ⏱ **%d min** — %s", minutes, text)
	})

	var done []int
	for _, m := range beastDoneFlag.FindAllStringSubmatch(clean, -1) {
		for _, part := range strings.Split(m[1], ",") {
			n, err := strconv.Atoi(strings.TrimSpace(part))
			if err == nil && n > 0 && !slices.Contains(done, n) {
				done = append(done, n)
			}
		}
	}
	clean = beastDoneFlag.ReplaceAllString(clean, "")

	return strings.TrimSpace(collapseBlankLines(clean)), orders, done
}

var blankRun = regexp.MustCompile(`\n{3,}`)

// collapseBlankLines tidies the holes cut-out lines leave behind.
func collapseBlankLines(s string) string {
	return blankRun.ReplaceAllString(s, "\n\n")
}
