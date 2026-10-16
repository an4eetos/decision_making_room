// Energy: one tap a day, at the evening check-in or on the campaign page.
//
// It is reported, never inferred from what you write: a supply line guessed
// from your tone is one you would stop trusting. The tap is about a day, not
// a moment, so the evening check-in sends its own day and a tap after
// midnight still lands on the evening it was about.

const ENERGY_LEVELS = [
    { level: 1, label: "Spent" },
    { level: 2, label: "Low" },
    { level: 3, label: "Steady" },
    { level: 4, label: "Good" },
    { level: 5, label: "Sharp" },
];

function energyLabel(level) {
    return ENERGY_LEVELS.find((e) => e.level === level)?.label ?? "";
}

// energyTapHTML is the five buttons for a day, with the day's level lit.
function energyTapHTML(day, current, ask = "Energy today") {
    return `
    <div class="energy-tap" data-energy-day="${escapeHTML(day)}" role="group" aria-label="${escapeHTML(ask)}">
        <span class="energy-ask">${escapeHTML(ask)}</span>
        ${ENERGY_LEVELS.map((e) => `
            <button type="button" class="energy-level lvl-${e.level} ${e.level === current ? "on" : ""}"
                    data-energy="${e.level}" aria-pressed="${e.level === current}"
                    title="${e.label}${e.level === current ? " — tap again to take it back" : ""}">
                <span class="energy-n">${e.level}</span><span class="energy-word">${e.label}</span>
            </button>`).join("")}
    </div>`;
}

function markEnergy(container, level) {
    container.querySelectorAll(".energy-level").forEach((button) => {
        const on = Number(button.dataset.energy) === level;
        button.classList.toggle("on", on);
        button.setAttribute("aria-pressed", String(on));
    });
}

// tapEnergy reports the level for the container's day, or takes it back when
// it is already the day's level. It returns the day's level afterwards.
async function tapEnergy(container, level) {
    const day = container.dataset.energyDay;
    const current = Number(container.querySelector(".energy-level.on")?.dataset.energy || 0);
    container.classList.add("busy");
    try {
        if (current === level) {
            await apiJSON(`/api/campaign/energy/${encodeURIComponent(day)}`, { method: "DELETE" });
            markEnergy(container, 0);
            return 0;
        }
        await apiJSON("/api/campaign/energy", {
            method: "PUT",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ day, level }),
        });
        markEnergy(container, level);
        return level;
    } finally {
        container.classList.remove("busy");
    }
}
