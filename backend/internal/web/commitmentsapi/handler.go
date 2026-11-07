// Package commitmentsapi exposes open loops over JSON.
package commitmentsapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/commitments/domain"
	"github.com/an4eetos/decision-room/internal/commitments/port"
	"github.com/an4eetos/decision-room/internal/commitments/usecase"
)

type Handler struct {
	manage *usecase.Manage
}

func NewHandler(manage *usecase.Manage) *Handler {
	return &Handler{manage: manage}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/commitments", h.list)
	mux.HandleFunc("POST /api/commitments", h.create)
	mux.HandleFunc("PATCH /api/commitments/{id}", h.update)
}

type commitmentDTO struct {
	ID         uuid.UUID  `json:"id"`
	Text       string     `json:"text"`
	Status     string     `json:"status"`
	Due        string     `json:"due,omitempty"`
	Overdue    bool       `json:"overdue"`
	Source     string     `json:"source"`
	SessionID  *uuid.UUID `json:"session_id,omitempty"`
	Confidence float64    `json:"confidence"`
	// Kind is order or recon; TargetID is the campaign item it is aimed at.
	Kind string `json:"kind"`
	// Mode is the mode of the turn a proposal came from.
	Mode      string     `json:"mode,omitempty"`
	TargetID  *uuid.UUID `json:"target_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func toDTO(c domain.Commitment, now time.Time) commitmentDTO {
	dto := commitmentDTO{
		ID:         c.ID,
		Text:       c.Text,
		Status:     string(c.Status),
		Source:     string(c.Source),
		SessionID:  c.SessionID,
		Confidence: c.Confidence,
		Kind:       string(c.Kind),
		Mode:       c.ModeID,
		TargetID:   c.TargetID,
		CreatedAt:  c.CreatedAt,
		UpdatedAt:  c.UpdatedAt,
	}
	if dto.Kind == "" {
		dto.Kind = string(domain.KindOrder)
	}
	if c.DueAt != nil {
		dto.Due = c.DueAt.Format("2006-01-02")
		dto.Overdue = c.Status == domain.StatusOpen && c.DueAt.Before(now)
	}
	return dto
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	var statuses []domain.Status
	if raw := r.URL.Query().Get("status"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			s := domain.Status(strings.TrimSpace(part))
			if !s.Valid() {
				httpError(w, http.StatusBadRequest, "unknown status: "+string(s))
				return
			}
			statuses = append(statuses, s)
		}
	}

	items, err := h.manage.List(r.Context(), statuses)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now()
	out := make([]commitmentDTO, 0, len(items))
	for _, c := range items {
		out = append(out, toDTO(c, now))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Text      string     `json:"text"`
		Due       string     `json:"due"`
		SessionID *uuid.UUID `json:"session_id"`
		MessageID *uuid.UUID `json:"message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	due, err := parseDue(req.Due)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	source := domain.SourceManual
	if req.MessageID != nil {
		source = domain.SourceChat
	}

	c, err := h.manage.Create(r.Context(), usecase.CreateInput{
		Text:      req.Text,
		DueAt:     due,
		Source:    source,
		SessionID: req.SessionID,
		MessageID: req.MessageID,
	})
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toDTO(c, time.Now()))
}

// update handles both status changes and edits. Sending text or due is an edit;
// sending status is a transition. Both in one request applies the edit first.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}

	var req struct {
		Status *string `json:"status"`
		Text   *string `json:"text"`
		Due    *string `json:"due"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	var c domain.Commitment

	if req.Text != nil || req.Due != nil {
		current, err := h.manage.Get(r.Context(), id)
		if err != nil {
			respond(w, err)
			return
		}
		text := current.Text
		if req.Text != nil {
			text = *req.Text
		}
		due := current.DueAt
		if req.Due != nil {
			if due, err = parseDue(*req.Due); err != nil {
				httpError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if c, err = h.manage.Edit(r.Context(), id, text, due); err != nil {
			respond(w, err)
			return
		}
	}

	if req.Status != nil {
		if c, err = h.manage.SetStatus(r.Context(), id, domain.Status(*req.Status)); err != nil {
			respond(w, err)
			return
		}
	}

	if c.ID == uuid.Nil {
		httpError(w, http.StatusBadRequest, "nothing to update")
		return
	}
	writeJSON(w, http.StatusOK, toDTO(c, time.Now()))
}

// parseDue accepts an empty string as "no due date", which is how the UI clears
// one.
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
	case errors.Is(err, port.ErrNotFound):
		httpError(w, http.StatusNotFound, "not found")
	case errors.Is(err, port.ErrDuplicate):
		httpError(w, http.StatusConflict, "an identical commitment is already open")
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
