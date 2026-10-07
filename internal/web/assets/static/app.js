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

// apiStream posts JSON and reads the response as server-sent events, calling
// onEvent(name, data) for each. EventSource cannot be used: it only does GET, and
// the question goes in the body.
//
// The timeout is for silence, not for the whole answer. A deep answer that is
// visibly arriving should never be cut off because it is long; one that has
// said nothing for that long has stalled.
async function apiStream(url, { body, idleTimeoutMs = 90000, onEvent }) {
    const controller = new AbortController();
    let timeout = null;
    const arm = () => {
        clearTimeout(timeout);
        timeout = setTimeout(() => controller.abort(), idleTimeoutMs);
    };

    arm();
    try {
        const response = await fetch(url, {
            method: "POST",
            headers: { "Content-Type": "application/json", Accept: "text/event-stream" },
            body: JSON.stringify(body),
            signal: controller.signal,
        });
        if (!response.ok) {
            const text = await response.text();
            throw new Error(text || response.statusText);
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";

        for (;;) {
            const { value, done } = await reader.read();
            if (done) {
                break;
            }
            arm();
            buffer += decoder.decode(value, { stream: true });

            // Events end with a blank line; anything after the last one is a
            // partial event still on the wire.
            let end;
            while ((end = buffer.indexOf("\n\n")) !== -1) {
                const frame = buffer.slice(0, end);
                buffer = buffer.slice(end + 2);

                let name = "message";
                const data = [];
                for (const line of frame.split("\n")) {
                    if (line.startsWith("event:")) {
                        name = line.slice(6).trim();
                    } else if (line.startsWith("data:")) {
                        data.push(line.slice(5).trimStart());
                    }
                }
                if (data.length > 0) {
                    onEvent(name, JSON.parse(data.join("\n")));
                }
            }
        }
    } catch (error) {
        if (error && error.name === "AbortError") {
            throw new Error("The answer stalled. Try a shallower depth, or switch to another Gemini model.");
        }
        throw error;
    } finally {
        clearTimeout(timeout);
    }
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

    return `<div class="sources"><span class="sources-label">Sources <button type="button" class="hint" data-hint="sources"></button></span><ul>${items}</ul></div>`;
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

    // What you wrote during an interrogation sits in the same room as the
    // questions, so it takes the same background.
    const marked = messages.map((m, i) => (m.role === "user" && messages[i + 1]?.mode === INTERROGATION_MODE
        ? { ...m, interrogation: true }
        : m));
    container.innerHTML = marked.map(chatMessageHTML).join("");
    container.scrollTop = container.scrollHeight;
}

// chatMessageHTML renders one stored message. Split out so a streamed answer can
// be swapped for its finished form the moment it is saved.
function chatMessageHTML(message) {
    const isAssistant = message.role === "assistant";
    const interrogating = isAssistant ? message.mode === INTERROGATION_MODE : Boolean(message.interrogation);
    // Only assistant answers are markdown. What you typed is shown exactly as
    // you typed it — rendering your own text would mangle anything containing
    // an asterisk or a hash.
    const structured = !isAssistant
        ? null
        : interrogating
            ? renderInterrogation(message.content)
            : renderBriefing(message.content);
    const body = structured
        ? `<div class="chat-message-body structured">${structured}</div>`
        : isAssistant
            ? `<div class="chat-message-body markdown">${renderMarkdown(message.content)}</div>`
            : `<div class="chat-message-body">${escapeHTML(message.content)}</div>`;

    const tier = isAssistant && message.tier
        ? `<span class="tier-badge tier-${escapeHTML(message.tier)}">${escapeHTML(TIER_LABELS[message.tier] || message.tier)}</span>`
        : "";

    // Which lenses produced this answer, and whether you picked them. An
    // auto-selected lens should never look like one you chose.
    const mode = isAssistant && message.mode && message.mode !== "open" && !interrogating
        ? `<span class="mode-badge">${escapeHTML(modeChipName(message.mode))}</span>`
        : "";
    const room = interrogating
        ? `<span class="interrogation-badge">${isAssistant ? "Interrogation" : "Under questioning"}</span>`
        : "";

    const lenses = isAssistant && message.generals?.length
        ? `<span class="lens-badges">${message.generals.map((id) =>
                `<span class="lens-badge">${escapeHTML(lensName(id))}</span>`).join("")}` +
          `${message.generals_method === "auto" ? '<span class="lens-auto" title="Chosen for you">auto</span>' : ""}` +
          `${message.generals_method === "sticky" ? '<span class="lens-auto" title="Kept from earlier in this conversation">auto · kept</span>' : ""}</span>`
        : "";

    return `
    <div class="chat-message ${escapeHTML(message.role)}${interrogating ? " interrogation" : ""}">
        <div class="chat-message-head">
            <span class="chat-message-role">${escapeHTML(message.role)}</span>
            ${room}
            ${mode}
            ${tier}
            ${lenses}
        </div>
        ${body}
        ${isAssistant ? renderChatSources(message.sources) : ""}
        ${isAssistant ? suggestionHTML(message) : ""}
        ${isAssistant && message.id ? `<button type="button" class="track-btn" data-track-message="${escapeHTML(message.id)}">Track something from this</button>` : ""}
    </div>`;
}

// busyIDs are conversations still waiting on an answer, marked so a send left
// running in the background can be found again.
function renderChatSessions(container, sessions, activeSessionID, busyIDs = new Set()) {
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
                <span class="chat-session-meta muted">${busyIDs.has(session.id)
                    ? '<span class="chat-session-busy">Thinking<span class="waiting-dots"></span></span>'
                    : escapeHTML(formatDate(session.updated_at))}</span>
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

function elementFromHTML(html) {
    const shell = document.createElement("div");
    shell.innerHTML = html.trim();
    return shell.firstElementChild;
}

// optimisticMessageElement builds a message that is not stored yet. It is an
// element rather than markup so a send can keep writing into it while another
// conversation is on screen, and be put back when you return to its own.
function optimisticMessageElement(role, content) {
    return elementFromHTML(`
        <div class="chat-message ${escapeHTML(role)} pending">
            <div class="chat-message-head">
                <span class="chat-message-role">${escapeHTML(role)}</span>
            </div>
            <div class="chat-message-body">${escapeHTML(content)}</div>
        </div>
    `);
}

function appendToConversation(container, ...elements) {
    const placeholder = container.querySelector(".muted");
    if (placeholder && placeholder.textContent === "Start a conversation.") {
        container.innerHTML = "";
    }
    container.append(...elements);
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
    const intel = setupIntelRail();

    const modeChip = setupModeChip({ onChange: () => syncInterrogation() });
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
        setInterrogationRoster(generals.all());
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

    // The interrogation bar sits on the composer while the conversation is
    // pinned to an interrogation: who is asking, Take a position, Leave.
    const interrogationBar = document.getElementById("interrogation-bar");
    const interrogationWho = document.getElementById("interrogation-who");
    const takePositionButton = document.getElementById("take-position");
    const leaveButton = document.getElementById("leave-interrogation");
    // Set by Take a position for the one send it triggers.
    let concludeNext = false;

    function interrogating() {
        return modeChip?.pinned() === INTERROGATION_MODE;
    }

    // syncInterrogation puts the room in or out of interrogation. Given the
    // stored messages, it also names who is asking and lights up Take a
    // position once the last answer says nothing left would change it.
    function syncInterrogation(stored) {
        const on = interrogating();
        messages.classList.toggle("interrogating", on);
        form.classList.toggle("interrogating", on);
        if (interrogationBar) {
            interrogationBar.hidden = !on;
        }
        if (!on || stored === undefined) {
            return;
        }

        const last = [...stored].reverse().find((m) => m.role === "assistant" && m.mode === INTERROGATION_MODE);
        takePositionButton?.classList.toggle("ready", Boolean(last && isReadyToConclude(last.content)));
        const who = last?.generals?.length ? last.generals : (generals?.selected() ?? []);
        if (interrogationWho) {
            interrogationWho.textContent = who.length
                ? `${who.map((id) => lensName(id)).join(", ")} asking`
                : "The council is asking";
        }
    }

    async function setSessionMode(sessionID, mode) {
        await apiJSON(`/api/chat/sessions/${sessionID}/mode`, {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ mode }),
        });
    }

    // Leaving unlocks the conversation on the server at once, so reopening it
    // does not put you back in the room.
    async function leaveInterrogation(sessionID = activeSessionID) {
        modeChip?.setPinned(null);
        modeChip?.setDetected(null);
        syncInterrogation();
        if (!sessionID) {
            return;
        }
        try {
            await setSessionMode(sessionID, "");
        } catch (error) {
            showMessage(result, error.message, "error");
        }
    }

    function sendNow(fallbackText) {
        if (currentRun()) {
            return;
        }
        if (!form.question.value.trim()) {
            form.question.value = fallbackText;
        }
        form.requestSubmit();
    }

    function enterInterrogation() {
        if (currentRun()) {
            return;
        }
        modeChip?.setPinned(INTERROGATION_MODE);
        syncInterrogation();
        sendNow("Interrogate me on this.");
    }

    takePositionButton?.addEventListener("click", () => {
        if (currentRun()) {
            return;
        }
        concludeNext = true;
        sendNow("Take a position.");
    });

    leaveButton?.addEventListener("click", () => {
        leaveInterrogation();
    });

    let activeSessionID = null;
    let sessionLoadToken = 0;
    let cachedSessions = [];
    let sessionsRefresh = null;
    let sessionsRefreshAgain = false;

    // Sends still waiting on an answer, by session. Switching away does not
    // stop one: the request stays open, so the server finishes and saves the
    // answer, and its elements are put back if you return before it lands.
    const runs = new Map();
    // A send from a new chat has no session until the server creates one;
    // draftRun is that send while its new chat is still the one on screen.
    let draftRun = null;
    // Errors from sends that failed while you were looking at another
    // conversation, shown when you open the one they belong to.
    const failures = new Map();

    function currentRun() {
        return activeSessionID ? (runs.get(activeSessionID) ?? null) : draftRun;
    }

    function isShown(run) {
        return run.sessionID ? run.sessionID === activeSessionID : run === draftRun;
    }

    // One question at a time per conversation, not per page.
    function syncSendButton() {
        const button = form.querySelector("button[type=submit]");
        if (button) {
            button.disabled = currentRun() !== null;
        }
    }

    function renderSessionList() {
        renderChatSessions(sessions, cachedSessions, activeSessionID, new Set(runs.keys()));
    }

    // A refresh asked for while one is running runs once more after it, rather
    // than being dropped: with sends finishing in the background, a skipped
    // refresh would leave a conversation marked as thinking.
    function refreshSessions() {
        if (sessionsRefresh) {
            sessionsRefreshAgain = true;
            return sessionsRefresh;
        }
        sessionsRefresh = (async () => {
            try {
                do {
                    sessionsRefreshAgain = false;
                    if (cachedSessions.length === 0) {
                        sessions.innerHTML = '<p class="muted">Loading chats...</p>';
                    }
                    cachedSessions = await apiJSON("/api/chat/sessions");
                    renderSessionList();
                } while (sessionsRefreshAgain);
                return cachedSessions;
            } finally {
                sessionsRefresh = null;
            }
        })();
        return sessionsRefresh;
    }

    // showRun puts a send's own elements back under the stored conversation.
    // The question is saved before the answer starts and the answer the moment
    // it is written, so either may already be in what was just loaded.
    function showRun(run, stored) {
        if (run.answerID && stored.some((m) => m.id === run.answerID)) {
            return;
        }
        const last = stored[stored.length - 1];
        const questionStored = run.answerID
            || (last?.role === "user" && last.content === run.question);
        if (questionStored) {
            appendToConversation(messages, run.assistantEl);
        } else {
            appendToConversation(messages, run.userEl, run.assistantEl);
        }
        messages.scrollTop = messages.scrollHeight;
    }

    async function loadSession(sessionID) {
        if (!sessionID) {
            renderChatMessages(messages, []);
            result.innerHTML = "";
            return;
        }

        const token = ++sessionLoadToken;
        // Switch over now, not once loaded, so a send still running for the
        // previous conversation stops drawing into this one straight away.
        activeSessionID = sessionID;
        draftRun = null;
        persistActiveSession(sessionID);
        syncSendButton();
        renderChatMessages(messages, null);

        try {
            const data = await apiJSON(`/api/chat/sessions/${sessionID}`);
            if (token !== sessionLoadToken) {
                return;
            }
            renderChatMessages(messages, data.messages);
            const run = runs.get(sessionID);
            if (run) {
                showRun(run, data.messages ?? []);
            }
            // A session remembers the depth and the lenses it was last used
            // with, so reopening a conversation does not silently reset either.
            applyTier(data.session?.tier);
            generals?.setMax(LENSES_PER_TIER[selectedTier()] ?? 2);
            generals?.set(data.session?.generals);
            modeChip?.setPinned(data.session?.mode_locked ? data.session?.mode : null);
            modeChip?.setDetected(data.session?.mode, "sticky");
            syncInterrogation(data.messages ?? []);
            result.innerHTML = "";
            const failure = failures.get(sessionID);
            if (failure) {
                failures.delete(sessionID);
                showMessage(result, failure, "error");
            }
        } catch (error) {
            if (token !== sessionLoadToken) {
                return;
            }
            showMessage(result, error.message, "error");
        }
    }

    // createSession only creates: whether the new conversation goes on screen
    // is up to the send, since you may have moved on while it was created.
    async function createSession() {
        const session = await apiJSON("/api/chat/sessions", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ title: "New chat" }),
        });
        return session.id;
    }

    function startNewChat() {
        generals?.set([]);
        modeChip?.setPinned(null);
        modeChip?.setDetected(null);
        syncInterrogation([]);
        activeSessionID = null;
        // A send from the previous new chat carries on in the background.
        draftRun = null;
        sessionLoadToken++;
        form.question.value = "";
        result.innerHTML = "";
        renderChatMessages(messages, []);
        persistActiveSession(null);
        syncSendButton();
        refreshSessions();
    }

    async function deleteSession(sessionID) {
        if (!sessionID) {
            return;
        }
        if (runs.has(sessionID)) {
            showMessage(result, "This chat is still answering. Delete it once the answer is in.", "error");
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
                syncSendButton();
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
        newChatButton.addEventListener("click", startNewChat);
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

    // The banner under an answer: go into the interrogation, or wave it away.
    messages.addEventListener("click", (e) => {
        if (e.target.closest("[data-interrogate]")) {
            enterInterrogation();
            return;
        }
        const dismiss = e.target.closest("[data-dismiss-suggestion]");
        if (dismiss) {
            const banner = dismiss.closest(".interrogate-suggest");
            dismissSuggestion(banner?.dataset.suggestionFor);
            banner?.remove();
        }
    });

    // A check-in's Reply opens its conversation here.
    document.addEventListener("open-session", async (e) => {
        const id = e.detail?.id;
        if (!id) {
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
        await loadSession(id);
        await refreshSessions();
    });

    window.addEventListener("popstate", () => {
        const sessionID = getSessionFromURL();
        if (sessionID && cachedSessions.some((s) => s.id === sessionID)) {
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
        const question = form.question.value.trim();
        if (!question || currentRun()) {
            return;
        }
        form.question.value = "";

        const tier = selectedTier();
        const conclude = concludeNext;
        concludeNext = false;
        // Fixed for this send: switching mode mid-answer does not restyle it.
        const underQuestioning = interrogating();

        // Everything this send touches hangs off run, never off "whatever is on
        // screen": you can open another conversation, or start a new one, while
        // it is still thinking.
        const run = {
            sessionID: activeSessionID,
            question,
            answerID: null,
            userEl: optimisticMessageElement("user", question),
            assistantEl: optimisticMessageElement("assistant", ""),
        };
        if (underQuestioning) {
            run.userEl.classList.add("interrogation");
            run.assistantEl.classList.add("interrogation");
        }
        if (run.sessionID) {
            runs.set(run.sessionID, run);
        } else {
            draftRun = run;
        }
        syncSendButton();
        renderSessionList();
        appendToConversation(messages, run.userEl, run.assistantEl);
        messages.scrollTop = messages.scrollHeight;

        // Narrate the wait using the lenses actually pinned. Auto-selected ones
        // are named once the server says which it chose.
        const pendingBody = run.assistantEl.querySelector(".chat-message-body");
        const waiting = startWaiting(pendingBody, {
            tier,
            lensNames: (generals?.selected() ?? []).map((id) => lensName(id)),
        });

        // Streamed text is re-rendered as markdown at most once a frame: parsing
        // the whole answer per token would be quadratic on a long one.
        let streamed = "";
        let frame = 0;
        let writing = false;
        // finished is the "done" payload; answered is whether the answer was
        // saved, which decides what an error after that point should undo.
        let finished = null;
        let answered = false;

        // Only when this send's answer is the one on screen. Scrolling for a
        // send running in the background would yank the conversation you
        // switched to.
        function followsAnswer() {
            return run.assistantEl.isConnected
                && messages.scrollHeight - messages.scrollTop - messages.clientHeight < 80;
        }

        function paint() {
            frame = 0;
            // Follow the answer down only if you were already at the bottom;
            // scrolling up to reread something should not be yanked back.
            const follow = followsAnswer();
            const text = stripInterrogateFlag(streamed);
            pendingBody.innerHTML = (underQuestioning && renderInterrogation(text)) || renderMarkdown(text);
            if (follow) {
                messages.scrollTop = messages.scrollHeight;
            }
        }

        function onEvent(name, data) {
            switch (name) {
            case "stage":
                if (!writing) {
                    waiting.stage(data.stage, data.detail);
                }
                break;
            case "plan": {
                if (data.generals?.length) {
                    waiting.setLenses(data.generals.map((id) => lensName(id)));
                }
                // Badges go up before the answer, so you know who is talking.
                const head = run.assistantEl.querySelector(".chat-message-head");
                const planned = elementFromHTML(chatMessageHTML({ ...data, role: "assistant", content: "" }));
                head.innerHTML = planned.querySelector(".chat-message-head")?.innerHTML ?? head.innerHTML;
                if (data.mode && isShown(run)) {
                    modeChip?.setDetected(data.mode, data.mode_method);
                }
                break;
            }
            case "delta":
                if (!writing) {
                    writing = true;
                    waiting.stop();
                    pendingBody.classList.add("markdown");
                }
                streamed += data.text;
                frame ||= requestAnimationFrame(paint);
                break;
            case "reset":
                // The model wrote a little, then went to look something up
                // instead. That text was not the answer.
                streamed = "";
                writing = false;
                cancelAnimationFrame(frame);
                frame = 0;
                pendingBody.classList.remove("markdown");
                waiting.stage("digging");
                break;
            case "answer": {
                // Saved. Show it finished — sources, badges, the track button —
                // while the server tidies up the conversation summary.
                cancelAnimationFrame(frame);
                frame = 0;
                answered = true;
                run.answerID = data.id;
                waiting.stop();
                const follow = followsAnswer();
                const answerEl = elementFromHTML(chatMessageHTML(data));
                // A no-op when this conversation is not on screen; showRun
                // puts the finished answer back if you return before "done".
                run.assistantEl.replaceWith(answerEl);
                run.assistantEl = answerEl;
                run.userEl.classList.remove("pending");
                if (follow) {
                    messages.scrollTop = messages.scrollHeight;
                }
                break;
            }
            case "done":
                finished = data;
                break;
            case "error":
                throw new Error(data.message || "The answer failed.");
            }
        }

        try {
            if (!run.sessionID) {
                run.sessionID = await createSession();
                runs.set(run.sessionID, run);
                // Still looking at the new chat: it becomes this conversation.
                // Otherwise it carries on in the background and shows up in
                // the list.
                if (draftRun === run) {
                    draftRun = null;
                    activeSessionID = run.sessionID;
                    persistActiveSession(run.sessionID);
                }
                refreshSessions().catch(() => {});
            }

            await apiStream(`/api/chat/sessions/${run.sessionID}/messages/stream`, {
                body: {
                    content: question,
                    tier,
                    generals: generals?.selected() ?? [],
                    mode: modeChip ? (modeChip.pinned() ?? "") : undefined,
                    conclude: conclude || undefined,
                },
                idleTimeoutMs: TIER_TIMEOUTS[tier] ?? TIER_TIMEOUTS.standard,
                onEvent,
            });
            if (!finished) {
                throw new Error("The connection closed before the answer was saved.");
            }
            const data = finished;

            if (isShown(run)) {
                renderChatMessages(messages, data.messages);
                result.innerHTML = "";

                // Show what the room actually decided this question was.
                const answer = [...data.messages].reverse().find((m) => m.role === "assistant");
                if (answer?.mode) {
                    modeChip?.setDetected(answer.mode, answer.mode_method);
                }
            }

            // The position closes the interrogation.
            if (conclude && underQuestioning) {
                if (isShown(run)) {
                    await leaveInterrogation(run.sessionID);
                } else {
                    setSessionMode(run.sessionID, "").catch(() => {});
                }
            }
            if (isShown(run)) {
                syncInterrogation(data.messages);
            }

            // Anything you committed to is extracted in the background; pick
            // up the proposals when they land.
            loops?.refreshSoon();
            intel?.refreshSoon();
        } catch (error) {
            run.userEl.remove();
            run.assistantEl.remove();
            if (isShown(run)) {
                showMessage(result, error.message, "error");
                // The answer is stored even though something after it failed,
                // so show the conversation as the server has it.
                if (answered) {
                    loadSession(run.sessionID).then(() => showMessage(result, error.message, "error"));
                }
            } else if (run.sessionID) {
                failures.set(run.sessionID, error.message);
            }
        } finally {
            waiting.stop();
            cancelAnimationFrame(frame);
            run.userEl.remove();
            run.assistantEl.remove();
            if (run.sessionID && runs.get(run.sessionID) === run) {
                runs.delete(run.sessionID);
            }
            if (draftRun === run) {
                draftRun = null;
            }
            syncSendButton();
            refreshSessions().catch((error) => showMessage(result, error.message, "error"));
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
