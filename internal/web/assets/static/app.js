function escapeHTML(value) {
    return String(value)
        .replaceAll("&", "&amp;")
        .replaceAll("<", "&lt;")
        .replaceAll(">", "&gt;")
        .replaceAll('"', "&quot;");
}

function truncate(text, max) {
    const value = String(text || "").trim();
    if (value.length <= max) {
        return value;
    }
    return value.slice(0, max) + "...";
}

function formatDate(iso) {
    if (!iso) {
        return "";
    }
    return iso.slice(0, 10);
}

function showMessage(container, message, type) {
    container.innerHTML = `<div class="${type}">${escapeHTML(message)}</div>`;
}

async function apiJSON(url, options) {
    const controller = new AbortController();
    const timeoutMs = options?.timeoutMs ?? 45000;
    const timeout = setTimeout(() => controller.abort(), timeoutMs);

    let response;
    try {
        response = await fetch(url, { ...options, signal: controller.signal });
    } catch (error) {
        clearTimeout(timeout);
        if (error && error.name === "AbortError") {
            throw new Error("Request timed out. Try a shallower depth, or switch to another Gemini model.");
        }
        throw error;
    }

    clearTimeout(timeout);
    if (!response.ok) {
        const text = await response.text();
        throw new Error(text || response.statusText);
    }
    if (response.status === 204) {
        return null;
    }
    return response.json();
}

const TIER_LABELS = { quick: "Quick", standard: "Standard", deep: "Deep" };

// How long a tier is allowed to take. Deep runs several tool rounds against a
// wider candidate pool; the old flat 45s aborted it mid-answer.
const TIER_TIMEOUTS = { quick: 30000, standard: 90000, deep: 240000 };

function renderConsultResult(container, result) {
    let sourcesHTML = "";
    if (result.sources && result.sources.length > 0) {
        const items = result.sources.map((source) => {
            const score = source.score > 0
                ? ` <span class="muted">(${Math.round(source.score * 100)}% match)</span>`
                : "";
            return `<li><span class="badge">${escapeHTML(source.kind)}</span> ${escapeHTML(source.title || "(untitled)")}${score}</li>`;
        }).join("");
        sourcesHTML = `<div class="sources"><h4>Sources</h4><ul>${items}</ul></div>`;
    }

    container.innerHTML = `
        <div class="answer">
            <h3>Answer</h3>
            <div class="answer-body markdown">${renderMarkdown(result.answer)}</div>
        </div>
        ${sourcesHTML}
    `;
}

function renderChatSources(sources) {
    if (!sources || sources.length === 0) {
        return "";
    }

    const items = sources.map((source) => {
        const score = source.score > 0
            ? ` <span class="muted">(${Math.round(source.score * 100)}% match)</span>`
            : "";
        return `<li><span class="badge">${escapeHTML(source.kind)}</span> ${escapeHTML(source.title || "(untitled)")}${score}</li>`;
    }).join("");

    return `<div class="sources"><ul>${items}</ul></div>`;
}

// Messages store lens ids; the roster has the display names. Falls back to a
// tidied id so a lens removed from an overlay still renders readably.
let lensNames = {};

function lensName(id) {
    return lensNames[id] || String(id).replace(/_/g, " ");
}

// Set once the mode list loads, so badges show display names rather than ids.
let modeNames = {};

function modeChipName(id) {
    return modeNames[id] || String(id).replace(/_/g, " ");
}

function renderChatMessages(container, messages) {
    if (messages === null) {
        container.innerHTML = '<p class="muted">Loading conversation...</p>';
        return;
    }

    if (!messages || messages.length === 0) {
        container.innerHTML = '<p class="muted">Start a conversation.</p>';
        return;
    }

    container.innerHTML = messages.map((message) => {
        const isAssistant = message.role === "assistant";
        // Only assistant answers are markdown. What you typed is shown exactly as
        // you typed it — rendering your own text would mangle anything containing
        // an asterisk or a hash.
        const briefing = isAssistant ? renderBriefing(message.content) : null;
        const body = briefing
            ? `<div class="chat-message-body">${briefing}</div>`
            : isAssistant
                ? `<div class="chat-message-body markdown">${renderMarkdown(message.content)}</div>`
                : `<div class="chat-message-body">${escapeHTML(message.content)}</div>`;

        const tier = isAssistant && message.tier
            ? `<span class="tier-badge tier-${escapeHTML(message.tier)}">${escapeHTML(TIER_LABELS[message.tier] || message.tier)}</span>`
            : "";

        // Which lenses produced this answer, and whether you picked them. An
        // auto-selected lens should never look like one you chose.
        const mode = isAssistant && message.mode && message.mode !== "open"
            ? `<span class="mode-badge">${escapeHTML(modeChipName(message.mode))}</span>`
            : "";

        const lenses = isAssistant && message.generals?.length
            ? `<span class="lens-badges">${message.generals.map((id) =>
                    `<span class="lens-badge">${escapeHTML(lensName(id))}</span>`).join("")}` +
              `${message.generals_method === "auto" ? '<span class="lens-auto" title="Chosen for you">auto</span>' : ""}` +
              `${message.generals_method === "sticky" ? '<span class="lens-auto" title="Kept from earlier in this conversation">auto · kept</span>' : ""}</span>`
            : "";

        return `
        <div class="chat-message ${escapeHTML(message.role)}">
            <div class="chat-message-head">
                <span class="chat-message-role">${escapeHTML(message.role)}</span>
                ${mode}
                ${tier}
                ${lenses}
            </div>
            ${body}
            ${isAssistant ? renderChatSources(message.sources) : ""}
            ${isAssistant && message.id ? `<button type="button" class="track-btn" data-track-message="${escapeHTML(message.id)}">Track something from this</button>` : ""}
        </div>`;
    }).join("");
    container.scrollTop = container.scrollHeight;
}

function renderChatSessions(container, sessions, activeSessionID) {
    if (!sessions || sessions.length === 0) {
        container.innerHTML = '<p class="muted">No chats yet.</p>';
        return;
    }

    container.innerHTML = sessions.map((session) => `
        <div class="chat-session-row ${session.id === activeSessionID ? "active" : ""}">
            <button
                type="button"
                class="chat-session-item"
                data-session-id="${escapeHTML(session.id)}">
                <span class="chat-session-title">${escapeHTML(session.title || "New chat")}</span>
                <span class="chat-session-meta muted">${escapeHTML(formatDate(session.updated_at))}</span>
            </button>
            <button
                type="button"
                class="chat-session-delete"
                data-session-id="${escapeHTML(session.id)}"
                title="Delete chat"
                aria-label="Delete chat">
                <span class="chat-session-delete-icon" aria-hidden="true">Del</span>
            </button>
        </div>
    `).join("");
}

function memoryRowCellsHTML(entry) {
    return `
        <td>${escapeHTML(formatDate(entry.created_at))}</td>
        <td><span class="badge">${escapeHTML(entry.kind)}</span></td>
        <td>${escapeHTML(entry.title || "")}</td>
        <td class="preview">${escapeHTML(truncate(entry.body, 120))}</td>
        <td>${escapeHTML((entry.tags || []).join(", "))}</td>
        <td class="row-actions">
            <button
                type="button"
                class="row-edit"
                data-memory-id="${escapeHTML(entry.id)}"
                title="Edit memory"
                aria-label="Edit memory">
                Edit
            </button>
            <button
                type="button"
                class="row-delete"
                data-memory-id="${escapeHTML(entry.id)}"
                title="Delete memory"
                aria-label="Delete memory">
                Del
            </button>
        </td>
    `;
}

// memoryKindOptionsHTML mirrors the search form's kind <select>, built server
// side from domain.MemoryKind, so the edit form never hardcodes its own copy
// of the kind list.
function memoryKindOptionsHTML(selected) {
    const source = document.getElementById("search-kind");
    if (!source) {
        return "";
    }
    return Array.from(source.options)
        .filter((opt) => opt.value !== "")
        .map((opt) => `<option value="${escapeHTML(opt.value)}" ${opt.value === selected ? "selected" : ""}>${escapeHTML(opt.textContent)}</option>`)
        .join("");
}

function memoryEditRowHTML(entry) {
    return `
        <td colspan="6">
            <div class="edit-form">
                <select class="edit-kind">${memoryKindOptionsHTML(entry.kind)}</select>
                <input class="edit-title" type="text" placeholder="Title" value="${escapeHTML(entry.title || "")}">
                <textarea class="edit-body" rows="4">${escapeHTML(entry.body || "")}</textarea>
                <input class="edit-tags" type="text" placeholder="tags, comma, separated" value="${escapeHTML((entry.tags || []).join(", "))}">
                <div class="edit-actions">
                    <button type="button" class="btn edit-save">Save</button>
                    <button type="button" class="btn-secondary edit-cancel">Cancel</button>
                </div>
            </div>
        </td>
    `;
}

function renderMemoriesTable(container, entries) {
    if (!entries || entries.length === 0) {
        container.innerHTML = '<p class="muted">No memories yet. <a href="/ingest">Add your first entry</a>.</p>';
        return;
    }

    const rows = entries.map((entry) => `
        <tr data-memory-id="${escapeHTML(entry.id)}">${memoryRowCellsHTML(entry)}</tr>
    `).join("");

    container.innerHTML = `
        <table class="table">
            <thead>
                <tr>
                    <th>Date</th>
                    <th>Kind</th>
                    <th>Title</th>
                    <th>Preview</th>
                    <th>Tags</th>
                    <th></th>
                </tr>
            </thead>
            <tbody>${rows}</tbody>
        </table>
    `;
}

const ACTIVE_CHAT_STORAGE_KEY = "activeChatSession";

function getSessionFromURL() {
    return new URLSearchParams(window.location.search).get("session");
}

function persistActiveSession(sessionID) {
    const url = new URL(window.location.href);
    if (sessionID) {
        url.searchParams.set("session", sessionID);
        localStorage.setItem(ACTIVE_CHAT_STORAGE_KEY, sessionID);
    } else {
        url.searchParams.delete("session");
        localStorage.removeItem(ACTIVE_CHAT_STORAGE_KEY);
    }
    history.replaceState({ sessionID: sessionID || null }, "", url);
}

function resolveActiveSessionID(sessions) {
    const fromURL = getSessionFromURL();
    if (fromURL && sessions.some((s) => s.id === fromURL)) {
        return fromURL;
    }

    const fromStorage = localStorage.getItem(ACTIVE_CHAT_STORAGE_KEY);
    if (fromStorage && sessions.some((s) => s.id === fromStorage)) {
        return fromStorage;
    }

    if (sessions.length > 0) {
        return sessions[0].id;
    }

    return null;
}

function appendOptimisticMessage(container, role, content, id) {
    const placeholder = container.querySelector(".muted");
    if (placeholder && placeholder.textContent === "Start a conversation.") {
        container.innerHTML = "";
    }

    container.insertAdjacentHTML("beforeend", `
        <div id="${id}" class="chat-message ${role} pending">
            <div class="chat-message-head">
                <span class="chat-message-role">${escapeHTML(role)}</span>
            </div>
            <div class="chat-message-body">${escapeHTML(content)}</div>
        </div>
    `);
    container.scrollTop = container.scrollHeight;
}

function setupConsultForm() {
    const form = document.getElementById("consult-form");
    const result = document.getElementById("consult-result");
    const messages = document.getElementById("chat-messages");
    const sessions = document.getElementById("chat-sessions");
    const newChatButton = document.getElementById("new-chat-button");
    if (!form || !result || !messages || !sessions) {
        return;
    }

    const tierPicker = form.elements.tier;

    // The tier caps how many lenses an answer is written through, so the picker
    // follows it rather than letting you choose three for a quick answer.
    const LENSES_PER_TIER = { quick: 1, standard: 2, deep: 3 };

    const loops = setupCommitments();

    const modeChip = setupModeChip({ onChange: () => {} });
    modeChip?.load().then(async () => {
        try {
            modeNames = Object.fromEntries(
                (await apiJSON("/api/modes")).map((m) => [m.id, m.name]),
            );
        } catch {
            // Badges fall back to a tidied id.
        }
    });

    const generals = setupGeneralsPicker();
    generals?.load().then(() => {
        generals.setMax(LENSES_PER_TIER[selectedTier()] ?? 2);
        lensNames = generals.names();
        setBriefingRoster(generals.all());
        // Any messages already on screen were rendered before the roster
        // arrived, so redraw them with proper names.
        if (activeSessionID) {
            loadSession(activeSessionID);
        }
    });

    if (tierPicker) {
        for (const radio of tierPicker) {
            radio.addEventListener("change", () => {
                generals?.setMax(LENSES_PER_TIER[radio.value] ?? 2);
            });
        }
    }

    function selectedTier() {
        return tierPicker ? tierPicker.value : "standard";
    }

    function applyTier(tier) {
        if (!tier || !tierPicker) {
            return;
        }
        for (const radio of tierPicker) {
            radio.checked = radio.value === tier;
        }
    }

    let activeSessionID = null;
    let sendInFlight = false;
    let sessionsRefreshInFlight = false;
    let sessionLoadToken = 0;
    let sessionCreationPromise = null;
    let cachedSessions = [];

    async function refreshSessions() {
        if (sessionsRefreshInFlight) {
            return cachedSessions;
        }
        sessionsRefreshInFlight = true;
        try {
            sessions.innerHTML = '<p class="muted">Loading chats...</p>';
            const data = await apiJSON("/api/chat/sessions");
            cachedSessions = data;
            renderChatSessions(sessions, data, activeSessionID);
            return data;
        } finally {
            sessionsRefreshInFlight = false;
        }
    }

    async function loadSession(sessionID) {
        if (!sessionID) {
            renderChatMessages(messages, []);
            result.innerHTML = "";
            return;
        }

        const token = ++sessionLoadToken;
        renderChatMessages(messages, null);

        try {
            const data = await apiJSON(`/api/chat/sessions/${sessionID}`);
            if (token !== sessionLoadToken) {
                return;
            }
            activeSessionID = sessionID;
            persistActiveSession(sessionID);
            renderChatMessages(messages, data.messages);
            // A session remembers the depth and the lenses it was last used
            // with, so reopening a conversation does not silently reset either.
            applyTier(data.session?.tier);
            generals?.setMax(LENSES_PER_TIER[selectedTier()] ?? 2);
            generals?.set(data.session?.generals);
            modeChip?.setPinned(data.session?.mode_locked ? data.session?.mode : null);
            modeChip?.setDetected(data.session?.mode, "sticky");
            result.innerHTML = "";
        } catch (error) {
            if (token !== sessionLoadToken) {
                return;
            }
            showMessage(result, error.message, "error");
        }
    }

    async function ensureSession() {
        if (activeSessionID) {
            return activeSessionID;
        }

        if (!sessionCreationPromise) {
            sessionCreationPromise = apiJSON("/api/chat/sessions", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ title: "New chat" }),
            }).then(async (session) => {
                activeSessionID = session.id;
                persistActiveSession(session.id);
                await refreshSessions();
                return session.id;
            }).finally(() => {
                sessionCreationPromise = null;
            });
        }

        return sessionCreationPromise;
    }

    function startNewChat() {
        generals?.set([]);
        modeChip?.setPinned(null);
        modeChip?.setDetected(null);
        activeSessionID = null;
        sessionLoadToken++;
        form.question.value = "";
        result.innerHTML = "";
        renderChatMessages(messages, []);
        persistActiveSession(null);
        refreshSessions();
    }

    async function deleteSession(sessionID) {
        if (!sessionID || sendInFlight) {
            return;
        }
        if (!window.confirm("Delete this chat? This cannot be undone.")) {
            return;
        }

        try {
            await apiJSON(`/api/chat/sessions/${sessionID}`, { method: "DELETE" });
            if (activeSessionID === sessionID) {
                activeSessionID = null;
                sessionLoadToken++;
                renderChatMessages(messages, []);
                result.innerHTML = "";
                persistActiveSession(null);
            }

            const remaining = await refreshSessions();
            if (activeSessionID === null && remaining.length > 0) {
                await loadSession(remaining[0].id);
                await refreshSessions();
            }
        } catch (error) {
            showMessage(result, error.message, "error");
        }
    }

    async function initChat() {
        const sessionList = await refreshSessions();
        const restoredID = resolveActiveSessionID(sessionList);
        if (restoredID) {
            await loadSession(restoredID);
        } else {
            renderChatMessages(messages, []);
        }
        await refreshSessions();
    }

    if (newChatButton) {
        newChatButton.addEventListener("click", () => {
            if (sendInFlight) {
                return;
            }
            startNewChat();
        });
    }

    // "Track something from this" asks what, rather than guessing a line from
    // the answer: the answer is advice, and only you know which part you are
    // actually committing to.
    messages.addEventListener("click", async (e) => {
        const button = e.target.closest("[data-track-message]");
        if (!button || !loops) {
            return;
        }
        const text = window.prompt("What are you committing to?");
        if (!text || !text.trim()) {
            return;
        }
        try {
            await loops.track(text.trim(), activeSessionID, button.dataset.trackMessage);
            button.textContent = "Tracked";
            button.disabled = true;
        } catch (error) {
            showMessage(result, error.message, "error");
        }
    });

    // A check-in's Reply opens its conversation here.
    document.addEventListener("open-session", async (e) => {
        const id = e.detail?.id;
        if (!id || sendInFlight) {
            return;
        }
        // Load first, then refresh: the sidebar highlights whatever is active
        // at render time, so refreshing first left the previous chat selected.
        await loadSession(id);
        await refreshSessions();
        form.question.focus();
    });

    sessions.addEventListener("click", async (e) => {
        const deleteBtn = e.target.closest(".chat-session-delete");
        if (deleteBtn) {
            e.preventDefault();
            e.stopPropagation();
            await deleteSession(deleteBtn.dataset.sessionId);
            return;
        }

        const btn = e.target.closest(".chat-session-item");
        if (!btn) {
            return;
        }
        const id = btn.dataset.sessionId;
        if (!id || id === activeSessionID) {
            return;
        }
        activeSessionID = id;
        persistActiveSession(id);
        await loadSession(id);
        await refreshSessions();
    });

    window.addEventListener("popstate", () => {
        if (sendInFlight) {
            return;
        }
        const sessionID = getSessionFromURL();
        if (sessionID && cachedSessions.some((s) => s.id === sessionID)) {
            activeSessionID = sessionID;
            loadSession(sessionID);
            refreshSessions();
            return;
        }
        if (!sessionID) {
            startNewChat();
        }
    });

    initChat().catch((error) => {
        showMessage(result, error.message, "error");
    });

    form.addEventListener("submit", async (event) => {
        event.preventDefault();
        const button = form.querySelector("button[type=submit]");
        if (!button) {
            return;
        }
        const question = form.question.value.trim();
        if (!question) {
            return;
        }

        if (sendInFlight) {
            return;
        }

        button.disabled = true;
        sendInFlight = true;
        form.question.value = "";

        const tier = selectedTier();
        appendOptimisticMessage(messages, "user", question, "chat-pending-user");
        appendOptimisticMessage(messages, "assistant", "", "chat-pending-assistant");

        // Narrate the wait using the lenses actually pinned. Auto-selected ones
        // are unknown until the answer arrives, so those stay unnamed.
        const pendingBody = document
            .getElementById("chat-pending-assistant")
            ?.querySelector(".chat-message-body");
        const stopWaiting = pendingBody
            ? startWaiting(pendingBody, {
                tier,
                lensNames: (generals?.selected() ?? []).map((id) => lensName(id)),
            })
            : () => {};

        try {
            const sessionID = await ensureSession();
            const data = await apiJSON(`/api/chat/sessions/${sessionID}/messages`, {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({
                    content: question,
                    tier,
                    generals: generals?.selected() ?? [],
                    mode: modeChip ? (modeChip.pinned() ?? "") : undefined,
                }),
                timeoutMs: TIER_TIMEOUTS[tier] ?? TIER_TIMEOUTS.standard,
            });

            activeSessionID = data.session.id;
            persistActiveSession(data.session.id);
            renderChatMessages(messages, data.messages);
            result.innerHTML = "";

            // Anything you committed to is extracted in the background; pick
            // up the proposals when they land.
            loops?.refreshSoon();

            // Show what the room actually decided this question was.
            const answer = [...data.messages].reverse().find((m) => m.role === "assistant");
            if (answer?.mode) {
                modeChip?.setDetected(answer.mode, answer.mode_method);
            }

            await refreshSessions();
        } catch (error) {
            document.getElementById("chat-pending-user")?.remove();
            document.getElementById("chat-pending-assistant")?.remove();
            showMessage(result, error.message, "error");
        } finally {
            stopWaiting();
            document.getElementById("chat-pending-user")?.remove();
            document.getElementById("chat-pending-assistant")?.remove();
            button.disabled = false;
            sendInFlight = false;
        }
    });
}

function setupIngestForm() {
    const form = document.getElementById("ingest-form");
    const result = document.getElementById("ingest-result");
    if (!form || !result) {
        return;
    }

    form.addEventListener("submit", async (event) => {
        event.preventDefault();
        const button = form.querySelector("button[type=submit]");
        const formData = new FormData(form);

        button.disabled = true;

        try {
            const response = await fetch("/api/memories", {
                method: "POST",
                body: formData,
            });
            if (!response.ok) {
                const text = await response.text();
                throw new Error(text || response.statusText);
            }

            const data = await response.json();
            result.innerHTML = `<div class="success">Saved ${data.chunks} chunk(s). <a href="/memories">View memories</a></div>`;
            form.reset();
        } catch (error) {
            showMessage(result, error.message, "error");
        } finally {
            button.disabled = false;
        }
    });
}

function setupMemoriesTable() {
    const container = document.getElementById("memories-table");
    if (!container) {
        return;
    }

    // Keyed by id so an edit click can pull the full entry (the table itself
    // only shows a truncated preview) without a second round trip.
    let entriesByID = new Map();

    async function load() {
        const params = new URLSearchParams(window.location.search);
        const query = new URLSearchParams();
        ["q", "kind", "tags"].forEach((key) => {
            const value = params.get(key);
            if (value) {
                query.set(key, value);
            }
        });

        container.innerHTML = '<p class="muted">Loading...</p>';

        try {
            const entries = await apiJSON(`/api/memories/search?${query.toString()}`);
            entriesByID = new Map((entries || []).map((entry) => [entry.id, entry]));
            renderMemoriesTable(container, entries);
        } catch (error) {
            showMessage(container, error.message, "error");
        }
    }

    async function deleteMemory(deleteBtn) {
        const memoryID = deleteBtn.dataset.memoryId;
        if (!memoryID || !window.confirm("Delete this memory? This cannot be undone.")) {
            return;
        }

        deleteBtn.disabled = true;
        try {
            await apiJSON(`/api/memories/${memoryID}`, { method: "DELETE" });
            await load();
        } catch (error) {
            deleteBtn.disabled = false;
            showMessage(container, error.message, "error");
        }
    }

    function startEdit(row, entry) {
        row.innerHTML = memoryEditRowHTML(entry);
    }

    function cancelEdit(row, entry) {
        row.innerHTML = memoryRowCellsHTML(entry);
    }

    async function saveEdit(row, entry) {
        const kind = row.querySelector(".edit-kind").value;
        const title = row.querySelector(".edit-title").value.trim();
        const body = row.querySelector(".edit-body").value.trim();
        const tags = row.querySelector(".edit-tags").value
            .split(",")
            .map((tag) => tag.trim())
            .filter(Boolean);

        if (!body) {
            window.alert("Body is required.");
            return;
        }

        const saveBtn = row.querySelector(".edit-save");
        saveBtn.disabled = true;
        try {
            await apiJSON(`/api/memories/${entry.id}`, {
                method: "PUT",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ kind, title, body, tags }),
            });
            await load();
        } catch (error) {
            saveBtn.disabled = false;
            window.alert(error.message);
        }
    }

    container.addEventListener("click", (e) => {
        const editBtn = e.target.closest(".row-edit");
        if (editBtn) {
            const row = editBtn.closest("tr");
            const entry = entriesByID.get(editBtn.dataset.memoryId);
            if (row && entry) {
                startEdit(row, entry);
            }
            return;
        }

        const cancelBtn = e.target.closest(".edit-cancel");
        if (cancelBtn) {
            const row = cancelBtn.closest("tr");
            const entry = row && entriesByID.get(row.dataset.memoryId);
            if (row && entry) {
                cancelEdit(row, entry);
            }
            return;
        }

        const saveBtn = e.target.closest(".edit-save");
        if (saveBtn) {
            const row = saveBtn.closest("tr");
            const entry = row && entriesByID.get(row.dataset.memoryId);
            if (row && entry) {
                saveEdit(row, entry);
            }
            return;
        }

        const deleteBtn = e.target.closest(".row-delete");
        if (deleteBtn) {
            deleteMemory(deleteBtn);
        }
    });

    load();
}

// Quick capture writes straight to today's daily note. It exists so the thought
// you had at 3pm lands somewhere before it is gone, without starting a
// conversation about it.
function setupCaptureForm() {
    const form = document.getElementById("capture-form");
    const input = document.getElementById("capture-input");
    const result = document.getElementById("capture-result");
    if (!form || !input || !result) {
        return;
    }

    let clearTimer = null;

    form.addEventListener("submit", async (event) => {
        event.preventDefault();

        const text = input.value.trim();
        if (!text) {
            return;
        }

        const button = form.querySelector("button");
        button.disabled = true;

        try {
            const saved = await apiJSON("/api/capture", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ text }),
            });
            input.value = "";
            result.innerHTML = `<span class="muted">Logged to ${escapeHTML(saved.path)}</span>`;
            clearTimeout(clearTimer);
            clearTimer = setTimeout(() => {
                result.innerHTML = "";
            }, 4000);
        } catch (error) {
            showMessage(result, error.message, "error");
        } finally {
            button.disabled = false;
        }
    });
}

document.addEventListener("DOMContentLoaded", () => {
    setupConsultForm();
    setupCaptureForm();
    setupIngestForm();
    setupMemoriesTable();
});
