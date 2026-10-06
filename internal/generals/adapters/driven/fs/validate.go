package fs

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/an4eetos/decision-room/internal/generals/domain"
)

var validFamilies = []domain.Family{
	domain.FamilyContact, domain.FamilyScouting, domain.FamilyEndurance,
	domain.FamilyAdaptation, domain.FamilySystems, domain.FamilyConcentration,
	domain.FamilyPreservation,
}

// validate fails startup on a malformed roster. A lens missing its Job or Bias
// still loads and still renders a card, but the card is the entire contribution
// it makes to an answer — a silently degraded one is worse than a crash, because
// nothing downstream can tell.
func validate(r domain.Roster) error {
	var problems []error

	seen := make(map[string]string)
	for _, group := range [][]domain.Lens{r.Generals, r.Styles} {
		for _, lens := range group {
			where := fmt.Sprintf("%s %q", lens.Kind, lens.ID)

			if strings.TrimSpace(lens.ID) == "" {
				problems = append(problems, fmt.Errorf("%s: empty id", lens.Source))
				continue
			}
			if prev, dup := seen[lens.ID]; dup {
				problems = append(problems, fmt.Errorf("%s: duplicate id, also in %s", where, prev))
			}
			seen[lens.ID] = lens.Source

			if strings.TrimSpace(lens.Name) == "" {
				problems = append(problems, fmt.Errorf("%s: missing name", where))
			}
			if strings.TrimSpace(lens.Job) == "" {
				problems = append(problems, fmt.Errorf("%s: missing job", where))
			}
			// Without a blind spot a multi-lens answer converges on agreement,
			// which is the one thing it exists not to do.
			if strings.TrimSpace(lens.Bias) == "" {
				problems = append(problems, fmt.Errorf("%s: missing bias", where))
			}
			if len(lens.DeployWhen) == 0 {
				problems = append(problems, fmt.Errorf("%s: needs at least one deploy_when", where))
			}
			if !slices.Contains(validFamilies, lens.Family) {
				problems = append(problems, fmt.Errorf("%s: unknown family %q", where, lens.Family))
			}
			// A lens with no routing keywords can never be auto-selected, so it
			// would only ever appear if picked by hand.
			if len(lens.Routes.Keywords) == 0 {
				problems = append(problems, fmt.Errorf("%s: needs routing keywords", where))
			}
		}
	}

	if len(r.Generals) == 0 {
		problems = append(problems, errors.New("roster has no generals"))
	}

	// A rival that does not exist would silently vanish from the card, and the
	// pairing it was meant to force would never happen.
	generalIDs := make(map[string]bool, len(r.Generals))
	for _, g := range r.Generals {
		generalIDs[g.ID] = true
	}
	for _, g := range r.Generals {
		for _, rival := range g.Rivals {
			switch {
			case rival == g.ID:
				problems = append(problems, fmt.Errorf("general %q: lists itself as a rival", g.ID))
			case !generalIDs[rival]:
				problems = append(problems, fmt.Errorf("general %q: rival %q is not a general", g.ID, rival))
			}
		}
	}

	problems = append(problems, validateTraps(r)...)

	return errors.Join(problems...)
}

// validateTraps checks the catalogue and every lens's kills against it. A kill
// naming a trap that does not exist would silently drop out of the card and out
// of interrogation; a trap nobody kills would be detected and then suggested by
// no one.
func validateTraps(r domain.Roster) []error {
	var problems []error

	traps := make(map[string]bool, len(r.Traps))
	signals := make(map[string]string)
	for _, trap := range r.Traps {
		where := fmt.Sprintf("trap %q", trap.ID)
		if strings.TrimSpace(trap.ID) == "" {
			problems = append(problems, errors.New("trap with empty id"))
			continue
		}
		if traps[trap.ID] {
			problems = append(problems, fmt.Errorf("%s: duplicate id", where))
		}
		traps[trap.ID] = true

		if strings.TrimSpace(trap.Name) == "" {
			problems = append(problems, fmt.Errorf("%s: missing name", where))
		}
		if strings.TrimSpace(trap.Tell) == "" {
			problems = append(problems, fmt.Errorf("%s: missing tell", where))
		}
		if strings.TrimSpace(trap.Kill) == "" {
			problems = append(problems, fmt.Errorf("%s: missing kill question", where))
		}
		if len(trap.Signals) == 0 {
			problems = append(problems, fmt.Errorf("%s: needs signals to be detectable", where))
		}
		// One phrase in two traps would always name both, which says nothing
		// about which one is actually running.
		for _, signal := range trap.Signals {
			key := strings.ToLower(strings.TrimSpace(signal))
			if other, dup := signals[key]; dup && other != trap.ID {
				problems = append(problems, fmt.Errorf("%s: signal %q is also used by trap %q", where, signal, other))
			}
			signals[key] = trap.ID
		}
	}

	killed := make(map[string]bool, len(r.Traps))
	for _, group := range [][]domain.Lens{r.Generals, r.Styles} {
		for _, lens := range group {
			for _, id := range lens.Kills {
				if !traps[id] {
					problems = append(problems, fmt.Errorf("%s %q: kills unknown trap %q", lens.Kind, lens.ID, id))
				}
				killed[id] = true
			}
		}
	}
	for _, trap := range r.Traps {
		if trap.ID != "" && !killed[trap.ID] {
			problems = append(problems, fmt.Errorf("trap %q: no general kills it", trap.ID))
		}
	}

	return problems
}
