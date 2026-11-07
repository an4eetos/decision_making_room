package domain

import (
	"time"

	"github.com/google/uuid"
)

// Theatre is the campaign laid out as a map: one lane per front, one row per
// objective, and on each row the front line — how far you have advanced
// towards the objective, moved by the opposition cleared, the fog lifted and
// the orders carried out.
//
// It is what a daily snapshot stores, so the map of any past day is drawn by
// the same renderer as today's. Ground held is what moved in the last thirty
// days: a front nobody has touched in a month shows its line back at the
// start. Let a front go quiet and it shows.
type Theatre struct {
	// Day is the snapshot's date as YYYY-MM-DD, empty for the live map.
	Day     string         `json:"day,omitempty"`
	TakenAt time.Time      `json:"taken_at"`
	Fronts  []TheatreFront `json:"fronts"`
	Totals  TheatreTotals  `json:"totals"`
}

// TheatreFront is one lane. The unassigned lane, for items filed under no
// active front, has the nil ID.
type TheatreFront struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// Line is how far the front has advanced, 0 to 1: the ground held across
	// every row, weighted by how much stood in the way.
	Line float64      `json:"line"`
	Rows []TheatreRow `json:"rows"`
}

// TheatreRow is one objective with what stands between you and it. The loose
// row, with no objective, holds the front's opposition and fog that bears on
// no objective here.
type TheatreRow struct {
	Objective *TheatrePost `json:"objective,omitempty"`
	// Progress is the row's ground held, 0 to 1. A withdrawn objective's row
	// is drawn but counts for nothing.
	Progress  float64       `json:"progress"`
	Obstacles []TheatrePost `json:"obstacles"`
	Unknowns  []TheatrePost `json:"unknowns"`
	Orders    TheatreOrders `json:"orders"`
}

// TheatrePost is one item as drawn: an objective, an enemy position or a patch
// of fog.
type TheatrePost struct {
	ID     uuid.UUID `json:"id"`
	Text   string    `json:"text"`
	Status Status    `json:"status"`
	// Obstacles only. Estimated is a strength nobody has confirmed.
	Kind      Kind `json:"kind,omitempty"`
	Strength  int  `json:"strength,omitempty"`
	Estimated bool `json:"estimated,omitempty"`
	// Objectives only.
	Due     string `json:"due,omitempty"`
	Overdue bool   `json:"overdue,omitempty"`
}

// TheatreOrders counts the orders aimed at a row: at its objective, or at the
// opposition and fog in front of it.
type TheatreOrders struct {
	Open  int `json:"open"`
	Done  int `json:"done"`
	Recon int `json:"recon"`
}

// TheatreTotals is the whole map in numbers.
type TheatreTotals struct {
	Objectives int `json:"objectives"`
	Taken      int `json:"taken"`
	Withdrawn  int `json:"withdrawn"`
	Opposition int `json:"opposition"`
	Cleared    int `json:"cleared"`
	Fog        int `json:"fog"`
	Lifted     int `json:"lifted"`
	OrdersOpen int `json:"orders_open"`
	OrdersDone int `json:"orders_done"`
}

// Snapshot is the theatre as it stood at the end of one day.
type Snapshot struct {
	// Day is the local date, YYYY-MM-DD.
	Day     string
	TakenAt time.Time
	Theatre Theatre
}
