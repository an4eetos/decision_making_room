// The mode chip. Shows what the room decided this question is, and lets you
// correct it.
//
// Detection is server-side, so the chip reports a decision already made rather
// than driving one. That is the point: a misdetection becomes one click instead
// of a silently oddly-shaped answer.

const MODE_FAMILY_LABEL = {
    plan: "Plan",
    decide: "Decide",
    unblock: "Unblock",
    review: "Review",
    open: "Open",
};

const MODE_METHOD_HINT = {
    explicit: "You chose this",
    sticky: "Carried from earlier in this conversation",
    keyword: "Picked from what you asked",
    default: "Nothing specific matched",
};

function setupModeChip({ onChange } = {}) {
    const chip = document.getElementById("mode-chip");
    const label = document.getElementById("mode-chip-label");
    const menu = document.getElementById("mode-menu");
    if (!chip || !label || !menu) {
        return null;
    }

    let modes = [];
    // null means auto: let detection decide each turn.
    let pinned = null;
    let detected = null;

    function render() {
        const active = pinned
            ? modes.find((m) => m.id === pinned)
            : modes.find((m) => m.id === detected?.id);

        label.textContent = active ? active.name : "Open";
        chip.classList.toggle("pinned", Boolean(pinned));
        chip.title = pinned
            ? `Mode pinned to ${active?.name ?? pinned}. Click to change or go back to automatic.`
            : detected
                ? `${MODE_METHOD_HINT[detected.method] ?? "Chosen automatically"}. Click to pin a different one.`
                : "The room picks a mode from what you ask. Click to pin one.";
    }

    function renderMenu() {
        const families = ["plan", "decide", "unblock", "review", "open"];
        const groups = families.map((family) => {
            const inFamily = modes.filter((m) => m.family === family);
            if (inFamily.length === 0) {
                return "";
            }
            return `
            <div class="mode-group">
                <div class="mode-group-head">${escapeHTML(MODE_FAMILY_LABEL[family] || family)}</div>
                ${inFamily.map((m) => `
                    <button type="button" class="mode-option ${pinned === m.id ? "on" : ""}"
                            role="option" aria-selected="${pinned === m.id}" data-mode-id="${escapeHTML(m.id)}">
                        <span class="mode-option-name">${escapeHTML(m.name)}</span>
                        ${m.summary ? `<span class="mode-option-summary">${escapeHTML(m.summary)}</span>` : ""}
                    </button>`).join("")}
            </div>`;
        }).join("");

        menu.innerHTML = `
            <button type="button" class="mode-option auto ${pinned ? "" : "on"}" role="option" data-mode-id="">
                <span class="mode-option-name">Automatic</span>
                <span class="mode-option-summary">Let the room pick from what you ask.</span>
            </button>
            ${groups}`;
    }

    function toggleMenu(open) {
        const show = open ?? menu.hidden;
        if (show) {
            renderMenu();
        }
        menu.hidden = !show;
        chip.setAttribute("aria-expanded", String(show));
    }

    chip.addEventListener("click", (e) => {
        e.stopPropagation();
        toggleMenu();
    });

    menu.addEventListener("click", (e) => {
        const option = e.target.closest("[data-mode-id]");
        if (!option) {
            return;
        }
        pinned = option.dataset.modeId || null;
        toggleMenu(false);
        render();
        onChange?.(pinned);
    });

    document.addEventListener("click", () => toggleMenu(false));
    document.addEventListener("keydown", (e) => {
        if (e.key === "Escape") {
            toggleMenu(false);
        }
    });

    return {
        async load() {
            try {
                modes = await apiJSON("/api/modes");
                render();
            } catch {
                // The chip is an affordance, not a requirement. If the list
                // cannot load, detection still works server-side.
                chip.hidden = true;
            }
        },
        // Reflects what the server actually used for the last answer.
        setDetected(id, method) {
            detected = id ? { id, method } : null;
            render();
        },
        setPinned(id) {
            pinned = id || null;
            render();
        },
        // null means automatic; the API distinguishes absent from empty.
        pinned() {
            return pinned;
        },
        name(id) {
            return modes.find((m) => m.id === id)?.name ?? id;
        },
    };
}
