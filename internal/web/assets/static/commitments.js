// Open loops: what you said you would do.
//
// Proposals — commitments picked up from what you wrote — sit at the top with
// keep and drop. They never join the list on their own, because a list you did
// not agree to stops being trusted, and then stops being read.

function setupCommitments() {
    const panel = document.getElementById("loops");
    const list = document.getElementById("loops-list");
    const form = document.getElementById("loops-add");
    if (!panel || !list || !form) {
        return null;
    }

    let items = [];

    function row(c) {
        const due = c.due
            ? `<span class="loop-due ${c.overdue ? "overdue" : ""}">${c.overdue ? "overdue · " : ""}${escapeHTML(c.due)}</span>`
            : "";

        if (c.status === "proposed") {
            return `
            <li class="loop proposed" data-id="${escapeHTML(c.id)}">
                <span class="loop-text">${escapeHTML(c.text)}</span>
                ${due}
                <span class="loop-proposal-actions">
                    <span class="loop-hint">You said this — track it?</span>
                    <button type="button" class="loop-btn keep" data-set="open">Keep</button>
                    <button type="button" class="loop-btn drop" data-set="dropped">Drop</button>
                </span>
            </li>`;
        }

        return `
        <li class="loop" data-id="${escapeHTML(c.id)}">
            <input type="checkbox" class="loop-check" data-set="done" aria-label="Mark done">
            <span class="loop-text">${escapeHTML(c.text)}</span>
            ${due}
            <button type="button" class="loop-x" data-set="dropped" title="Drop" aria-label="Drop">×</button>
        </li>`;
    }

    function render() {
        const proposals = items.filter((c) => c.status === "proposed");
        const open = items.filter((c) => c.status === "open");

        const count = document.getElementById("loops-count");
        if (count) {
            count.textContent = open.length ? String(open.length) : "";
        }
        const overdue = open.filter((c) => c.overdue).length;
        setSituationChip("sit-loops", open.length
            ? `${open.length} open${overdue ? ` · ${overdue} overdue` : ""}`
            : "");

        if (items.length === 0) {
            list.innerHTML = '<li class="loop-empty muted">Nothing open. Say "I\'ll…" in a chat, or add one below.</li>';
            return;
        }

        list.innerHTML = [...proposals, ...open].map(row).join("");
    }

    async function load() {
        try {
            items = await apiJSON("/api/commitments");
            render();
        } catch (error) {
            list.innerHTML = `<li class="loop-empty muted">${escapeHTML(error.message)}</li>`;
        }
    }

    async function setStatus(id, status) {
        await apiJSON(`/api/commitments/${id}`, {
            method: "PATCH",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ status }),
        });
        await load();
    }

    list.addEventListener("click", async (e) => {
        const control = e.target.closest("[data-set]");
        const rowEl = e.target.closest("[data-id]");
        if (!control || !rowEl) {
            return;
        }
        // Let a checkbox show its tick before the row disappears.
        if (control.type === "checkbox") {
            rowEl.classList.add("closing");
            await new Promise((r) => setTimeout(r, 250));
        }
        try {
            await setStatus(rowEl.dataset.id, control.dataset.set);
        } catch (error) {
            showMessage(document.getElementById("consult-result"), error.message, "error");
            await load();
        }
    });

    form.addEventListener("submit", async (e) => {
        e.preventDefault();
        const input = form.elements.text;
        const text = input.value.trim();
        if (!text) {
            return;
        }
        try {
            await apiJSON("/api/commitments", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ text, due: form.elements.due?.value || "" }),
            });
            form.reset();
            await load();
        } catch (error) {
            showMessage(document.getElementById("consult-result"), error.message, "error");
        }
    });

    // Extraction runs in the background after a reply is sent, so new proposals
    // appear a few seconds later. Re-check twice rather than polling forever.
    function refreshSoon() {
        setTimeout(load, 4000);
        setTimeout(load, 12000);
    }

    // Track a line from an answer by hand.
    async function track(text, sessionID, messageID) {
        await apiJSON("/api/commitments", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ text, session_id: sessionID, message_id: messageID }),
        });
        await load();
    }

    load();
    return { load, refreshSoon, track };
}
