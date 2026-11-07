package campaignapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	"github.com/an4eetos/decision-room/internal/campaign/usecase"
)

func (h *Handler) registerRecord(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/campaign/record", h.recordPage)
	mux.HandleFunc("POST /api/campaign/record", h.openRecord)
	mux.HandleFunc("PATCH /api/campaign/record/{id}", h.patchRecord)
	mux.HandleFunc("DELETE /api/campaign/record/{id}", h.deleteRecord)
	mux.HandleFunc("POST /api/campaign/record/{id}/phase", h.advanceRecord)
	mux.HandleFunc("POST /api/campaign/record/{id}/objectives", h.assignObjective)
	mux.HandleFunc("DELETE /api/campaign/record/{id}/objectives/{itemID}", h.unassignObjective)
	mux.HandleFunc("PATCH /api/campaign/predictions/{id}", h.patchPrediction)
	mux.HandleFunc("POST /api/campaign/record/{id}/review", h.startReview)
}

func (h *Handler) recordPage(w http.ResponseWriter, r *http.Request) {
	page, err := h.record.Page(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toRecordPage(page))
}

func (h *Handler) openRecord(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Quarter string `json:"quarter"`
	}
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	c, err := h.record.Open(r.Context(), usecase.OpenInput{Name: req.Name, Quarter: req.Quarter})
	if err != nil {
		recordError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toCampaign(c, false))
}

func (h *Handler) patchRecord(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	c, err := h.record.SetNote(r.Context(), id, req.Note)
	if err != nil {
		recordError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toCampaign(c, false))
}

func (h *Handler) deleteRecord(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.record.Delete(r.Context(), id); err != nil {
		recordError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) advanceRecord(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Phase string `json:"phase"`
	}
	if !decode(w, r, &req) {
		return
	}
	c, err := h.record.Advance(r.Context(), id, domain.Phase(req.Phase))
	if err != nil {
		recordError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toCampaign(c, c.Phase == domain.PhaseClosed))
}

func (h *Handler) assignObjective(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		ItemID string `json:"item_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	item, parsed := parseID(w, req.ItemID, "item_id")
	if !parsed {
		return
	}
	if err := h.record.Assign(r.Context(), id, *item); err != nil {
		recordError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) unassignObjective(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, err := uuid.Parse(r.PathValue("itemID"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid item id")
		return
	}
	if err := h.record.Unassign(r.Context(), id, item); err != nil {
		recordError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) patchPrediction(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req predictionPatch
	if !decode(w, r, &req) {
		return
	}
	patch, err := req.toPatch()
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	pred, err := h.record.PatchPrediction(r.Context(), id, patch)
	if err != nil {
		recordError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toPrediction(pred))
}

func (h *Handler) startReview(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := h.record.StartReview(r.Context(), id)
	switch {
	case errors.Is(err, usecase.ErrReviewBusy), errors.Is(err, usecase.ErrNotInReview):
		httpError(w, http.StatusConflict, err.Error())
	case err != nil:
		recordError(w, err)
	default:
		writeJSON(w, http.StatusAccepted, map[string]any{"status": toReviewState(h.record.Status())})
	}
}

func recordError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, port.ErrNotFound):
		httpError(w, http.StatusNotFound, "not found")
	case errors.Is(err, port.ErrDuplicate):
		httpError(w, http.StatusConflict, "that quarter or prediction is already on the record")
	case errors.Is(err, usecase.ErrClosed), errors.Is(err, usecase.ErrPhase),
		errors.Is(err, usecase.ErrReviewBusy), errors.Is(err, usecase.ErrNotInReview):
		httpError(w, http.StatusConflict, err.Error())
	default:
		httpError(w, http.StatusBadRequest, err.Error())
	}
}

type predictionPatch struct {
	Status         string `json:"status"`
	Chance         string `json:"chance"`
	ClearChance    bool   `json:"clear_chance"`
	Outcome        string `json:"outcome"`
	ClearOutcome   bool   `json:"clear_outcome"`
	CampaignID     string `json:"campaign_id"`
	ClearCampaign  bool   `json:"clear_campaign"`
	ObjectiveID    string `json:"objective_id"`
	ClearObjective bool   `json:"clear_objective"`
}

func (p predictionPatch) toPatch() (usecase.PredictionPatch, error) {
	out := usecase.PredictionPatch{ClearChance: p.ClearChance, ClearOutcome: p.ClearOutcome, ClearCampaign: p.ClearCampaign, ClearObjective: p.ClearObjective}
	if p.Status != "" {
		status := domain.PredictionStatus(p.Status)
		out.Status = &status
	}
	if p.Chance != "" {
		chance := domain.Chance(p.Chance)
		out.Chance = &chance
	}
	if p.Outcome != "" {
		outcome := domain.Outcome(p.Outcome)
		out.Outcome = &outcome
	}
	var err error
	if out.CampaignID, err = optionalID(p.CampaignID); err != nil {
		return usecase.PredictionPatch{}, errors.New("campaign_id: invalid id")
	}
	if out.ObjectiveID, err = optionalID(p.ObjectiveID); err != nil {
		return usecase.PredictionPatch{}, errors.New("objective_id: invalid id")
	}
	return out, nil
}

type campaignDTO struct {
	ID            uuid.UUID  `json:"id"`
	Quarter       string     `json:"quarter"`
	Label         string     `json:"label"`
	Name          string     `json:"name"`
	Phase         string     `json:"phase"`
	Note          string     `json:"note"`
	Review        string     `json:"review,omitempty"`
	ReviewAt      *time.Time `json:"review_at,omitempty"`
	ClosedAt      *time.Time `json:"closed_at,omitempty"`
	Ended         bool       `json:"ended,omitempty"`
	Objectives    []refDTO   `json:"objectives"`
	Predictions   []predDTO  `json:"predictions"`
	Commendations []commDTO  `json:"commendations"`
	Calibration   calDTO     `json:"calibration"`
}

type refDTO struct {
	ID     uuid.UUID `json:"id"`
	Text   string    `json:"text"`
	Status string    `json:"status"`
}

type predDTO struct {
	ID          uuid.UUID  `json:"id"`
	CampaignID  *uuid.UUID `json:"campaign_id,omitempty"`
	ObjectiveID *uuid.UUID `json:"objective_id,omitempty"`
	Text        string     `json:"text"`
	Warning     string     `json:"warning,omitempty"`
	Status      string     `json:"status"`
	Chance      string     `json:"chance,omitempty"`
	Outcome     string     `json:"outcome,omitempty"`
	Source      string     `json:"source"`
}

type commDTO struct {
	Kind  string     `json:"kind"`
	Text  string     `json:"text"`
	RefID *uuid.UUID `json:"ref_id,omitempty"`
}

type bucketDTO struct {
	Chance string `json:"chance"`
	Called int    `json:"called"`
	Scored int    `json:"scored"`
}

type calDTO struct {
	Score   *float64    `json:"score"`
	N       int         `json:"n"`
	Min     int         `json:"min"`
	Buckets []bucketDTO `json:"buckets"`
}

type reviewDTO struct {
	Busy       bool       `json:"busy"`
	Since      *time.Time `json:"since,omitempty"`
	CampaignID *uuid.UUID `json:"campaign_id,omitempty"`
	Error      string     `json:"error,omitempty"`
}

func toRecordPage(page usecase.RecordPage) map[string]any {
	campaigns := make([]campaignDTO, 0, len(page.Campaigns))
	for _, rec := range page.Campaigns {
		campaigns = append(campaigns, toCampaignRecord(rec))
	}
	unfiled := make([]predDTO, 0, len(page.Unfiled))
	for _, p := range page.Unfiled {
		unfiled = append(unfiled, toPrediction(p))
	}
	return map[string]any{
		"today": page.Today, "quarter": page.Quarter, "label": domain.QuarterLabel(page.Quarter),
		"campaigns": campaigns, "unfiled": unfiled, "objectives": toRefs(page.Pool),
		"review": toReviewState(page.Review),
	}
}

func toCampaignRecord(rec usecase.CampaignRecord) campaignDTO {
	dto := toCampaign(rec.Campaign, rec.Ended)
	dto.Objectives = toRefs(rec.Objectives)
	dto.Predictions = make([]predDTO, 0, len(rec.Predictions))
	for _, p := range rec.Predictions {
		dto.Predictions = append(dto.Predictions, toPrediction(p))
	}
	dto.Commendations = make([]commDTO, 0, len(rec.Commendations))
	for _, line := range rec.Commendations {
		dto.Commendations = append(dto.Commendations, commDTO{Kind: string(line.Kind), Text: line.Text, RefID: line.RefID})
	}
	dto.Calibration = toCalibration(rec.Calibration)
	return dto
}

func toCampaign(c domain.Campaign, ended bool) campaignDTO {
	return campaignDTO{
		ID: c.ID, Quarter: c.Quarter, Label: domain.QuarterLabel(c.Quarter), Name: c.Name,
		Phase: string(c.Phase), Note: c.Note, Review: c.Review, ReviewAt: c.ReviewAt,
		ClosedAt: c.ClosedAt, Ended: ended,
		Objectives: []refDTO{}, Predictions: []predDTO{}, Commendations: []commDTO{},
		Calibration: calDTO{Min: domain.CalibrationMin, Buckets: []bucketDTO{}},
	}
}

func toPrediction(p domain.Prediction) predDTO {
	return predDTO{
		ID: p.ID, CampaignID: p.CampaignID, ObjectiveID: p.ObjectiveID, Text: p.Text,
		Warning: p.Warning, Status: string(p.Status), Chance: string(p.Chance),
		Outcome: string(p.Outcome), Source: p.Source,
	}
}

func toRefs(items []domain.Item) []refDTO {
	out := make([]refDTO, 0, len(items))
	for _, it := range items {
		out = append(out, refDTO{ID: it.ID, Text: it.Text, Status: string(it.Status)})
	}
	return out
}

func toCalibration(cal domain.Calibration) calDTO {
	dto := calDTO{Score: cal.Score, N: cal.N, Min: domain.CalibrationMin, Buckets: []bucketDTO{}}
	for _, b := range cal.Buckets {
		dto.Buckets = append(dto.Buckets, bucketDTO{Chance: string(b.Chance), Called: b.Called, Scored: b.Scored})
	}
	return dto
}

func toReviewState(s usecase.ReviewState) reviewDTO {
	dto := reviewDTO{Busy: s.Busy, Error: s.Error}
	if !s.Since.IsZero() {
		dto.Since = &s.Since
	}
	if s.CampaignID != uuid.Nil {
		dto.CampaignID = &s.CampaignID
	}
	return dto
}
