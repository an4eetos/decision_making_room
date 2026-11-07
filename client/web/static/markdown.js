// Assistant answers arrive as markdown. They used to be escaped and dumped into
// the DOM verbatim, so every ***emphasis***, table pipe and heading hash printed
// as literal punctuation.
//
// marked parses, DOMPurify sanitises. Both are vendored rather than loaded from
// a CDN: a self-hosted tool should not need the network to render its own UI.

(function () {
    const READY = typeof marked !== "undefined" && typeof DOMPurify !== "undefined";

    if (READY) {
        marked.setOptions({
            gfm: true,
            breaks: true, // people write chat messages with single newlines
        });
    }

    // Links open in a new tab and carry noopener. Answers can quote URLs from
    // your own notes, and a rendered link should not be able to reach back into
    // this page through window.opener.
    if (READY) {
        DOMPurify.addHook("afterSanitizeAttributes", (node) => {
            if (node.tagName === "A" && node.getAttribute("href")) {
                node.setAttribute("target", "_blank");
                node.setAttribute("rel", "noopener noreferrer");
            }
        });
    }

    const ALLOWED_TAGS = [
        "p", "br", "hr", "strong", "em", "del", "code", "pre", "blockquote",
        "h1", "h2", "h3", "h4", "h5", "h6",
        "ul", "ol", "li",
        "table", "thead", "tbody", "tr", "th", "td",
        "a", "span", "div",
    ];

    // Gives each table its own horizontal scroll container.
    //
    // The CSS-only alternative is `display: block; overflow-x: auto` on the table
    // itself, which makes thead and tbody block-level and so loses column
    // alignment entirely — the header renders as a detached box above the body.
    // This runs on already-sanitised HTML, so it cannot reintroduce anything.
    function wrapTables(html) {
        const host = document.createElement("div");
        host.innerHTML = html;

        for (const table of host.querySelectorAll("table")) {
            const wrap = document.createElement("div");
            wrap.className = "table-wrap";
            table.replaceWith(wrap);
            wrap.appendChild(table);
        }

        return host.innerHTML;
    }

    // Renders markdown to sanitised HTML. Falls back to escaped plain text if
    // either library failed to load, so a missing asset degrades to the old
    // behaviour rather than a blank message.
    window.renderMarkdown = function renderMarkdown(text) {
        const source = String(text ?? "");
        if (!READY) {
            return escapeHTML(source).replace(/\n/g, "<br>");
        }

        try {
            const clean = DOMPurify.sanitize(marked.parse(source), {
                ALLOWED_TAGS,
                ALLOWED_ATTR: ["href", "title", "class"],
                // No data: or javascript: URLs, whatever the source said.
                ALLOWED_URI_REGEXP: /^(?:https?:|mailto:|#|\/)/i,
            });
            return wrapTables(clean);
        } catch (error) {
            console.error("markdown render failed", error);
            return escapeHTML(source).replace(/\n/g, "<br>");
        }
    };
})();
