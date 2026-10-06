// Hints: a small "?" next to a feature that explains what it is for and what
// happens to what you put in it.
//
// The copy lives here rather than in title attributes because the useful part —
// where your text goes, what reads it later — is too long for a tooltip, and
// tooltips never show on touch screens. Each hint has the same three parts so
// they read as one system: what it is, how it is used, and the thing people get
// wrong about it.
//
// Markup: <button type="button" class="hint" data-hint="capture"></button>.
// Clicks are delegated from the document, so hints inside content rendered
// later (the relocation plan) need no wiring.

const HINTS = {
    capture: {
        title: "Log a line",
        what: "Appends one timestamped line to today's note, <code>journal/daily/YYYY-MM-DD.md</code>. No model call, no answer — it is a notebook, not a question.",
        uses: [
            "The journal watcher picks the file up within a second, chunks it and embeds it as a <em>daily log</em> memory.",
            "Every question searches it. Standard and Deep answers also pull in your most recent entries whatever you asked, so “what did I do this week” works.",
            "Check-ins read from the same memory, so a line you log at lunch can come back in the evening debrief.",
        ],
        notes: [
            "It is a plain markdown file — still yours if you stop using this app. You can edit it in any editor and the change is re-ingested.",
            "Good for decisions as you make them, what you finished, what is bothering you. Short is fine.",
            "Logging does not reset the “quiet for a while” nudge — only chat messages count as activity there. Loops are not picked up from logs either; say it in a conversation or add it below.",
        ],
    },
    operations: {
        title: "Operations",
        what: "Your conversations. Each one keeps its own history, depth and mode.",
        uses: [
            "Earlier turns are sent with each new question, so follow-ups can say “that” and “the second option”. Long threads are summarised rather than cut off.",
            "A conversation remembers the depth you last used and stays in its mode unless you clearly change the subject.",
        ],
        notes: [
            "Start a new one when the topic changes — an old thread carries its mode and context into the new question.",
            "Conversations are not searched as memories. If a conclusion matters, log it or ingest it.",
        ],
    },
    loops: {
        title: "Open loops",
        what: "Things you said you would do, with due dates where you gave one.",
        uses: [
            "When you write something like “I'll call the landlord tomorrow” in a conversation, it shows up here as a <em>proposal</em> with the date resolved. Keep it or drop it — nothing joins the list without you.",
            "Only your own words are read. Suggestions the assistant made are not your promises.",
            "Check-ins name open and overdue loops by name, and the count shows in the briefing bar.",
        ],
        notes: [
            "Anything open and untouched for two weeks goes stale and stops appearing in check-ins. You get one “gone quiet” nudge asking if you still mean it.",
            "Type in the box below to add one by hand; the date is optional.",
        ],
    },
    checkins: {
        title: "Check-ins",
        what: "The room starting the conversation instead of waiting for you.",
        uses: [
            "Morning asks what today is for, midday whether you are on it, evening what closed. Each names something specific — an overdue loop, a recent note — and ends with one question.",
            "Replying opens it as a normal conversation in the matching mode. One you never answer never clutters the chat list.",
            "<strong>Check in now</strong> works any time, whether or not scheduled check-ins are on, and picks its shape from the time of day.",
        ],
        notes: [
            "Scheduled ones are off by default — set <code>CHECKIN_ENABLED=true</code>. A slot missed while the laptop slept fires on wake, within a grace window.",
            "Each scheduled check-in is one quick model call. Nudges cost nothing; they are written from what the database already knows.",
        ],
    },
    mode: {
        title: "Mode",
        what: "The shape of the answer — a day plan, a hard call, a stalled task, a debrief. Fifteen modes plus <strong>Open</strong>, which imposes nothing, and <strong>Interrogation</strong>, which you only ever enter on purpose.",
        uses: [
            "The room detects it from what you asked and shows why. Pick one from the menu to pin it.",
            "Modes also bias retrieval: a debrief weights recent notes heavily; a pre-mortem barely cares how old a decision is.",
        ],
        notes: [
            "A conversation keeps its mode unless you clearly change the subject — switching the answer's shape mid-thread is worse than a slightly wrong mode.",
            "A wrong pick is one click to fix. Add your own modes with markdown files in <code>MODES_DIR</code>.",
        ],
    },
    interrogation: {
        title: "Interrogation",
        what: "No advice yet. The seated generals question you, one question each, until your position no longer rests on a cognitive trap.",
        uses: [
            "Every turn shows what is <em>on the record</em>, what is <em>still dark</em>, any trap <em>caught</em> in your own words, and the questions — each from the general who asks it.",
            "Vague answers get sent back: “soon” and “maybe” need a date, a number or a name. Every turn ends with a probe you can check in reality within 48 hours.",
            "<strong>Take a position</strong> ends it: your stance, the traps that no longer get a vote, orders with a direction, a time and a fallback, and the unknowns you chose to carry.",
        ],
        notes: [
            "It lights up when the generals say nothing left would change the position, but you can take one at any point.",
            "<strong>Leave</strong> goes back to ordinary advice without a position. The room will not suggest another interrogation for the next few answers.",
        ],
    },
    "interrogate-suggest": {
        title: "Why a general recommends this",
        what: "Something in what you wrote looks like a cognitive trap: shame forecast as a verdict, pain forecast as permanent, “no choice”, money already spent voting on what happens next.",
        uses: [
            "Found two ways: phrases in your message (quoted), and the model reading between the lines (marked as such). Neither costs an extra model call.",
            "It is signed by the seated general whose doctrine kills that trap.",
        ],
        notes: [
            "It is a suggestion, not a diagnosis. <strong>Not now</strong> hides it on this device, and the room waits a few answers before suggesting again.",
            "Check-ins never suggest one.",
        ],
    },
        depth: {
        title: "Answer depth",
        what: "How much work one question is worth.",
        uses: [
            "<strong>Quick</strong> — 3 memories, no digging, one lens, under 120 words. Seconds.",
            "<strong>Standard</strong> — 8 memories from full hybrid search, plus recent entries, and one round of digging if context is thin. Up to two lenses.",
            "<strong>Deep</strong> — splits the question into separate searches, ranks a pool of 60 with the model, keeps digging for up to three rounds. Up to three lenses. Slow.",
        ],
        notes: [
            "If Quick finds nothing it escalates to Standard rather than answering blind. Each answer is labelled with the depth that actually produced it.",
            "The server can cap the depth with <code>MAX_TIER</code>.",
        ],
    },
    council: {
        title: "Council",
        what: "The generals whose lens your next answer is written through.",
        uses: [
            "Seat up to three from the roster. With seats empty, the room picks per question by matching what you asked.",
            "With more than one lens the answer gives each position, then where they disagree, then a call that says which lens it sided with and what that cost.",
        ],
        notes: [
            "Depth limits how many seats are used: one on Quick, two on Standard, three on Deep. Seat three and ask a Quick question, and only the first one speaks.",
            "Picks the room makes stay for the rest of a conversation, so the argument stays consistent. A new general replaces one only on strong evidence.",
            "When fewer lenses fit than the depth allows, the extra seats go to opposing families on purpose — two lenses that agree tell you nothing one would not.",
        ],
    },
    roster: {
        title: "Roster",
        what: "Twenty strategic lenses. Each has a job, a doctrine, and a declared blind spot — what it systematically gets wrong.",
        uses: [
            "Click a portrait to seat that general on the council; hover for what it is for.",
            "Only a compact card and a few relevant doctrine passages reach the model, never the full text — except on Deep, where it can read more.",
        ],
        notes: [
            "They are lenses, not endorsements. Replace or add your own through <code>GENERALS_DIR</code>.",
        ],
    },
    sources: {
        title: "Sources",
        what: "The memories this answer was given to work from.",
        uses: [
            "The match is how well each one ranked for your question: meaning search and keyword search combined, then adjusted for recency and so one document does not crowd out the rest.",
        ],
        notes: [
            "If the right note is missing, it was not found — not ignored. Try Deep, or reword with the words the note actually uses.",
        ],
    },
    ingest: {
        title: "Ingest",
        what: "Paste or upload something you want the room to know — a past decision, a plan, notes from a meeting.",
        uses: [
            "Long text is split by markdown heading, and each chunk keeps its heading path so it is findable out of context.",
            "Every chunk is embedded and indexed for both meaning and keyword search. Questions retrieve the relevant chunks, not the whole document.",
        ],
        notes: [
            "Pasted text lives only in the database. For something you will keep editing, put a markdown file in your <code>journal/</code> folder instead — it is watched and re-ingested on save.",
            "Facts that should be in every answer — who you are, how you work — belong in <code>journal/about-me.md</code> or <code>journal/context/</code>, not here.",
        ],
    },
    kind: {
        title: "Kind",
        what: "What sort of thing this is.",
        uses: [
            "<strong>decision</strong> — something you chose, ideally with why. <strong>plan</strong> — what you intend. <strong>note</strong> — everything else. <strong>daily_log</strong> — what happened on a day.",
            "The model can filter by kind when it digs, and you can filter by it in Memories.",
        ],
        notes: [
            "Files in <code>journal/daily/</code> are filed as daily logs automatically; other journal files are notes.",
        ],
    },
    memories: {
        title: "Memories",
        what: "Everything the room can retrieve: your journal files, logged lines and anything ingested.",
        uses: [
            "Search here uses the same hybrid retrieval as a question, so it shows what an answer would find.",
            "Edit fixes a memory and re-embeds it, so searches match the new text. Delete removes it from retrieval.",
        ],
        notes: [
            "Memories from journal files are copies. Editing or deleting one here does not touch the file, and the next time that file changes it is re-read from disk — fix the file itself to make a change stick.",
            "<code>about-me.md</code> and <code>context/</code> are not listed: they are sent with every question instead of being searched.",
        ],
    },
    stays: {
        title: "Stays",
        what: "Each stay is one trip: a destination, dates, and the checklist built for it.",
        uses: [
            "What you mark as owned, bought or skipped is saved per stay. Prices you confirm are remembered per city for the next stay there.",
        ],
        notes: [],
    },
    "reloc-form": {
        title: "Building the list",
        what: "These answers decide which items appear, how many of each, and which pitfalls apply.",
        uses: [
            "Durable goods do not scale with time — two towels for two weeks or six months. Consumables do, rounded up, so a long stay is a different list rather than a longer one.",
            "Housing and climate filter items and pitfalls: furnished rarely includes bedding or a sharp knife; tropical adds what humid months need.",
        ],
        notes: [
            "Left blank, a field widens the list rather than guessing — you will see items that may not apply.",
        ],
    },
    budget: {
        title: "Setup cost",
        what: "What it costs to set up this stay, from the items you still need.",
        uses: [
            "Prices start as rough global anchors in USD. <strong>Price for this city</strong> asks the model to localise them — still estimates, marked <span class=\"reloc-estimate\">est</span> on every line with the model's confidence.",
            "Type a real price on any line and it replaces the estimate, counts as confirmed, and is remembered for the next stay in that city.",
        ],
        notes: [
            "The caveat under the total says how much of it is guesswork. Treat an estimated total as a range, not a quote.",
        ],
    },
    comfort: {
        title: "Comfort",
        what: "How livable this stay is with what you have now, scored from the checklist. No model is involved.",
        uses: [
            "Each item serves one part of daily life — hygiene, sleep, food — and is critical, important or nice. A missing critical item caps its area at 35 however complete the rest is: no towel is not a 95% bathroom.",
            "The overall score leans on the worst area. Good coffee does not make up for no sheets.",
            "<strong>First night</strong> scores only what you need before any shop opens. <strong>Fix these first</strong> is the shortest path up, in the order to buy.",
        ],
        notes: [
            "Have it and Bought count as covered. Skip means the item does not apply here, so it neither helps nor hurts.",
        ],
    },
    pitfalls: {
        title: "Pitfalls",
        what: "Mistakes people discover too late, filtered to the ones that apply to this stay.",
        uses: [
            "Critical ones can cost you the trip or a fine — a prescription that is controlled where you are going. Costly ones cost money; annoying ones cost days.",
            "Tick <strong>Handled</strong> once you have dealt with one; it stays in the list, greyed out.",
        ],
        notes: [],
    },
};

function setupHints() {
    let popover = null;
    let owner = null;

    function close({ restoreFocus = false } = {}) {
        if (!popover) {
            return;
        }
        popover.remove();
        popover = null;
        owner?.setAttribute("aria-expanded", "false");
        if (restoreFocus) {
            owner?.focus();
        }
        owner = null;
    }

    function list(items) {
        return `<ul>${items.map((i) => `<li>${i}</li>`).join("")}</ul>`;
    }

    // Copy is static and authored above, so it is trusted HTML.
    function render(hint) {
        return `
            <div class="hint-pop-head">
                <h3 id="hint-pop-title">${escapeHTML(hint.title)}</h3>
                <button type="button" class="hint-pop-close" aria-label="Close">×</button>
            </div>
            <p>${hint.what}</p>
            ${hint.uses.length ? `<h4>How it's used</h4>${list(hint.uses)}` : ""}
            ${hint.notes.length ? `<h4>Good to know</h4>${list(hint.notes)}` : ""}`;
    }

    // Fixed positioning against the viewport, below the button when it fits and
    // above when it does not, clamped so it never runs off a phone screen.
    function place(button) {
        const gap = 8;
        const margin = 12;
        const rect = button.getBoundingClientRect();
        const width = popover.offsetWidth;
        const height = popover.offsetHeight;

        let left = rect.left + rect.width / 2 - width / 2;
        left = Math.max(margin, Math.min(left, window.innerWidth - width - margin));

        let top = rect.bottom + gap;
        if (top + height > window.innerHeight - margin && rect.top - gap - height >= margin) {
            top = rect.top - gap - height;
        }

        popover.style.left = `${left}px`;
        popover.style.top = `${Math.max(margin, top)}px`;
    }

    function open(button) {
        const hint = HINTS[button.dataset.hint];
        if (!hint) {
            return;
        }

        popover = document.createElement("div");
        popover.className = "hint-pop";
        popover.id = "hint-pop";
        popover.setAttribute("role", "dialog");
        popover.setAttribute("aria-labelledby", "hint-pop-title");
        popover.innerHTML = render(hint);
        document.body.appendChild(popover);

        owner = button;
        owner.setAttribute("aria-expanded", "true");
        place(button);
    }

    // Buttons get their label and state here so the markup stays a one-liner.
    function decorate(root) {
        root.querySelectorAll(".hint:not([data-hint-ready])").forEach((button) => {
            const hint = HINTS[button.dataset.hint];
            button.dataset.hintReady = "";
            button.textContent = "?";
            button.setAttribute("aria-label", hint ? `What is ${hint.title}?` : "What is this?");
            button.setAttribute("aria-haspopup", "dialog");
            button.setAttribute("aria-expanded", "false");
            button.setAttribute("aria-controls", "hint-pop");
        });
    }

    decorate(document);
    new MutationObserver(() => decorate(document)).observe(document.body, { childList: true, subtree: true });

    document.addEventListener("click", (e) => {
        const button = e.target.closest(".hint");
        if (button) {
            // A hint inside a <summary> or <label> must not also toggle it.
            e.preventDefault();
            e.stopPropagation();
            const wasOwner = button === owner;
            close();
            if (!wasOwner) {
                open(button);
            }
            return;
        }

        if (e.target.closest(".hint-pop-close")) {
            close({ restoreFocus: true });
            return;
        }

        if (popover && !popover.contains(e.target)) {
            close();
        }
    }, true);

    document.addEventListener("keydown", (e) => {
        if (e.key === "Escape" && popover) {
            close({ restoreFocus: true });
        }
    });

    // Re-anchoring on every scroll frame is not worth it for a short explainer.
    // Scrolling the popover itself is reading it, not leaving it.
    window.addEventListener("resize", () => close());
    document.addEventListener("scroll", (e) => {
        if (popover && !popover.contains(e.target)) {
            close();
        }
    }, true);
}

document.addEventListener("DOMContentLoaded", setupHints);
