// Renders a multi-general answer as a briefing: each general's position as a
// card, the fork between them in red, and the call below.
//
// It only works on the exchange format the server asks for — "## Name —
// position", then "## The fork" and "## The call". Anything else, including an
// answer where the model drifted from the format, falls back to ordinary
// markdown. The briefing is an enhancement; it must never render an answer
// worse than plain text would.

// Set by the chat page once the roster loads: lower-cased name -> general.
let briefingRoster = {};

function setBriefingRoster(generals) {
    briefingRoster = {};
    for (const g of generals || []) {
        briefingRoster[g.name.toLowerCase()] = g;
    }
}

function splitSections(markdown) {
    const sections = [];
    let current = null;

    for (const line of String(markdown).split("\n")) {
        const heading = line.match(/^##\s+(.+?)\s*$/);
        if (heading) {
            current = { heading: heading[1], lines: [] };
            sections.push(current);
        } else if (current) {
            current.lines.push(line);
        }
    }
    return sections.map((s) => ({ heading: s.heading, body: s.lines.join("\n").trim() }));
}

// Pulls the "**Answers X:**" and "**Concedes:**" lines out of a position so they
// can be shown as the exchange they are, rather than buried in the paragraph.
function splitStance(body) {
    const rest = [];
    let answers = null;
    let concedes = null;

    for (const line of body.split("\n")) {
        const a = line.match(/^\*\*Answers\s+(.+?):\*\*\s*(.*)$/i);
        const c = line.match(/^\*\*Concedes:\*\*\s*(.*)$/i);
        if (a) {
            answers = { to: a[1], text: a[2] };
        } else if (c) {
            concedes = c[1];
        } else {
            rest.push(line);
        }
    }
    return { body: rest.join("\n").trim(), answers, concedes };
}

function renderBriefing(markdown) {
    const sections = splitSections(markdown);
    const stances = [];
    let fork = null;
    let call = null;

    for (const s of sections) {
        const title = s.heading.toLowerCase();
        if (title === "the fork" || title === "where they disagree") {
            fork = s.body;
            continue;
        }
        if (title === "the call") {
            call = s.body;
            continue;
        }
        const named = s.heading.match(/^(.+?)\s+[—–-]\s+(.+)$/);
        if (named) {
            stances.push({ name: named[1].trim(), position: named[2].trim(), ...splitStance(s.body) });
        }
    }

    // Not the exchange format: let ordinary markdown handle it.
    if (stances.length < 2 || !call) {
        return null;
    }

    const cards = stances.map((st) => {
        const g = briefingRoster[st.name.toLowerCase()];
        const color = g ? familyStyle(g.family).color : "var(--wr-muted)";
        const face = g ? portrait(g, 34, "tall") : "";

        return `
        <article class="stance" style="--family:${color}">
            <header class="stance-head">
                ${face}
                <div>
                    <span class="stance-name">${escapeHTML(st.name)}</span>
                    <span class="stance-position">${escapeHTML(st.position)}</span>
                </div>
            </header>
            <div class="stance-body markdown">${renderMarkdown(st.body)}</div>
            ${st.answers ? `<div class="stance-row answers"><span class="stance-key">Answers ${escapeHTML(st.answers.to)}</span>${renderMarkdown(st.answers.text)}</div>` : ""}
            ${st.concedes ? `<div class="stance-row concedes"><span class="stance-key">Concedes</span>${renderMarkdown(st.concedes)}</div>` : ""}
        </article>`;
    });

    // Two positions face each other across the fork; three sit side by side.
    const grid = stances.length === 2
        ? `<div class="stances duel">${cards[0]}<div class="duel-mark" aria-hidden="true">⇄</div>${cards[1]}</div>`
        : `<div class="stances" style="--cols:${Math.min(stances.length, 3)}">${cards.join("")}</div>`;

    return `
    <div class="briefing">
        ${grid}
        ${fork ? `<div class="fork"><span class="fork-key">The fork</span>${renderMarkdown(fork)}</div>` : ""}
        <div class="call"><span class="call-key">The call</span><div class="markdown">${renderMarkdown(call)}</div></div>
    </div>`;
}
