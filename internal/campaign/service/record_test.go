package service

import (
	"strings"
	"testing"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
)

func TestWithRecordAddsOnlyWhatTheQuarterSays(t *testing.T) {
	t.Parallel()
	base := domain.Sitrep{Readings: []domain.Reading{{Kind: domain.ReadingQuiet, Text: "Health has gone quiet"}}}

	untouched := WithRecord(base, nil)
	if len(untouched.Readings) != 1 || untouched.Readings[0].Text != "Health has gone quiet" {
		t.Fatalf("no campaign adds nothing: %+v", untouched.Readings)
	}

	score := 0.18
	noted := WithRecord(base, &domain.RecordNote{
		Quarter: "2026-Q4", Phase: domain.PhaseReview, Unscored: 1,
		Calibration: domain.Calibration{Score: &score, N: 7},
	})
	text := readingsText(noted)
	if !strings.Contains(text, "1 pre-mortem prediction on 2026 Q4 is still unscored") {
		t.Fatalf("unscored: %s", text)
	}
	if !strings.Contains(text, "Calibration on 2026 Q4 is 0.18 over 7") || !strings.Contains(text, "0.25 is a coin flip") {
		t.Fatalf("calibration: %s", text)
	}

	fighting := WithRecord(base, &domain.RecordNote{
		Quarter: "2026-Q4", Phase: domain.PhaseOperations, Unscored: 3,
		Calibration: domain.Calibration{N: 2},
	})
	if got := readingsText(fighting); got != "Health has gone quiet" {
		t.Fatalf("operations with a short record says nothing: %s", got)
	}
}

func readingsText(s domain.Sitrep) string {
	var parts []string
	for _, r := range s.Readings {
		parts = append(parts, r.Text)
	}
	return strings.Join(parts, "\n")
}
