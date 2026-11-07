// Sign-in and first-time setup. Standalone: these pages load nothing else.
(function () {
    const form = document.querySelector("form[data-auth]");
    const result = document.getElementById("auth-result");
    if (!form || !result) {
        return;
    }
    const mode = form.dataset.auth;

    // The login page shows only the ways in this room has turned on.
    async function offerWaysIn() {
        const error = new URLSearchParams(location.search).get("error");
        if (error) {
            show(error);
        }
        let state;
        try {
            const response = await fetch("/api/auth/state");
            state = await response.json();
        } catch (_) {
            show("Could not reach the server.");
            form.hidden = false;
            return;
        }
        if (state.setup_required && !state.oidc.enabled) {
            location.replace("/setup");
            return;
        }
        const passwordShown = state.password && !state.setup_required;
        if (state.oidc.enabled) {
            const link = document.getElementById("oidc-signin");
            link.textContent = `Sign in with ${state.oidc.name}`;
            link.href = `${state.oidc.start_url}?next=${encodeURIComponent(nextPath())}`;
            link.hidden = false;
            document.getElementById("auth-or").hidden = !passwordShown;
        }
        form.hidden = !passwordShown;
        if (passwordShown) {
            form.querySelector("input[type=password]").focus();
        }
        document.getElementById("auth-setup").hidden = !state.setup_required;
    }

    function show(message) {
        result.innerHTML = "";
        const div = document.createElement("div");
        div.className = "error";
        div.textContent = message;
        result.appendChild(div);
    }

    // Only a path on this site, so ?next= cannot bounce the browser elsewhere
    // after it signs in.
    function nextPath() {
        const next = new URLSearchParams(location.search).get("next") || "/";
        if (!next.startsWith("/") || next.startsWith("//") || next.startsWith("/\\")) {
            return "/";
        }
        return next;
    }

    // A readable name for the sessions list: "Safari on macOS".
    function deviceName() {
        const ua = navigator.userAgent;
        const browser =
            /Edg\//.test(ua) ? "Edge" :
            /Firefox\//.test(ua) ? "Firefox" :
            /Chrome\//.test(ua) ? "Chrome" :
            /Safari\//.test(ua) ? "Safari" : "Browser";
        const os =
            /iPhone|iPad/.test(ua) ? "iOS" :
            /Android/.test(ua) ? "Android" :
            /Mac OS X/.test(ua) ? "macOS" :
            /Windows/.test(ua) ? "Windows" :
            /Linux/.test(ua) ? "Linux" : "";
        return os ? `${browser} on ${os}` : browser;
    }

    form.addEventListener("submit", async (event) => {
        event.preventDefault();
        const data = new FormData(form);
        const body = { password: data.get("password"), client: deviceName(), cookie: true };

        if (mode === "setup") {
            if (data.get("password") !== data.get("confirm")) {
                show("The two passwords differ.");
                return;
            }
            body.code = data.get("code");
        }

        const button = form.querySelector("button[type=submit]");
        button.disabled = true;
        try {
            const response = await fetch(`/api/auth/${mode}`, {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify(body),
            });
            if (response.ok) {
                location.replace(nextPath());
                return;
            }
            if (response.status === 409 && mode === "login") {
                location.replace("/setup");
                return;
            }
            let message = response.statusText;
            try {
                message = (await response.json()).error || message;
            } catch (_) {
                // not JSON; keep the status text
            }
            show(message);
        } catch (error) {
            show(error.message || "Could not reach the server.");
        } finally {
            button.disabled = false;
        }
    });

    if (mode === "login") {
        offerWaysIn();
    }
})();
