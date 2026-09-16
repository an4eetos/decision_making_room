// Text shown while an answer is being produced.
//
// The sequence mirrors what the server actually does — embed the question,
// search, read what came back, argue between lenses, write — so the wait is
// informative rather than decorative. The timings are estimates, not progress:
// nothing streams back, so this cannot claim to know which stage is running.
// Lines are phrased accordingly, and none of them assert completion.

const WAITING_LINES = {
    searching: [
        "Searching your notes",
        "Looking through what you've written",
        "Pulling the threads that match",
        "Finding what you said about this before",
    ],
    reading: [
        "Reading what came back",
        "Connecting the dots",
        "Working out which of these actually matters",
        "Sorting signal from the rest",
        "Lining up what you decided against what you asked",
    ],
    thinking: [
        "Thinking it through",
        "Turning it over",
        "Weighing what you're giving up",
        "Looking for the part you haven't said",
    ],
    digging: [
        "Going back for more",
        "That wasn't enough — searching again",
        "Chasing something it half-remembered",
    ],
    writing: [
        "Writing it up",
        "Putting it in order",
        "Getting to the point",
    ],
};

// Stage plans per depth. Quick barely has time for two; deep earns the full arc.
const WAITING_PLANS = {
    quick: [
        { key: "searching", at: 0 },
        { key: "thinking", at: 2500 },
        { key: "writing", at: 6000 },
    ],
    standard: [
        { key: "searching", at: 0 },
        { key: "reading", at: 2500 },
        { key: "thinking", at: 6000 },
        { key: "writing", at: 11000 },
    ],
    deep: [
        { key: "searching", at: 0 },
        { key: "reading", at: 3000 },
        { key: "thinking", at: 8000 },
        { key: "digging", at: 15000 },
        { key: "thinking", at: 22000 },
        { key: "writing", at: 32000 },
    ],
};

// After this long, show elapsed seconds too. Deep answers legitimately take
// half a minute, and a number is more reassuring than another cheerful line.
const SHOW_ELAPSED_AFTER = 12000;

function pick(list, exclude) {
    const options = list.length > 1 ? list.filter((l) => l !== exclude) : list;
    return options[Math.floor(Math.random() * options.length)];
}

// lensLine describes the argument about to happen, using the lenses actually
// picked. Returns null when they were auto-selected, because the client does not
// know which ones the server chose until the answer arrives — and naming the
// wrong ones would be worse than saying nothing.
function lensLine(lensNames) {
    if (!lensNames || lensNames.length === 0) {
        return null;
    }
    if (lensNames.length === 1) {
        return `Thinking through ${lensNames[0]}`;
    }
    if (lensNames.length === 2) {
        return `${lensNames[0]} and ${lensNames[1]} are disagreeing`;
    }
    const last = lensNames[lensNames.length - 1];
    return `${lensNames.slice(0, -1).join(", ")} and ${last} are arguing`;
}

// startWaiting swaps text into an element on a schedule and returns a stop
// function. The caller owns the element.
function startWaiting(element, { tier = "standard", lensNames = [] } = {}) {
    const plan = WAITING_PLANS[tier] ?? WAITING_PLANS.standard;
    const started = Date.now();
    const lens = lensLine(lensNames);

    let previous = null;
    let index = -1;
    let timer = null;

    function textFor(stage) {
        // Substitute the named-lens line for one generic "thinking" step, so the
        // personalised version appears without crowding out the rest.
        if (lens && stage.key === "thinking" && !textFor.usedLens) {
            textFor.usedLens = true;
            return lens;
        }
        return pick(WAITING_LINES[stage.key], previous);
    }

    function render(text) {
        const elapsed = Date.now() - started;
        const suffix = elapsed > SHOW_ELAPSED_AFTER
            ? ` <span class="waiting-elapsed">${Math.round(elapsed / 1000)}s</span>`
            : "";
        element.innerHTML = `<span class="waiting-text">${escapeHTML(text)}<span class="waiting-dots"></span></span>${suffix}`;
        previous = text;
    }

    function advance() {
        index += 1;
        const stage = plan[Math.min(index, plan.length - 1)];
        render(textFor(stage));

        // Past the end of the plan, keep cycling the last stage rather than
        // freezing — an answer that overruns the estimate is still coming.
        const next = plan[index + 1];
        const delay = next ? next.at - stage.at : 7000;
        timer = setTimeout(advance, Math.max(delay, 1500));
    }

    advance();

    // Refresh the elapsed counter between stage changes.
    const ticker = setInterval(() => {
        if (previous && Date.now() - started > SHOW_ELAPSED_AFTER) {
            render(previous);
        }
    }, 1000);

    return function stop() {
        clearTimeout(timer);
        clearInterval(ticker);
    };
}
