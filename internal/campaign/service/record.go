package service

import (
	"fmt"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
)

// WithRecord appends what the quarter's record says to a sitrep's readings.
// A nil note adds nothing, so a room with no campaign reads as it did.
func WithRecord(s domain.Sitrep, note *domain.RecordNote) domain.Sitrep {
	if note == nil {
		return s
	}
	if note.Phase == domain.PhaseReview && note.Unscored > 0 {
		s.Readings = append(s.Readings, domain.Reading{
			Kind: domain.ReadingUnscored,
			Text: unscoredText(*note),
		})
	}
	if note.Calibration.Score != nil {
		s.Readings = append(s.Readings, domain.Reading{
			Kind: domain.ReadingCalibration,
			Text: calibrationText(*note),
		})
	}
	return s
}

func unscoredText(note domain.RecordNote) string {
	noun, verb := "predictions", "are"
	if note.Unscored == 1 {
		noun, verb = "prediction", "is"
	}
	return fmt.Sprintf("%d pre-mortem %s on %s %s still unscored; the after-action is open.",
		note.Unscored, noun, domain.QuarterLabel(note.Quarter), verb)
}

func calibrationText(note domain.RecordNote) string {
	return fmt.Sprintf("Calibration on %s is %.2f over %d scored predictions. 0.25 is a coin flip; lower means the chances you stated matched what happened.",
		domain.QuarterLabel(note.Quarter), *note.Calibration.Score, note.Calibration.N)
}
