// Package beastapi exposes beast mode's board over JSON.
package beastapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/beast/domain"
	"github.com/an4eetos/decision-room/internal/beast/port"
	"github.com/an4eetos/decision-room/internal/beast/usecase"
)

type Handler struct {
	board *usecase.Board
}

func NewHandler(board *usecase.Board) *Handler {
	return &Handler{board: board}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/beast/sessions/{id}/tasks", h.list)
	mux.HandleFunc("POST /api/beast/sessions/{id}/tasks", h.create)
	mux.HandleFunc("PATCH /api/beast/tasks/{id}", h.update)
}

type taskDTO struct {
	ID       uuid.UUID `json:"id"`
	Number   int       `json:"number"`
	Text     string    `json:"text"`
	Minutes  int       `json:"minutes"`
	Deadline time.Time `json:"deadline"`
	Status   string    `json:"status"`
	Expiries int       `json:"expiries"`
	// Loop says the task finishes an open loop, which closes with it.
	Loop bool `json:"loop"`
}

func toDTO(t domain.Task) taskDTO {
	return taskDTO{
		ID:       t.ID,
		Number:   t.Seq,
		Text:     t.Text,
		Minutes:  t.Minutes,
		Deadline: t.DeadlineAt,
		Status:   string(t.Status),
		Expiries: t.Expiries,
		Loop:     t.CommitmentID != nil,
	}
}

// boardDTO carries the server's clock, so the page counts down against the
// server rather than a laptop clock that is two minutes off.
type boardDTO struct {
	Now   time.Time `json:"now"`
	Tasks []taskDTO `json:"tasks"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	tasks, err := h.board.List(r.Context(), sessionID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := boardDTO{Now: time.Now().UTC(), Tasks: make([]taskDTO, 0, len(tasks))}
	for _, t := range tasks {
		out.Tasks = append(out.Tasks, toDTO(t))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	var req struct {
		Text    string `json:"text"`
		Minutes int    `json:"minutes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	t, err := h.board.Add(r.Context(), sessionID, req.Text, req.Minutes)
	if err != nil {
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toDTO(t))
}

// update edits an open task. Status "done" reports it, which is the only way a
// clock stops; "abandoned" strikes it off. Nothing reopens.
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid task id")
		return
	}
	var req struct {
		Text    *string `json:"text"`
		Minutes *int    `json:"minutes"`
		Extend  int     `json:"extend"`
		Status  *string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	var t domain.Task
	if req.Status != nil {
		switch domain.Status(*req.Status) {
		case domain.StatusDone:
			t, err = h.board.Complete(r.Context(), id)
		case domain.StatusAbandoned:
			t, err = h.board.Abandon(r.Context(), id)
		default:
			httpError(w, http.StatusBadRequest, "status must be done or abandoned")
			return
		}
	} else {
		t, err = h.board.Edit(r.Context(), id, usecase.EditInput{Text: req.Text, Minutes: req.Minutes, Extend: req.Extend})
	}
	if err != nil {
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDTO(t))
}

func respond(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, port.ErrNotFound):
		httpError(w, http.StatusNotFound, "not found")
	case errors.Is(err, usecase.ErrBadTransition):
		httpError(w, http.StatusConflict, err.Error())
	case errors.Is(err, usecase.ErrEmptyText):
		httpError(w, http.StatusBadRequest, err.Error())
	default:
		httpError(w, http.StatusInternalServerError, err.Error())
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
