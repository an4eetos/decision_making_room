package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
)

// MinConfidence is the floor below which a candidate is discarded. Everything
// that survives is a proposal you keep or drop, so the floor only keeps noise
// out.
const MinConfidence = 0.6

// maxPerType stops one turn flooding the map.
const maxPerType = 2

// maxTextRunes bounds an item. An objective that needs more is a plan.
const maxTextRunes = 200

// Candidate is one item the model thinks you named.
type Candidate struct {
	Type domain.Type
	Text string
	// Front is the front's name as the model gave it; empty is unassigned.
	Front string
	// Objective refers to what an obstacle or unknown bears on: ObjectiveIndex
	// is 1-based into the existing objectives the prompt listed, ObjectiveText
	// is the text of an objective proposed in the same response. Both zero is
	// none.
	ObjectiveIndex int
	ObjectiveText  string
	Kind           domain.Kind
	Strength       int
	Due            *time.Time
	Confidence     float64
}

type wireItem struct {
	Text       string          `json:"text"`
	Front      string          `json:"front"`
	Objective  json.RawMessage `json:"objective"`
	Kind       string          `json:"kind"`
	Strength   int             `json:"strength"`
	Due        *string         `json:"due"`
	Confidence float64         `json:"confidence"`
}

type wireResponse struct {
	Objectives []wireItem `json:"objectives"`
	Obstacles  []wireItem `json:"obstacles"`
	Unknowns   []wireItem `json:"unknowns"`
}

// ParseCandidates reads the extraction response. Tolerant of code fences and
// prose around the JSON, and never an error for an empty result: "nothing to
// map" is the common answer.
func ParseCandidates(answer string, now time.Time) ([]Candidate, error) {
	raw := strings.TrimSpace(answer)
	if raw == "" {
		return nil, nil
	}
	start := strings.IndexByte(raw, '{')
	end := strings.LastIndexByte(raw, '}')
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object in extraction response")
	}

	var wire wireResponse
	if err := json.Unmarshal([]byte(raw[start:end+1]), &wire); err != nil {
		return nil, fmt.Errorf("parse extraction response: %w", err)
	}

	var out []Candidate
	for _, group := range []struct {
		typ   domain.Type
		items []wireItem
	}{
		{domain.TypeObjective, wire.Objectives},
		{domain.TypeObstacle, wire.Obstacles},
		{domain.TypeUnknown, wire.Unknowns},
	} {
		kept := 0
		for _, w := range group.items {
			c, ok := candidate(group.typ, w, now)
			if !ok {
				continue
			}
			out = append(out, c)
			if kept++; kept == maxPerType {
				break
			}
		}
	}
	return out, nil
}

func candidate(typ domain.Type, w wireItem, now time.Time) (Candidate, bool) {
	text := strings.TrimSpace(w.Text)
	if text == "" || len([]rune(text)) > maxTextRunes || w.Confidence < MinConfidence {
		return Candidate{}, false
	}

	c := Candidate{
		Type:       typ,
		Text:       text,
		Front:      strings.TrimSpace(w.Front),
		Confidence: clamp(w.Confidence),
	}

	if typ != domain.TypeObjective {
		c.ObjectiveIndex, c.ObjectiveText = objectiveRef(w.Objective)
	}
	if typ == domain.TypeObstacle {
		if k := domain.Kind(strings.TrimSpace(w.Kind)); k.Valid() {
			c.Kind = k
		}
		// An estimate is always on the scale; anything the model gets wrong
		// becomes the middle, which you then confirm or correct.
		c.Strength = w.Strength
		if !domain.ValidStrength(c.Strength) {
			c.Strength = domain.StrengthDugIn
		}
	}
	if typ == domain.TypeObjective && w.Due != nil {
		c.Due = parseDue(*w.Due, now)
	}
	return c, true
}

// objectiveRef accepts a 1-based index, an index written as a string, or the
// text of an objective proposed alongside. Anything else is no reference.
func objectiveRef(raw json.RawMessage) (int, string) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, ""
	}
	var n int
	if json.Unmarshal(raw, &n) == nil && n > 0 {
		return n, ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		s = strings.TrimSpace(s)
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n, ""
		}
		return 0, s
	}
	return 0, ""
}

// parseDue pins a plain date to the end of that day. A date already past is
// dropped as a misparse rather than creating an objective overdue on arrival.
func parseDue(s string, now time.Time) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "null") {
		return nil
	}
	d, err := time.ParseInLocation("2006-01-02", s, now.Location())
	if err != nil {
		return nil
	}
	end := d.Add(24*time.Hour - time.Second)
	if end.Before(now) {
		return nil
	}
	return &end
}

func clamp(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}
