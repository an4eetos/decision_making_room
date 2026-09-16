// The general picker: a strip of emblems above the composer. Pick none and the
// room chooses; pick up to three and they argue.
//
// Emblems are generated rather than drawn. Historical portraits would be a
// licensing problem and a tone problem, and twenty consistent illustrations is a
// project of its own — a monogram in the family's colour carries the one thing
// that matters, which is telling them apart at a glance.

const FAMILY_STYLE = {
    contact:       { color: "#d9704f", label: "Contact" },
    scouting:      { color: "#5b9bd5", label: "Scouting" },
    endurance:     { color: "#7aa35c", label: "Endurance" },
    adaptation:    { color: "#c8a24a", label: "Adaptation" },
    systems:       { color: "#8d84c4", label: "Systems" },
    concentration: { color: "#c25b7a", label: "Concentration" },
    preservation:  { color: "#4fb0a5", label: "Preservation" },
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

function emblem(general, size = 34) {
    const { color } = familyStyle(general.family);
    const text = initials(general.name);
    const fontSize = text.length > 1 ? size * 0.36 : size * 0.44;

    return `
    <svg viewBox="0 0 ${size} ${size}" width="${size}" height="${size}" aria-hidden="true" focusable="false">
        <circle cx="${size / 2}" cy="${size / 2}" r="${size / 2 - 1}"
                fill="${color}22" stroke="${color}" stroke-width="1.5"/>
        <text x="50%" y="50%" dy="0.36em" text-anchor="middle"
              fill="${color}" font-size="${fontSize}" font-weight="700"
              font-family="-apple-system, BlinkMacSystemFont, sans-serif">${escapeHTML(text)}</text>
    </svg>`;
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
                    title="${escapeHTML(g.name)}${g.epithet ? " — " + escapeHTML(g.epithet) : ""}\n${escapeHTML(label)}\n\n${escapeHTML(g.job)}">
                ${emblem(g)}
                <span class="general-name">${escapeHTML(g.name)}</span>
            </button>`;
        }).join("");

        if (summary) {
            summary.textContent = selected.length === 0
                ? `No lens picked — the room chooses, up to ${max}.`
                : `${selected.length} of ${max} picked.`;
        }
    }

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
    };
}
