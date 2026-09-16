package fs

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/an4eetos/decision-room/internal/modes/domain"
)

var validFamilies = []domain.Family{
	domain.FamilyPlan, domain.FamilyDecide, domain.FamilyUnblock,
	domain.FamilyReview, domain.FamilyOpen,
}

var validKinds = []string{"decision", "plan", "note", "daily_log"}

// validate fails startup rather than letting a broken mode degrade silently. A
// mode with no system prompt still resolves and still answers — just without the
// behaviour it exists to provide, which nothing downstream could detect.
func validate(r domain.Registry) error {
	var problems []error
	seen := make(map[string]string)
	triggers := make(map[string]string)

	for _, mode := range r.Modes {
		where := fmt.Sprintf("mode %q", mode.ID)

		if strings.TrimSpace(mode.ID) == "" {
			problems = append(problems, fmt.Errorf("%s: empty id", mode.Source))
			continue
		}
		if prev, dup := seen[mode.ID]; dup {
			problems = append(problems, fmt.Errorf("%s: duplicate id, also in %s", where, prev))
		}
		seen[mode.ID] = mode.Source

		if strings.TrimSpace(mode.Name) == "" {
			problems = append(problems, fmt.Errorf("%s: missing name", where))
		}
		if strings.TrimSpace(mode.SystemPrompt) == "" {
			problems = append(problems, fmt.Errorf("%s: missing a '## System' section", where))
		}
		if !slices.Contains(validFamilies, mode.Family) {
			problems = append(problems, fmt.Errorf("%s: unknown family %q", where, mode.Family))
		}
		for _, kind := range mode.Retrieval.Kinds {
			if !slices.Contains(validKinds, kind) {
				problems = append(problems, fmt.Errorf("%s: unknown memory kind %q", where, kind))
			}
		}
		if mode.Retrieval.RecencyWeight < 0 || mode.Retrieval.RecencyWeight > 1 {
			problems = append(problems, fmt.Errorf("%s: recency_weight must be between 0 and 1", where))
		}
		// Every mode but the open default needs a way to be detected.
		if mode.ID != DefaultModeID && len(mode.Triggers.Keywords) == 0 {
			problems = append(problems, fmt.Errorf("%s: needs trigger keywords to be detectable", where))
		}

		// A phrase shared by two modes cannot discriminate between them: they
		// score equally, the ambiguity guard rejects both, and the question
		// silently falls back to open. Catching it here is the only place it is
		// visible.
		for _, phrase := range mode.Triggers.Keywords {
			key := strings.ToLower(strings.TrimSpace(phrase))
			if key == "" {
				continue
			}
			if other, dup := triggers[key]; dup && other != mode.ID {
				problems = append(problems, fmt.Errorf(
					"%s: trigger %q is also used by mode %q; neither can ever win on it", where, phrase, other))
				continue
			}
			triggers[key] = mode.ID
		}
	}

	if _, ok := seen[DefaultModeID]; !ok {
		problems = append(problems, fmt.Errorf("roster has no %q mode to fall back to", DefaultModeID))
	}

	return errors.Join(problems...)
}
