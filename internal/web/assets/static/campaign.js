// The campaign: fronts, and on them the objectives you are trying to take, the
// opposition dug in between you and them, and the fog — what you do not know
// yet. Plus the orders aimed at each.
//
// Everything extracted from what you write arrives as a proposal, here and in
// the chat's left rail, and stays off the map until you keep it.

const TYPE_LABEL = { objective: "Objective", obstacle: "Opposition", unknown: "Fog" };

// The five kinds of stuck, from the Stalled mode, each with its treatment:
// they need opposite ones.
const KIND_LABEL = {
    undefined: "Next step undefined",
    waiting: "Waiting on input",
    fear: "Afraid of the result",
    too_big: "Too big to hold",
    unwanted: "Not really wanted",
};

const KIND_TREATMENT = {
    undefined: "Define the next step.",
    waiting: "Chase the input.",
    fear: "Look at the result.",
    too_big: "Cut it down to something that fits in a sitting.",
    unwanted: "Consider withdrawing the objective instead.",
};

const STRENGTH_LABEL = { 1: "Outpost", 2: "Dug in", 3: "Fortress" };

async function campaignCall(url, method, body) {
    return apiJSON(url, {
        method,
        headers: { "Content-Type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
    });
}

function patchItem(id, body) {
    return campaignCall(`/api/campaign/items/${id}`, "PATCH", body);
}

// liftUnknown asks what reconnaissance found. Fog lifted without an answer has
// only been forgotten, so cancelling leaves it in place.
async function liftUnknown(id, question) {
    const answer = window.prompt(`What did you find out?\n\n${question}`);
    if (!answer || !answer.trim()) {
        return false;
    }
    await campaignCall(`/api/campaign/items/${id}/lift`, "POST", { answer: answer.trim() });
    return true;
}

function frontOptions(fronts, selected) {
    const active = (fronts || []).filter((f) => f.status === "active");
    return `<option value="">No front</option>` + active.map((f) =>
        `<option value="${escapeHTML(f.id)}" ${f.id === selected ? "selected" : ""}>${escapeHTML(f.name)}</option>`).join("");
}

function strengthPips(strength) {
    return [1, 2, 3].map((n) => `<span class="pip ${n <= strength ? "on" : ""}"></span>`).join("");
}

// proposalHTML is one item waiting for you: what it is, what the room read it
// as, and a front to file it under before you keep it.
function proposalHTML(item, fronts) {
    let meta = "";
    if (item.type === "obstacle") {
        meta = `<span class="intel-meta">${escapeHTML(KIND_LABEL[item.kind] || "Kind unknown")}
            · <span class="strength-inline">${strengthPips(item.strength || 2)}</span> ${escapeHTML(STRENGTH_LABEL[item.strength] || "")} <span class="est">est</span></span>`;
    } else if (item.type === "objective" && item.due) {
        meta = `<span class="intel-meta">by ${escapeHTML(item.due)}</span>`;
    }
    const from = item.source === "interrogation" ? "from an interrogation" : "you said this";

    return `
    <li class="intel proposed type-${escapeHTML(item.type)}" data-id="${escapeHTML(item.id)}">
        <span class="intel-type">${escapeHTML(TYPE_LABEL[item.type] || item.type)}</span>
        <span class="intel-text">${escapeHTML(item.text)}</span>
        ${meta}
        <span class="intel-actions">
            <span class="intel-hint">${from} — map it?</span>
            <select data-front aria-label="Front">${frontOptions(fronts, item.front_id)}</select>
            <button type="button" class="loop-btn keep" data-action="keep">Keep</button>
            <button type="button" class="loop-btn drop" data-action="drop">Drop</button>
        </span>
    </li>`;
}

// keepProposal files a proposal under the chosen front and keeps it.
async function keepProposal(li) {
    const select = li.querySelector("[data-front]");
    const body = { status: "active" };
    if (select) {
        body.front_id = select.value;
    }
    await patchItem(li.dataset.id, body);
}

// ---------- The chat rail ----------

// setupIntelRail shows proposals in the chat's left rail, so you can keep or
// drop them without leaving the conversation.
function setupIntelRail() {
    const panel = document.getElementById("intel");
    const list = document.getElementById("intel-list");
    if (!panel || !list) {
        return null;
    }

    async function load() {
        try {
            const data = await apiJSON("/api/campaign/proposals");
            const items = data.items || [];
            panel.hidden = items.length === 0;
            const count = document.getElementById("intel-count");
            if (count) {
                count.textContent = items.length ? String(items.length) : "";
            }
            list.innerHTML = items.map((it) => proposalHTML(it, data.fronts)).join("");
        } catch {
            // The rail is an affordance; the campaign page still has everything.
            panel.hidden = true;
        }
    }

    list.addEventListener("click", async (e) => {
        const button = e.target.closest("[data-action]");
        const li = e.target.closest("[data-id]");
        if (!button || !li) {
            return;
        }
        try {
            if (button.dataset.action === "keep") {
                await keepProposal(li);
            } else if (button.dataset.action === "drop") {
                await patchItem(li.dataset.id, { status: "dropped" });
            }
        } catch (error) {
            showMessage(document.getElementById("consult-result"), error.message, "error");
        }
        await load();
    });

    // Extraction runs after the reply, so proposals land a few seconds later.
    function refreshSoon() {
        setTimeout(load, 5000);
        setTimeout(load, 15000);
    }

    load();
    return { load, refreshSoon };
}

// ---------- The campaign page ----------

function setupCampaignPage() {
    const root = document.querySelector(".campaign");
    const frontsEl = document.getElementById("campaign-fronts");
    if (!root || !frontsEl) {
        return;
    }
    const result = document.getElementById("campaign-result");

    let state = { fronts: [], items: [], orders: [], starters: [], loops: [] };

    async function load() {
        try {
            const [map, loops] = await Promise.all([
                apiJSON("/api/campaign"),
                apiJSON("/api/commitments?status=open,proposed").catch(() => []),
            ]);
            state = { ...map, loops: loops || [] };
            render();
        } catch (error) {
            showMessage(result, error.message, "error");
        }
    }

    const live = (it) => it.status === "active";

    // An obstacle or unknown can bear on an objective filed on another front;
    // say which, or the link is invisible.
    function bearsOnHTML(it, frontItems) {
        if (!it.objective_id || frontItems.some((o) => o.id === it.objective_id)) {
            return "";
        }
        const objective = state.items.find((o) => o.id === it.objective_id);
        return objective ? `<span class="bears-on">bears on ◆ ${escapeHTML(objective.text)}</span>` : "";
    }
    const ordersFor = (id) => state.orders.filter((o) => o.target_id === id
        && (o.status === "open" || o.status === "proposed" || o.status === "stale"));

    function orderHTML(o) {
        return `
        <li class="order ${o.kind === "recon" ? "recon" : ""}" data-order-id="${escapeHTML(o.id)}"
            data-target="${escapeHTML(o.target_id || "")}">
            ${o.status === "proposed"
                ? `<span class="order-proposed">proposed</span>`
                : `<input type="checkbox" data-action="order-done" aria-label="Mark done">`}
            ${o.kind === "recon" ? '<span class="order-tag">Recon</span>' : ""}
            <span class="order-text">${escapeHTML(o.text)}</span>
            ${o.due ? `<span class="loop-due ${o.overdue ? "overdue" : ""}">${escapeHTML(o.due)}</span>` : ""}
        </li>`;
    }

    function ordersHTML(id) {
        const orders = ordersFor(id);
        return orders.length ? `<ul class="campaign-orders">${orders.map(orderHTML).join("")}</ul>` : "";
    }

    function obstacleHTML(it, frontItems = []) {
        const strength = it.strength || 0;
        const strengthSelect = `<select data-field="strength" aria-label="Strength">
            ${[1, 2, 3].map((n) => `<option value="${n}" ${n === strength ? "selected" : ""}>${STRENGTH_LABEL[n]}</option>`).join("")}
        </select>`;
        const kindSelect = `<select data-field="kind" aria-label="Kind of stuck">
            <option value="" ${it.kind ? "" : "selected"}>Kind?</option>
            ${Object.entries(KIND_LABEL).map(([k, label]) => `<option value="${k}" ${k === it.kind ? "selected" : ""}>${label}</option>`).join("")}
        </select>`;

        return `
        <div class="enemy" data-id="${escapeHTML(it.id)}">
            <div class="enemy-row">
                <span class="enemy-mark" aria-hidden="true">▲</span>
                <span class="enemy-text">${escapeHTML(it.text)}</span>
                <span class="row-actions">
                    <button type="button" class="mini-btn" data-action="status" data-status="resolved" title="It no longer stands in the way">Cleared</button>
                    <button type="button" class="loop-x" data-action="status" data-status="dropped" title="Take off the map">×</button>
                </span>
            </div>
            ${bearsOnHTML(it, frontItems)}
            <div class="enemy-intel">
                ${kindSelect}
                <span class="strength s${strength}">${strengthPips(strength)}</span>
                ${strengthSelect}
                ${strength && !it.strength_confirmed
                    ? `<button type="button" class="est" data-action="confirm-strength" title="An estimate until you confirm it. Click to confirm.">est</button>`
                    : ""}
                ${it.kind ? `<span class="treatment">${escapeHTML(KIND_TREATMENT[it.kind] || "")}</span>` : ""}
            </div>
            ${ordersHTML(it.id)}
        </div>`;
    }

    function unknownHTML(it, frontItems = []) {
        const scouting = ordersFor(it.id).some((o) => o.kind === "recon");
        return `
        <div class="fog" data-id="${escapeHTML(it.id)}">
            <div class="fog-row">
                <span class="fog-mark" aria-hidden="true">?</span>
                <span class="fog-text">${escapeHTML(it.text)}</span>
                <span class="row-actions">
                    ${scouting ? "" : `<button type="button" class="mini-btn" data-action="recon" title="Add an open loop to find this out">Send recon</button>`}
                    <button type="button" class="mini-btn" data-action="lift" title="Say what you found out">Lift</button>
                    <button type="button" class="loop-x" data-action="status" data-status="dropped" title="Take off the map">×</button>
                </span>
            </div>
            ${bearsOnHTML(it, frontItems)}
            ${ordersHTML(it.id)}
        </div>`;
    }

    function objectiveHTML(it, children) {
        return `
        <div class="objective" data-id="${escapeHTML(it.id)}">
            <div class="objective-row">
                <span class="objective-mark" aria-hidden="true">◆</span>
                <span class="objective-text">${escapeHTML(it.text)}</span>
                ${it.due ? `<span class="loop-due ${it.overdue ? "overdue" : ""}">${it.overdue ? "overdue · " : ""}${escapeHTML(it.due)}</span>` : ""}
                <span class="row-actions">
                    <button type="button" class="mini-btn" data-action="status" data-status="resolved" title="Objective taken">Taken</button>
                    <button type="button" class="mini-btn" data-action="status" data-status="withdrawn" title="An orderly withdrawal: a legitimate order, not a loss">Withdraw</button>
                    <button type="button" class="loop-x" data-action="status" data-status="dropped" title="Take off the map">×</button>
                </span>
            </div>
            ${ordersHTML(it.id)}
            ${children.length ? `<div class="objective-children">${children.join("")}</div>` : ""}
        </div>`;
    }

    function addFormHTML(frontID, objectives) {
        return `
        <form class="add-item" data-front="${escapeHTML(frontID || "")}">
            <select name="type" aria-label="What to add">
                <option value="objective">Objective</option>
                <option value="obstacle">Opposition</option>
                <option value="unknown">Fog</option>
            </select>
            <input name="text" type="text" maxlength="200" placeholder="Add to this front" autocomplete="off">
            <select name="objective_id" aria-label="Bears on" hidden>
                <option value="">No objective</option>
                ${objectives.map((o) => `<option value="${escapeHTML(o.id)}">${escapeHTML(o.text)}</option>`).join("")}
            </select>
            <button type="submit" class="mini-btn">Add</button>
        </form>`;
    }

    function frontBody(items) {
        const objectives = items.filter((it) => it.type === "objective");
        const byObjective = (id) => items.filter((it) => it.objective_id === id && it.type !== "objective");
        const loose = items.filter((it) => it.type !== "objective"
            && (!it.objective_id || !objectives.some((o) => o.id === it.objective_id)));

        const child = (it) => (it.type === "obstacle" ? obstacleHTML(it, items) : unknownHTML(it, items));
        // Opposition before fog: what stands in the way, then what is not known.
        const ordered = (list) => [...list.filter((i) => i.type === "obstacle"), ...list.filter((i) => i.type === "unknown")];

        return `
            ${objectives.map((o) => objectiveHTML(o, ordered(byObjective(o.id)).map(child))).join("")}
            ${loose.length ? `<div class="loose">${objectives.length ? '<span class="loose-label">Not tied to an objective</span>' : ""}${ordered(loose).map(child).join("")}</div>` : ""}
            ${items.length === 0 ? '<p class="front-empty muted">Quiet front. Add an objective, or talk about it in a chat.</p>' : ""}`;
    }

    function frontHTML(front, items) {
        const counts = {
            objective: items.filter((i) => i.type === "objective").length,
            obstacle: items.filter((i) => i.type === "obstacle").length,
            unknown: items.filter((i) => i.type === "unknown").length,
        };
        return `
        <article class="front" data-front-id="${escapeHTML(front.id)}">
            <header class="front-head">
                <h2>${escapeHTML(front.name)}</h2>
                <span class="front-counts">
                    ${counts.objective} ◆ · ${counts.obstacle} ▲ · ${counts.unknown} ?
                </span>
                <span class="row-actions">
                    <button type="button" class="mini-btn" data-action="rename-front">Rename</button>
                    <button type="button" class="mini-btn" data-action="withdraw-front" title="Close this front. What is on it stays on the record.">Withdraw</button>
                </span>
            </header>
            ${frontBody(items)}
            ${addFormHTML(front.id, items.filter((i) => i.type === "objective"))}
        </article>`;
    }

    function render() {
        const items = state.items || [];
        const fronts = state.fronts || [];
        const activeFronts = fronts.filter((f) => f.status === "active");
        const withdrawn = fronts.filter((f) => f.status === "withdrawn");
        const onMap = items.filter(live);
        const proposals = items.filter((i) => i.status === "proposed");

        // Summary: what is on the map, with how much of the opposition is still
        // only an estimate.
        const opposition = onMap.filter((i) => i.type === "obstacle");
        const unconfirmed = opposition.filter((i) => !i.strength_confirmed).length;
        document.getElementById("campaign-summary").innerHTML = `
            <span class="sum"><b>${onMap.filter((i) => i.type === "objective").length}</b> objectives</span>
            <span class="sum"><b>${opposition.length}</b> opposition${unconfirmed ? ` <span class="est">${unconfirmed} est</span>` : ""}</span>
            <span class="sum"><b>${onMap.filter((i) => i.type === "unknown").length}</b> in fog</span>
            ${proposals.length ? `<span class="sum warn"><b>${proposals.length}</b> waiting</span>` : ""}`;

        const proposalsEl = document.getElementById("campaign-proposals");
        proposalsEl.hidden = proposals.length === 0;
        document.getElementById("campaign-proposal-list").innerHTML =
            proposals.map((p) => proposalHTML(p, fronts)).join("");

        const activeIDs = new Set(activeFronts.map((f) => f.id));
        const cards = activeFronts.map((f) => frontHTML(f, onMap.filter((i) => i.front_id === f.id)));
        const unassigned = onMap.filter((i) => !i.front_id || !activeIDs.has(i.front_id));
        if (unassigned.length) {
            cards.push(`
            <article class="front unassigned">
                <header class="front-head"><h2>No front</h2>
                    <span class="front-counts muted">File these under a front from their proposal, or leave them loose.</span>
                </header>
                ${frontBody(unassigned)}
            </article>`);
        }

        frontsEl.innerHTML = cards.length
            ? cards.join("")
            : `<div class="campaign-empty">
                <p>No fronts yet. A front is an area of life or work — the map files everything else under one.</p>
                <button type="button" class="btn" data-action="starters">Open ${escapeHTML((state.starters || []).join(", "))}</button>
                <span class="muted">or name your own below.</span>
               </div>`;

        if (withdrawn.length) {
            frontsEl.insertAdjacentHTML("beforeend", `
            <p class="withdrawn-fronts muted">Withdrawn: ${withdrawn.map((f) =>
                `<button type="button" class="link-btn" data-action="restore-front" data-front="${escapeHTML(f.id)}">${escapeHTML(f.name)} ↺</button>`).join(" ")}</p>`);
        }

        renderUnaimed(onMap);
        renderResolved(items);
    }

    // Open loops aimed at nothing on the map, each with a select to aim it.
    function renderUnaimed(onMap) {
        const loops = (state.loops || []).filter((c) => c.status === "open" && !c.target_id);
        const section = document.getElementById("campaign-unaimed");
        section.hidden = loops.length === 0 || onMap.length === 0;
        if (section.hidden) {
            return;
        }
        const group = (type) => onMap.filter((i) => i.type === type);
        const options = ["objective", "obstacle", "unknown"].map((type) => {
            const list = group(type);
            return list.length ? `<optgroup label="${TYPE_LABEL[type]}">${list.map((i) =>
                `<option value="${escapeHTML(i.id)}">${escapeHTML(i.text)}</option>`).join("")}</optgroup>` : "";
        }).join("");

        document.getElementById("campaign-unaimed-list").innerHTML = loops.map((c) => `
            <li class="order" data-order-id="${escapeHTML(c.id)}">
                <span class="order-text">${escapeHTML(c.text)}</span>
                <select data-aim aria-label="Aim at"><option value="">Aim at…</option>${options}</select>
            </li>`).join("");
    }

    function renderResolved(items) {
        const done = items.filter((i) => i.status === "resolved" || i.status === "withdrawn");
        const section = document.getElementById("campaign-resolved");
        section.hidden = done.length === 0;
        const verb = (i) => (i.status === "withdrawn" ? "Withdrawn"
            : { objective: "Taken", obstacle: "Cleared", unknown: "Lifted" }[i.type]);
        document.getElementById("campaign-resolved-list").innerHTML = done.map((i) => `
            <li class="resolved type-${escapeHTML(i.type)} ${escapeHTML(i.status)}" data-id="${escapeHTML(i.id)}">
                <span class="resolved-verb">${escapeHTML(verb(i))}</span>
                <span class="resolved-text">${escapeHTML(i.text)}</span>
                ${i.answer ? `<span class="resolved-answer">→ ${escapeHTML(i.answer)}</span>` : ""}
                <button type="button" class="link-btn" data-action="status" data-status="active">Reopen</button>
            </li>`).join("");
    }

    async function act(fn) {
        try {
            await fn();
        } catch (error) {
            showMessage(result, error.message, "error");
        }
        await load();
    }

    root.addEventListener("click", (e) => {
        const button = e.target.closest("[data-action]");
        if (!button || button.tagName === "INPUT") {
            return;
        }
        const itemEl = button.closest("[data-id]");
        const frontEl = button.closest("[data-front-id]");
        const id = itemEl?.dataset.id;

        switch (button.dataset.action) {
        case "keep":
            act(() => keepProposal(itemEl));
            break;
        case "drop":
            act(() => patchItem(id, { status: "dropped" }));
            break;
        case "status":
            act(() => patchItem(id, { status: button.dataset.status }));
            break;
        case "confirm-strength":
            act(() => patchItem(id, { confirm_strength: true }));
            break;
        case "recon":
            act(() => campaignCall(`/api/campaign/items/${id}/recon`, "POST"));
            break;
        case "lift": {
            const text = itemEl.querySelector(".fog-text")?.textContent ?? "";
            act(() => liftUnknown(id, text));
            break;
        }
        case "starters":
            act(() => campaignCall("/api/campaign/fronts/starters", "POST"));
            break;
        case "rename-front": {
            const current = frontEl.querySelector("h2")?.textContent ?? "";
            const name = window.prompt("Rename this front", current);
            if (name && name.trim() && name.trim() !== current) {
                act(() => campaignCall(`/api/campaign/fronts/${frontEl.dataset.frontId}`, "PATCH", { name: name.trim() }));
            }
            break;
        }
        case "withdraw-front":
            if (window.confirm("Withdraw this front? What is on it stays on the record, and you can restore it.")) {
                act(() => campaignCall(`/api/campaign/fronts/${frontEl.dataset.frontId}`, "PATCH", { status: "withdrawn" }));
            }
            break;
        case "restore-front":
            act(() => campaignCall(`/api/campaign/fronts/${button.dataset.front}`, "PATCH", { status: "active" }));
            break;
        }
    });

    root.addEventListener("change", (e) => {
        const target = e.target;
        const itemEl = target.closest("[data-id]");

        if (target.matches('[data-action="order-done"]')) {
            const li = target.closest("[data-order-id]");
            act(async () => {
                await campaignCall(`/api/commitments/${li.dataset.orderId}`, "PATCH", { status: "done" });
                // A recon order done should say what it found; that lifts the fog.
                const unknown = state.items.find((i) => i.id === li.dataset.target && i.type === "unknown" && i.status === "active");
                if (li.classList.contains("recon") && unknown) {
                    await liftUnknown(unknown.id, unknown.text);
                }
            });
            return;
        }
        if (target.matches("[data-aim]") && target.value) {
            const li = target.closest("[data-order-id]");
            act(() => campaignCall(`/api/campaign/orders/${li.dataset.orderId}/target`, "PUT", { target_id: target.value }));
            return;
        }
        if (target.matches('[data-field="strength"]') && itemEl) {
            act(() => patchItem(itemEl.dataset.id, { strength: Number(target.value) }));
            return;
        }
        if (target.matches('[data-field="kind"]') && itemEl) {
            act(() => patchItem(itemEl.dataset.id, { kind: target.value }));
            return;
        }
        // The add form shows "bears on" only for opposition and fog.
        if (target.matches('.add-item [name="type"]')) {
            const form = target.closest("form");
            form.elements.objective_id.hidden = target.value === "objective";
        }
    });

    root.addEventListener("submit", (e) => {
        const form = e.target;
        if (form.matches(".add-item")) {
            e.preventDefault();
            const text = form.elements.text.value.trim();
            if (!text) {
                return;
            }
            const type = form.elements.type.value;
            act(() => campaignCall("/api/campaign/items", "POST", {
                type,
                text,
                front_id: form.dataset.front,
                objective_id: type === "objective" ? "" : form.elements.objective_id.value,
                // Opposition you add yourself starts dug in, confirmed: you
                // know your own blocker better than an estimate does.
                strength: type === "obstacle" ? 2 : 0,
            }));
            return;
        }
        if (form.id === "campaign-front-form") {
            e.preventDefault();
            const name = form.elements.name.value.trim();
            if (!name) {
                return;
            }
            act(async () => {
                await campaignCall("/api/campaign/fronts", "POST", { name });
                form.reset();
            });
        }
    });

    load();
}

document.addEventListener("DOMContentLoaded", setupCampaignPage);
