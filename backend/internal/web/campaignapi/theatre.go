package campaignapi

import (
	"log"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
)

// ---------- The theatre map ----------

// load returns the live theatre, or with a day the snapshot for it, plus the
// earlier theatre to draw the previous front line against.
func (h *Handler) load(r *http.Request) (domain.Theatre, *domain.Theatre, error) {
	ctx := r.Context()
	day := r.URL.Query().Get("day")

	var (
		theatre domain.Theatre
		err     error
	)
	if day == "" {
		day = h.snapshots.Today()
		theatre, err = h.manage.Theatre(ctx)
	} else {
		theatre, err = h.snapshots.Day(ctx, day)
	}
	if err != nil {
		return domain.Theatre{}, nil, err
	}

	prior, err := h.snapshots.Prior(ctx, day)
	if err != nil {
		// The map still draws without its earlier line.
		log.Printf("campaign: prior snapshot: %v", err)
		prior = nil
	}
	return theatre, prior, nil
}

// mapSVG serves the map as an image: today's, or a past day's with ?day=.
func (h *Handler) mapSVG(w http.ResponseWriter, r *http.Request) {
	theatre, prior, err := h.load(r)
	if err != nil {
		respond(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(RenderSVG(theatre, prior))
}

// theatre serves the laid-out map as JSON, for anything that wants to draw
// it another way.
func (h *Handler) theatre(w http.ResponseWriter, r *http.Request) {
	theatre, prior, err := h.load(r)
	if err != nil {
		respond(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"theatre": theatre, "prior": prior})
}

type timelapseFrontDTO struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Line float64   `json:"line"`
}

type timelapseDayDTO struct {
	Day     string               `json:"day"`
	TakenAt time.Time            `json:"taken_at"`
	Fronts  []timelapseFrontDTO  `json:"fronts"`
	Totals  domain.TheatreTotals `json:"totals"`
}

// timelapse lists the days on record, oldest first, with each day's lines and
// totals; the frames themselves come from map.svg?day=.
func (h *Handler) timelapse(w http.ResponseWriter, r *http.Request) {
	snaps, err := h.snapshots.Timelapse(r.Context())
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	days := make([]timelapseDayDTO, 0, len(snaps))
	for _, s := range snaps {
		fronts := make([]timelapseFrontDTO, 0, len(s.Theatre.Fronts))
		for _, f := range s.Theatre.Fronts {
			fronts = append(fronts, timelapseFrontDTO{ID: f.ID, Name: f.Name, Line: f.Line})
		}
		days = append(days, timelapseDayDTO{Day: s.Day, TakenAt: s.TakenAt, Fronts: fronts, Totals: s.Theatre.Totals})
	}
	writeJSON(w, http.StatusOK, map[string]any{"days": days, "today": h.snapshots.Today()})
}
