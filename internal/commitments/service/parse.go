package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Candidate is one commitment the model thinks was made.
type Candidate struct {
	Text       string
	Due        *time.Time
	Confidence float64
}

// MinConfidence is the floor below which a candidate is discarded outright.
// Everything that survives becomes a proposal, never an open commitment, so the
// floor only has to keep out noise — you are the one who confirms.
const MinConfidence = 0.6

// maxCandidates stops one turn flooding the list.
const maxCandidates = 3

type wireCandidate struct {
	Text       string  `json:"text"`
	Due        *string `json:"due"`
	Confidence float64 `json:"confidence"`
}

// ParseCandidates reads the extraction response. Tolerant of code fences and
// prose, because models add them regardless of instructions, and never an error
// for an empty result: "no commitments made" is the common answer.
func ParseCandidates(answer string, now time.Time) ([]Candidate, error) {
	raw := strings.TrimSpace(answer)
	if raw == "" {
		return nil, nil
	}

	start := strings.IndexByte(raw, '[')
	end := strings.LastIndexByte(raw, ']')
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array in extraction response")
	}

	var wire []wireCandidate
	if err := json.Unmarshal([]byte(raw[start:end+1]), &wire); err != nil {
		return nil, fmt.Errorf("parse extraction response: %w", err)
	}

	out := make([]Candidate, 0, len(wire))
	for _, w := range wire {
		text := strings.TrimSpace(w.Text)
		if text == "" || len([]rune(text)) > 240 {
			continue
		}
		if w.Confidence < MinConfidence {
			continue
		}

		c := Candidate{Text: text, Confidence: clamp(w.Confidence)}
		if w.Due != nil {
			c.Due = parseDue(*w.Due, now)
		}
		out = append(out, c)

		if len(out) == maxCandidates {
			break
		}
	}
	return out, nil
}

// parseDue accepts a plain date and pins it to the end of that day, which is
// what "by Friday" means. A date in the past is dropped rather than creating a
// commitment that is overdue on arrival — that is almost always a misparse.
func parseDue(s string, now time.Time) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "null") {
		return nil
	}

	d, err := time.ParseInLocation("2006-01-02", s, now.Location())
	if err != nil {
		return nil
	}
	endOfDay := d.Add(24*time.Hour - time.Second)
	if endOfDay.Before(now) {
		return nil
	}
	return &endOfDay
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
