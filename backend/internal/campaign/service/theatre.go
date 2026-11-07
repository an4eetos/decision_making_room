package service

import (
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/an4eetos/decision-room/internal/campaign/domain"
	comdomain "github.com/an4eetos/decision-room/internal/commitments/domain"
)

// Weights for ground held. An objective taken moves the line on its own, so
// it carries weight even when nothing stood in its way; opposition weighs its
// strength, with an unestimated one counted as dug in; fog and orders weigh
// one each. Deterministic by design: the map should not need a model to say
// where the line is.
const (
	weightObjective = 2
	weightUnknown   = 1
	weightOrder     = 1
)

// Layout draws the campaign as a theatre: active fronts as lanes, in their
// display order, each with one row per objective and a loose row for what
// bears on no objective there. Proposals stay off: nothing goes on the map
// without your agreement. Items under no active front share an unassigned
// lane at the end.
func Layout(fronts []domain.Front, items []domain.Item, orders []comdomain.Commitment, now time.Time) domain.Theatre {
	t := domain.Theatre{TakenAt: now, Fronts: []domain.TheatreFront{}}

	active := map[uuid.UUID]bool{}
	for _, f := range fronts {
		if f.Status == domain.FrontActive {
			active[f.ID] = true
		}
	}

	// Orders by target, so a row can count what is aimed at its objective and
	// at the opposition in front of it.
	byTarget := map[uuid.UUID][]comdomain.Commitment{}
	for _, o := range orders {
		if o.TargetID == nil || o.Status == comdomain.StatusProposed || o.Status == comdomain.StatusDropped {
			continue
		}
		byTarget[*o.TargetID] = append(byTarget[*o.TargetID], o)
	}

	byFront := map[uuid.UUID][]domain.Item{}
	var unassigned []domain.Item
	for _, it := range items {
		if it.Status == domain.StatusProposed || it.Status == domain.StatusDropped {
			continue
		}
		if it.FrontID == nil || !active[*it.FrontID] {
			unassigned = append(unassigned, it)
			continue
		}
		byFront[*it.FrontID] = append(byFront[*it.FrontID], it)
	}

	for _, f := range fronts {
		if f.Status != domain.FrontActive {
			continue
		}
		t.Fronts = append(t.Fronts, lane(f.ID, f.Name, byFront[f.ID], byTarget, now, &t.Totals))
	}
	if len(unassigned) > 0 {
		t.Fronts = append(t.Fronts, lane(uuid.Nil, "No front", unassigned, byTarget, now, &t.Totals))
	}
	return t
}

func lane(id uuid.UUID, name string, items []domain.Item, byTarget map[uuid.UUID][]comdomain.Commitment, now time.Time, totals *domain.TheatreTotals) domain.TheatreFront {
	front := domain.TheatreFront{ID: id, Name: name, Rows: []domain.TheatreRow{}}

	var objectives []domain.Item
	here := map[uuid.UUID]bool{}
	for _, it := range items {
		if it.Type == domain.TypeObjective {
			objectives = append(objectives, it)
			here[it.ID] = true
		}
	}
	// Live objectives first, then taken, then withdrawn; stable, so the
	// repository's order (due date, then recency) holds within each.
	sort.SliceStable(objectives, func(i, j int) bool {
		return rank(objectives[i].Status) < rank(objectives[j].Status)
	})

	var held, total float64
	for _, o := range objectives {
		row, h, w := buildRow(&o, children(items, &o.ID, here), byTarget, now, totals)
		front.Rows = append(front.Rows, row)
		if o.Status != domain.StatusWithdrawn {
			held, total = held+h, total+w
		}
	}
	if loose := children(items, nil, here); len(loose) > 0 {
		row, h, w := buildRow(nil, loose, byTarget, now, totals)
		front.Rows = append(front.Rows, row)
		held, total = held+h, total+w
	}
	if total > 0 {
		front.Line = held / total
	}
	return front
}

func rank(s domain.Status) int {
	switch s {
	case domain.StatusActive:
		return 0
	case domain.StatusResolved:
		return 1
	default:
		return 2
	}
}

// children picks the opposition and fog bearing on one objective, or with a
// nil objective the loose ones: tied to no objective, or to one that is not on
// this front.
func children(items []domain.Item, objective *uuid.UUID, here map[uuid.UUID]bool) []domain.Item {
	var out []domain.Item
	for _, it := range items {
		if it.Type == domain.TypeObjective {
			continue
		}
		switch {
		case objective != nil:
			if it.ObjectiveID != nil && *it.ObjectiveID == *objective {
				out = append(out, it)
			}
		case it.ObjectiveID == nil || !here[*it.ObjectiveID]:
			out = append(out, it)
		}
	}
	return out
}

// buildRow lays out one row and returns it with the ground held and the
// weight of everything on it.
func buildRow(objective *domain.Item, kids []domain.Item, byTarget map[uuid.UUID][]comdomain.Commitment, now time.Time, totals *domain.TheatreTotals) (domain.TheatreRow, float64, float64) {
	row := domain.TheatreRow{Obstacles: []domain.TheatrePost{}, Unknowns: []domain.TheatrePost{}}
	var held, total float64

	countOrders := func(id uuid.UUID) {
		for _, o := range byTarget[id] {
			total += weightOrder
			if o.Status == comdomain.StatusDone {
				held += weightOrder
				row.Orders.Done++
				totals.OrdersDone++
				continue
			}
			row.Orders.Open++
			totals.OrdersOpen++
			if o.Kind == comdomain.KindRecon {
				row.Orders.Recon++
			}
		}
	}

	if objective != nil {
		p := post(*objective, now)
		row.Objective = &p
		totals.Objectives++
		switch objective.Status {
		case domain.StatusResolved:
			totals.Taken++
			held += weightObjective
		case domain.StatusWithdrawn:
			totals.Withdrawn++
		}
		total += weightObjective
		countOrders(objective.ID)
	}

	for _, it := range kids {
		p := post(it, now)
		switch it.Type {
		case domain.TypeObstacle:
			row.Obstacles = append(row.Obstacles, p)
			w := float64(p.Strength)
			total += w
			if it.Status == domain.StatusResolved {
				held += w
				totals.Cleared++
			} else {
				totals.Opposition++
			}
		case domain.TypeUnknown:
			row.Unknowns = append(row.Unknowns, p)
			total += weightUnknown
			if it.Status == domain.StatusResolved {
				held += weightUnknown
				totals.Lifted++
			} else {
				totals.Fog++
			}
		}
		countOrders(it.ID)
	}

	if objective != nil && objective.Status == domain.StatusWithdrawn {
		return row, 0, 0
	}
	if total > 0 {
		row.Progress = held / total
	}
	return row, held, total
}

func post(it domain.Item, now time.Time) domain.TheatrePost {
	p := domain.TheatrePost{ID: it.ID, Text: it.Text, Status: it.Status}
	if it.Type == domain.TypeObstacle {
		p.Kind = it.Kind
		p.Strength = it.Strength
		if p.Strength == 0 {
			p.Strength = domain.StrengthDugIn
		}
		p.Estimated = !it.StrengthConfirmed
	}
	if it.Type == domain.TypeObjective && it.DueAt != nil {
		p.Due = it.DueAt.Format("2006-01-02")
		p.Overdue = it.Status == domain.StatusActive && it.DueAt.Before(now)
	}
	return p
}
