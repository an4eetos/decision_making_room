// Red team: one general takes command of the opposition on your map and
// attacks your plan.
//
// A red team answer comes in fixed sections — Enemy commander, How I hold,
// Where the plan breaks, Your fog my cover, I concede, To beat me. Like the
// briefing and the interrogation, the rendering is an enhancement: anything
// that does not parse falls back to ordinary markdown.

const RED_TEAM_MODE = "red_team";

const RED_TEAM_SECTIONS = ["enemy commander", "how i hold", "where the plan breaks", "your fog, my cover", "i concede", "to beat me"];

const RED_TEAM_HEADING = new RegExp(
    `^\\s*(#{1,4}\\s*)?(\\*\\*)?(${RED_TEAM_SECTIONS.join("|")})\\s*:?\\s*\\2\\s*:?\\s*(.*)$`, "i");

function splitRedTeam(markdown) {
    const sections = {};
    let current = null;
    for (const line of String(markdown).split("\n")) {
        const m = line.match(RED_TEAM_HEADING);
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

function redTeamBlock(cls, key, body) {
    return `<div class="rt-block ${cls}"><span class="rt-key">${escapeHTML(key)}</span><div class="markdown">${renderMarkdown(body)}</div></div>`;
}

// renderRedTeam returns null when the answer is not in the red team's shape,
// so the caller can fall back to markdown.
function renderRedTeam(markdown) {
    const s = splitRedTeam(markdown);
    if (RED_TEAM_SECTIONS.filter((k) => s[k] !== undefined).length < 2) {
        return null;
    }
    return `
    <div class="redteam-answer">
        ${s["enemy commander"] ? redTeamBlock("rt-commander", "Enemy commander", s["enemy commander"]) : ""}
        ${s["how i hold"] ? redTeamBlock("rt-hold", "How I hold", s["how i hold"]) : ""}
        ${s["where the plan breaks"] ? redTeamBlock("rt-breaks", "Where the plan breaks", s["where the plan breaks"]) : ""}
        ${s["your fog, my cover"] ? redTeamBlock("rt-cover", "Your fog, my cover", s["your fog, my cover"]) : ""}
        ${s["i concede"] ? redTeamBlock("rt-concede", "I concede", s["i concede"]) : ""}
        ${s["to beat me"] ? redTeamBlock("rt-beat", "To beat me", s["to beat me"]) : ""}
    </div>`;
}

// redTeamQuestion writes the opening attack from what the campaign page
// handed over: one objective, one front, or the whole map.
function redTeamQuestion(kind, text) {
    const target = String(text || "").trim();
    if (kind === "objective" && target) {
        return `Red team my plan for the objective "${target}". Take command of what stands against it and attack.`;
    }
    if (kind === "front" && target) {
        return `Red team the ${target} front. Take command of the opposition there and attack my plan.`;
    }
    return "Red team my plan. Take command of the opposition on my map and attack.";
}
