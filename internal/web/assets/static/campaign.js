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

// redTeamHref opens the chat with the red team mode pinned and the attack
// aimed at one objective or one front. The chat page reads the parameters
// and writes the opening question; the map itself reaches the general
// through the answer's context.
function redTeamHref(kind, text) {
    const params = new URLSearchParams({ redteam: text, kind });
    return `/?${params.toString()}`;
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
    const from = { interrogation: "from an interrogation", red_team: "the red team's estimate" }[item.source] || "you said this";

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
            await Promise.all([loadTheatre(), loadStaff(), loadLogistics()]);
        } catch (error) {
            showMessage(result, error.message, "error");
        }
    }

    const live = (it) => it.status === "active";

    // amountLabel is a whole amount with its currency, as the brief writes it.
    const amountLabel = (value, currency) => `${Math.round(value).toLocaleString()}${currency ? ` ${currency}` : ""}`;

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
                ${it.cost != null ? `<button type="button" class="cost-chip" data-action="cost" title="What taking it will cost, drawn on the front's funds. Click to change.">${escapeHTML(amountLabel(it.cost, it.cost_currency))}</button>` : ""}
                <span class="row-actions">
                    ${it.cost == null ? `<button type="button" class="mini-btn" data-action="cost" title="What taking it will cost, drawn on the front's funds">Cost</button>` : ""}
                    <button type="button" class="mini-btn" data-action="status" data-status="resolved" title="Objective taken">Taken</button>
                    <button type="button" class="mini-btn" data-action="status" data-status="withdrawn" title="An orderly withdrawal: a legitimate order, not a loss">Withdraw</button>
                    ${it.status === "active" ? `<a class="mini-btn red-team-link" href="${redTeamHref("objective", it.text)}" title="One general takes command of what stands against this objective and attacks your plan for it">Red team</a>` : ""}
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
                    <button type="button" class="mini-btn" data-action="appraise-front" data-front="${escapeHTML(front.id)}" title="The staff appraises this front alone">Appraise</button>
                    ${items.some((i) => i.type === "objective") ? `<a class="mini-btn red-team-link" href="${redTeamHref("front", front.name)}" title="One general takes command of the opposition on this front and attacks your plan">Red team</a>` : ""}
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

    // ---------- The theatre map and its timelapse ----------
    //
    // The map is an SVG drawn on the server; the page inlines it so its titles
    // show as tooltips. Frames are the daily snapshots, oldest first, with the
    // live map as the last stop.

    const theatre = {
        section: document.getElementById("theatre"),
        map: document.getElementById("theatre-map"),
        scrub: document.getElementById("theatre-scrub"),
        day: document.getElementById("theatre-day"),
        caption: document.getElementById("theatre-caption"),
        play: document.getElementById("theatre-play"),
        open: document.getElementById("theatre-open"),
        frames: [],
        timer: null,
        token: 0,
    };

    function captionFor(frame) {
        const t = frame.totals;
        if (!t) {
            return "";
        }
        const parts = [
            `${t.objectives} objective${t.objectives === 1 ? "" : "s"}${t.taken ? ` · ${t.taken} taken` : ""}${t.withdrawn ? ` · ${t.withdrawn} withdrawn` : ""}`,
            `${t.opposition} opposition${t.cleared ? ` · ${t.cleared} cleared` : ""}`,
            `${t.fog} in fog${t.lifted ? ` · ${t.lifted} lifted` : ""}`,
            `${t.orders_open} orders open${t.orders_done ? ` · ${t.orders_done} carried out` : ""}`,
        ];
        return parts.join("  —  ");
    }

    async function showFrame(index) {
        const frame = theatre.frames[index];
        if (!frame) {
            return;
        }
        const token = ++theatre.token;
        const url = frame.live ? "/api/campaign/map.svg" : `/api/campaign/map.svg?day=${encodeURIComponent(frame.day)}`;
        theatre.day.textContent = frame.live ? "Today" : frame.day;
        theatre.day.classList.toggle("past", !frame.live);
        theatre.open.href = url;
        try {
            const response = await fetch(url, { headers: { Accept: "image/svg+xml" } });
            if (!response.ok) {
                throw new Error(`map: ${response.status}`);
            }
            const svg = await response.text();
            if (token !== theatre.token) {
                return; // a later frame won
            }
            theatre.map.innerHTML = svg;
            theatre.caption.textContent = frame.live ? captionFor(theatre.liveTotals ? { totals: theatre.liveTotals } : {}) : captionFor(frame);
        } catch (error) {
            if (token === theatre.token) {
                theatre.map.innerHTML = `<div class="error">${escapeHTML(error.message)}</div>`;
            }
        }
    }

    function stopPlaying() {
        if (theatre.timer) {
            clearInterval(theatre.timer);
            theatre.timer = null;
        }
        theatre.play.textContent = "▶ Timelapse";
    }

    function startPlaying() {
        const last = theatre.frames.length - 1;
        if (last < 1) {
            return;
        }
        let i = Number(theatre.scrub.value);
        if (i >= last) {
            i = 0;
        }
        theatre.play.textContent = "■ Stop";
        const step = () => {
            theatre.scrub.value = String(i);
            showFrame(i);
            if (i >= last) {
                stopPlaying();
                return;
            }
            i += 1;
        };
        step();
        theatre.timer = setInterval(step, 600);
    }

    // loadTheatre refreshes the frame list and shows today. It runs after
    // every reload of the page's data, so an action moves the line at once.
    async function loadTheatre() {
        if (!theatre.section) {
            return;
        }
        stopPlaying();
        let days = [];
        try {
            const data = await apiJSON("/api/campaign/timelapse");
            days = (data.days || []).filter((d) => d.day !== data.today);
        } catch {
            // No record yet; the live map still draws.
        }
        try {
            const live = await apiJSON("/api/campaign/theatre");
            theatre.liveTotals = live.theatre?.totals || null;
        } catch {
            theatre.liveTotals = null;
        }
        theatre.frames = [...days, { live: true }];

        const hasFronts = (state.fronts || []).some((f) => f.status === "active");
        const hasItems = (state.items || []).some(live);
        theatre.section.hidden = !hasFronts && !hasItems && days.length === 0;
        if (theatre.section.hidden) {
            return;
        }
        const last = theatre.frames.length - 1;
        theatre.scrub.max = String(last);
        theatre.scrub.value = String(last);
        theatre.scrub.disabled = last < 1;
        theatre.play.disabled = last < 1;
        theatre.play.title = last < 1
            ? "The timelapse needs more than one day on record"
            : "Play the campaign so far, one day per frame";
        await showFrame(last);
    }

    if (theatre.section) {
        theatre.scrub.addEventListener("input", () => {
            stopPlaying();
            showFrame(Number(theatre.scrub.value));
        });
        theatre.play.addEventListener("click", () => {
            if (theatre.timer) {
                stopPlaying();
            } else {
                startPlaying();
            }
        });
    }

    // ---------- The staff ----------
    //
    // Two layers, like the rest of the campaign. The readings are arithmetic
    // from the sitrep, drawn on every load with no model. The appraisal is
    // the generals' judgment, written only when you ask and kept with its
    // date, so the page shows what the staff last said rather than
    // recomputing it.

    const staff = {
        section: document.getElementById("staff"),
        readings: document.getElementById("staff-readings"),
        appraisal: document.getElementById("staff-appraisal"),
        earlier: document.getElementById("staff-earlier"),
        earlierList: document.getElementById("staff-earlier-list"),
        front: document.getElementById("staff-front"),
        tier: document.getElementById("staff-tier"),
        ask: document.getElementById("staff-ask"),
        roster: {},
        sitrep: null,
        appraisals: [],
    };

    const READING_LABEL = {
        overdue: "Overdue",
        overextended: "Overextended",
        quiet: "Quiet",
        fortress: "Fortress",
        unwanted: "Not wanted",
        unscouted: "Unscouted",
        estimated: "Estimates",
        advanced: "Closest",
        outrun: "Outrunning supply",
        cut: "Supply cut",
        overdrawn: "Overdrawn",
        deployment: "Deployment",
        energy: "Energy",
    };

    // The appraisal runs on the server in the background; the page asks for
    // it and then polls at this interval until the staff reports back.
    const STAFF_POLL_MS = 4000;
    let staffPoll = null;

    function readingsHTML(readings) {
        if (!readings || readings.length === 0) {
            return `<li class="reading muted">Nothing the arithmetic can say yet. Aim orders at the map and let a day pass.</li>`;
        }
        return readings.map((r) => `
            <li class="reading kind-${escapeHTML(r.kind)}">
                <span class="reading-kind">${escapeHTML(READING_LABEL[r.kind] || r.kind)}</span>
                <span class="reading-text">${escapeHTML(r.text)}</span>
            </li>`).join("");
    }

    function generalChips(ids) {
        return (ids || []).map((id) => {
            const g = staff.roster[id];
            if (!g) {
                return `<span class="lens-badge">${escapeHTML(String(id).replace(/_/g, " "))}</span>`;
            }
            return `<span class="appraisal-general" style="--family:${familyStyle(g.family).color}">${portrait(g, 26, "round")}<span>${escapeHTML(g.name)}</span></span>`;
        }).join("");
    }

    function frontName(id) {
        if (!id) {
            return "";
        }
        return (state.fronts || []).find((f) => f.id === id)?.name ?? "";
    }

    function appraisalHTML(a) {
        const front = frontName(a.front_id);
        return `
        <article class="appraisal" data-appraisal="${escapeHTML(a.id)}">
            <header class="appraisal-head">
                <span class="appraisal-generals">${generalChips(a.generals)}</span>
                <span class="appraisal-meta">
                    ${front ? `<span class="appraisal-front">${escapeHTML(front)}</span>` : `<span class="appraisal-front">Whole map</span>`}
                    ${a.tier ? `<span class="tier-badge tier-${escapeHTML(a.tier)}">${escapeHTML(a.tier)}</span>` : ""}
                    <span class="muted">${escapeHTML(timeAgo(a.created_at))}</span>
                </span>
                <span class="appraisal-actions">
                    <button type="button" class="mini-btn" data-action="discuss" title="Open it as a conversation with the generals who wrote it">${a.session_id ? "Continue the discussion" : "Discuss"}</button>
                </span>
            </header>
            <div class="appraisal-body markdown">${renderMarkdown(a.body)}</div>
        </article>`;
    }

    function renderStaff() {
        if (!staff.section) {
            return;
        }
        const s = staff.sitrep;
        const hasMap = s && !s.empty;
        const hasAppraisals = staff.appraisals.length > 0;
        staff.section.hidden = !hasMap && !hasAppraisals;
        if (staff.section.hidden) {
            return;
        }

        const active = (state.fronts || []).filter((f) => f.status === "active");
        const chosen = staff.front.value;
        staff.front.innerHTML = `<option value="">The whole map</option>` + active.map((f) =>
            `<option value="${escapeHTML(f.id)}" ${f.id === chosen ? "selected" : ""}>${escapeHTML(f.name)}</option>`).join("");

        // Busy on the server, whoever asked: the button waits and the page
        // polls, so an appraisal asked for in another tab still lands here.
        const busy = Boolean(staff.status?.busy);
        staff.section.classList.toggle("busy", busy);
        staff.ask.disabled = !hasMap || busy;
        staff.ask.textContent = busy ? "The staff is reading the map…" : "Ask the staff";
        if (busy && !staffPoll) {
            staffPoll = setTimeout(() => {
                staffPoll = null;
                loadStaff();
            }, STAFF_POLL_MS);
        }

        staff.readings.innerHTML = hasMap ? readingsHTML(s.readings) : "";

        const [latest, ...rest] = staff.appraisals;
        const failure = !busy && staff.status?.error
            ? `<p class="staff-failed error">The last appraisal failed: ${escapeHTML(staff.status.error)}</p>`
            : "";
        staff.appraisal.innerHTML = failure + (latest
            ? appraisalHTML(latest)
            : `<p class="staff-empty muted">The staff has not read the map yet. Ask, and the generals say where the point of main effort is, where to yield ground, and what is overextended.</p>`);

        staff.earlier.hidden = rest.length === 0;
        staff.earlierList.innerHTML = rest.map((a) => `
            <li class="appraisal-earlier" data-appraisal="${escapeHTML(a.id)}">
                <span class="muted">${escapeHTML(formatDate(a.created_at))}</span>
                <span>${escapeHTML(frontName(a.front_id) || "Whole map")}</span>
                <span class="appraisal-generals">${generalChips(a.generals)}</span>
                <button type="button" class="link-btn" data-action="show-appraisal">Show</button>
            </li>`).join("");
    }

    async function loadStaff() {
        if (!staff.section) {
            return;
        }
        if (Object.keys(staff.roster).length === 0) {
            try {
                const roster = await apiJSON("/api/generals");
                staff.roster = Object.fromEntries(roster.map((g) => [g.id, g]));
            } catch {
                // Names fall back to tidied ids.
            }
        }
        const [sitrep, appraisals] = await Promise.all([
            apiJSON("/api/campaign/sitrep").catch(() => null),
            apiJSON("/api/campaign/appraisals").catch(() => ({ items: [], status: null })),
        ]);
        staff.sitrep = sitrep;
        staff.appraisals = appraisals.items || [];
        staff.status = appraisals.status || null;
        renderStaff();
    }

    // askStaff starts the appraisal and comes straight back; renderStaff
    // keeps polling while the server says the staff is busy.
    async function askStaff(frontID) {
        if (!staff.ask || staff.ask.disabled) {
            return;
        }
        const tier = staff.tier?.value || "standard";
        staff.ask.disabled = true;
        try {
            const started = await apiJSON("/api/campaign/appraisals", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ front_id: frontID || "", tier }),
                timeoutMs: 30000,
            });
            staff.status = started.status || { busy: true };
            renderStaff();
        } catch (error) {
            showMessage(result, error.message, "error");
            staff.ask.disabled = false;
        }
    }

    async function discussAppraisal(id) {
        try {
            const { session_id: sessionID } = await apiJSON(`/api/campaign/appraisals/${id}/open`, { method: "POST" });
            window.location.href = `/?session=${encodeURIComponent(sessionID)}`;
        } catch (error) {
            showMessage(result, error.message, "error");
        }
    }

    staff.ask?.addEventListener("click", () => askStaff(staff.front?.value || ""));

    staff.section?.addEventListener("click", (e) => {
        const button = e.target.closest("[data-action]");
        const card = e.target.closest("[data-appraisal]");
        if (!button || !card) {
            return;
        }
        if (button.dataset.action === "discuss") {
            discussAppraisal(card.dataset.appraisal);
        } else if (button.dataset.action === "show-appraisal") {
            const chosen = staff.appraisals.find((a) => a.id === card.dataset.appraisal);
            if (chosen) {
                staff.appraisal.innerHTML = appraisalHTML(chosen);
                staff.appraisal.scrollIntoView({ behavior: "smooth", block: "nearest" });
            }
        }
    });

    // ---------- Logistics ----------
    //
    // What supplies each front, as you report it: sorties logged, energy
    // tapped, funds set. The page draws the supply lines the sitrep reads;
    // the warnings themselves are readings, in the staff's strip above, so
    // they reach the generals the same way.

    const logistics = {
        section: document.getElementById("logistics"),
        energy: document.getElementById("logistics-energy"),
        fronts: document.getElementById("logistics-fronts"),
        sorties: document.getElementById("logistics-sorties"),
        sortieList: document.getElementById("logistics-sortie-list"),
        data: null,
        // The front whose detailed sortie form is open.
        open: null,
    };

    function hoursLabel(h) {
        if (!h) {
            return "0 h";
        }
        const minutes = Math.round(h * 60);
        return minutes < 60 ? `${minutes} min` : `${Math.round(h * 10) / 10} h`;
    }

    function weekdayOf(day) {
        return new Date(`${day}T12:00:00`).toLocaleDateString(undefined, { weekday: "short" });
    }

    // dayMinus counts back from a YYYY-MM-DD day in calendar days.
    function dayMinus(day, n) {
        const [y, m, d] = day.split("-").map(Number);
        return new Date(Date.UTC(y, m - 1, d - n)).toISOString().slice(0, 10);
    }

    // Open orders aimed at something filed under a front: what a sortie
    // there can be flown on.
    function ordersOnFront(frontID) {
        const here = new Set((state.items || []).filter((i) => i.front_id === frontID).map((i) => i.id));
        return (state.orders || []).filter((o) => (o.status === "open" || o.status === "stale") && here.has(o.target_id));
    }

    function timeHTML(f, l) {
        if (!l.sorties) {
            return `<span class="muted">No sorties logged yet</span>`;
        }
        if (f.sorties === 0) {
            return `<span class="muted">No sorties in ${l.window_days} days${f.earlier_hours ? `, ${hoursLabel(f.earlier_hours)} before that` : ""}</span>`;
        }
        return `<b>${hoursLabel(f.hours_per_week)}</b> a week
            <span class="muted">· ${f.sorties} sortie${f.sorties === 1 ? "" : "s"} in ${l.window_days} days</span>`;
    }

    function ordersSupplyHTML(f, l) {
        if (!f.orders_open) {
            return `<span class="muted">No orders open</span>`;
        }
        const rate = f.per_order_measured
            ? `<span class="muted" title="Sortie hours over the orders carried out here in the last four weeks">at ${hoursLabel(f.hours_per_order)} an order</span>`
            : `<span class="est" title="An estimate: one ${l.sortie_minutes}-minute sortie an order, until three orders done here give the front a rate of its own">est</span>`;
        const parts = [`<b>${f.orders_open}</b> open ≈ ${hoursLabel(f.backlog_hours)} ${rate}`];
        if (f.orders_due_soon) {
            parts.push(`<b>${f.orders_due_soon}</b> due this week`);
        }
        if (f.backlog_weeks) {
            parts.push(`${Math.round(f.backlog_weeks * 10) / 10} wk to clear`);
        }
        return parts.join(" · ");
    }

    function moneyHTML(f) {
        let html = `<span class="muted">No funds set</span>`;
        if (f.funds != null) {
            html = `<b>${escapeHTML(amountLabel(f.costs, f.currency))}</b> of ${escapeHTML(amountLabel(f.funds, f.currency))}`;
        } else if (f.costs > 0) {
            html = `<b>${escapeHTML(amountLabel(f.costs, f.currency))}</b> of costs, no funds set`;
        }
        if (f.costs_estimated > 0 && f.costs > 0) {
            html += ` <span class="est" title="The share of these costs nobody confirmed: a deployment's estimated prices">${Math.round((f.costs_estimated / f.costs) * 100)}% est</span>`;
        }
        if (f.unconverted?.length) {
            html += ` <span class="muted" title="Costs in another currency: listed, never converted at a guessed rate">+ ${escapeHTML(f.unconverted.map((m) => amountLabel(m.amount, m.currency)).join(" + "))}</span>`;
        }
        return html;
    }

    function deploymentsHTML(f) {
        const chips = (f.deployments || []).map((d) => {
            let when = "no arrival date";
            if (d.lands_in !== undefined && d.lands_in !== null) {
                when = d.lands_in < 0 ? "deployed" : d.lands_in === 0 ? "lands today" : `lands in ${d.lands_in} d`;
            }
            const bits = [when];
            if (d.cost > 0) {
                bits.push(`${amountLabel(d.cost, d.currency)}${d.estimated_share > 0 ? ` (${Math.round(d.estimated_share * 100)}% est)` : ""}`);
            }
            if (d.items_needed) {
                bits.push(`${d.items_needed} still needed`);
            }
            if (d.critical_open) {
                bits.push(`${d.critical_open} critical pitfall${d.critical_open === 1 ? "" : "s"}`);
            }
            return `<a class="deployment-chip ${d.critical_open ? "critical" : ""}" href="/relocation?plan=${encodeURIComponent(d.plan_id)}"
                title="A stay deployed on this front: its setup budget draws on the front's funds">⌂ ${escapeHTML(d.destination)} · ${bits.map(escapeHTML).join(" · ")}</a>`;
        });
        return chips.length ? `<div class="supply-deployments">${chips.join("")}</div>` : "";
    }

    function sortieFormHTML(f, l, today) {
        const m = l.sortie_minutes;
        const lengths = [...new Set([m, m * 2, m * 3, 90, 120])].filter((v) => v <= 480).sort((a, b) => a - b);
        const days = [0, 1, 2, 3, 4, 5, 6].map((n) => {
            const day = dayMinus(today, n);
            const label = n === 0 ? "Today" : n === 1 ? "Yesterday" : `${weekdayOf(day)} ${day.slice(5)}`;
            return `<option value="${day}">${escapeHTML(label)}</option>`;
        }).join("");
        return `
        <form class="sortie-form" data-front="${escapeHTML(f.front_id)}">
            <select name="minutes" aria-label="Length">${lengths.map((v) =>
                `<option value="${v}" ${v === m ? "selected" : ""}>${v} min</option>`).join("")}</select>
            <select name="order_id" aria-label="Flown on">
                <option value="">On the front in general</option>
                ${ordersOnFront(f.front_id).map((o) => `<option value="${escapeHTML(o.id)}">${escapeHTML(o.text)}</option>`).join("")}
            </select>
            <select name="day" aria-label="Day">${days}</select>
            <input name="note" type="text" maxlength="200" placeholder="Note (optional)" autocomplete="off">
            <button type="submit" class="mini-btn">Log sortie</button>
            <button type="button" class="link-btn" data-action="sortie-cancel">Cancel</button>
        </form>`;
    }

    function supplyRowHTML(f, l, today) {
        const warn = f.outrun ? "outrun" : f.cut ? "cut" : "";
        return `
        <div class="supply-row ${warn}" data-supply-front="${escapeHTML(f.front_id)}">
            <span class="supply-front">${escapeHTML(f.front)}</span>
            <span class="supply-cell supply-time"><span class="supply-label">Time</span>${timeHTML(f, l)}</span>
            <span class="supply-cell supply-orders"><span class="supply-label">Orders</span>${ordersSupplyHTML(f, l)}</span>
            <span class="supply-cell supply-money ${f.overdrawn ? "overdrawn" : ""}"><span class="supply-label">Money</span>${moneyHTML(f)}</span>
            <span class="row-actions">
                <button type="button" class="mini-btn" data-action="sortie" title="Log one ${l.sortie_minutes}-minute sortie on this front, today">+ Sortie</button>
                <button type="button" class="mini-btn" data-action="sortie-form" aria-expanded="${logistics.open === f.front_id}" title="Log a sortie with its length, its order and its day">…</button>
                <button type="button" class="mini-btn" data-action="funds" title="Set the money this front can draw on">Funds</button>
            </span>
            ${deploymentsHTML(f)}
            ${logistics.open === f.front_id ? sortieFormHTML(f, l, today) : ""}
        </div>`;
    }

    function sortiesHTML(sorties) {
        return sorties.map((s) => {
            const front = (state.fronts || []).find((f) => f.id === s.front_id)?.name ?? "";
            const order = s.order_id ? (state.orders || []).find((o) => o.id === s.order_id)?.text : "";
            return `
            <li class="sortie" data-sortie="${escapeHTML(s.id)}">
                <span class="sortie-day muted">${escapeHTML(weekdayOf(s.day))} ${escapeHTML(s.day.slice(5))}</span>
                <span class="sortie-front">${escapeHTML(front)}</span>
                <span class="sortie-len">${s.minutes} min</span>
                ${order ? `<span class="sortie-order">on ${escapeHTML(order)}</span>` : ""}
                ${s.note ? `<span class="sortie-note muted">${escapeHTML(s.note)}</span>` : ""}
                <button type="button" class="loop-x" data-action="sortie-delete" title="Take this sortie off the record">×</button>
            </li>`;
        }).join("");
    }

    function energyHTML(l, today) {
        const reports = l.energy?.reports || [];
        const week = reports.length
            ? `<span class="energy-week" title="Energy as reported, the last seven days">${reports.map((r) =>
                `<span class="energy-dot lvl-${r.level}" title="${escapeHTML(weekdayOf(r.day))}: ${escapeHTML(energyLabel(r.level))}">${escapeHTML(weekdayOf(r.day).slice(0, 2))}</span>`).join("")}</span>`
            : "";
        return energyTapHTML(today, l.energy?.today || 0) + week;
    }

    function renderLogistics() {
        if (!logistics.section) {
            return;
        }
        const d = logistics.data;
        const active = (state.fronts || []).some((f) => f.status === "active");
        logistics.section.hidden = !d || !active;
        if (logistics.section.hidden) {
            return;
        }
        const l = d.logistics;
        logistics.energy.innerHTML = energyHTML(l, d.today);
        logistics.fronts.innerHTML = (l.fronts || []).map((f) => supplyRowHTML(f, l, d.today)).join("");
        const sorties = d.sorties || [];
        logistics.sorties.hidden = sorties.length === 0;
        logistics.sortieList.innerHTML = sortiesHTML(sorties);
    }

    async function loadLogistics() {
        if (!logistics.section) {
            return;
        }
        try {
            logistics.data = await apiJSON("/api/campaign/logistics");
        } catch {
            // Supply is an addition to the map; the rest of the page stands.
            logistics.data = null;
        }
        renderLogistics();
    }

    // askAmount reads "2500 EUR", "2,500" or "EUR 2500". Empty clears;
    // cancelling changes nothing.
    function askAmount(message, current, fallbackCurrency) {
        const raw = window.prompt(message, current);
        if (raw === null) {
            return undefined;
        }
        const text = raw.trim().toUpperCase();
        if (text === "") {
            return null;
        }
        const currency = (text.match(/[A-Z]{3}/) || [fallbackCurrency || "USD"])[0];
        const amount = Number(text.replace(/[A-Z]{3}/, "").replace(/[\s,]/g, ""));
        if (!Number.isFinite(amount) || amount < 0) {
            throw new Error("An amount is a number, with a three-letter currency if it is not the front's: 2500 EUR.");
        }
        return { amount, currency };
    }

    async function setFunds(frontID) {
        const front = (state.fronts || []).find((f) => f.id === frontID);
        const current = front?.funds != null ? `${front.funds} ${front.funds_currency}` : "";
        const value = askAmount(`Money ${front?.name ?? "this front"} can draw on, e.g. 2500 EUR. Empty clears it.`, current, front?.funds_currency);
        if (value === undefined) {
            return;
        }
        await campaignCall(`/api/campaign/fronts/${frontID}`, "PATCH",
            value === null ? { clear_funds: true } : { funds: value.amount, currency: value.currency });
    }

    async function setCost(itemID) {
        const item = (state.items || []).find((i) => i.id === itemID);
        const front = (state.fronts || []).find((f) => f.id === item?.front_id);
        const current = item?.cost != null ? `${item.cost} ${item.cost_currency}` : "";
        const value = askAmount(`What will taking “${item?.text ?? "this"}” cost? e.g. 300 EUR. Empty clears it.`, current, front?.funds_currency);
        if (value === undefined) {
            return;
        }
        await patchItem(itemID, value === null ? { clear_cost: true } : { cost: value.amount, cost_currency: value.currency });
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
        // The energy tap: one reading a day, and the readings move with it.
        const tap = e.target.closest("[data-energy]");
        if (tap) {
            act(() => tapEnergy(tap.closest("[data-energy-day]"), Number(tap.dataset.energy)));
            return;
        }

        const button = e.target.closest("[data-action]");
        if (!button || button.tagName === "INPUT") {
            return;
        }
        const supplyEl = button.closest("[data-supply-front]");
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
        case "cost":
            act(() => setCost(id));
            break;
        case "sortie":
            act(() => campaignCall("/api/campaign/sorties", "POST", { front_id: supplyEl.dataset.supplyFront }));
            break;
        case "sortie-form":
            logistics.open = logistics.open === supplyEl.dataset.supplyFront ? null : supplyEl.dataset.supplyFront;
            renderLogistics();
            logistics.fronts.querySelector(".sortie-form select")?.focus();
            break;
        case "sortie-cancel":
            logistics.open = null;
            renderLogistics();
            break;
        case "sortie-delete":
            act(() => campaignCall(`/api/campaign/sorties/${button.closest("[data-sortie]").dataset.sortie}`, "DELETE"));
            break;
        case "funds":
            act(() => setFunds(supplyEl.dataset.supplyFront));
            break;
        case "appraise-front":
            if (staff.front) {
                staff.front.value = button.dataset.front;
            }
            staff.section?.scrollIntoView({ behavior: "smooth", block: "start" });
            askStaff(button.dataset.front);
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
        if (form.matches(".sortie-form")) {
            e.preventDefault();
            act(async () => {
                await campaignCall("/api/campaign/sorties", "POST", {
                    front_id: form.dataset.front,
                    minutes: Number(form.elements.minutes.value),
                    order_id: form.elements.order_id.value,
                    day: form.elements.day.value,
                    note: form.elements.note.value.trim(),
                });
                logistics.open = null;
            });
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
