// Package checkinapi exposes check-ins over JSON.
package checkinapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/checkin/domain"
	"github.com/an4eetos/decision-room/internal/checkin/port"
	"github.com/an4eetos/decision-room/internal/checkin/usecase"
)

type Handler struct {
	gen       *usecase.Generate
	scheduler *usecase.Scheduler
}

func NewHandler(gen *usecase.Generate, scheduler *usecase.Scheduler) *Handler {
	return &Handler{gen: gen, scheduler: scheduler}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/checkins/unseen", h.unseen)
	mux.HandleFunc("POST /api/checkins/now", h.now)
	mux.HandleFunc("POST /api/checkins/{id}/open", h.open)
	mux.HandleFunc("POST /api/checkins/{id}/dismiss", h.dismiss)
}

type checkinDTO struct {
	ID        uuid.UUID `json:"id"`
	Kind      string    `json:"kind"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

func toDTO(c domain.CheckIn) checkinDTO {
	return checkinDTO{ID: c.ID, Kind: string(c.Kind), Title: c.Title, Body: c.Body, CreatedAt: c.CreatedAt}
}

// unseen is polled by every page, so it is one indexed query and nothing else.
func (h *Handler) unseen(w http.ResponseWriter, r *http.Request) {
	items, err := h.gen.Unseen(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	out := make([]checkinDTO, 0, len(items))
	for _, c := range items {
		out = append(out, toDTO(c))
	}

	body := map[string]any{
		"count":     len(out),
		"items":     out,
		"scheduled": h.scheduler.Enabled(),
	}
	if next := h.scheduler.Next(); next != nil {
		body["next_at"] = next.Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, body)
}

func (h *Handler) now(w http.ResponseWriter, r *http.Request) {
	c, err := h.gen.Now(r.Context())
	if errors.Is(err, port.ErrAlreadyFired) {
		httpError(w, http.StatusConflict, "you just asked for one — give it a minute")
		return
	}
	if err != nil {
		httpError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, toDTO(c))
}

func (h *Handler) open(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	sessionID, err := h.gen.Open(r.Context(), id)
	if errors.Is(err, port.ErrNotFound) {
		httpError(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"session_id": sessionID.String()})
}

func (h *Handler) dismiss(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.gen.Dismiss(r.Context(), id); errors.Is(err, port.ErrNotFound) {
		httpError(w, http.StatusNotFound, "not found")
		return
	} else if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func pathID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return uuid.Nil, false
	}
	return id, true
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
