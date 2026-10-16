package campaignapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/usecase"
)

// ---------- Logistics ----------

// logistics serves the supply lines with what they are read from: the
// sorties of the last two weeks, so a mistaken one can be taken back, and
// the day the page should tap energy for.
func (h *Handler) logistics(w http.ResponseWriter, r *http.Request) {
	m, err := h.manage.Map(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	supply, err := h.logs.Read(r.Context(), m, time.Now())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sorties, err := h.logs.Sorties(r.Context(), 14)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"logistics":      supply,
		"sorties":        sorties,
		"today":          h.logs.Today(),
		"sortie_minutes": h.logs.SortieMinutes(),
	})
}

func (h *Handler) listSorties(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	sorties, err := h.logs.Sorties(r.Context(), days)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sorties)
}

// logSortie records a focus block on a front. Minutes default to the
// configured block, the day to today.
func (h *Handler) logSortie(w http.ResponseWriter, r *http.Request) {
	var req struct {
		FrontID string `json:"front_id"`
		OrderID string `json:"order_id"`
		Minutes int    `json:"minutes"`
		Day     string `json:"day"`
		Note    string `json:"note"`
	}
	if !decode(w, r, &req) {
		return
	}
	front, err := uuid.Parse(req.FrontID)
	if err != nil {
		httpError(w, http.StatusBadRequest, "front_id: a sortie is flown on a front")
		return
	}
	order, err := optionalID(req.OrderID)
	if err != nil {
		httpError(w, http.StatusBadRequest, "order_id: "+err.Error())
		return
	}
	s, err := h.logs.LogSortie(r.Context(), usecase.SortieInput{
		FrontID: front, OrderID: order, Minutes: req.Minutes, Day: req.Day, Note: req.Note,
	})
	if err != nil {
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s)
}

func (h *Handler) deleteSortie(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.logs.DeleteSortie(r.Context(), id); err != nil {
		respond(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) listEnergy(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	reports, err := h.logs.Energy(r.Context(), days)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"today": h.logs.Today(), "reports": reports})
}

// reportEnergy is the one tap. An empty day is today; the evening check-in
// sends its own day, so a tap after midnight lands on the evening it was
// about.
func (h *Handler) reportEnergy(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Day   string `json:"day"`
		Level int    `json:"level"`
	}
	if !decode(w, r, &req) {
		return
	}
	report, err := h.logs.ReportEnergy(r.Context(), req.Day, req.Level)
	if err != nil {
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (h *Handler) clearEnergy(w http.ResponseWriter, r *http.Request) {
	if err := h.logs.ClearEnergy(r.Context(), r.PathValue("day")); err != nil {
		respond(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listFronts serves the fronts alone, for pages that file something under
// one without loading the map: the relocation planner's deployment picker.
func (h *Handler) listFronts(w http.ResponseWriter, r *http.Request) {
	fronts, err := h.manage.Fronts(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toFronts(fronts))
}
