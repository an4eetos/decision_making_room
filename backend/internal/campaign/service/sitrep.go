package service

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
)

// Thresholds for the readings. Arithmetic, so the staff argues from the same
// numbers every time and a check-in can name them without a model.
const (
	// overextendedOrders is how many open orders on one front, with no
	// ground gained since the last snapshot, read as overextension.
	overextendedOrders = 4
	// movedWindow is how far back Movement looks.
	movedWindow = 24 * time.Hour
	// lineMoved is the smallest change in a line worth reporting.
	lineMoved = 0.005
)

// Read lays the map out as the staff reads it: the theatre, the orders on
// each position by name, what moved in the last day, and the readings. prior
// is the last snapshot before today, or nil; lines are compared against it.
//
// Like Layout, it keeps proposals off: nothing on the map without agreement.
func Read(fronts []domain.Front, items []domain.Item, orders []comdomain.Commitment, prior *domain.Theatre, now time.Time) domain.Sitrep {
	s := domain.Sitrep{Day: now.Format("2006-01-02"), TakenAt: now, Fronts: []domain.SitrepFront{}, Readings: []domain.Reading{}}
	s.Moved = domain.Movement{Taken: []string{}, Cleared: []string{}, Lifted: []string{}, Withdrawn: []string{}, OrdersDone: []string{}, Lines: []domain.LineMove{}}

	theatre := Layout(fronts, items, orders, now)
	s.Totals = theatre.Totals
	if prior != nil {
		s.Since = prior.Day
	}

	byTarget := map[uuid.UUID][]domain.SitrepOrder{}
	for _, o := range orders {
		if o.TargetID == nil || o.Status == comdomain.StatusProposed || o.Status == comdomain.StatusDropped {
			continue
		}
		byTarget[*o.TargetID] = append(byTarget[*o.TargetID], order(o, now))
	}

	hasFront := len(theatre.Fronts) > 0
	for _, lane := range theatre.Fronts {
		f := domain.SitrepFront{ID: lane.ID, Name: lane.Name, Line: lane.Line, Objectives: []domain.SitrepObjective{}, Loose: []domain.SitrepPost{}}
		if p := priorFront(prior, lane.ID); p != nil {
			f.PriorLine, f.HasPrior = p.Line, true
		}
		for _, row := range lane.Rows {
			if row.Objective == nil {
				for _, post := range posts(row, byTarget, &f) {
					f.Loose = append(f.Loose, post)
				}
				continue
			}
			o := domain.SitrepObjective{TheatrePost: *row.Objective, Progress: row.Progress, Orders: ordersOf(row.Objective.ID, byTarget, &f), Opposition: []domain.SitrepPost{}, Fog: []domain.SitrepPost{}}
			for _, post := range posts(row, byTarget, &f) {
				if post.Type == domain.TypeObstacle {
					o.Opposition = append(o.Opposition, post)
				} else {
					o.Fog = append(o.Fog, post)
				}
			}
			f.Objectives = append(f.Objectives, o)
		}
		// Quiet needs a record to be quiet against: a front opened today has
		// not gone quiet, it has not started.
		moved := math.Abs(f.Line-f.PriorLine) >= lineMoved
		f.Quiet = f.HasPrior && f.OrdersOpen == 0 && !moved && hasLive(f)
		s.Fronts = append(s.Fronts, f)
	}

	s.Empty = !hasFront
	s.Moved = movement(items, orders, s.Fronts, now)
	s.Readings = readings(s)
	return s
}

// posts lays out a row's opposition and fog with their orders, counting the
// orders on the front as it goes.
func posts(row domain.TheatreRow, byTarget map[uuid.UUID][]domain.SitrepOrder, f *domain.SitrepFront) []domain.SitrepPost {
	var out []domain.SitrepPost
	for _, p := range row.Obstacles {
		out = append(out, domain.SitrepPost{TheatrePost: p, Type: domain.TypeObstacle, Orders: ordersOf(p.ID, byTarget, f)})
	}
	for _, p := range row.Unknowns {
		post := domain.SitrepPost{TheatrePost: p, Type: domain.TypeUnknown, Orders: ordersOf(p.ID, byTarget, f)}
		for _, o := range post.Orders {
			if o.Recon && !o.Done {
				post.Scouted = true
			}
		}
		out = append(out, post)
	}
	return out
}

func ordersOf(id uuid.UUID, byTarget map[uuid.UUID][]domain.SitrepOrder, f *domain.SitrepFront) []domain.SitrepOrder {
	list := byTarget[id]
	if list == nil {
		list = []domain.SitrepOrder{}
	}
	for _, o := range list {
		if o.Done {
			continue
		}
		f.OrdersOpen++
		if o.DueToday || o.Overdue {
			f.OrdersDue++
		}
	}
	return list
}

func order(c comdomain.Commitment, now time.Time) domain.SitrepOrder {
	o := domain.SitrepOrder{ID: c.ID, Text: c.Text, Recon: c.Kind == comdomain.KindRecon, Done: c.Status == comdomain.StatusDone}
	if c.DueAt != nil {
		due := c.DueAt.In(now.Location())
		o.Due = due.Format("2006-01-02")
		if !o.Done {
			o.Overdue = due.Before(now) && !sameDay(due, now)
			o.DueToday = sameDay(due, now)
		}
	}
	return o
}

func sameDay(a, b time.Time) bool {
	y1, m1, d1 := a.Date()
	y2, m2, d2 := b.Date()
	return y1 == y2 && m1 == m2 && d1 == d2
}

func hasLive(f domain.SitrepFront) bool {
	for _, o := range f.Objectives {
		if o.Status == domain.StatusActive {
			return true
		}
	}
	for _, p := range f.Loose {
		if p.Status == domain.StatusActive {
			return true
		}
	}
	return false
}

func priorFront(prior *domain.Theatre, id uuid.UUID) *domain.TheatreFront {
	if prior == nil {
		return nil
	}
	for i := range prior.Fronts {
		if prior.Fronts[i].ID == id {
			return &prior.Fronts[i]
		}
	}
	return nil
}

// movement collects what was resolved in the window, and the lines that moved.
func movement(items []domain.Item, orders []comdomain.Commitment, fronts []domain.SitrepFront, now time.Time) domain.Movement {
	m := domain.Movement{Taken: []string{}, Cleared: []string{}, Lifted: []string{}, Withdrawn: []string{}, OrdersDone: []string{}, Lines: []domain.LineMove{}}
	cutoff := now.Add(-movedWindow)
	for _, it := range items {
		if it.ResolvedAt == nil || it.ResolvedAt.Before(cutoff) {
			continue
		}
		switch {
		case it.Status == domain.StatusWithdrawn:
			m.Withdrawn = append(m.Withdrawn, it.Text)
		case it.Status != domain.StatusResolved:
		case it.Type == domain.TypeObjective:
			m.Taken = append(m.Taken, it.Text)
		case it.Type == domain.TypeObstacle:
			m.Cleared = append(m.Cleared, it.Text)
		case it.Type == domain.TypeUnknown:
			m.Lifted = append(m.Lifted, it.Text)
		}
	}
	for _, o := range orders {
		if o.Status == comdomain.StatusDone && o.ResolvedAt != nil && !o.ResolvedAt.Before(cutoff) {
			m.OrdersDone = append(m.OrdersDone, o.Text)
		}
	}
	for _, f := range fronts {
		if f.HasPrior && math.Abs(f.Line-f.PriorLine) >= lineMoved {
			m.Lines = append(m.Lines, domain.LineMove{Front: f.Name, From: f.PriorLine, To: f.Line})
		}
	}
	return m
}

// readings are what the numbers say, in a fixed order so the brief is stable
// from one read to the next: what is late first, then what is stuck, then
// what is uncertain, then what is going well.
func readings(s domain.Sitrep) []domain.Reading {
	var out []domain.Reading
	add := func(kind domain.ReadingKind, text string, front, item *uuid.UUID) {
		out = append(out, domain.Reading{Kind: kind, Text: text, FrontID: front, ItemID: item})
	}
	frontID := func(f domain.SitrepFront) *uuid.UUID {
		if f.ID == uuid.Nil {
			return nil
		}
		id := f.ID
		return &id
	}

	var (
		estimated int
		best      *domain.SitrepObjective
		bestFront string
	)
	for i := range s.Fronts {
		f := &s.Fronts[i]
		for j := range f.Objectives {
			o := &f.Objectives[j]
			if o.Status == domain.StatusActive && o.Overdue {
				add(domain.ReadingOverdue, fmt.Sprintf("◆ %s on %s is overdue (was due %s).", o.Text, f.Name, o.Due), frontID(*f), ptr(o.ID))
			}
			if o.Status == domain.StatusActive && o.Progress > 0 && o.Progress < 1 && (best == nil || o.Progress > best.Progress) {
				best, bestFront = o, f.Name
			}
		}
	}

	supply := supplyOf(s)
	for i := range s.Fronts {
		f := &s.Fronts[i]
		line := supply[f.ID]
		switch {
		case line != nil && line.Outrun:
			add(domain.ReadingOutrun, outrunText(*line, *s.Logistics), frontID(*f), nil)
		case line != nil && line.Cut:
			add(domain.ReadingCut, cutText(*line, *s.Logistics), frontID(*f), nil)
		// Overextension by count is the proxy for a front with no supply
		// to weigh its orders against. Where sorties measure it, the supply
		// decides: a front its sorties keep up with is not overextended.
		case (line == nil || line.Sorties == 0) && f.OrdersOpen >= overextendedOrders && (!f.HasPrior || math.Abs(f.Line-f.PriorLine) < lineMoved):
			since := "nothing has moved"
			if s.Since != "" {
				since = "the line has not moved since " + s.Since
			}
			add(domain.ReadingOverextended, fmt.Sprintf("%s is overextended: %d orders open and %s.", f.Name, f.OrdersOpen, since), frontID(*f), nil)
		case f.Quiet:
			since := ""
			if s.Since != "" {
				since = " since " + s.Since
			}
			add(domain.ReadingQuiet, fmt.Sprintf("%s has gone quiet: no open orders and nothing moved%s.", f.Name, since), frontID(*f), nil)
		}
	}

	// Money and deployments come after the fronts' time, and energy after
	// both: it thins every front at once.
	if supply != nil {
		for _, line := range s.Logistics.Fronts {
			if line.Overdrawn {
				add(domain.ReadingOverdrawn, overdrawnText(line), ptr(line.FrontID), nil)
			}
		}
		for _, line := range s.Logistics.Fronts {
			for _, d := range line.Deployments {
				if landing(d) {
					add(domain.ReadingDeployment, deploymentText(d, line.Front), ptr(line.FrontID), nil)
				}
			}
		}
		if s.Logistics.Energy.Low {
			add(domain.ReadingEnergy, energyText(s.Logistics.Energy), nil, nil)
		}
	}

	eachPost := func(f domain.SitrepFront, objective *domain.SitrepObjective, p domain.SitrepPost) {
		if p.Status != domain.StatusActive {
			return
		}
		if p.Type == domain.TypeUnknown {
			if !p.Scouted {
				add(domain.ReadingUnscouted, fmt.Sprintf("Fog on %s, %q, has no reconnaissance sent against it.", f.Name, p.Text), frontID(f), ptr(p.ID))
			}
			return
		}
		if p.Estimated {
			estimated++
		}
		open := 0
		for _, o := range p.Orders {
			if !o.Done {
				open++
			}
		}
		if p.Strength == domain.StrengthFortress && open == 0 {
			add(domain.ReadingFortress, fmt.Sprintf("▲ %q on %s is a fortress with nothing aimed at it: go around it, or commit to it.", p.Text, f.Name), frontID(f), ptr(p.ID))
		}
		if p.Kind == domain.KindUnwanted && objective != nil {
			add(domain.ReadingUnwanted, fmt.Sprintf("▲ %q is typed as an outcome not really wanted: a case for withdrawing ◆ %s.", p.Text, objective.Text), frontID(f), ptr(p.ID))
		}
	}
	for _, f := range s.Fronts {
		for i := range f.Objectives {
			o := &f.Objectives[i]
			for _, p := range o.Opposition {
				eachPost(f, o, p)
			}
			for _, p := range o.Fog {
				eachPost(f, o, p)
			}
		}
		for _, p := range f.Loose {
			eachPost(f, nil, p)
		}
	}

	if estimated > 0 {
		noun := "strengths are"
		if estimated == 1 {
			noun = "strength is"
		}
		add(domain.ReadingEstimated, fmt.Sprintf("%d enemy %s still an estimate nobody confirmed.", estimated, noun), nil, nil)
	}
	if best != nil {
		add(domain.ReadingAdvanced, fmt.Sprintf("◆ %s on %s is the closest to taken, %d%% held.", best.Text, bestFront, int(math.Round(best.Progress*100))), nil, ptr(best.ID))
	}
	if out == nil {
		out = []domain.Reading{}
	}
	return out
}

func ptr(id uuid.UUID) *uuid.UUID { return &id }

// Brief limits. A front with twenty objectives gets its first few; the brief
// is a map, not a dump.
const (
	briefObjectives = 8
	briefPosts      = 6
	briefOrders     = 4
	briefRunes      = 4000
)

// briefHead introduces the map to a model. The symbols match the page, so
// an answer that names "◆ Ship v2" matches what the person sees.
const briefHead = `The campaign map, as the person keeps it. ◆ is an objective (a position to take), ▲ is opposition (a problem dug in front of it, typed by its kind of stuck, with a strength from outpost to fortress; "est" is a strength nobody confirmed), ? is fog (something not known yet). A line is the ground held on that front in the last thirty days. Use it where it bears on the question and name positions by their text; do not recite the map back.`

// Brief renders a sitrep for a prompt: the map, what moved, and the
// readings. Empty on an empty map, so the room answers exactly as it would
// without one.
func Brief(s domain.Sitrep) string {
	if s.Empty {
		return ""
	}
	var b strings.Builder
	b.WriteString(briefHead)
	b.WriteString("\n")

	for _, f := range s.Fronts {
		b.WriteString("\n")
		fmt.Fprintf(&b, "Front: %s — line %s", f.Name, pct(f.Line))
		if f.HasPrior && s.Since != "" {
			fmt.Fprintf(&b, " (was %s on %s)", pct(f.PriorLine), s.Since)
		}
		if f.OrdersOpen > 0 {
			fmt.Fprintf(&b, ", %d order%s open", f.OrdersOpen, plural(f.OrdersOpen))
			if f.OrdersDue > 0 {
				fmt.Fprintf(&b, " (%d due today or overdue)", f.OrdersDue)
			}
		}
		if f.Quiet {
			b.WriteString(", quiet")
		}
		b.WriteString("\n")

		for i, o := range f.Objectives {
			if i == briefObjectives {
				fmt.Fprintf(&b, "  … and %d more objective%s\n", len(f.Objectives)-i, plural(len(f.Objectives)-i))
				break
			}
			fmt.Fprintf(&b, "  ◆ %s — %s", o.Text, objectiveState(o))
			b.WriteString("\n")
			writePosts(&b, "    ", o.Opposition, o.Fog)
			writeOrders(&b, "    ", o.Orders)
		}
		if len(f.Loose) > 0 {
			if len(f.Objectives) > 0 {
				b.WriteString("  Tied to no objective:\n")
			}
			var opp, fog []domain.SitrepPost
			for _, p := range f.Loose {
				if p.Type == domain.TypeObstacle {
					opp = append(opp, p)
				} else {
					fog = append(fog, p)
				}
			}
			writePosts(&b, "    ", opp, fog)
		}
	}

	b.WriteString("\nMoved in the last 24 hours: ")
	b.WriteString(movedLine(s.Moved))
	b.WriteString("\n")

	if len(s.Readings) > 0 {
		b.WriteString("\nStaff readings (arithmetic, not judgment):\n")
		for _, r := range s.Readings {
			fmt.Fprintf(&b, "- %s\n", r.Text)
		}
	}

	// Last, so a long map is cut here first: the readings above already
	// carry what the supply lines warn of.
	writeSupply(&b, s.Logistics)

	return truncateRunes(strings.TrimRight(b.String(), "\n"), briefRunes)
}

func objectiveState(o domain.SitrepObjective) string {
	switch o.Status {
	case domain.StatusResolved:
		return "taken"
	case domain.StatusWithdrawn:
		return "withdrawn"
	}
	parts := []string{"active"}
	if o.Due != "" {
		if o.Overdue {
			parts = append(parts, "overdue, was due "+o.Due)
		} else {
			parts = append(parts, "due "+o.Due)
		}
	}
	parts = append(parts, pct(o.Progress)+" held")
	return strings.Join(parts, ", ")
}

func writePosts(b *strings.Builder, indent string, opposition, fog []domain.SitrepPost) {
	n := 0
	for _, p := range opposition {
		if n == briefPosts {
			fmt.Fprintf(b, "%s… and more\n", indent)
			return
		}
		n++
		fmt.Fprintf(b, "%s▲ %s — %s", indent, p.Text, oppositionState(p))
		b.WriteString("\n")
		writeOrders(b, indent+"  ", p.Orders)
	}
	for _, p := range fog {
		if n == briefPosts {
			fmt.Fprintf(b, "%s… and more\n", indent)
			return
		}
		n++
		state := "fog"
		switch {
		case p.Status == domain.StatusResolved:
			state = "lifted"
		case p.Scouted:
			state = "fog, reconnaissance sent"
		default:
			state = "fog, no reconnaissance"
		}
		fmt.Fprintf(b, "%s? %s — %s\n", indent, p.Text, state)
		writeOrders(b, indent+"  ", p.Orders)
	}
}

func oppositionState(p domain.SitrepPost) string {
	if p.Status == domain.StatusResolved {
		return "cleared"
	}
	parts := []string{}
	if p.Kind != "" {
		parts = append(parts, kindLabel(p.Kind))
	}
	strength := strengthLabel(p.Strength)
	if p.Estimated {
		strength += " (est)"
	}
	parts = append(parts, strength)
	return strings.Join(parts, ", ")
}

func writeOrders(b *strings.Builder, indent string, orders []domain.SitrepOrder) {
	var open, done []string
	for _, o := range orders {
		if o.Done {
			done = append(done, o.Text)
			continue
		}
		text := o.Text
		var tags []string
		if o.Recon {
			tags = append(tags, "recon")
		}
		switch {
		case o.Overdue:
			tags = append(tags, "overdue, was due "+o.Due)
		case o.DueToday:
			tags = append(tags, "due today")
		case o.Due != "":
			tags = append(tags, "due "+o.Due)
		}
		if len(tags) > 0 {
			text += " (" + strings.Join(tags, ", ") + ")"
		}
		open = append(open, text)
	}
	if len(open) > briefOrders {
		open = append(open[:briefOrders], fmt.Sprintf("and %d more", len(open)-briefOrders))
	}
	if len(open) > 0 {
		fmt.Fprintf(b, "%sorders: %s\n", indent, strings.Join(open, "; "))
	}
	if len(done) > 0 {
		fmt.Fprintf(b, "%sdone: %d order%s\n", indent, len(done), plural(len(done)))
	}
}

func movedLine(m domain.Movement) string {
	if !m.Any() {
		return "nothing."
	}
	var parts []string
	each := func(verb string, list []string) {
		if len(list) > 0 {
			parts = append(parts, verb+" "+quoteList(list))
		}
	}
	each("taken", m.Taken)
	each("cleared", m.Cleared)
	each("lifted", m.Lifted)
	each("withdrawn", m.Withdrawn)
	each("orders done", m.OrdersDone)
	for _, l := range m.Lines {
		parts = append(parts, fmt.Sprintf("%s line %s → %s", l.Front, pct(l.From), pct(l.To)))
	}
	return strings.Join(parts, "; ") + "."
}

func quoteList(list []string) string {
	quoted := make([]string, 0, len(list))
	for i, s := range list {
		if i == 4 {
			quoted = append(quoted, fmt.Sprintf("and %d more", len(list)-i))
			break
		}
		quoted = append(quoted, fmt.Sprintf("%q", s))
	}
	return strings.Join(quoted, ", ")
}

func kindLabel(k domain.Kind) string {
	switch k {
	case domain.KindUndefined:
		return "next step undefined"
	case domain.KindWaiting:
		return "waiting on input"
	case domain.KindFear:
		return "afraid of the result"
	case domain.KindTooBig:
		return "too big to hold"
	case domain.KindUnwanted:
		return "not really wanted"
	}
	return string(k)
}

func strengthLabel(s int) string {
	switch s {
	case domain.StrengthOutpost:
		return "outpost"
	case domain.StrengthFortress:
		return "fortress"
	default:
		return "dug in"
	}
}

func pct(v float64) string {
	return fmt.Sprintf("%d%%", int(math.Round(v*100)))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
