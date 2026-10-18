package service

import (
	"math"
	"testing"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
)

func TestCalibrateWithholdsAShortRecord(t *testing.T) {
	t.Parallel()
	preds := make([]domain.Prediction, 4)
	for i := range preds {
		preds[i] = scored(domain.ChanceEven, domain.OutcomeMissed)
	}
	cal := Calibrate(preds)
	if cal.Score != nil || cal.N != 4 {
		t.Fatalf("four scored predictions are not a number yet: %+v", cal)
	}
	if len(cal.Buckets) != 1 || cal.Buckets[0].Chance != domain.ChanceEven || cal.Buckets[0].Called != 0 || cal.Buckets[0].Scored != 4 {
		t.Fatalf("buckets: %+v", cal.Buckets)
	}
}

func TestCalibrateBrier(t *testing.T) {
	t.Parallel()
	even := make([]domain.Prediction, 5)
	for i := range even {
		even[i] = scored(domain.ChanceEven, domain.OutcomeMissed)
	}
	cal := Calibrate(even)
	if cal.Score == nil || math.Abs(*cal.Score-0.25) > 1e-9 || cal.N != 5 {
		t.Fatalf("a coin-flip record sits at 0.25: %+v", cal)
	}

	likely := make([]domain.Prediction, 5)
	for i := range likely {
		likely[i] = scored(domain.ChanceLikely, domain.OutcomeCalled)
	}
	cal = Calibrate(likely)
	if cal.Score == nil || math.Abs(*cal.Score-0.04) > 1e-9 {
		t.Fatalf("likely and called is 0.04: %+v", cal)
	}
	if cal.Buckets[0].Called != 5 {
		t.Fatalf("bucket: %+v", cal.Buckets)
	}
}

func TestCalibrateIgnoresWhatWasNotScored(t *testing.T) {
	t.Parallel()
	preds := []domain.Prediction{
		{Status: domain.PredictionProposed, Chance: domain.ChanceLikely, Outcome: domain.OutcomeCalled},
		{Status: domain.PredictionKept, Chance: domain.ChanceNone, Outcome: domain.OutcomeCalled},
		{Status: domain.PredictionKept, Chance: domain.ChanceLong, Outcome: domain.OutcomeNone},
		{Status: domain.PredictionDropped, Chance: domain.ChanceEven, Outcome: domain.OutcomeMissed},
	}
	cal := Calibrate(preds)
	if cal.N != 0 || cal.Score != nil || len(cal.Buckets) != 0 {
		t.Fatalf("unstated and unscored predictions do not move the number: %+v", cal)
	}
}

func scored(chance domain.Chance, outcome domain.Outcome) domain.Prediction {
	return domain.Prediction{Status: domain.PredictionKept, Chance: chance, Outcome: outcome}
}
