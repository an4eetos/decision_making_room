package service

import (
	"strings"
	"testing"
)

const preMortemAnswer = `**How it failed**
- The lease ran to March and you could not leave.
- The beta shipped without tests and the first user found the hole.
- You waited on legal and the window closed.
- A fourth failure the template does not ask for.

**Early warnings**
- The landlord's reply slips a week.
- A week with no test run.
- Legal has not answered by Friday.
- A fourth warning.

**The cheapest insurance**
Buy the test day now.
`

func TestParsePreMortemZipsMatchingWarnings(t *testing.T) {
	t.Parallel()
	intel := ParsePreMortem(preMortemAnswer)
	if len(intel.Failures) != 3 {
		t.Fatalf("at most three failures, got %d", len(intel.Failures))
	}
	if intel.Failures[0].Text != "The lease ran to March and you could not leave" {
		t.Fatalf("text: %q", intel.Failures[0].Text)
	}
	if intel.Failures[1].Warning != "A week with no test run" {
		t.Fatalf("warning zipped by position: %q", intel.Failures[1].Warning)
	}
	for _, f := range intel.Failures {
		if strings.Contains(f.Text, "test day") || strings.Contains(f.Warning, "fourth") {
			t.Fatalf("insurance and the fourth pair are not predictions: %+v", f)
		}
	}
}

func TestParsePreMortemLeavesUnmatchedWarningsOff(t *testing.T) {
	t.Parallel()
	answer := "**How it failed**\n- The lease ran out.\n- The beta had no tests.\n\n**Early warnings**\n- Only one warning.\n"
	intel := ParsePreMortem(answer)
	if len(intel.Failures) != 2 {
		t.Fatalf("failures: %+v", intel.Failures)
	}
	for _, f := range intel.Failures {
		if f.Warning != "" {
			t.Fatalf("a mismatched warning list is not guessed onto a failure: %+v", f)
		}
	}
}

func TestParsePreMortemEmptyWhenTheShapeIsMissing(t *testing.T) {
	t.Parallel()
	if intel := ParsePreMortem("It might go badly."); len(intel.Failures) != 0 {
		t.Fatalf("prose is not a prediction: %+v", intel)
	}
}
