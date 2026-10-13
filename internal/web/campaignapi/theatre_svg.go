package campaignapi

import (
	"fmt"
	"html"
	"strings"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
)

// The theatre map is drawn server side as one SVG, so it is the same picture
// in the page, opened on its own, saved, or replayed from a snapshot. Left is
// your side; the objective is the diamond at the right; the opposition and
// the fog sit on the ground ahead of the front line, and the line advances as
// they clear. No script, no model: a view of the data, nothing more.
//
// Colours are the war room palette, repeated here so the file stands alone.
const (
	svgWidth   = 960.0
	svgPadX    = 16.0
	trackX0    = 262.0
	trackX1    = 900.0
	rowH       = 30.0
	laneHeadH  = 34.0
	laneFootH  = 10.0
	laneGap    = 10.0
	legendH    = 40.0
	labelRunes = 36
	maxArrows  = 4

	colBg        = "#121210"
	colPanel     = "#1a1a17"
	colLine      = "#3a3934"
	colLineSoft  = "#2a2925"
	colText      = "#e8e5da"
	colText2     = "#bdbaad"
	colMuted     = "#8b897f"
	colFaint     = "#605e56"
	colAmber     = "#e3a33b"
	colAmberLine = "#8a6526"
	colAmberText = "#f2c776"
	colRed       = "#e5534b"
	colRedSoft   = "#33191a"
	colGreen     = "#8fb86a"
	colFog       = "#7d9cc0"
	colFogSoft   = "#141a21"
	colFogText   = "#bcd0e6"
)

// RenderSVG draws a theatre. With a prior theatre it also draws, dashed,
// where each front line stood then, so the map shows what moved.
func RenderSVG(t domain.Theatre, prior *domain.Theatre) []byte {
	var b strings.Builder

	if len(t.Fronts) == 0 {
		openSVG(&b, 96)
		fmt.Fprintf(&b, `<text class="tm-empty" x="%.1f" y="52" text-anchor="middle">Nothing on the map yet. Open a front and put an objective on it.</text>`, svgWidth/2)
		b.WriteString("</svg>\n")
		return []byte(b.String())
	}

	height := 8.0
	for _, f := range t.Fronts {
		height += laneHeadH + rowH*float64(max(len(f.Rows), 1)) + laneFootH + laneGap
	}
	height += legendH
	openSVG(&b, height)

	y := 8.0
	for _, f := range t.Fronts {
		y = drawLane(&b, f, priorLane(prior, f.ID), y)
	}
	drawLegend(&b, y+6)

	b.WriteString("</svg>\n")
	return []byte(b.String())
}

func openSVG(b *strings.Builder, height float64) {
	fmt.Fprintf(b, `<svg xmlns="http://www.w3.org/2000/svg" class="theatre-map" viewBox="0 0 %.0f %.0f" width="100%%" role="img" aria-label="Theatre map">`, svgWidth, height)
	b.WriteString("\n<style>")
	fmt.Fprintf(b, `.theatre-map{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;font-size:12px;background:%s}`, colBg)
	fmt.Fprintf(b, `.theatre-map .tm-lane{fill:%s;stroke:%s}`, colPanel, colLine)
	fmt.Fprintf(b, `.theatre-map .tm-front{font-family:ui-monospace,"SF Mono",Menlo,monospace;font-size:11px;letter-spacing:.16em;text-transform:uppercase;fill:%s}`, colAmberText)
	fmt.Fprintf(b, `.theatre-map .tm-held{font-family:ui-monospace,"SF Mono",Menlo,monospace;font-size:11px;fill:%s}`, colMuted)
	fmt.Fprintf(b, `.theatre-map .tm-label{fill:%s}.theatre-map .tm-label.taken{fill:%s}.theatre-map .tm-label.withdrawn{fill:%s;text-decoration:line-through}.theatre-map .tm-label.loose{fill:%s;font-style:italic}`, colText2, colGreen, colFaint, colMuted)
	fmt.Fprintf(b, `.theatre-map .tm-ahead{stroke:%s;stroke-width:1;stroke-dasharray:3 4}`, colLineSoft)
	fmt.Fprintf(b, `.theatre-map .tm-ground{stroke:%s;stroke-width:3;stroke-linecap:round}`, colAmberLine)
	fmt.Fprintf(b, `.theatre-map .tm-frontline{fill:none;stroke:%s;stroke-width:2;stroke-linejoin:round}`, colText)
	fmt.Fprintf(b, `.theatre-map .tm-prior{fill:none;stroke:%s;stroke-width:1.5;stroke-dasharray:4 3}`, colMuted)
	fmt.Fprintf(b, `.theatre-map .tm-objective{fill:%s;stroke:%s;stroke-width:1}.theatre-map .tm-objective.overdue{stroke:%s;stroke-width:2}.theatre-map .tm-objective.taken{fill:%s;stroke:%s}.theatre-map .tm-objective.withdrawn{fill:none;stroke:%s}`, colAmber, colAmberLine, colRed, colGreen, colGreen, colFaint)
	fmt.Fprintf(b, `.theatre-map .tm-enemy{fill:%s;stroke:%s;stroke-width:1}.theatre-map .tm-enemy.est{fill:%s;stroke:%s;stroke-dasharray:2 2}.theatre-map .tm-enemy.cleared{fill:none;stroke:%s;stroke-dasharray:none}`, colRed, colRedSoft, colRedSoft, colRed, colFaint)
	fmt.Fprintf(b, `.theatre-map .tm-fog{fill:%s;stroke:%s;stroke-width:1}.theatre-map .tm-fog.lifted{stroke:%s}.theatre-map .tm-fogmark{font-size:10px;font-weight:700;fill:%s}.theatre-map .tm-fog-lifted-mark{fill:%s}`, colFogSoft, colFog, colFaint, colFogText, colFaint)
	fmt.Fprintf(b, `.theatre-map .tm-order{fill:none;stroke:%s;stroke-width:1.5;stroke-linecap:round}`, colAmberText)
	fmt.Fprintf(b, `.theatre-map .tm-legend{fill:%s;font-size:11px}.theatre-map .tm-empty{fill:%s;font-size:13px}`, colMuted, colMuted)
	b.WriteString("</style>\n")
}

// priorLane finds the same front in the earlier theatre.
func priorLane(prior *domain.Theatre, id uuid.UUID) *domain.TheatreFront {
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

// priorProgress finds the same row — by objective, or the loose row — in the
// earlier lane.
func priorProgress(lane *domain.TheatreFront, row domain.TheatreRow) (float64, bool) {
	if lane == nil {
		return 0, false
	}
	for _, r := range lane.Rows {
		switch {
		case row.Objective == nil && r.Objective == nil:
			return r.Progress, true
		case row.Objective != nil && r.Objective != nil && r.Objective.ID == row.Objective.ID:
			return r.Progress, true
		}
	}
	return 0, false
}

func drawLane(b *strings.Builder, f domain.TheatreFront, prior *domain.TheatreFront, top float64) float64 {
	rows := max(len(f.Rows), 1)
	height := laneHeadH + rowH*float64(rows) + laneFootH
	fmt.Fprintf(b, `<g class="tm-lane-g" data-front="%s">`+"\n", f.ID)
	fmt.Fprintf(b, `<rect class="tm-lane" x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="6"/>`+"\n", svgPadX/2, top, svgWidth-svgPadX, height)
	fmt.Fprintf(b, `<text class="tm-front" x="%.1f" y="%.1f">%s</text>`+"\n", svgPadX+8, top+21, esc(f.Name))

	held := fmt.Sprintf("%d%% held", pct(f.Line))
	if prior != nil && pct(prior.Line) != pct(f.Line) {
		held += fmt.Sprintf(" · was %d%%", pct(prior.Line))
	}
	fmt.Fprintf(b, `<text class="tm-held" x="%.1f" y="%.1f" text-anchor="end">%s</text>`+"\n", trackX1, top+21, held)

	if len(f.Rows) == 0 {
		fmt.Fprintf(b, `<text class="tm-label loose" x="%.1f" y="%.1f">Quiet front. Nothing on it yet.</text>`+"\n", svgPadX+8, top+laneHeadH+rowH/2+4)
		b.WriteString("</g>\n")
		return top + height + laneGap
	}

	// The front line steps from row to row like a contour, and breaks at a
	// withdrawn row: there is no line through ground you chose to leave.
	var line, was contour
	for i, row := range f.Rows {
		y := top + laneHeadH + rowH*float64(i) + rowH/2
		xp := drawRow(b, row, y)
		if row.Objective != nil && row.Objective.Status == domain.StatusWithdrawn {
			line.gap()
			was.gap()
			continue
		}
		line.add(xp, y)
		if p, ok := priorProgress(prior, row); ok {
			was.add(headX(p), y)
		} else {
			was.gap()
		}
	}
	was.write(b, "tm-prior", "Front line a week ago")
	line.write(b, "tm-frontline", "Front line")
	b.WriteString("</g>\n")
	return top + height + laneGap
}

// contour collects a front line's points, one per row, and writes it as
// stepped polylines: down a row's tick, across between rows, down the next.
type contour struct {
	runs [][][2]float64
	open bool
}

func (c *contour) add(x, y float64) {
	if !c.open {
		c.runs = append(c.runs, nil)
		c.open = true
	}
	c.runs[len(c.runs)-1] = append(c.runs[len(c.runs)-1], [2]float64{x, y})
}

func (c *contour) gap() { c.open = false }

func (c *contour) write(b *strings.Builder, class, title string) {
	for _, run := range c.runs {
		var points strings.Builder
		for i, p := range run {
			x, y := p[0], p[1]
			if i > 0 {
				prev := run[i-1]
				mid := (prev[1] + y) / 2
				fmt.Fprintf(&points, "%.1f,%.1f %.1f,%.1f ", prev[0], mid, x, mid)
			}
			fmt.Fprintf(&points, "%.1f,%.1f %.1f,%.1f ", x, y-11, x, y+11)
		}
		fmt.Fprintf(b, `<polyline class="%s" points="%s"><title>%s</title></polyline>`+"\n", class, strings.TrimSpace(points.String()), esc(title))
	}
}

// headX is where a row's front line stands for a given progress, leaving the
// diamond clear at the far end.
func headX(progress float64) float64 {
	return trackX0 + progress*(trackX1-16-trackX0)
}

// drawRow draws one row and returns the x of its front line.
func drawRow(b *strings.Builder, row domain.TheatreRow, y float64) float64 {
	xp := headX(row.Progress)
	withdrawn := row.Objective != nil && row.Objective.Status == domain.StatusWithdrawn

	// Label.
	class, label := "", "Not tied to an objective"
	if row.Objective == nil {
		class = "loose"
	} else {
		label = row.Objective.Text
		switch row.Objective.Status {
		case domain.StatusResolved:
			class = "taken"
		case domain.StatusWithdrawn:
			class = "withdrawn"
		}
	}
	fmt.Fprintf(b, `<text class="tm-label %s" x="%.1f" y="%.1f"><title>%s</title>%s</text>`+"\n",
		class, svgPadX+8, y+4, esc(label), esc(truncate(label, labelRunes)))

	// Ground: what lies ahead, dashed; what is held, solid.
	fmt.Fprintf(b, `<line class="tm-ahead" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`+"\n", trackX0, y, trackX1-10, y)
	if row.Progress > 0 && !withdrawn {
		fmt.Fprintf(b, `<line class="tm-ground" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`+"\n", trackX0, y, xp, y)
	}

	// Posts: the opposition and fog still standing go ahead of the line, what
	// cleared or lifted sits behind it.
	var ahead, behind []domain.TheatrePost
	for _, p := range row.Obstacles {
		if p.Status == domain.StatusResolved {
			behind = append(behind, p)
		} else {
			ahead = append(ahead, p)
		}
	}
	for _, p := range row.Unknowns {
		if p.Status == domain.StatusResolved {
			behind = append(behind, p)
		} else {
			ahead = append(ahead, p)
		}
	}
	for i, x := range spread(xp+14, trackX1-22, len(ahead)) {
		drawPost(b, ahead[i], x, y)
	}
	behindEnd := max(xp-10, trackX0+8+12*float64(len(behind)))
	for i, x := range spread(trackX0+6, behindEnd, len(behind)) {
		drawPost(b, behind[i], x, y)
	}

	// Open orders: chevrons at the head of the line, pointing at the objective.
	if n := row.Orders.Open; n > 0 && !withdrawn {
		title := fmt.Sprintf("%d open order%s", n, plural(n))
		if row.Orders.Recon > 0 {
			title += fmt.Sprintf(", %d reconnaissance", row.Orders.Recon)
		}
		if row.Orders.Done > 0 {
			title += fmt.Sprintf("; %d carried out", row.Orders.Done)
		}
		b.WriteString(`<g class="tm-orders"><title>` + esc(title) + `</title>`)
		for i := 0; i < min(n, maxArrows); i++ {
			x := xp + 3 + float64(i)*6
			fmt.Fprintf(b, `<polyline class="tm-order" points="%.1f,%.1f %.1f,%.1f %.1f,%.1f"/>`, x, y-9, x+3, y-6, x, y-3)
		}
		b.WriteString("</g>\n")
	}

	// The objective, a diamond at the far end.
	if o := row.Objective; o != nil {
		class := ""
		switch {
		case o.Status == domain.StatusResolved:
			class = "taken"
		case o.Status == domain.StatusWithdrawn:
			class = "withdrawn"
		case o.Overdue:
			class = "overdue"
		}
		title := o.Text
		switch {
		case o.Status == domain.StatusResolved:
			title = "Taken: " + title
		case o.Status == domain.StatusWithdrawn:
			title = "Withdrawn: " + title
		case o.Overdue:
			title += " (overdue, due " + o.Due + ")"
		case o.Due != "":
			title += " (due " + o.Due + ")"
		}
		fmt.Fprintf(b, `<polygon class="tm-objective %s" points="%.1f,%.1f %.1f,%.1f %.1f,%.1f %.1f,%.1f"><title>%s</title></polygon>`+"\n",
			class, trackX1, y-8, trackX1+8, y, trackX1, y+8, trackX1-8, y, esc(title))
	}
	return xp
}

func drawPost(b *strings.Builder, p domain.TheatrePost, x, y float64) {
	if p.Kind != "" || p.Strength != 0 {
		// Opposition: a triangle sized by strength.
		hw := 3 + 2*float64(p.Strength)
		h := 6 + 2*float64(p.Strength)
		class := ""
		title := "Opposition: " + p.Text
		switch {
		case p.Status == domain.StatusResolved:
			class, title = "cleared", "Cleared: "+p.Text
		case p.Estimated:
			class = "est"
		}
		if p.Kind != "" {
			title += " (" + kindLabel(p.Kind) + ")"
		}
		title += " — " + strengthLabel(p.Strength)
		if p.Estimated && p.Status != domain.StatusResolved {
			title += ", est"
		}
		fmt.Fprintf(b, `<polygon class="tm-enemy %s" points="%.1f,%.1f %.1f,%.1f %.1f,%.1f"><title>%s</title></polygon>`+"\n",
			class, x, y-h/2, x+hw, y+h/2, x-hw, y+h/2, esc(title))
		return
	}
	// Fog: a question mark in a patch.
	class, mark, title := "", "tm-fogmark", "Fog: "+p.Text
	if p.Status == domain.StatusResolved {
		class, mark, title = "lifted", "tm-fogmark tm-fog-lifted-mark", "Lifted: "+p.Text
	}
	fmt.Fprintf(b, `<g><title>%s</title><circle class="tm-fog %s" cx="%.1f" cy="%.1f" r="7"/><text class="%s" x="%.1f" y="%.1f" text-anchor="middle">?</text></g>`+"\n",
		esc(title), class, x, y, mark, x, y+3.5)
}

func drawLegend(b *strings.Builder, y float64) {
	x := svgPadX + 8
	item := func(draw func(cx float64), label string) {
		draw(x + 8)
		fmt.Fprintf(b, `<text class="tm-legend" x="%.1f" y="%.1f">%s</text>`, x+20, y+4, esc(label))
		// Roughly 5.6px a character at 11px, plus a gap.
		x += 20 + 5.6*float64(len([]rune(label))) + 18
	}
	item(func(cx float64) {
		fmt.Fprintf(b, `<polygon class="tm-objective" points="%.1f,%.1f %.1f,%.1f %.1f,%.1f %.1f,%.1f"/>`, cx, y-6, cx+6, y, cx, y+6, cx-6, y)
	}, "objective")
	item(func(cx float64) {
		fmt.Fprintf(b, `<polygon class="tm-enemy" points="%.1f,%.1f %.1f,%.1f %.1f,%.1f"/>`, cx, y-5, cx+6, y+5, cx-6, y+5)
	}, "opposition, sized by strength, dashed if est")
	item(func(cx float64) {
		fmt.Fprintf(b, `<circle class="tm-fog" cx="%.1f" cy="%.1f" r="6"/><text class="tm-fogmark" x="%.1f" y="%.1f" text-anchor="middle">?</text>`, cx, y, cx, y+3.5)
	}, "fog")
	item(func(cx float64) {
		fmt.Fprintf(b, `<line class="tm-ground" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`, cx-7, y, cx+7, y)
	}, "ground held")
	item(func(cx float64) {
		fmt.Fprintf(b, `<polyline class="tm-frontline" points="%.1f,%.1f %.1f,%.1f"/>`, cx, y-7, cx, y+7)
	}, "front line")
	item(func(cx float64) {
		fmt.Fprintf(b, `<polyline class="tm-prior" points="%.1f,%.1f %.1f,%.1f"/>`, cx, y-7, cx, y+7)
	}, "a week ago")
	item(func(cx float64) {
		fmt.Fprintf(b, `<polyline class="tm-order" points="%.1f,%.1f %.1f,%.1f %.1f,%.1f"/>`, cx-2, y-4, cx+1, y-1, cx-2, y+2)
	}, "open order")
	b.WriteString("\n")
}

// spread places n posts evenly between from and to.
func spread(from, to float64, n int) []float64 {
	if n == 0 {
		return nil
	}
	if to < from {
		to = from
	}
	out := make([]float64, n)
	step := (to - from) / float64(n+1)
	for i := range out {
		out[i] = from + step*float64(i+1)
	}
	return out
}

func pct(v float64) int { return int(v*100 + 0.5) }

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
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
	default:
		return string(k)
	}
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

func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

func esc(s string) string { return html.EscapeString(s) }
