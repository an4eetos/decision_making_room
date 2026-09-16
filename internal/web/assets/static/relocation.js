// Relocation planner. Kept separate from app.js because it shares nothing with
// the chat page beyond apiJSON and escapeHTML.

const CATEGORY_LABELS = {
    arrival: "First 48 hours",
    housing: "Housing",
    bathroom: "Bathroom",
    skincare: "Face and skin",
    kitchen: "Kitchen",
    bedding: "Bedding",
    laundry: "Laundry",
    workspace: "Workspace",
    health: "Health",
    connectivity: "Money and connectivity",
    admin: "Admin and legal",
    climate: "Climate",
    cleaning: "Cleaning",
    exit: "Leaving",
};

const SEVERITY_LABELS = {
    critical: "Critical",
    costly: "Expensive",
    annoying: "Annoying",
};

function money(value, currency) {
    if (value === null || value === undefined) {
        return "—";
    }
    const rounded = Math.round(value);
    return `${rounded.toLocaleString()} ${currency}`;
}

function setupRelocation() {
    const list = document.getElementById("reloc-plans");
    const form = document.getElementById("reloc-form");
    const planEl = document.getElementById("reloc-plan");
    const errorEl = document.getElementById("reloc-error");
    if (!list || !form || !planEl) {
        return;
    }

    let activeID = null;
    let activePlan = null;

    function fail(error) {
        showMessage(errorEl, error.message, "error");
    }

    function clearError() {
        errorEl.innerHTML = "";
    }

    async function refreshPlans() {
        const plans = await apiJSON("/api/relocation/plans");
        if (plans.length === 0) {
            list.innerHTML = '<p class="muted">No stays yet.</p>';
        } else {
            list.innerHTML = plans.map((p) => `
                <button type="button" class="reloc-plan-item ${p.id === activeID ? "active" : ""}"
                        data-plan-id="${escapeHTML(p.id)}">
                    <span class="reloc-plan-dest">${escapeHTML(p.destination)}</span>
                    <span class="muted">${p.nights} nights${p.arrive_on ? " · from " + escapeHTML(p.arrive_on) : ""}</span>
                </button>
            `).join("");
        }
        return plans;
    }

    function showForm(show) {
        form.hidden = !show;
        form.querySelector("[data-cancel]").hidden = !activePlan;
        if (show) {
            planEl.innerHTML = "";
        }
    }

    async function loadPlan(id) {
        activeID = id;
        planEl.innerHTML = '<p class="muted">Loading...</p>';
        try {
            activePlan = await apiJSON(`/api/relocation/plans/${id}`);
            showForm(false);
            renderPlan();
            await refreshPlans();
        } catch (error) {
            fail(error);
        }
    }

    function renderPlan() {
        const plan = activePlan;
        const currency = plan.budget.currency;

        const groups = new Map();
        for (const item of plan.items) {
            if (!groups.has(item.category)) {
                groups.set(item.category, []);
            }
            groups.get(item.category).push(item);
        }

        const categoryTotals = new Map(plan.budget.by_category.map((c) => [c.category, c]));

        const sections = [...groups.entries()].map(([category, items]) => {
            const total = categoryTotals.get(category);
            return `
            <section class="reloc-group">
                <header class="reloc-group-head">
                    <h3>${escapeHTML(CATEGORY_LABELS[category] || category)}</h3>
                    <span class="muted">${total ? money(total.total, currency) : ""}</span>
                </header>
                <ul class="reloc-items">
                    ${items.map(renderItem).join("")}
                </ul>
            </section>`;
        }).join("");

        planEl.innerHTML = `
            ${renderHeader(plan)}
            ${renderBudget(plan.budget)}
            ${renderPitfalls(plan.pitfalls)}
            <div class="reloc-groups">${sections}</div>
        `;
    }

    function renderHeader(plan) {
        const bits = [
            `${plan.nights} nights`,
            plan.party_size > 1 ? `${plan.party_size} people` : null,
            plan.housing || null,
            plan.climate || null,
            plan.budget_style,
        ].filter(Boolean);

        return `
        <header class="card reloc-header">
            <div>
                <h1>${escapeHTML(plan.destination)}</h1>
                <p class="muted">${bits.map(escapeHTML).join(" · ")}</p>
            </div>
            <div class="reloc-header-actions">
                <button type="button" class="btn" data-action="edit">Edit stay</button>
                <button type="button" class="btn primary" data-action="reprice">Price for this city</button>
            </div>
        </header>`;
    }

    function renderBudget(budget) {
        const estimatedPct = Math.round(budget.estimated_share * 100);

        // The honesty line. A budget that cannot tell you how much of itself is a
        // guess is worse than one with no numbers at all.
        let caveat = "";
        if (budget.estimated_share > 0) {
            caveat = `<p class="reloc-caveat">
                ${estimatedPct}% of this total is estimated, not confirmed.
                Type a real price on any line to replace the estimate — it is
                remembered for next time you come here.
            </p>`;
        }
        if (budget.mixed_currencies) {
            caveat += `<p class="reloc-caveat warn">
                Some lines are priced in ${escapeHTML((budget.other_currencies || []).join(", "))}
                and are excluded from this total. Use <strong>Price for this city</strong>
                to convert everything to one currency.
            </p>`;
        }

        return `
        <section class="card reloc-budget">
            <div class="reloc-budget-figures">
                <div class="reloc-figure">
                    <span class="reloc-figure-value">${money(budget.total, budget.currency)}</span>
                    <span class="muted">setup cost</span>
                </div>
                <div class="reloc-figure">
                    <span class="reloc-figure-value">${budget.items_needed}</span>
                    <span class="muted">still to sort</span>
                </div>
                <div class="reloc-figure">
                    <span class="reloc-figure-value">${money(budget.already_covered, budget.currency)}</span>
                    <span class="muted">covered by what you own</span>
                </div>
                ${budget.unpriced_items ? `
                <div class="reloc-figure">
                    <span class="reloc-figure-value">${budget.unpriced_items}</span>
                    <span class="muted">unpriced</span>
                </div>` : ""}
            </div>
            ${caveat}
        </section>`;
    }

    function renderPitfalls(pitfalls) {
        if (!pitfalls || pitfalls.length === 0) {
            return "";
        }
        const open = pitfalls.filter((p) => !p.acknowledged);
        const done = pitfalls.length - open.length;

        return `
        <section class="card reloc-pitfalls">
            <h2>Worth checking before you commit</h2>
            <p class="muted">Only the ones that apply to this stay.${done ? ` ${done} handled.` : ""}</p>
            ${pitfalls.map((p) => `
                <details class="reloc-pitfall sev-${escapeHTML(p.severity)} ${p.acknowledged ? "done" : ""}">
                    <summary>
                        <span class="reloc-sev">${escapeHTML(SEVERITY_LABELS[p.severity] || p.severity)}</span>
                        <span>${escapeHTML(p.title)}</span>
                    </summary>
                    <p>${escapeHTML(p.body)}</p>
                    <p class="reloc-pitfall-action"><strong>Do this:</strong> ${escapeHTML(p.action)}</p>
                    <label class="reloc-ack">
                        <input type="checkbox" data-pitfall-id="${escapeHTML(p.id)}" ${p.acknowledged ? "checked" : ""}>
                        Handled
                    </label>
                </details>
            `).join("")}
        </section>`;
    }

    function renderItem(item) {
        const cost = item.unit_cost === null
            ? `<span class="muted">no price</span>`
            : `${money(item.unit_cost, item.currency)}`;

        const estimateFlag = item.estimated && item.unit_cost
            ? `<span class="reloc-estimate" title="Estimated, not confirmed">est${
                item.confidence !== undefined && item.confidence !== null
                    ? ` ${Math.round(item.confidence * 100)}%` : ""}</span>`
            : "";

        return `
        <li class="reloc-item status-${escapeHTML(item.status)}" data-item-id="${escapeHTML(item.id)}">
            <select class="reloc-status" data-item-id="${escapeHTML(item.id)}">
                <option value="needed"  ${item.status === "needed" ? "selected" : ""}>Need</option>
                <option value="have"    ${item.status === "have" ? "selected" : ""}>Have it</option>
                <option value="bought"  ${item.status === "bought" ? "selected" : ""}>Bought</option>
                <option value="skipped" ${item.status === "skipped" ? "selected" : ""}>Skip</option>
            </select>

            <div class="reloc-item-body">
                <span class="reloc-item-name">
                    ${escapeHTML(item.name)}
                    ${item.commonly_forgotten ? '<span class="reloc-forgotten" title="Commonly forgotten">often missed</span>' : ""}
                </span>
                ${item.note ? `<span class="reloc-item-note muted">${escapeHTML(item.note)}</span>` : ""}
            </div>

            <span class="reloc-qty muted">${item.quantity % 1 === 0 ? item.quantity : item.quantity.toFixed(1)}${item.unit ? " " + escapeHTML(item.unit) : ""}</span>

            <span class="reloc-cost">
                <input class="reloc-price" type="number" min="0" step="1"
                       value="${item.unit_cost === null ? "" : item.unit_cost}"
                       placeholder="${cost.replace(/<[^>]*>/g, "")}"
                       data-item-id="${escapeHTML(item.id)}"
                       aria-label="Unit price for ${escapeHTML(item.name)}">
                ${estimateFlag}
            </span>
        </li>`;
    }

    list.addEventListener("click", async (e) => {
        const btn = e.target.closest("[data-plan-id]");
        if (!btn) {
            return;
        }
        clearError();
        await loadPlan(btn.dataset.planId);
    });

    document.getElementById("reloc-new").addEventListener("click", () => {
        activePlan = null;
        activeID = null;
        form.reset();
        clearError();
        showForm(true);
    });

    form.addEventListener("click", (e) => {
        if (e.target.matches("[data-cancel]")) {
            showForm(false);
            renderPlan();
        }
    });

    form.addEventListener("submit", async (event) => {
        event.preventDefault();
        clearError();

        const data = Object.fromEntries(new FormData(form).entries());
        data.party_size = Number(data.party_size) || 1;
        data.country_code = (data.country_code || "").toUpperCase();
        data.currency = (data.currency || "USD").toUpperCase();

        const button = form.querySelector("button[type=submit]");
        button.disabled = true;
        try {
            const editing = Boolean(activePlan);
            const plan = await apiJSON(
                editing ? `/api/relocation/plans/${activePlan.id}` : "/api/relocation/plans",
                {
                    method: editing ? "PATCH" : "POST",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify(data),
                },
            );
            activePlan = plan;
            activeID = plan.id;
            showForm(false);
            renderPlan();
            await refreshPlans();
        } catch (error) {
            fail(error);
        } finally {
            button.disabled = false;
        }
    });

    planEl.addEventListener("click", async (e) => {
        const action = e.target.closest("[data-action]")?.dataset.action;
        if (!action || !activePlan) {
            return;
        }
        clearError();

        if (action === "edit") {
            for (const [key, value] of Object.entries(activePlan)) {
                const field = form.elements[key];
                if (field && value !== null && value !== undefined) {
                    field.value = value;
                }
            }
            showForm(true);
            return;
        }

        if (action === "reprice") {
            const button = e.target;
            button.disabled = true;
            button.textContent = "Pricing...";
            try {
                activePlan = await apiJSON(
                    `/api/relocation/plans/${activePlan.id}/build?reprice=true`,
                    { method: "POST", timeoutMs: 120000 },
                );
                renderPlan();
            } catch (error) {
                fail(error);
                button.disabled = false;
                button.textContent = "Price for this city";
            }
        }
    });

    planEl.addEventListener("change", async (e) => {
        clearError();

        if (e.target.matches(".reloc-status")) {
            await patchItem(e.target.dataset.itemId, { status: e.target.value });
            return;
        }
        if (e.target.matches(".reloc-price")) {
            const raw = e.target.value.trim();
            if (raw === "") {
                return;
            }
            await patchItem(e.target.dataset.itemId, {
                unit_cost: Number(raw),
                currency: activePlan.currency,
            });
            return;
        }
        if (e.target.matches("[data-pitfall-id]")) {
            try {
                await apiJSON(`/api/relocation/pitfalls/${e.target.dataset.pitfallId}`, {
                    method: "PATCH",
                    headers: { "Content-Type": "application/json" },
                    body: JSON.stringify({ acknowledged: e.target.checked }),
                });
                await loadPlan(activePlan.id);
            } catch (error) {
                fail(error);
            }
        }
    });

    async function patchItem(itemID, patch) {
        try {
            await apiJSON(`/api/relocation/items/${itemID}`, {
                method: "PATCH",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify(patch),
            });
            // Refetch rather than patching in place: one edit changes the budget,
            // the category subtotal and the estimated share all at once.
            activePlan = await apiJSON(`/api/relocation/plans/${activePlan.id}`);
            renderPlan();
        } catch (error) {
            fail(error);
        }
    }

    (async () => {
        try {
            const plans = await refreshPlans();
            if (plans.length > 0) {
                await loadPlan(plans[0].id);
            } else {
                showForm(true);
            }
        } catch (error) {
            fail(error);
        }
    })();
}

document.addEventListener("DOMContentLoaded", setupRelocation);
