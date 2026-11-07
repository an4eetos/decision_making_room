package usecase

import (
	"context"
	"errors"

	camdomain "github.com/an4eetos/decision-room/internal/campaign/domain"
	"github.com/an4eetos/decision-room/internal/relocation/domain"
	"github.com/an4eetos/decision-room/internal/relocation/port"
	"github.com/an4eetos/decision-room/internal/relocation/service"
)

// Deployments reads the stays deployed on a campaign front, as the campaign
// asks for them: a forward base with its own supply line. It implements the
// campaign's DeploymentSource, so the campaign never imports the planner.
//
// The supply line is the plan's own arithmetic — the budget this page shows,
// with the share of it that is an estimate — so the map and the planner
// never disagree about what a stay costs.
type Deployments struct {
	plans port.PlanRepository
}

func NewDeployments(plans port.PlanRepository) *Deployments {
	return &Deployments{plans: plans}
}

// Deployments lists the live plans that sit on a front. A finished or
// abandoned stay is no longer a forward base.
func (d *Deployments) Deployments(ctx context.Context) ([]camdomain.Deployment, error) {
	plans, err := d.plans.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	var out []camdomain.Deployment
	for _, summary := range plans {
		if summary.FrontID == nil || (summary.Status != domain.PlanDraft && summary.Status != domain.PlanActive) {
			continue
		}
		plan, err := d.plans.GetPlan(ctx, summary.ID)
		if errors.Is(err, port.ErrNotFound) {
			continue // deleted between the two reads
		}
		if err != nil {
			return nil, err
		}
		out = append(out, deployment(plan))
	}
	return out, nil
}

func deployment(plan domain.Plan) camdomain.Deployment {
	budget := service.ComputeBudget(plan)
	dep := camdomain.Deployment{
		PlanID:         plan.ID,
		FrontID:        *plan.FrontID,
		Destination:    plan.Destination,
		Status:         string(plan.Status),
		Currency:       budget.Currency,
		Cost:           budget.Total,
		EstimatedShare: budget.EstimatedShare,
		Unpriced:       budget.UnpricedItems,
		ItemsNeeded:    budget.ItemsNeeded,
		ItemsResolved:  budget.ItemsResolved,
	}
	if !plan.ArriveOn.IsZero() {
		dep.ArriveOn = plan.ArriveOn.Format("2006-01-02")
	}
	if !plan.DepartOn.IsZero() {
		dep.DepartOn = plan.DepartOn.Format("2006-01-02")
	}
	for _, p := range plan.Pitfalls {
		if p.Severity == domain.SeverityCritical && p.AcknowledgedAt == nil {
			dep.CriticalOpen++
		}
	}
	return dep
}
