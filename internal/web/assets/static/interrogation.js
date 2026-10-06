// Interrogation: the generals question you instead of advising, until your
// position no longer rests on a trap.
//
// An interrogation answer comes in fixed sections — On the record, Still dark,
// Caught, Questions — and the closing one in Position, Killed, Orders, Accepted
// dark. Like the briefing, the rendering is an enhancement: anything that does
// not parse falls back to ordinary markdown.

const INTERROGATION_MODE = "interrogation";

// The line the model writes under Still dark once nothing left would change
// the position. It lights up "Take a position".
const READY_LINE = /ready to take a position/i;

// id -> general, set once the roster loads. Suggestions are signed by id.
let interrogationRoster = {};

function setInterrogationRoster(generals) {
    interrogationRoster = {};
    for (const g of generals || []) {
        interrogationRoster[g.id] = g;
    }
}

function generalByName(name) {
    return briefingRoster[String(name).trim().toLowerCase()] ?? null;
}

const QUESTIONING = ["on the record", "still dark", "caught", "questions"];
const CLOSING = ["position", "killed", "orders", "accepted dark"];
const KNOWN = new Set([...QUESTIONING, ...CLOSING]);

// Section headings arrive as "**On the record**", sometimes as "## On the
// record" or with a colon; text after the heading on the same line belongs to
// the section. Only the known names count, so a bolded phrase in the middle of
// an answer is never mistaken for a heading.
const HEADING = new RegExp(
    `^\\s*(#{1,4}\\s*)?(\\*\\*)?(${[...KNOWN].join("|")})\\s*:?\\s*\\2\\s*:?\\s*(.*)$`, "i");

function splitInterrogation(markdown) {
    const sections = {};
    let current = null;

    for (const line of String(markdown).split("\n")) {
        const m = line.match(HEADING);
        // A heading is bold or a markdown heading; bare words are prose.
        if (m && (m[1] || m[2])) {
            current = m[3].toLowerCase();
            const rest = m[4].replace(/^[—–-]\s*/, "");
            sections[current] = rest ? [rest] : [];
            continue;
        }
        if (current) {
            sections[current].push(line);
        }
    }
    for (const key of Object.keys(sections)) {
        sections[key] = sections[key].join("\n").trim();
    }
    return sections;
}

// "**Zhukov:** question" lines become one block per general, tinted in the
// general's family colour — the same way stances are in a briefing. Lines that
// carry no name stay with the block above them.
function attributedBlocks(body, cls) {
    const blocks = [];
    for (const raw of body.split("\n")) {
        const line = raw.replace(/^\s*(?:[-*]|\d+\.)\s+/, "");
        const m = line.match(/^\*\*(.+?):\*\*\s*(.*)$/) || line.match(/^\*\*(.+?)\*\*:\s*(.*)$/);
        if (m) {
            blocks.push({ name: m[1].trim(), text: m[2] });
        } else if (line.trim() && blocks.length > 0) {
            blocks[blocks.length - 1].text += "\n" + line;
        } else if (line.trim()) {
            blocks.push({ name: "", text: line });
        }
    }

    return blocks.map((b) => {
        const g = b.name ? generalByName(b.name) : null;
        const color = g ? familyStyle(g.family).color : "var(--wr-int)";
        const face = g ? portrait(g, 28, "round") : "";
        return `
        <div class="${cls}" style="--family:${color}">
            ${face}
            <div class="${cls}-body">
                ${b.name ? `<span class="${cls}-name">${escapeHTML(b.name)}</span>` : ""}
                <div class="markdown">${renderMarkdown(b.text)}</div>
            </div>
        </div>`;
    }).join("");
}

function interrogationBlock(cls, key, body) {
    return `<div class="int-${cls}"><span class="int-key">${escapeHTML(key)}</span><div class="markdown">${renderMarkdown(body)}</div></div>`;
}

// renderInterrogation returns null when the answer is not in either format, so
// the caller can fall back to markdown.
function renderInterrogation(markdown) {
    const s = splitInterrogation(markdown);
    const closing = CLOSING.filter((k) => s[k] !== undefined).length;
    const questioning = QUESTIONING.filter((k) => s[k] !== undefined).length;

    if (closing >= 2) {
        return `
        <div class="interrogation-answer closing">
            ${s.position ? `<div class="int-position"><span class="int-key">Position</span><div class="markdown">${renderMarkdown(s.position)}</div></div>` : ""}
            ${s.killed ? interrogationBlock("killed", "Killed", s.killed) : ""}
            ${s.orders ? `<div class="int-orders"><span class="int-key">Orders</span>${attributedBlocks(s.orders, "order")}</div>` : ""}
            ${s["accepted dark"] ? interrogationBlock("dark", "Accepted dark", s["accepted dark"]) : ""}
        </div>`;
    }

    if (questioning < 2) {
        return null;
    }

    const dark = s["still dark"];
    const ready = dark !== undefined && READY_LINE.test(dark);

    return `
    <div class="interrogation-answer">
        ${s["on the record"] ? interrogationBlock("record", "On the record", s["on the record"]) : ""}
        ${dark !== undefined
            ? ready
                ? `<div class="int-dark ready"><span class="int-key">Still dark</span><p>Nothing left that would change the position. Ready to take a position.</p></div>`
                : interrogationBlock("dark", "Still dark", dark)
            : ""}
        ${s.caught ? interrogationBlock("caught", "Caught", s.caught) : ""}
        ${s.questions ? `<div class="int-questions"><span class="int-key">Questions</span>${attributedBlocks(s.questions, "interrogator")}</div>` : ""}
    </div>`;
}

// isReadyToConclude reports whether an interrogation answer says nothing left
// would change the position.
function isReadyToConclude(markdown) {
    const dark = splitInterrogation(markdown)["still dark"];
    return dark !== undefined && READY_LINE.test(dark);
}

// The model's flag line is cut out before the answer is stored, but it streams
// in like any other text. Hide it, and any half-arrived start of it, while the
// answer is still being written.
function stripInterrogateFlag(text) {
    return String(text)
        .replace(/^[ \t]*\[\[(?:interrogate:)?[a-z_, \t]*(?:\]\]?)?[ \t]*$/gim, "")
        .replace(/\n[ \t]*\[{1,2}(?:i(?:n(?:t(?:e(?:r(?:r(?:o(?:g(?:a(?:t(?:e)?)?)?)?)?)?)?)?)?)?)?$/, "")
        .trimEnd();
}

// Suggestions you waved away stay away, on this device. Storage can be blocked
// or empty; the banner then simply comes back on reload.
const DISMISSED_KEY = "dismissedInterrogations";

function dismissedSuggestions() {
    try {
        return new Set(JSON.parse(localStorage.getItem(DISMISSED_KEY) || "[]"));
    } catch {
        return new Set();
    }
}

function dismissSuggestion(messageID) {
    try {
        const ids = [...dismissedSuggestions(), messageID].slice(-200);
        localStorage.setItem(DISMISSED_KEY, JSON.stringify(ids));
    } catch {
        // Not remembered; nothing else depends on it.
    }
}

// suggestionHTML is the room recommending an interrogation, signed by the
// general whose job the trap is.
function suggestionHTML(message) {
    const s = message.suggestion;
    if (!s || !s.traps?.length || message.mode === INTERROGATION_MODE) {
        return "";
    }
    if (message.id && dismissedSuggestions().has(message.id)) {
        return "";
    }

    const g = interrogationRoster[s.by];
    const name = g ? g.name : "The room";
    const color = g ? familyStyle(g.family).color : "var(--wr-int)";
    const face = g ? portrait(g, 34, "tall") : "";

    // Several traps often come from one sentence; quote it once, under all of
    // their names, rather than repeat it per trap.
    const byQuote = new Map();
    for (const t of s.traps) {
        const key = t.quote || "";
        if (!byQuote.has(key)) {
            byQuote.set(key, []);
        }
        byQuote.get(key).push(t.name);
    }
    const traps = [...byQuote].map(([quote, names]) => `
        <li>
            <span class="trap-names">${names.map((n) => `<span class="trap-name">${escapeHTML(n)}</span>`).join("")}</span>
            ${quote
                ? `<q>${escapeHTML(quote)}</q>`
                : '<span class="trap-unsaid">read between the lines</span>'}
        </li>`).join("");

    return `
    <div class="interrogate-suggest" style="--family:${color}" data-suggestion-for="${escapeHTML(message.id || "")}">
        ${face}
        <div class="interrogate-suggest-body">
            <p class="interrogate-suggest-head"><strong>${escapeHTML(name)}</strong> recommends an interrogation
                <button type="button" class="hint" data-hint="interrogate-suggest"></button></p>
            <ul class="interrogate-traps">${traps}</ul>
            <div class="interrogate-suggest-actions">
                <button type="button" class="btn primary" data-interrogate>Get interrogated</button>
                <button type="button" class="btn-secondary" data-dismiss-suggestion>Not now</button>
            </div>
        </div>
    </div>`;
}
