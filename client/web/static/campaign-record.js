// The campaign record: one quarter, from planning through operations to an
// after-action. Predictions come from a pre-mortem. You state the chance and
// you score them. Nothing here is inferred from what you write.

const RECORD_PHASE = {
    planning: "Planning",
    operations: "Operations",
    review: "After-action",
    closed: "Closed",
};

const RECORD_CHANCE = [
    { id: "long", label: "Long shot" },
    { id: "even", label: "Even" },
    { id: "likely", label: "Likely" },
];

const RECORD_KIND = {
    taken: "Objective taken",
    withdrawal: "Honest withdrawal",
    called: "Pre-mortem called it",
};

function setupCampaignRecord() {
    const root = document.getElementById("record");
    const body = document.getElementById("record-body");
    const actions = document.getElementById("record-actions");
    if (!root || !body) {
        return;
    }
    const result = document.getElementById("campaign-result");
    let page = null;
    let poll = 0;

    async function load() {
        try {
            page = await apiJSON("/api/campaign/record");
            render();
        } catch (error) {
            if (result) {
                showMessage(result, recordErrorText(error), "error");
            }
        }
    }

    function render() {
        clearTimeout(poll);
        if (!page) {
            return;
        }
        const focused = document.activeElement;
        const draft = focused?.matches?.("[data-note]") ? {
            id: focused.closest("[data-campaign]")?.dataset.campaign,
            value: focused.value,
            start: focused.selectionStart,
        } : null;
        const campaigns = page.campaigns || [];
        const open = campaigns.filter((c) => c.phase !== "closed");
        const closed = campaigns.filter((c) => c.phase === "closed");
        const todayExists = campaigns.some((c) => c.quarter === page.quarter);
        actions.innerHTML = todayExists ? "" : `<button type="button" class="mini-btn" data-record="open">Open ${escapeHTML(page.label)}</button>`;
        const earlier = closed.length === 0 ? "" : `<details class="record-earlier"><summary>Closed quarters</summary>${closed.map((c) => campaignHTML(c, page)).join("")}</details>`;
        body.innerHTML = open.map((c) => campaignHTML(c, page)).join("") + unfiledHTML(page.unfiled || [], open[0]) + earlier;
        if (draft?.id) {
            const area = body.querySelector(`[data-campaign="${draft.id}"] [data-note]`);
            if (area) {
                area.value = draft.value;
                area.focus();
                area.selectionStart = area.selectionEnd = draft.start;
            }
        }
        if (page.review && page.review.busy) {
            poll = setTimeout(load, 1500);
        }
    }

    async function act(fn) {
        try {
            await fn();
            await load();
        } catch (error) {
            if (result) {
                showMessage(result, recordErrorText(error), "error");
            }
        }
    }

    root.addEventListener("click", (event) => {
        const button = event.target.closest("[data-record]");
        if (!button) {
            return;
        }
        const card = button.closest("[data-campaign]");
        const id = card?.dataset.campaign;
        const pred = button.closest("[data-prediction]")?.dataset.prediction;
        switch (button.dataset.record) {
        case "open":
            act(() => campaignCall("/api/campaign/record", "POST", {}));
            break;
        case "phase":
            act(() => campaignCall(`/api/campaign/record/${id}/phase`, "POST", { phase: button.dataset.phase }));
            break;
        case "delete":
            if (window.confirm("Drop this quarter? Predictions filed on it stay, unfiled.")) {
                act(() => campaignCall(`/api/campaign/record/${id}`, "DELETE"));
            }
            break;
        case "assign": {
            const select = card.querySelector("[data-assign]");
            if (select?.value) {
                act(() => campaignCall(`/api/campaign/record/${id}/objectives`, "POST", { item_id: select.value }));
            }
            break;
        }
        case "unassign":
            act(() => campaignCall(`/api/campaign/record/${id}/objectives/${button.dataset.item}`, "DELETE"));
            break;
        case "keep":
            act(() => campaignCall(`/api/campaign/predictions/${pred}`, "PATCH", { status: "kept" }));
            break;
        case "drop":
            act(() => campaignCall(`/api/campaign/predictions/${pred}`, "PATCH", { status: "dropped" }));
            break;
        case "file":
            act(() => campaignCall(`/api/campaign/predictions/${pred}`, "PATCH", { campaign_id: button.dataset.campaign }));
            break;
        case "chance":
            act(() => campaignCall(`/api/campaign/predictions/${pred}`, "PATCH", { chance: button.dataset.chance }));
            break;
        case "outcome":
            act(() => campaignCall(`/api/campaign/predictions/${pred}`, "PATCH", { outcome: button.dataset.outcome }));
            break;
        case "review":
            act(async () => {
                await campaignCall(`/api/campaign/record/${id}/review`, "POST", {});
            });
            break;
        default:
        }
    });

    root.addEventListener("change", (event) => {
        const note = event.target.closest("[data-note]");
        if (!note) {
            return;
        }
        const id = note.closest("[data-campaign]")?.dataset.campaign;
        act(() => campaignCall(`/api/campaign/record/${id}`, "PATCH", { note: note.value }));
    });

    window.refreshCampaignRecord = load;
    load();
}

function campaignHTML(c, page) {
    const closed = c.phase === "closed";
    const earlier = closed ? "record-closed" : "";
    return `
    <article class="record-card ${earlier}" data-campaign="${escapeHTML(c.id)}">
        <header class="record-card-head">
            <div>
                <h3>${escapeHTML(c.name)} <span class="record-phase">${escapeHTML(RECORD_PHASE[c.phase] || c.phase)}</span></h3>
                <p class="muted">${escapeHTML(c.label)}</p>
            </div>
            <div class="record-card-actions">${phaseButtons(c)}</div>
        </header>
        ${c.ended ? `<p class="record-ended">This quarter has ended. The after-action is open when you want it.</p>` : ""}
        ${calibrationHTML(c.calibration)}
        ${commendationsHTML(c.commendations)}
        ${objectivesHTML(c, page.objectives || [], closed)}
        ${predictionsHTML(c.predictions || [], closed)}
        ${reviewHTML(c, page.review)}
        ${closed ? "" : `<label class="record-note-field">Your note<textarea data-note rows="2">${escapeHTML(c.note || "")}</textarea></label>`}
    </article>`;
}

function phaseButtons(c) {
    if (c.phase === "planning") {
        return `<button type="button" class="mini-btn" data-record="phase" data-phase="operations">Begin operations</button>
            <button type="button" class="mini-btn" data-record="delete">Drop quarter</button>`;
    }
    if (c.phase === "operations") {
        return `<button type="button" class="mini-btn" data-record="phase" data-phase="review">Open the after-action</button>
            <button type="button" class="mini-btn" data-record="delete">Drop quarter</button>`;
    }
    if (c.phase === "review") {
        return `<button type="button" class="mini-btn" data-record="review">Write the after-action</button>
            <button type="button" class="mini-btn" data-record="phase" data-phase="operations">Back to operations</button>
            <button type="button" class="mini-btn" data-record="phase" data-phase="closed">Close the quarter</button>
            <button type="button" class="mini-btn" data-record="delete">Drop quarter</button>`;
    }
    return "";
}

function calibrationHTML(cal) {
    if (!cal) {
        return "";
    }
    const buckets = (cal.buckets || []).map((b) => {
        const label = RECORD_CHANCE.find((c) => c.id === b.chance)?.label || b.chance;
        return `<li>When you said ${escapeHTML(label.toLowerCase())}, it showed ${b.called} of ${b.scored}.</li>`;
    }).join("");
    let number = `<p class="record-calibration-wait">Calibration withheld, ${cal.n} of ${cal.min} scored.</p>`;
    if (cal.score != null) {
        number = `<p class="record-calibration"><span>${Number(cal.score).toFixed(2)}</span> over ${cal.n} scored predictions. 0.25 is a coin flip; lower means the chances you stated matched what happened.</p>`;
    }
    if (cal.n === 0 && buckets === "") {
        return "";
    }
    return `<div class="record-calibration-block">${number}${buckets ? `<ul class="record-buckets">${buckets}</ul>` : ""}</div>`;
}

function commendationsHTML(lines) {
    if (!lines || lines.length === 0) {
        return "";
    }
    const items = lines.map((line) =>
        `<li><span class="record-kind">${escapeHTML(RECORD_KIND[line.kind] || line.kind)}</span> ${escapeHTML(line.text)}</li>`).join("");
    return `<ul class="record-commendations">${items}</ul>`;
}

function objectivesHTML(c, pool, closed) {
    const assigned = new Set((c.objectives || []).map((o) => o.id));
    const rows = (c.objectives || []).map((o) => `<li>
        <span>${escapeHTML(o.text)}</span>
        <span class="muted">${escapeHTML(o.status)}</span>
        ${closed ? "" : `<button type="button" class="mini-btn" data-record="unassign" data-item="${escapeHTML(o.id)}">Remove</button>`}
    </li>`).join("");
    const options = pool.filter((o) => !assigned.has(o.id)).map((o) =>
        `<option value="${escapeHTML(o.id)}">${escapeHTML(o.text)}</option>`).join("");
    const picker = closed || options === "" ? "" : `<div class="record-assign">
        <select data-assign aria-label="Objective to assign"><option value="">Assign an objective</option>${options}</select>
        <button type="button" class="mini-btn" data-record="assign">Assign</button>
    </div>`;
    if (rows === "" && picker === "") {
        return `<p class="muted">No objectives assigned. A multi-year goal is not pulled in by the date.</p>`;
    }
    return `<div class="record-objectives"><ul>${rows}</ul>${picker}</div>`;
}

function predictionsHTML(predictions, closed, fileOnto) {
    if (!predictions || predictions.length === 0) {
        return "";
    }
    return `<ul class="record-predictions">${predictions.map((p) => predictionHTML(p, closed, fileOnto)).join("")}</ul>`;
}

function predictionHTML(p, closed, fileOnto) {
    const warning = p.warning ? `<p class="record-warning">Early warning: ${escapeHTML(p.warning)}</p>` : "";
    const file = !closed && fileOnto && !p.campaign_id
        ? `<button type="button" class="mini-btn" data-record="file" data-campaign="${escapeHTML(fileOnto)}">File on this quarter</button>`
        : "";
    let controls = file;
    if (!closed && p.status === "proposed") {
        controls += `<button type="button" class="loop-btn keep" data-record="keep">Keep</button>
            <button type="button" class="loop-btn drop" data-record="drop">Drop</button>`;
    }
    if (!closed && p.status === "kept") {
        controls += chanceHTML(p) + outcomeHTML(p);
    }
    const scored = p.outcome === "called" ? "Showed" : p.outcome === "missed" ? "Did not show" : "";
    return `<li data-prediction="${escapeHTML(p.id)}">
        <span class="intel-type">${p.status === "proposed" ? "Proposal" : "Prediction"}</span>
        <span class="intel-text">${escapeHTML(p.text)}</span>
        ${scored ? `<span class="record-outcome">${scored}</span>` : ""}
        ${warning}
        <span class="intel-actions">${controls}</span>
    </li>`;
}

function chanceHTML(p) {
    return RECORD_CHANCE.map((c) =>
        `<button type="button" class="mini-btn ${p.chance === c.id ? "on" : ""}" data-record="chance" data-chance="${c.id}">${c.label}</button>`).join("");
}

function outcomeHTML(p) {
    return `<button type="button" class="mini-btn ${p.outcome === "called" ? "on" : ""}" data-record="outcome" data-outcome="called">It showed</button>
        <button type="button" class="mini-btn ${p.outcome === "missed" ? "on" : ""}" data-record="outcome" data-outcome="missed">It did not</button>`;
}

function reviewHTML(c, review) {
    const writing = review && review.busy && review.campaign_id === c.id;
    const failed = review && review.error && review.campaign_id === c.id;
    let text = "";
    if (writing) {
        text = `<p class="muted">Writing the after-action…</p>`;
    } else if (failed) {
        text = `<p class="record-ended">${escapeHTML(review.error)}</p>`;
    }
    if (!c.review) {
        return text;
    }
    return `${text}<div class="record-review">${window.renderMarkdown ? renderMarkdown(c.review) : escapeHTML(c.review)}</div>`;
}

function unfiledHTML(predictions, open) {
    if (predictions.length === 0) {
        return "";
    }
    return `<section class="record-unfiled"><h3>Not filed on a quarter</h3>${predictionsHTML(predictions, false, open && open.id)}</section>`;
}

function recordErrorText(error) {
    try {
        const parsed = JSON.parse(error.message);
        if (parsed && parsed.error) {
            return parsed.error;
        }
    } catch {
        // The body was already plain text.
    }
    return error.message;
}

document.addEventListener("DOMContentLoaded", setupCampaignRecord);
