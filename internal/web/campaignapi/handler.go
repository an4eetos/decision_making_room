// Package campaignapi exposes the campaign over JSON: fronts, the objectives,
// opposition and fog on them, and the orders aimed at each.
package campaignapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/port"
	"github.com/an4eetos/decision-room/internal/campaign/usecase"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
	comport "github.com/an4eetos/decision-room/internal/commitments/port"
)

type Handler struct {
	manage *usecase.Manage
}

func NewHandler(manage *usecase.Manage) *Handler {
	return &Handler{manage: manage}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/campaign", h.getMap)
	mux.HandleFunc("GET /api/campaign/proposals", h.proposals)
	mux.HandleFunc("POST /api/campaign/fronts", h.createFront)
	mux.HandleFunc("POST /api/campaign/fronts/starters", h.addStarters)
	mux.HandleFunc("PATCH /api/campaign/fronts/{id}", h.updateFront)
	mux.HandleFunc("POST /api/campaign/items", h.createItem)
	mux.HandleFunc("PATCH /api/campaign/items/{id}", h.updateItem)
	mux.HandleFunc("POST /api/campaign/items/{id}/lift", h.lift)
	mux.HandleFunc("POST /api/campaign/items/{id}/recon", h.recon)
	mux.HandleFunc("PUT /api/campaign/orders/{id}/target", h.aim)
}

// ---------- DTOs ----------

type frontDTO struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Status   string    `json:"status"`
	Position int       `json:"position"`
}

func toFront(f domain.Front) frontDTO {
	return frontDTO{ID: f.ID, Name: f.Name, Status: string(f.Status), Position: f.Position}
}

type itemDTO struct {
	ID                uuid.UUID  `json:"id"`
	Type              string     `json:"type"`
	FrontID           *uuid.UUID `json:"front_id"`
	ObjectiveID       *uuid.UUID `json:"objective_id"`
	Text              string     `json:"text"`
	Status            string     `json:"status"`
	Kind              string     `json:"kind,omitempty"`
	Strength          int        `json:"strength,omitempty"`
	StrengthConfirmed bool       `json:"strength_confirmed"`
	Answer            string     `json:"answer,omitempty"`
	Due               string     `json:"due,omitempty"`
	Overdue           bool       `json:"overdue"`
	Source            string     `json:"source"`
	SessionID         *uuid.UUID `json:"session_id,omitempty"`
	Confidence        float64    `json:"confidence"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	ResolvedAt        *time.Time `json:"resolved_at,omitempty"`
}

func toItem(it domain.Item, now time.Time) itemDTO {
	dto := itemDTO{
		ID: it.ID, Type: string(it.Type), FrontID: it.FrontID, ObjectiveID: it.ObjectiveID,
		Text: it.Text, Status: string(it.Status), Kind: string(it.Kind), Strength: it.Strength,
		StrengthConfirmed: it.StrengthConfirmed, Answer: it.Answer, Source: string(it.Source),
		SessionID: it.SessionID, Confidence: it.Confidence, CreatedAt: it.CreatedAt,
		UpdatedAt: it.UpdatedAt, ResolvedAt: it.ResolvedAt,
	}
	if it.DueAt != nil {
		dto.Due = it.DueAt.Format("2006-01-02")
		dto.Overdue = it.Status == domain.StatusActive && it.DueAt.Before(now)
	}
	return dto
}

type orderDTO struct {
	ID       uuid.UUID  `json:"id"`
	Text     string     `json:"text"`
	Status   string     `json:"status"`
	Kind     string     `json:"kind"`
	TargetID *uuid.UUID `json:"target_id"`
	Due      string     `json:"due,omitempty"`
	Overdue  bool       `json:"overdue"`
}

func toOrder(c comdomain.Commitment, now time.Time) orderDTO {
	kind := c.Kind
	if kind == "" {
		kind = comdomain.KindOrder
	}
	dto := orderDTO{ID: c.ID, Text: c.Text, Status: string(c.Status), Kind: string(kind), TargetID: c.TargetID}
	if c.DueAt != nil {
		dto.Due = c.DueAt.Format("2006-01-02")
		dto.Overdue = c.Status == comdomain.StatusOpen && c.DueAt.Before(now)
	}
	return dto
}

func toItems(items []domain.Item, now time.Time) []itemDTO {
	out := make([]itemDTO, 0, len(items))
	for _, it := range items {
		out = append(out, toItem(it, now))
	}
	return out
}

func toFronts(fronts []domain.Front) []frontDTO {
	out := make([]frontDTO, 0, len(fronts))
	for _, f := range fronts {
		out = append(out, toFront(f))
	}
	return out
}

// ---------- Handlers ----------

func (h *Handler) getMap(w http.ResponseWriter, r *http.Request) {
	m, err := h.manage.Map(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	orders := make([]orderDTO, 0, len(m.Orders))
	for _, c := range m.Orders {
		orders = append(orders, toOrder(c, now))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"fronts":   toFronts(m.Fronts),
		"items":    toItems(m.Items, now),
		"orders":   orders,
		"starters": domain.StarterFronts,
	})
}

// proposals serves the chat rail: what is waiting for you, and the fronts to
// file it under.
func (h *Handler) proposals(w http.ResponseWriter, r *http.Request) {
	items, err := h.manage.Proposals(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	fronts, err := h.manage.Fronts(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":  toItems(items, time.Now()),
		"fronts": toFronts(fronts),
	})
}

func (h *Handler) createFront(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if !decode(w, r, &req) {
		return
	}
	f, err := h.manage.CreateFront(r.Context(), req.Name)
	if err != nil {
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toFront(f))
}

func (h *Handler) addStarters(w http.ResponseWriter, r *http.Request) {
	fronts, err := h.manage.AddStarters(r.Context())
	if err != nil {
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toFronts(fronts))
}

func (h *Handler) updateFront(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name   *string `json:"name"`
		Status *string `json:"status"`
	}
	if !decode(w, r, &req) {
		return
	}
	var status *domain.FrontStatus
	if req.Status != nil {
		s := domain.FrontStatus(*req.Status)
		status = &s
	}
	f, err := h.manage.UpdateFront(r.Context(), id, req.Name, status)
	if err != nil {
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toFront(f))
}

func (h *Handler) createItem(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		FrontID     string `json:"front_id"`
		ObjectiveID string `json:"objective_id"`
		Kind        string `json:"kind"`
		Strength    int    `json:"strength"`
		Due         string `json:"due"`
	}
	if !decode(w, r, &req) {
		return
	}
	front, err := optionalID(req.FrontID)
	if err != nil {
		httpError(w, http.StatusBadRequest, "front_id: "+err.Error())
		return
	}
	objective, err := optionalID(req.ObjectiveID)
	if err != nil {
		httpError(w, http.StatusBadRequest, "objective_id: "+err.Error())
		return
	}
	due, err := parseDue(req.Due)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	item, err := h.manage.CreateItem(r.Context(), usecase.CreateInput{
		Type: domain.Type(req.Type), Text: req.Text, FrontID: front, ObjectiveID: objective,
		Kind: domain.Kind(req.Kind), Strength: req.Strength, DueAt: due,
	})
	if err != nil {
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toItem(item, time.Now()))
}

// updateItem takes any subset of fields. An empty front_id, objective_id or due
// clears it.
func (h *Handler) updateItem(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Text            *string `json:"text"`
		FrontID         *string `json:"front_id"`
		ObjectiveID     *string `json:"objective_id"`
		Kind            *string `json:"kind"`
		Strength        *int    `json:"strength"`
		ConfirmStrength bool    `json:"confirm_strength"`
		Status          *string `json:"status"`
		Due             *string `json:"due"`
	}
	if !decode(w, r, &req) {
		return
	}

	p := usecase.Patch{Text: req.Text, Strength: req.Strength, ConfirmStrength: req.ConfirmStrength}
	if req.FrontID != nil {
		if *req.FrontID == "" {
			p.ClearFront = true
		} else if p.FrontID, ok = parseID(w, *req.FrontID, "front_id"); !ok {
			return
		}
	}
	if req.ObjectiveID != nil {
		if *req.ObjectiveID == "" {
			p.ClearObjective = true
		} else if p.ObjectiveID, ok = parseID(w, *req.ObjectiveID, "objective_id"); !ok {
			return
		}
	}
	if req.Kind != nil {
		k := domain.Kind(*req.Kind)
		p.Kind = &k
	}
	if req.Status != nil {
		s := domain.Status(*req.Status)
		p.Status = &s
	}
	if req.Due != nil {
		due, err := parseDue(*req.Due)
		if err != nil {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		p.DueAt, p.ClearDue = due, due == nil
	}

	item, err := h.manage.Update(r.Context(), id, p)
	if err != nil {
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toItem(item, time.Now()))
}

func (h *Handler) lift(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Answer string `json:"answer"`
	}
	if !decode(w, r, &req) {
		return
	}
	item, err := h.manage.Lift(r.Context(), id, req.Answer)
	if err != nil {
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toItem(item, time.Now()))
}

func (h *Handler) recon(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	order, err := h.manage.SendRecon(r.Context(), id)
	if err != nil {
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toOrder(order, time.Now()))
}

func (h *Handler) aim(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		TargetID string `json:"target_id"`
	}
	if !decode(w, r, &req) {
		return
	}
	target, err := optionalID(req.TargetID)
	if err != nil {
		httpError(w, http.StatusBadRequest, "target_id: "+err.Error())
		return
	}
	order, err := h.manage.Aim(r.Context(), id, target)
	if err != nil {
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOrder(order, time.Now()))
}

// ---------- Helpers ----------

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON")
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

func parseID(w http.ResponseWriter, raw, field string) (*uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		httpError(w, http.StatusBadRequest, field+": invalid id")
		return nil, false
	}
	return &id, true
}

func optionalID(raw string) (*uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, errors.New("invalid id")
	}
	return &id, nil
}

// parseDue accepts an empty string as no date, which is how the UI clears one.
func parseDue(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	d, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return nil, errors.New("due must be YYYY-MM-DD")
	}
	end := d.Add(24*time.Hour - time.Second)
	return &end, nil
}

func respond(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, port.ErrNotFound), errors.Is(err, comport.ErrNotFound):
		httpError(w, http.StatusNotFound, "not found")
	case errors.Is(err, port.ErrDuplicate):
		httpError(w, http.StatusConflict, "that is already on the map")
	case errors.Is(err, usecase.ErrBadTransition):
		httpError(w, http.StatusConflict, err.Error())
	default:
		httpError(w, http.StatusBadRequest, err.Error())
	}
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
