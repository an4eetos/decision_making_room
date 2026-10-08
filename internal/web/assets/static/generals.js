// The general picker: a strip of emblems above the composer. Pick none and the
// room chooses; pick up to three and they argue.
//
// Emblems are generated rather than drawn. Historical portraits would be a
// licensing problem and a tone problem, and twenty-one consistent illustrations is a
// project of its own — a monogram in the family's colour carries the one thing
// that matters, which is telling them apart at a glance.

const FAMILY_STYLE = {
    contact:       { color: "#e5734b", label: "Contact" },
    scouting:      { color: "#7fa7d6", label: "Scouting" },
    endurance:     { color: "#8fb86a", label: "Endurance" },
    adaptation:    { color: "#e3a33b", label: "Adaptation" },
    systems:       { color: "#a99bd8", label: "Systems" },
    concentration: { color: "#d4708f", label: "Concentration" },
    preservation:  { color: "#5fb8a8", label: "Preservation" },
};

function familyStyle(family) {
    return FAMILY_STYLE[family] || { color: "#7d8b9c", label: family || "" };
}

// Initials from the display name: two letters for a two-word name, otherwise one.
function initials(name) {
    const words = String(name || "?").trim().split(/[\s-]+/).filter(Boolean);
    if (words.length === 0) {
        return "?";
    }
    if (words.length === 1) {
        return words[0].slice(0, 1).toUpperCase();
    }
    return (words[0][0] + words[1][0]).toUpperCase();
}

// portrait draws a general's picture when there is one, and the monogram when
// there is not. Every photo and painting goes through the same treatment —
// greyscale, then the family colour laid over it — so twenty-one sources from two
// centuries read as one set rather than a scrapbook.
function portrait(general, size = 34, shape = "round") {
    const { color } = familyStyle(general.family);
    const style = `--family:${color};width:${size}px;height:${shape === "tall" ? Math.round(size * 1.25) : size}px`;

    if (general.portrait) {
        return `<span class="portrait ${shape}" style="${style}">
            <img src="${escapeHTML(general.portrait)}" alt="" loading="lazy"
                 onerror="this.parentElement.classList.add('failed')">
            <span class="portrait-initials">${escapeHTML(initials(general.name))}</span>
        </span>`;
    }
    return `<span class="portrait ${shape} failed" style="${style}">
        <span class="portrait-initials">${escapeHTML(initials(general.name))}</span>
    </span>`;
}

// emblem is kept as the old name so existing callers keep working.
function emblem(general, size = 34) {
    return portrait(general, size, "round");
}

function setupGeneralsPicker({ onChange } = {}) {
    const strip = document.getElementById("generals-strip");
    const summary = document.getElementById("generals-summary");
    if (!strip) {
        return null;
    }

    let roster = [];
    let selected = [];
    let max = 3;

    const seats = document.getElementById("council-seats");

    function render() {
        strip.innerHTML = roster.map((g) => {
            const { label } = familyStyle(g.family);
            const isOn = selected.includes(g.id);
            const atLimit = !isOn && selected.length >= max;

            return `
            <button type="button"
                    class="general ${isOn ? "on" : ""}"
                    data-general-id="${escapeHTML(g.id)}"
                    ${atLimit ? "disabled" : ""}
                    aria-pressed="${isOn}"
                    title="${escapeHTML(g.name)}${g.epithet ? " — " + escapeHTML(g.epithet) : ""}\n${escapeHTML(label)}\n\n${escapeHTML(g.job)}${g.portrait_credit ? "\n\nPortrait: " + escapeHTML(g.portrait_credit) : ""}">
                ${portrait(g, 40)}
                <span class="general-name">${escapeHTML(g.name)}</span>
            </button>`;
        }).join("");

        renderSeats();

        if (summary) {
            summary.textContent = selected.length === 0
                ? `auto · up to ${max}`
                : `${selected.length} of ${max}`;
        }
    }

    // The council is the generals you seated, shown large. With none seated the
    // room picks per question, and the empty chairs say so rather than leaving
    // a blank panel.
    function renderSeats() {
        if (!seats) {
            return;
        }

        const chosen = selected.map((id) => roster.find((g) => g.id === id)).filter(Boolean);
        const empty = Math.max(0, max - chosen.length);

        seats.innerHTML = chosen.map((g) => {
            const { label } = familyStyle(g.family);
            return `
            <div class="seat" style="--family:${familyStyle(g.family).color}">
                ${portrait(g, 52, "tall")}
                <div class="seat-text">
                    <span class="seat-name">${escapeHTML(g.name)}</span>
                    <span class="seat-epithet">${escapeHTML(g.epithet || label)}</span>
                    <span class="seat-job">${escapeHTML(g.job)}</span>
                </div>
                <button type="button" class="seat-leave" data-general-id="${escapeHTML(g.id)}" aria-label="Unseat ${escapeHTML(g.name)}">×</button>
            </div>`;
        }).join("") + Array.from({ length: empty }, () => `
            <div class="seat empty">
                <span class="seat-chair" aria-hidden="true"></span>
                <span class="seat-empty-text">${chosen.length === 0 ? "The room picks for each question" : "Open seat"}</span>
            </div>`).join("");
    }

    seats?.addEventListener("click", (e) => {
        const leave = e.target.closest(".seat-leave");
        if (!leave) {
            return;
        }
        selected = selected.filter((x) => x !== leave.dataset.generalId);
        render();
        onChange?.(selected);
    });

    strip.addEventListener("click", (e) => {
        const button = e.target.closest("[data-general-id]");
        if (!button || button.disabled) {
            return;
        }

        const id = button.dataset.generalId;
        selected = selected.includes(id)
            ? selected.filter((x) => x !== id)
            : [...selected, id];

        render();
        onChange?.(selected);
    });

    return {
        async load() {
            try {
                roster = await apiJSON("/api/generals");
                render();
            } catch (error) {
                strip.innerHTML = `<span class="muted">Could not load the roster: ${escapeHTML(error.message)}</span>`;
            }
        },
        // Reflects the session's saved pick when a conversation is reopened.
        set(ids) {
            selected = Array.isArray(ids) ? ids.slice(0, max) : [];
            render();
        },
        selected() {
            return selected;
        },
        // The tier decides how many lenses an answer is written through, so the
        // picker's limit follows it.
        setMax(value) {
            max = Math.max(1, value || 1);
            if (selected.length > max) {
                selected = selected.slice(0, max);
            }
            render();
        },
        byID(id) {
            return roster.find((g) => g.id === id);
        },
        names() {
            return Object.fromEntries(roster.map((g) => [g.id, g.name]));
        },
        all() {
            return roster;
        },
    };
}
