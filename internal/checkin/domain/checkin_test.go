package domain

import "testing"

func TestOnlyTheEveningCheckInAsksForEnergy(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		checkin CheckIn
		asks    bool
	}{
		{CheckIn{Kind: KindScheduled, ModeID: "debrief"}, true},
		{CheckIn{Kind: KindScheduled, ModeID: "day_plan"}, false},
		{CheckIn{Kind: KindScheduled, ModeID: "block_start"}, false},
		// "Quiet for a while" opens as a debrief too, but it is a nudge, not
		// the end of the day.
		{CheckIn{Kind: KindNudge, ModeID: "debrief"}, false},
	} {
		if got := c.checkin.AsksEnergy(); got != c.asks {
			t.Errorf("%s %s: asks energy %v, want %v", c.checkin.Kind, c.checkin.ModeID, got, c.asks)
		}
	}
}
