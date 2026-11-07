// Package settingsapi exposes the settings a client may change.
package settingsapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/an4eetos/decision-room/internal/settings/domain"
	"github.com/an4eetos/decision-room/internal/settings/usecase"
)

// maxBodyBytes covers every setting at its longest.
const maxBodyBytes = 128 << 10

type Handler struct {
	svc *usecase.Service
}

func NewHandler(svc *usecase.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", h.get)
	mux.HandleFunc("PUT /api/settings", h.put)
	mux.HandleFunc("POST /api/settings/test", h.test)
}

// changesRequest maps a key to its new value; null removes the stored value,
// back to the default. Keys left out are left alone.
type changesRequest struct {
	Values map[string]*string `json:"values"`
}

func (h *Handler) get(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.svc.View())
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	var req changesRequest
	if !decode(w, r, &req) {
		return
	}
	if len(req.Values) == 0 {
		httpError(w, http.StatusBadRequest, "values is empty")
		return
	}
	view, err := h.svc.Update(r.Context(), req.Values)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// test tries the providers with the given changes over what is stored,
// without saving anything. It makes real calls, so it can take seconds.
func (h *Handler) test(w http.ResponseWriter, r *http.Request) {
	var req changesRequest
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	result, err := h.svc.Test(r.Context(), req.Values)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, usecase.ErrUnknownKey), errors.Is(err, domain.ErrInvalidValue):
		httpError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, usecase.ErrLocked), errors.Is(err, domain.ErrNoSettingsKey):
		httpError(w, http.StatusConflict, err.Error())
	default:
		log.Printf("settings: %v", err)
		httpError(w, http.StatusInternalServerError, "internal error")
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func httpError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	// Masked or not, settings are not for a shared cache.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
