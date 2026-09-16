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

	return errors.Join(problems...)
}
