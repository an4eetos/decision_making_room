// Beast mode: the crisis gear. The generals shout, one of them swears, and
// every order they give runs on a clock until you report it done.
//
// The board is the server's: clocks are counted against the server's time,
// and when one runs out the server decides whether it really did. The page
// only notices, and asks.

const BEAST_MODE = "beast";

// The fallback ranking when the modes list has not loaded: the same order the
// shipped mode file gives.
const BEAST_SHOUTERS = ["zhukov", "patton", "chuikov", "konev", "suvorov", "guderian", "alexander"];

// Spacing between eruptions the page asks for on its own. The server enforces
// the same; this just saves asking when the answer is known to be no.
const BEAST_ERUPTION_GAP_MS = 2 * 60 * 1000;

// The order lines are rewritten on the server before the answer is stored, but
// they stream in raw. Do the same rewrite while it is written, drop done
// lines, and hide a line still arriving.
function stripBeastFlags(text) {
    return String(text)
        .replace(/^[ \t]*(?:[-*][ \t]*)?\[\[order:[ \t]*(\d{1,4})[ \t]*(?:m|min|mins|minutes)?[ \t]*\|[ \t]*([^|\]\n]+?)[ \t]*(?:\|[ \t]*loop[ \t]*\d{1,3}[ \t]*)?\]\][ \t]*$/gim,
            (_, minutes, action) => `- ⏱ **${Math.max(1, Math.min(240, Number(minutes)))} min** — ${action.trim()}`)
        .replace(/^[ \t]*\[\[done:[ \t]*[\d, \t]+\]\][ \t]*$/gim, "")
        .replace(/\n[ \t]*(?:[-*][ \t]*)?\[\[[^\]\n]*\]?$/, "")
        .replace(/\n{3,}/g, "\n\n")
        .trimEnd();
}

// pickBeastShouter mirrors the server: the first seated general in the
// ranking, else whoever is seated first.
function pickBeastShouter(seated, ranking) {
    const order = ranking?.length ? ranking : BEAST_SHOUTERS;
    return order.find((id) => seated.includes(id)) ?? seated[0] ?? null;
}

function formatClock(ms) {
    const total = Math.max(0, Math.round(Math.abs(ms) / 1000));
    const m = Math.floor(total / 60);
    const s = String(total % 60).padStart(2, "0");
    return `${m}:${s}`;
}

// setupBeastBoard wires the board under the composer. onExpired is asked to
// send the eruption turn and returns whether it could; onReport hears about a
// task you reported done.
function setupBeastBoard({ onExpired, onReport, onError } = {}) {
    const list = document.getElementById("beast-tasks");
    const addRow = document.getElementById("beast-add");
    if (!list) {
        return null;
    }

    let sessionID = null;
    let active = false;
    let tasks = [];
    // Server time minus page time, so a laptop clock that is off does not
    // decide when you get shouted at.
    let skew = 0;
    let ticker = 0;
    let lastEruption = 0;
    let loadToken = 0;

    function now() {
        return Date.now() + skew;
    }

    function remaining(task) {
        return new Date(task.deadline).getTime() - now();
    }

    function row(t) {
        if (t.status !== "open") {
            return `
            <li class="beast-task ${escapeHTML(t.status)}" data-id="${escapeHTML(t.id)}">
                <span class="beast-num">${t.number}</span>
                <span class="beast-text">${escapeHTML(t.text)}</span>
                <span class="beast-state">${t.status === "done" ? "Done" : "Abandoned"}</span>
            </li>`;
        }
        const missed = t.expiries > 0 ? `<span class="beast-missed" title="Deadlines missed">×${t.expiries}</span>` : "";
        return `
        <li class="beast-task open" data-id="${escapeHTML(t.id)}">
            <span class="beast-num">${t.number}</span>
            <span class="beast-text" data-edit title="Click to edit">${escapeHTML(t.text)}</span>
            ${t.loop ? '<span class="beast-loop" title="Reporting this done also closes the open loop it finishes">closes loop</span>' : ""}
            ${missed}
            <span class="beast-time" data-clock></span>
            <span class="beast-actions">
                <button type="button" class="beast-btn" data-extend="-5" title="Five minutes less">−5</button>
                <button type="button" class="beast-btn" data-extend="5" title="Five minutes more">+5</button>
                <button type="button" class="beast-btn done" data-status="done" title="Report it done. Nothing else stops the clock.">Report done</button>
                <button type="button" class="beast-btn drop" data-status="abandoned" title="Strike it off. The generals will call it what it is." aria-label="Abandon">×</button>
            </span>
        </li>`;
    }

    function render() {
        const open = tasks.filter((t) => t.status === "open");
        // Resolved ones stay a little while as a record, newest last.
        const resolved = tasks.filter((t) => t.status !== "open").slice(-4);
        if (open.length === 0 && resolved.length === 0) {
            list.innerHTML = '<li class="beast-empty muted">No orders yet. The generals will put them here, or add one below.</li>';
            return;
        }
        list.innerHTML = [...open, ...resolved].map(row).join("");
        tick();
    }

    function tick() {
        if (!active) {
            return;
        }
        let expired = false;
        for (const el of list.querySelectorAll("[data-clock]")) {
            const task = tasks.find((t) => t.id === el.closest("[data-id]")?.dataset.id);
            if (!task) {
                continue;
            }
            const left = remaining(task);
            const late = left <= 0;
            el.textContent = late ? `−${formatClock(left)}` : formatClock(left);
            const rowEl = el.closest(".beast-task");
            rowEl.classList.toggle("late", late);
            rowEl.classList.toggle("urgent", !late && left < 60 * 1000);
            expired ||= late;
        }
        if (expired && Date.now() - lastEruption >= BEAST_ERUPTION_GAP_MS) {
            const late = tasks.filter((t) => t.status === "open" && remaining(t) <= 0);
            if (onExpired?.(late)) {
                lastEruption = Date.now();
            }
        }
    }

    async function load() {
        if (!sessionID) {
            tasks = [];
            render();
            return;
        }
        const token = ++loadToken;
        try {
            const board = await apiJSON(`/api/beast/sessions/${sessionID}/tasks`);
            if (token !== loadToken) {
                return;
            }
            skew = new Date(board.now).getTime() - Date.now();
            tasks = board.tasks ?? [];
            render();
        } catch (error) {
            onError?.(error);
        }
    }

    async function patch(id, body) {
        await apiJSON(`/api/beast/tasks/${id}`, {
            method: "PATCH",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify(body),
        });
    }

    list.addEventListener("click", async (e) => {
        const rowEl = e.target.closest("[data-id]");
        if (!rowEl) {
            return;
        }
        const id = rowEl.dataset.id;
        const task = tasks.find((t) => t.id === id);
        try {
            const status = e.target.closest("[data-status]")?.dataset.status;
            const extend = e.target.closest("[data-extend]")?.dataset.extend;
            if (status === "done") {
                rowEl.classList.add("closing");
                await patch(id, { status });
                await load();
                onReport?.(task);
            } else if (status === "abandoned") {
                if (!window.confirm("Abandon this order? It stays on the board as abandoned.")) {
                    return;
                }
                await patch(id, { status });
                await load();
            } else if (extend) {
                await patch(id, { extend: Number(extend) });
                await load();
            } else if (e.target.closest("[data-edit]")) {
                const text = window.prompt("Edit the order", task?.text ?? "");
                if (text && text.trim() && text.trim() !== task?.text) {
                    await patch(id, { text: text.trim() });
                    await load();
                }
            }
        } catch (error) {
            onError?.(error);
            await load();
        }
    });

    async function addTask() {
        if (!sessionID) {
            onError?.(new Error("Send a message first; the board belongs to a conversation."));
            return;
        }
        const textInput = document.getElementById("beast-add-text");
        const minutesInput = document.getElementById("beast-add-minutes");
        const text = textInput?.value.trim();
        if (!text) {
            return;
        }
        try {
            await apiJSON(`/api/beast/sessions/${sessionID}/tasks`, {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ text, minutes: Number(minutesInput?.value) || 15 }),
            });
            textInput.value = "";
            await load();
        } catch (error) {
            onError?.(error);
        }
    }

    document.getElementById("beast-add-button")?.addEventListener("click", addTask);
    // Enter adds the order instead of submitting the chat it sits inside.
    addRow?.addEventListener("keydown", (e) => {
        if (e.key === "Enter") {
            e.preventDefault();
            addTask();
        }
    });

    return {
        // setSession points the board at a conversation and starts or stops
        // its clocks. Off, the clocks keep running on the server; they are
        // just not watched, so nobody erupts at a tab you left.
        setSession(id, on) {
            const changed = id !== sessionID;
            sessionID = id;
            active = Boolean(on && id);
            clearInterval(ticker);
            ticker = active ? setInterval(tick, 1000) : 0;
            if (changed) {
                tasks = [];
                lastEruption = 0;
            }
            if (active) {
                load();
            } else if (changed) {
                render();
            }
        },
        load,
        // The eruption the page asked for was declined, or failed: let the
        // next tick ask again once the gap has passed rather than never.
        eruptionDone() {
            load();
        },
    };
}

// The beast button pulses while something is due today or overdue: that is
// the crisis the button is for. Open loops announce themselves via an event,
// so this does not reach into the loops panel.
function setupBeastAlarm() {
    const button = document.getElementById("beast-now");
    if (!button) {
        return;
    }
    document.addEventListener("loops-crisis", (e) => {
        const count = e.detail?.count ?? 0;
        button.classList.toggle("crisis", count > 0);
        button.title = count > 0
            ? `${count} open loop${count === 1 ? "" : "s"} due today or overdue. The generals stop being polite.`
            : "The generals stop being polite: orders on a clock, and one of them swears at you.";
    });
}
