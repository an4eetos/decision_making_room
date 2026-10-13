package campaignapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/campaign/usecase"
)

// ---------- The staff ----------

// sitrep serves the map as the staff reads it: fronts with their lines and
// orders by name, what moved in the last day, and the readings. No model.
func (h *Handler) sitrep(w http.ResponseWriter, r *http.Request) {
	s, err := h.sitreps.Sitrep(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

type appraisalDTO struct {
	ID        uuid.UUID  `json:"id"`
	FrontID   *uuid.UUID `json:"front_id"`
	Tier      string     `json:"tier"`
	Generals  []string   `json:"generals"`
	Body      string     `json:"body"`
	SessionID *uuid.UUID `json:"session_id,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func toAppraisal(a domain.Appraisal) appraisalDTO {
	generals := a.Generals
	if generals == nil {
		generals = []string{}
	}
	return appraisalDTO{
		ID: a.ID, FrontID: a.FrontID, Tier: a.Tier, Generals: generals, Body: a.Body,
		SessionID: a.SessionID, CreatedAt: a.CreatedAt,
	}
}

// appraisals lists what the staff has said, latest first.
func (h *Handler) appraisals(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 50 {
			limit = n
		}
	}
	list, err := h.staff.Appraisals(r.Context(), limit)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]appraisalDTO, 0, len(list))
	for _, a := range list {
		out = append(out, toAppraisal(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

// appraise asks the staff to read the map now. It costs model calls at the
// asked depth and waits for them, so the page calls it with a long timeout.
func (h *Handler) appraise(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FrontID string `json:"front_id"`
		Tier    string `json:"tier"`
	}
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	front, err := optionalID(req.FrontID)
	if err != nil {
		httpError(w, http.StatusBadRequest, "front_id: "+err.Error())
		return
	}

	a, err := h.staff.Appraise(r.Context(), usecase.AppraiseInput{FrontID: front, Tier: req.Tier})
	switch {
	case errors.Is(err, usecase.ErrStaffBusy):
		httpError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, usecase.ErrNothingToAppraise):
		httpError(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAppraisal(a))
}

// openAppraisal turns an appraisal into a conversation, once.
func (h *Handler) openAppraisal(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	sessionID, err := h.staff.Open(r.Context(), id)
	if err != nil {
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"session_id": sessionID.String()})
}
