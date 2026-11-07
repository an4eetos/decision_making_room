// The settings page. It draws whatever GET /api/settings describes, so a
// setting added on the server needs no change here.
(function () {
    const root = document.getElementById("settings-root");
    if (!root) {
        return;
    }
    const problems = document.getElementById("settings-problems");
    const result = document.getElementById("settings-result");
    const saveButton = document.getElementById("settings-save");
    const testButton = document.getElementById("settings-test");
    const dirtyNote = document.getElementById("settings-dirty");

    const groupNames = {
        provider: "Provider",
        gemini: "Gemini",
        anthropic: "Anthropic (Claude)",
        ollama: "Ollama (local only)",
        answers: "Answers",
        checkins: "Check-ins",
        campaign: "Campaign",
    };
    const sourceNames = { env: "environment", stored: "saved here", default: "default" };

    // key → new value, or null to go back to the default.
    const dirty = new Map();

    function node(tag, attrs = {}, ...children) {
        const el = document.createElement(tag);
        for (const [k, v] of Object.entries(attrs)) {
            if (k === "class") {
                el.className = v;
            } else if (v !== undefined && v !== null && v !== false) {
                el.setAttribute(k, v === true ? "" : v);
            }
        }
        for (const child of children) {
            if (child !== null && child !== undefined) {
                el.append(child);
            }
        }
        return el;
    }

    // apiJSON throws the response body; show its "error" field when it has one.
    function reason(error) {
        try {
            return JSON.parse(error.message).error || error.message;
        } catch (_) {
            return error.message;
        }
    }

    function markDirty(key, value) {
        if (value === undefined) {
            dirty.delete(key);
        } else {
            dirty.set(key, value);
        }
        saveButton.disabled = dirty.size === 0;
        dirtyNote.textContent = dirty.size ? `${dirty.size} unsaved` : "";
    }

    function control(item) {
        const id = `setting-${item.key}`;
        const common = { id, name: item.key, disabled: item.locked };
        let input;

        switch (item.kind) {
        case "secret":
            input = node("input", {
                ...common, type: "password", autocomplete: "off", spellcheck: "false",
                placeholder: item.value || "not set",
            });
            input.addEventListener("input", () => markDirty(item.key, input.value.trim() || undefined));
            return input;
        case "choice":
            input = node("select", common,
                ...item.choices.map((c) => node("option", { value: c, selected: c === item.value }, c || "(none)")));
            break;
        case "bool":
            input = node("select", common,
                node("option", { value: "true", selected: item.value === "true" }, "on"),
                node("option", { value: "false", selected: item.value !== "true" }, "off"));
            break;
        case "int":
            input = node("input", { ...common, type: "number", value: item.value });
            break;
        default:
            input = node("input", { ...common, type: "text", value: item.value, spellcheck: "false" });
        }
        input.addEventListener("change", () => markDirty(item.key, input.value));
        return input;
    }

    function row(item) {
        const source = node("span", { class: `badge settings-source settings-source--${item.source}` },
            sourceNames[item.source] || item.source);
        const label = node("label", { for: `setting-${item.key}` }, item.label, " ", source);

        const extras = [];
        if (item.source === "stored" && !item.locked) {
            const reset = node("button", { type: "button", class: "row-edit" }, "Use default");
            reset.addEventListener("click", () => {
                markDirty(item.key, null);
                reset.textContent = "Reverts on save";
                reset.disabled = true;
            });
            extras.push(reset);
        }

        return node("div", { class: "settings-item" },
            label,
            node("div", { class: "settings-control" }, control(item), ...extras),
            item.help ? node("p", { class: "muted settings-help" }, item.help) : null,
            item.locked ? node("p", { class: "muted settings-help" }, "Set in the server's environment.") : null,
            item.pending_restart ? node("p", { class: "settings-note" }, "Saved; applies after the server restarts.") : null,
            item.problem ? node("p", { class: "settings-problem" }, `Stored value ignored: ${item.problem}`) : null);
    }

    function render(view) {
        dirty.clear();
        markDirty("", undefined);
        root.replaceChildren();
        problems.replaceChildren();

        for (const p of [view.chat_problem, view.embed_problem]) {
            if (p) {
                problems.append(node("div", { class: "error" }, p));
            }
        }
        if (!view.secrets_writable) {
            problems.append(node("p", { class: "muted" },
                "Keys cannot be saved here until the server has a SETTINGS_KEY (openssl rand -base64 32). " +
                "Until then, set them as environment variables."));
        }

        const groups = new Map();
        for (const item of view.items) {
            if (!groups.has(item.group)) {
                groups.set(item.group, []);
            }
            groups.get(item.group).push(item);
        }
        for (const [group, items] of groups) {
            root.append(node("fieldset", { class: "settings-group" },
                node("legend", {}, groupNames[group] || group),
                ...items.map(row)));
        }
    }

    async function load() {
        try {
            render(await apiJSON("/api/settings"));
        } catch (error) {
            showMessage(result, reason(error), "error");
        }
    }

    saveButton.addEventListener("click", async () => {
        saveButton.disabled = true;
        try {
            const view = await apiJSON("/api/settings", {
                method: "PUT",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ values: Object.fromEntries(dirty) }),
            });
            render(view);
            showMessage(result, "Saved.", "success");
        } catch (error) {
            showMessage(result, reason(error), "error");
            saveButton.disabled = dirty.size === 0;
        }
    });

    function checkLine(name, check) {
        const head = `${name}: ${check.provider} · ${check.model}`;
        return check.ok
            ? node("div", { class: "success" }, `${head} — OK in ${check.ms} ms`)
            : node("div", { class: "error" }, `${head} — ${check.error}`);
    }

    testButton.addEventListener("click", async () => {
        testButton.disabled = true;
        result.replaceChildren(node("p", { class: "muted" }, "Asking the provider…"));
        try {
            const res = await apiJSON("/api/settings/test", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ values: Object.fromEntries(dirty) }),
                timeoutMs: 90000,
            });
            result.replaceChildren(checkLine("Chat", res.chat), checkLine("Embeddings", res.embed));
        } catch (error) {
            showMessage(result, reason(error), "error");
        } finally {
            testButton.disabled = false;
        }
    });

    // Devices: every session, the current one marked, each revocable.
    const devices = document.getElementById("devices-root");

    async function loadDevices() {
        let sessions;
        try {
            sessions = await apiJSON("/api/auth/sessions");
        } catch (error) {
            showMessage(devices, reason(error), "error");
            return;
        }
        devices.replaceChildren(...sessions.map((s) => {
            const out = node("button", { type: "button", class: "row-delete" }, s.current ? "Sign out" : "Revoke");
            out.addEventListener("click", async () => {
                out.disabled = true;
                try {
                    await apiJSON(`/api/auth/sessions/${s.id}`, { method: "DELETE" });
                    if (s.current) {
                        location.assign("/login");
                        return;
                    }
                    loadDevices();
                } catch (error) {
                    showMessage(devices, reason(error), "error");
                }
            });
            return node("div", { class: "settings-device" },
                node("span", {}, s.client || "Unnamed device", s.current ? node("span", { class: "badge" }, "this device") : null),
                node("span", { class: "muted" }, `last seen ${formatDate(s.last_seen_at)}`),
                out);
        }));
    }

    load();
    loadDevices();
})();
