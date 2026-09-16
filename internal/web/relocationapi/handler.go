// Package relocationapi exposes relocation plans over JSON.
package relocationapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/relocation/domain"
	"github.com/an4eetos/decision-room/internal/relocation/port"
	"github.com/an4eetos/decision-room/internal/relocation/usecase"
)

type Handler struct {
	plans  port.PlanRepository
	prices port.PriceRepository
	build  *usecase.Build
}

func NewHandler(plans port.PlanRepository, prices port.PriceRepository, build *usecase.Build) *Handler {
	return &Handler{plans: plans, prices: prices, build: build}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/relocation/plans", h.listPlans)
	mux.HandleFunc("POST /api/relocation/plans", h.createPlan)
	mux.HandleFunc("GET /api/relocation/plans/{id}", h.getPlan)
	mux.HandleFunc("PATCH /api/relocation/plans/{id}", h.updatePlan)
	mux.HandleFunc("DELETE /api/relocation/plans/{id}", h.deletePlan)
	mux.HandleFunc("POST /api/relocation/plans/{id}/build", h.buildPlan)
	mux.HandleFunc("POST /api/relocation/plans/{id}/items", h.addItem)
	mux.HandleFunc("PATCH /api/relocation/items/{id}", h.updateItem)
	mux.HandleFunc("DELETE /api/relocation/items/{id}", h.deleteItem)
	mux.HandleFunc("PATCH /api/relocation/pitfalls/{id}", h.acknowledgePitfall)
}

type planRequest struct {
	Destination *string `json:"destination"`
	CountryCode *string `json:"country_code"`
	ArriveOn    *string `json:"arrive_on"`
	DepartOn    *string `json:"depart_on"`
	Nights      *int    `json:"nights"`
	PartySize   *int    `json:"party_size"`
	Housing     *string `json:"housing"`
	Climate     *string `json:"climate"`
	BudgetStyle *string `json:"budget_style"`
	Currency    *string `json:"currency"`
	Status      *string `json:"status"`
	Notes       *string `json:"notes"`
}

func (h *Handler) createPlan(w http.ResponseWriter, r *http.Request) {
	var req planRequest
	if !decode(w, r, &req) {
		return
	}

	plan := domain.Plan{
		PartySize:   1,
		BudgetStyle: domain.BudgetStandard,
		Currency:    "USD",
		Status:      domain.PlanDraft,
	}
	applyPlanRequest(&plan, req)

	if plan.Destination == "" {
		httpError(w, http.StatusBadRequest, "destination is required")
		return
	}
	if plan.Nights <= 0 {
		httpError(w, http.StatusBadRequest, "nights must be positive, or give arrive_on and depart_on")
		return
	}

	created, err := h.plans.CreatePlan(r.Context(), plan)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// A plan with no items is useless, so build it immediately. Pricing is left
	// off: it costs a model call and the caller may not want one yet.
	built, err := h.build.Execute(r.Context(), usecase.BuildInput{PlanID: created.ID})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, toPlanResponse(built))
}

func (h *Handler) updatePlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	existing, err := h.plans.GetPlan(r.Context(), id)
	if respondRepoError(w, err) {
		return
	}

	var req planRequest
	if !decode(w, r, &req) {
		return
	}
	applyPlanRequest(&existing, req)

	updated, err := h.plans.UpdatePlan(r.Context(), existing)
	if respondRepoError(w, err) {
		return
	}

	// Changing dates, housing or climate changes which items apply, so rebuild.
	// Statuses and confirmed prices survive it.
	rebuilt, err := h.build.Execute(r.Context(), usecase.BuildInput{PlanID: updated.ID})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toPlanResponse(rebuilt))
}

func applyPlanRequest(plan *domain.Plan, req planRequest) {
	assign(&plan.Destination, req.Destination)
	assign(&plan.CountryCode, req.CountryCode)
	assign(&plan.Notes, req.Notes)
	assign(&plan.Currency, req.Currency)
	assignInt(&plan.Nights, req.Nights)
	assignInt(&plan.PartySize, req.PartySize)

	if req.Housing != nil {
		plan.Housing = domain.Housing(*req.Housing)
	}
	if req.Climate != nil {
		plan.Climate = domain.Climate(*req.Climate)
	}
	if req.BudgetStyle != nil {
		plan.BudgetStyle = domain.BudgetStyle(*req.BudgetStyle)
	}
	if req.Status != nil {
		plan.Status = domain.PlanStatus(*req.Status)
	}
	if d, ok := parseDate(req.ArriveOn); ok {
		plan.ArriveOn = d
	}
	if d, ok := parseDate(req.DepartOn); ok {
		plan.DepartOn = d
	}

	// Dates win over an explicit night count when both are present: they are the
	// thing the user actually knows.
	if !plan.ArriveOn.IsZero() && !plan.DepartOn.IsZero() && plan.DepartOn.After(plan.ArriveOn) {
		plan.Nights = int(plan.DepartOn.Sub(plan.ArriveOn).Hours() / 24)
	}
	if plan.PartySize < 1 {
		plan.PartySize = 1
	}
}

func (h *Handler) listPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.plans.ListPlans(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	out := make([]planSummary, 0, len(plans))
	for _, p := range plans {
		out = append(out, planSummary{
			ID:          p.ID,
			Destination: p.Destination,
			Nights:      p.Nights,
			ArriveOn:    formatDate(p.ArriveOn),
			DepartOn:    formatDate(p.DepartOn),
			Status:      string(p.Status),
			Currency:    p.Currency,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) getPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	plan, err := h.plans.GetPlan(r.Context(), id)
	if respondRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, toPlanResponse(plan))
}

func (h *Handler) deletePlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if respondRepoError(w, h.plans.DeletePlan(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) buildPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	reprice := r.URL.Query().Get("reprice") == "true"
	plan, err := h.build.Execute(r.Context(), usecase.BuildInput{PlanID: id, Reprice: reprice})
	if respondRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, toPlanResponse(plan))
}

type itemRequest struct {
	Category *string  `json:"category"`
	Name     *string  `json:"name"`
	Quantity *float64 `json:"quantity"`
	Unit     *string  `json:"unit"`
	UnitCost *float64 `json:"unit_cost"`
	Currency *string  `json:"currency"`
	Status   *string  `json:"status"`
	Note     *string  `json:"note"`
}

func (h *Handler) addItem(w http.ResponseWriter, r *http.Request) {
	planID, ok := pathID(w, r)
	if !ok {
		return
	}
	plan, err := h.plans.GetPlan(r.Context(), planID)
	if respondRepoError(w, err) {
		return
	}

	var req itemRequest
	if !decode(w, r, &req) {
		return
	}
	if req.Name == nil || *req.Name == "" {
		httpError(w, http.StatusBadRequest, "name is required")
		return
	}

	item := domain.Item{
		PlanID:   plan.ID,
		Category: domain.CategoryHousing,
		Quantity: 1,
		Currency: plan.Currency,
		// Anything you type is a fact about your own plan, not an estimate.
		Source: domain.CostFromUser,
		Status: domain.ItemNeeded,
	}
	applyItemRequest(&item, req)

	created, err := h.plans.AddItem(r.Context(), item)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toItemResponse(created))
}

func (h *Handler) updateItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	var req itemRequest
	if !decode(w, r, &req) {
		return
	}

	// Read-modify-write so a PATCH of one field does not blank the others.
	existing, err := h.plans.GetItem(r.Context(), id)
	if respondRepoError(w, err) {
		return
	}
	plan, err := h.plans.GetPlan(r.Context(), existing.PlanID)
	if respondRepoError(w, err) {
		return
	}

	before := existing.UnitCost
	applyItemRequest(&existing, req)

	// A price you typed is a real observation. Record it so the next stay in this
	// city starts from what you actually paid rather than from a guess.
	if req.UnitCost != nil && (before == nil || *before != *req.UnitCost) {
		existing.Source = domain.CostFromUser
		existing.Confidence = nil
		if existing.CatalogID != "" {
			if err := h.prices.Record(r.Context(), existing.CatalogID, plan.Destination, *req.UnitCost, existing.Currency); err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}

	updated, err := h.plans.UpdateItem(r.Context(), existing)
	if respondRepoError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, toItemResponse(updated))
}

func applyItemRequest(item *domain.Item, req itemRequest) {
	assign(&item.Name, req.Name)
	assign(&item.Unit, req.Unit)
	assign(&item.Note, req.Note)
	assign(&item.Currency, req.Currency)
	if req.Category != nil {
		item.Category = domain.Category(*req.Category)
	}
	if req.Status != nil {
		item.Status = domain.ItemStatus(*req.Status)
	}
	if req.Quantity != nil && *req.Quantity > 0 {
		item.Quantity = *req.Quantity
	}
	if req.UnitCost != nil {
		cost := *req.UnitCost
		item.UnitCost = &cost
	}
}

func (h *Handler) deleteItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if respondRepoError(w, h.plans.DeleteItem(r.Context(), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) acknowledgePitfall(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	var req struct {
		Acknowledged bool `json:"acknowledged"`
	}
	if !decode(w, r, &req) {
		return
	}
	if respondRepoError(w, h.plans.AcknowledgePitfall(r.Context(), id, req.Acknowledged)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return uuid.Nil, false
	}
	return id, true
}

// respondRepoError maps a not-found to 404 rather than letting every failure
// become a 500.
func respondRepoError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, port.ErrNotFound):
		httpError(w, http.StatusNotFound, "not found")
	default:
		httpError(w, http.StatusInternalServerError, err.Error())
	}
	return true
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func assign[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}

func assignInt(dst *int, src *int) {
	if src != nil && *src > 0 {
		*dst = *src
	}
}

func parseDate(s *string) (time.Time, bool) {
	if s == nil || *s == "" {
		return time.Time{}, false
	}
	d, err := time.Parse("2006-01-02", *s)
	if err != nil {
		return time.Time{}, false
	}
	return d, true
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}
