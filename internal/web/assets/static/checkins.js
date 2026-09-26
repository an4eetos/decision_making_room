// Check-ins: the room asking how things are going.
//
// There is no push infrastructure. A check-in written while the page is closed
// is simply waiting when you next open it, and the nav badge polls once a
// minute while a tab is open. For a single user on localhost that is both
// cheaper and more reliable than holding a connection open.

const CHECKIN_POLL_MS = 60000;

function setupCheckins() {
    const cards = document.getElementById("checkin-cards");
    const badge = document.getElementById("nav-checkin-badge");
    const nowButton = document.getElementById("checkin-now");

    let latest = [];

    function renderBadge(count) {
        if (!badge) {
            return;
        }
        badge.textContent = count > 0 ? String(count) : "";
        badge.hidden = count === 0;
    }

    function renderCards() {
        if (!cards) {
            return;
        }
        if (latest.length === 0) {
            cards.innerHTML = "";
            return;
        }

        cards.innerHTML = latest.map((c) => `
            <article class="checkin-card ${c.kind === "nudge" ? "nudge" : ""}" data-checkin="${escapeHTML(c.id)}">
                <header class="checkin-head">
                    <span class="checkin-title">${escapeHTML(c.title)}</span>
                    <span class="checkin-time muted">${escapeHTML(timeAgo(c.created_at))}</span>
                </header>
                <div class="checkin-body markdown">${renderMarkdown(c.body)}</div>
                <div class="checkin-actions">
                    <button type="button" class="btn primary" data-checkin-open>Reply</button>
                    <button type="button" class="btn" data-checkin-dismiss>Not now</button>
                </div>
            </article>`).join("");
    }

    async function poll() {
        try {
            const data = await apiJSON("/api/checkins/unseen");
            latest = data.items || [];
            renderBadge(data.count || 0);
            if (typeof setSituationChip === "function") {
                setSituationChip("sit-next", data.next_at
                    ? `Next check-in ${new Date(data.next_at).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false })}`
                    : "");
            }
            renderCards();
        } catch {
            // A failed poll is not worth interrupting anyone over; try again next
            // minute.
        }
    }

    cards?.addEventListener("click", async (e) => {
        const card = e.target.closest("[data-checkin]");
        if (!card) {
            return;
        }
        const id = card.dataset.checkin;

        try {
            if (e.target.closest("[data-checkin-open]")) {
                const { session_id: sessionID } = await apiJSON(`/api/checkins/${id}/open`, { method: "POST" });
                // The chat page owns session loading; hand it over rather than
                // reaching into it.
                document.dispatchEvent(new CustomEvent("open-session", { detail: { id: sessionID } }));
            } else if (e.target.closest("[data-checkin-dismiss]")) {
                await apiJSON(`/api/checkins/${id}/dismiss`, { method: "POST" });
            } else {
                return;
            }
            await poll();
        } catch (error) {
            showMessage(document.getElementById("consult-result"), error.message, "error");
        }
    });

    nowButton?.addEventListener("click", async () => {
        nowButton.disabled = true;
        const label = nowButton.textContent;
        nowButton.textContent = "Checking in…";
        try {
            await apiJSON("/api/checkins/now", { method: "POST", timeoutMs: 60000 });
            await poll();
        } catch (error) {
            showMessage(document.getElementById("consult-result"), error.message, "error");
        } finally {
            nowButton.disabled = false;
            nowButton.textContent = label;
        }
    });

    poll();
    setInterval(() => {
        // Nothing to learn from polling a hidden tab; catch up when it is shown.
        if (!document.hidden) {
            poll();
        }
    }, CHECKIN_POLL_MS);
    document.addEventListener("visibilitychange", () => {
        if (!document.hidden) {
            poll();
        }
    });
}

function timeAgo(iso) {
    const minutes = Math.round((Date.now() - new Date(iso).getTime()) / 60000);
    if (minutes < 1) return "just now";
    if (minutes < 60) return `${minutes} min ago`;
    const hours = Math.round(minutes / 60);
    if (hours < 24) return `${hours} h ago`;
    return `${Math.round(hours / 24)} d ago`;
}

document.addEventListener("DOMContentLoaded", setupCheckins);
