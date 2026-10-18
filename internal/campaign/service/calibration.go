package service

import "github.com/an4eetos/decision-room/internal/campaign/domain"

// chanceOrder is the order buckets are reported in.
var chanceOrder = []domain.Chance{domain.ChanceLong, domain.ChanceEven, domain.ChanceLikely}

// Calibrate scores predictions that were kept, given a chance, and then
// scored. The Brier score is the mean of (chance − outcome)². It is withheld
// until CalibrationMin of them exist; the buckets are reported as soon as
// one is.
func Calibrate(predictions []domain.Prediction) domain.Calibration {
	counts := map[domain.Chance]*domain.Bucket{}
	for _, chance := range chanceOrder {
		counts[chance] = &domain.Bucket{Chance: chance}
	}

	var sum float64
	n := 0
	for _, p := range predictions {
		if p.Status != domain.PredictionKept {
			continue
		}
		prob, ok := p.Chance.Probability()
		if !ok || p.Outcome == domain.OutcomeNone {
			continue
		}
		outcome := 0.0
		if p.Outcome == domain.OutcomeCalled {
			outcome = 1
		}
		sum += (prob - outcome) * (prob - outcome)
		n++
		bucket := counts[p.Chance]
		bucket.Scored++
		if p.Outcome == domain.OutcomeCalled {
			bucket.Called++
		}
	}

	cal := domain.Calibration{N: n}
	for _, chance := range chanceOrder {
		if counts[chance].Scored > 0 {
			cal.Buckets = append(cal.Buckets, *counts[chance])
		}
	}
	if n >= domain.CalibrationMin {
		score := sum / float64(n)
		cal.Score = &score
	}
	return cal
}
