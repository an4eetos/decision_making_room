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
            throw new Error("Request timed out. Try again or switch to another Gemini model.");
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
            <div class="answer-body">${escapeHTML(result.answer)}</div>
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

function renderChatMessages(container, messages) {
    if (messages === null) {
        container.innerHTML = '<p class="muted">Loading conversation...</p>';
        return;
    }

    if (!messages || messages.length === 0) {
        container.innerHTML = '<p class="muted">Start a conversation.</p>';
        return;
    }

    container.innerHTML = messages.map((message) => `
        <div class="chat-message ${message.role}">
            <div class="chat-message-role">${escapeHTML(message.role)}</div>
            <div class="chat-message-body">${escapeHTML(message.content)}</div>
            ${message.role === "assistant" ? renderChatSources(message.sources) : ""}
        </div>
    `).join("");
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

function renderMemoriesTable(container, entries) {
    if (!entries || entries.length === 0) {
        container.innerHTML = '<p class="muted">No memories yet. <a href="/ingest">Add your first entry</a>.</p>';
        return;
    }

    const rows = entries.map((entry) => `
        <tr>
            <td>${escapeHTML(formatDate(entry.created_at))}</td>
            <td><span class="badge">${escapeHTML(entry.kind)}</span></td>
            <td>${escapeHTML(entry.title || "")}</td>
            <td class="preview">${escapeHTML(truncate(entry.body, 120))}</td>
            <td>${escapeHTML((entry.tags || []).join(", "))}</td>
        </tr>
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
            <div class="chat-message-role">${escapeHTML(role)}</div>
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

        appendOptimisticMessage(messages, "user", question, "chat-pending-user");
        appendOptimisticMessage(messages, "assistant", "Thinking...", "chat-pending-assistant");

        try {
            const sessionID = await ensureSession();
            const data = await apiJSON(`/api/chat/sessions/${sessionID}/messages`, {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ content: question }),
                timeoutMs: 45000,
            });

            activeSessionID = data.session.id;
            persistActiveSession(data.session.id);
            renderChatMessages(messages, data.messages);
            result.innerHTML = "";

            await refreshSessions();
        } catch (error) {
            document.getElementById("chat-pending-user")?.remove();
            document.getElementById("chat-pending-assistant")?.remove();
            showMessage(result, error.message, "error");
        } finally {
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

async function loadMemoriesTable() {
    const container = document.getElementById("memories-table");
    if (!container) {
        return;
    }

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
        renderMemoriesTable(container, entries);
    } catch (error) {
        showMessage(container, error.message, "error");
    }
}

document.addEventListener("DOMContentLoaded", () => {
    setupConsultForm();
    setupIngestForm();
    loadMemoriesTable();
});
