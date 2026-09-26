// The situation strip: the time, what is open, and when the room next checks
// in. Other scripts fill their own chip; this owns only the clock.

function setSituationChip(id, text) {
    const chip = document.getElementById(id);
    if (!chip) {
        return;
    }
    chip.textContent = text || "";
    chip.hidden = !text;
}

function setupSituationClock() {
    const clock = document.getElementById("sit-clock");
    if (!clock) {
        return;
    }
    const tick = () => {
        const now = new Date();
        const day = now.toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short" });
        const time = now.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false });
        clock.textContent = `${day} · ${time}`.toUpperCase();
    };
    tick();
    setInterval(tick, 15000);
}

document.addEventListener("DOMContentLoaded", setupSituationClock);
