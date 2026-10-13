package campaignapi

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
)

func wellFormed(t *testing.T, svg []byte) {
	t.Helper()
	dec := xml.NewDecoder(bytes.NewReader(svg))
	for {
		if _, err := dec.Token(); err == io.EOF {
			return
		} else if err != nil {
			t.Fatalf("svg is not well-formed xml: %v\n%s", err, svg)
		}
	}
}

func TestRenderSVGEmpty(t *testing.T) {
	t.Parallel()
	svg := RenderSVG(domain.Theatre{Fronts: []domain.TheatreFront{}}, nil)
	wellFormed(t, svg)
	if !strings.Contains(string(svg), "Nothing on the map yet") {
		t.Fatalf("an empty theatre should say so:\n%s", svg)
	}
}

func TestRenderSVGDrawsEverythingEscaped(t *testing.T) {
	t.Parallel()
	work := uuid.New()
	ship, wall, fog := uuid.New(), uuid.New(), uuid.New()
	theatre := domain.Theatre{
		TakenAt: time.Now(),
		Fronts: []domain.TheatreFront{{
			ID: work, Name: "Work & <Play>", Line: 0.5,
			Rows: []domain.TheatreRow{
				{
					Objective: &domain.TheatrePost{ID: ship, Text: `Ship "v2" <now>`, Status: domain.StatusActive, Due: "2026-01-01", Overdue: true},
					Progress:  0.5,
					Obstacles: []domain.TheatrePost{
						{ID: wall, Text: "No tests", Status: domain.StatusActive, Kind: domain.KindTooBig, Strength: 3, Estimated: true},
						{ID: uuid.New(), Text: "Old wall", Status: domain.StatusResolved, Strength: 1},
					},
					Unknowns: []domain.TheatrePost{{ID: fog, Text: "Who pays?", Status: domain.StatusActive}},
					Orders:   domain.TheatreOrders{Open: 6, Recon: 1, Done: 2},
				},
				{Objective: &domain.TheatrePost{ID: uuid.New(), Text: "Gone", Status: domain.StatusWithdrawn}},
				{Objective: &domain.TheatrePost{ID: uuid.New(), Text: "Done", Status: domain.StatusResolved}, Progress: 1},
				{Obstacles: []domain.TheatrePost{}, Unknowns: []domain.TheatrePost{{ID: uuid.New(), Text: "Loose", Status: domain.StatusResolved}}, Progress: 1},
			},
		}, {ID: uuid.Nil, Name: "No front", Rows: []domain.TheatreRow{}}},
	}
	prior := &domain.Theatre{Fronts: []domain.TheatreFront{{
		ID: work, Line: 0.25,
		Rows: []domain.TheatreRow{{Objective: &domain.TheatrePost{ID: ship}, Progress: 0.25}},
	}}}

	svg := RenderSVG(theatre, prior)
	wellFormed(t, svg)
	out := string(svg)

	for _, want := range []string{
		"Work &amp; &lt;Play&gt;",
		"Ship &#34;v2&#34; &lt;now&gt;",
		"50% held · was 25%",
		`class="tm-objective overdue"`,
		`class="tm-objective withdrawn"`,
		`class="tm-objective taken"`,
		`class="tm-enemy est"`,
		`class="tm-enemy cleared"`,
		`class="tm-fog "`,
		`class="tm-fog lifted"`,
		`class="tm-prior"`,
		`class="tm-frontline"`,
		"6 open orders, 1 reconnaissance; 2 carried out",
		"Quiet front",
		"too big to hold",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Count(out, `class="tm-order"`) != maxArrows+1 {
		t.Errorf("open orders cap at %d chevrons (plus the legend's one)", maxArrows)
	}
	if strings.Contains(out, "<Play>") || strings.Contains(out, `"v2"`) {
		t.Error("item text must be escaped")
	}
}
