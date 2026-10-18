package usecase

import (
	"context"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/service"
)

// CampaignRecord is one quarter as the page reads it: the objectives assigned
// to it, the predictions on it, the commendations, and the calibration.
type CampaignRecord struct {
	Campaign      domain.Campaign
	Objectives    []domain.Item
	Predictions   []domain.Prediction
	Commendations []domain.Commendation
	Calibration   domain.Calibration
	// Ended reports that the calendar quarter is over while the phase is
	// still planning or operations. The page says so; it does not advance it.
	Ended bool
}

// RecordPage is every quarter, plus predictions not filed on one, and the
// objectives that can be assigned.
type RecordPage struct {
	Today     string
	Quarter   string
	Campaigns []CampaignRecord
	Unfiled   []domain.Prediction
	Pool      []domain.Item
	Review    ReviewState
}

// Page reads the record. Open quarters derive their commendations and
// calibration; a closed quarter returns what was sealed.
func (u *Record) Page(ctx context.Context) (RecordPage, error) {
	now := u.now().In(u.loc)
	campaigns, predictions, items, err := u.load(ctx)
	if err != nil {
		return RecordPage{}, err
	}
	byID := indexItems(items)
	byCampaign, unfiled := groupPredictions(predictions)
	var records []CampaignRecord
	for _, c := range sortCampaigns(campaigns, domain.QuarterOf(now)) {
		rec, err := u.campaignRecord(ctx, c, byID, byCampaign[c.ID], now)
		if err != nil {
			return RecordPage{}, err
		}
		records = append(records, rec)
	}
	return RecordPage{
		Today: now.Format("2006-01-02"), Quarter: domain.QuarterOf(now),
		Campaigns: records, Unfiled: unfiled, Pool: assignable(items), Review: u.reviewStatus(),
	}, nil
}

// Note is the one quarter a sitrep may mention, or nil when the record has
// nothing to say. A closed quarter uses the number sealed with it.
func (u *Record) Note(ctx context.Context) (*domain.RecordNote, error) {
	now := u.now().In(u.loc)
	campaigns, err := u.repo.ListCampaigns(ctx)
	if err != nil {
		return nil, err
	}
	c := pickCampaign(campaigns, domain.QuarterOf(now))
	if c == nil {
		return nil, nil
	}
	preds, err := u.predictionsFor(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	return noteOf(*c, preds), nil
}

func (u *Record) load(ctx context.Context) ([]domain.Campaign, []domain.Prediction, []domain.Item, error) {
	campaigns, err := u.repo.ListCampaigns(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	predictions, err := u.repo.ListPredictions(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	items, err := u.items.ListItems(ctx, nil, 1000)
	if err != nil {
		return nil, nil, nil, err
	}
	return campaigns, predictions, items, nil
}

func (u *Record) campaignRecord(ctx context.Context, c domain.Campaign, byID map[uuid.UUID]domain.Item, preds []domain.Prediction, now time.Time) (CampaignRecord, error) {
	objectives, err := u.assigned(ctx, c.ID, byID)
	if err != nil {
		return CampaignRecord{}, err
	}
	cal := service.Calibrate(preds)
	lines := liveCommendations(c.ID, objectives, preds, now)
	if c.Phase == domain.PhaseClosed {
		cal.Score, cal.N = c.Calibration, c.CalibrationN
		lines, err = u.repo.ListCommendations(ctx, c.ID)
		if err != nil {
			return CampaignRecord{}, err
		}
	}
	return CampaignRecord{
		Campaign: c, Objectives: objectives, Predictions: preds,
		Commendations: lines, Calibration: cal, Ended: quarterEnded(c, now),
	}, nil
}

func (u *Record) assigned(ctx context.Context, campaignID uuid.UUID, byID map[uuid.UUID]domain.Item) ([]domain.Item, error) {
	ids, err := u.repo.ListAssignments(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	var out []domain.Item
	for _, id := range ids {
		if it, ok := byID[id]; ok && it.Type == domain.TypeObjective {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Text < out[j].Text })
	return out, nil
}

func (u *Record) seal(ctx context.Context, c domain.Campaign) (domain.Campaign, error) {
	now := u.now().In(u.loc)
	_, predictions, items, err := u.load(ctx)
	if err != nil {
		return domain.Campaign{}, err
	}
	objectives, err := u.assigned(ctx, c.ID, indexItems(items))
	if err != nil {
		return domain.Campaign{}, err
	}
	preds := predictionsFor(predictions, c.ID)
	cal := service.Calibrate(preds)
	c.ClosedAt = &now
	c.Calibration = cal.Score
	c.CalibrationN = cal.N
	return u.repo.SealCampaign(ctx, c, liveCommendations(c.ID, objectives, preds, now))
}

func (u *Record) predictionsFor(ctx context.Context, id uuid.UUID) ([]domain.Prediction, error) {
	all, err := u.repo.ListPredictions(ctx)
	if err != nil {
		return nil, err
	}
	return predictionsFor(all, id), nil
}

func predictionsFor(all []domain.Prediction, id uuid.UUID) []domain.Prediction {
	var out []domain.Prediction
	for _, p := range all {
		if p.CampaignID != nil && *p.CampaignID == id && p.Status.Live() {
			out = append(out, p)
		}
	}
	return out
}

func noteOf(c domain.Campaign, preds []domain.Prediction) *domain.RecordNote {
	cal := service.Calibrate(preds)
	if c.Phase == domain.PhaseClosed {
		cal.Score, cal.N = c.Calibration, c.CalibrationN
	}
	unscored := 0
	if c.Phase == domain.PhaseReview {
		unscored = countUnscored(preds)
	}
	if unscored == 0 && cal.Score == nil {
		return nil
	}
	return &domain.RecordNote{
		Name: c.Name, Quarter: c.Quarter, Phase: c.Phase, Unscored: unscored, Calibration: cal,
	}
}

func countUnscored(preds []domain.Prediction) int {
	n := 0
	for _, p := range preds {
		if p.Status == domain.PredictionKept && p.Outcome == domain.OutcomeNone {
			n++
		}
	}
	return n
}

func pickCampaign(campaigns []domain.Campaign, quarter string) *domain.Campaign {
	var review *domain.Campaign
	for i := range campaigns {
		if campaigns[i].Quarter == quarter {
			return &campaigns[i]
		}
		if campaigns[i].Phase == domain.PhaseReview && (review == nil || campaigns[i].UpdatedAt.After(review.UpdatedAt)) {
			review = &campaigns[i]
		}
	}
	return review
}

func quarterEnded(c domain.Campaign, now time.Time) bool {
	if c.Phase != domain.PhasePlanning && c.Phase != domain.PhaseOperations {
		return false
	}
	last, ok := domain.QuarterLastDay(c.Quarter)
	return ok && now.Format("2006-01-02") > last
}

func liveCommendations(campaignID uuid.UUID, objectives []domain.Item, predictions []domain.Prediction, now time.Time) []domain.Commendation {
	var out []domain.Commendation
	for _, it := range objectives {
		kind, ok := commendationKind(it.Status)
		if !ok {
			continue
		}
		when := now
		if it.ResolvedAt != nil {
			when = *it.ResolvedAt
		}
		id := it.ID
		out = append(out, domain.Commendation{
			CampaignID: campaignID, Kind: kind, Text: it.Text, RefID: &id, CreatedAt: when,
		})
	}
	for _, p := range predictions {
		if p.Outcome != domain.OutcomeCalled || p.Status != domain.PredictionKept {
			continue
		}
		when := now
		if p.ScoredAt != nil {
			when = *p.ScoredAt
		}
		id := p.ID
		out = append(out, domain.Commendation{
			CampaignID: campaignID, Kind: domain.CommendationCalled, Text: p.Text, RefID: &id, CreatedAt: when,
		})
	}
	return out
}

func commendationKind(status domain.Status) (domain.CommendationKind, bool) {
	switch status {
	case domain.StatusResolved:
		return domain.CommendationTaken, true
	case domain.StatusWithdrawn:
		return domain.CommendationWithdrawal, true
	default:
		return "", false
	}
}

func groupPredictions(predictions []domain.Prediction) (map[uuid.UUID][]domain.Prediction, []domain.Prediction) {
	byCampaign := map[uuid.UUID][]domain.Prediction{}
	var unfiled []domain.Prediction
	for _, p := range predictions {
		if !p.Status.Live() {
			continue
		}
		if p.CampaignID == nil {
			unfiled = append(unfiled, p)
			continue
		}
		byCampaign[*p.CampaignID] = append(byCampaign[*p.CampaignID], p)
	}
	return byCampaign, unfiled
}

func sortCampaigns(campaigns []domain.Campaign, today string) []domain.Campaign {
	out := append([]domain.Campaign(nil), campaigns...)
	sort.SliceStable(out, func(i, j int) bool {
		return campaignLess(out[i], out[j], today)
	})
	return out
}

func campaignLess(a, b domain.Campaign, today string) bool {
	aClosed, bClosed := a.Phase == domain.PhaseClosed, b.Phase == domain.PhaseClosed
	if aClosed != bClosed {
		return !aClosed
	}
	if (a.Quarter == today) != (b.Quarter == today) {
		return a.Quarter == today
	}
	return a.Quarter > b.Quarter
}

func indexItems(items []domain.Item) map[uuid.UUID]domain.Item {
	out := map[uuid.UUID]domain.Item{}
	for _, it := range items {
		out[it.ID] = it
	}
	return out
}

func assignable(items []domain.Item) []domain.Item {
	var out []domain.Item
	for _, it := range items {
		if it.Type == domain.TypeObjective && onMap(it.Status) {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Text < out[j].Text })
	return out
}
