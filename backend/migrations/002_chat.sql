-- +goose Up
CREATE TABLE chat_sessions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title              TEXT NOT NULL DEFAULT '',
    summary            TEXT NOT NULL DEFAULT '',
    summary_updated_at TIMESTAMPTZ,
    mode_id            TEXT NOT NULL DEFAULT '',
    mode_locked        BOOLEAN NOT NULL DEFAULT FALSE,
    -- Empty means "no explicit choice": the session follows the configured
    -- default, so changing DEFAULT_TIER moves existing sessions with it rather
    -- than leaving them pinned to whatever was default when they were created.
    tier               TEXT NOT NULL DEFAULT ''
                       CHECK (tier IN ('', 'quick', 'standard', 'deep')),
    generals           TEXT[] NOT NULL DEFAULT '{}',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- mode/tier/generals are recorded per message as well as per session: the mode
-- can change mid-conversation, and per-message rows are what let you later ask
-- "did deep tier actually answer better" without adding telemetry.
CREATE TABLE chat_messages (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id    UUID NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    role          TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content       TEXT NOT NULL,
    sources       JSONB NOT NULL DEFAULT '[]',
    mode_id       TEXT NOT NULL DEFAULT '',
    tier          TEXT NOT NULL DEFAULT '',
    generals      TEXT[] NOT NULL DEFAULT '{}',
    detect_method TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_chat_sessions_updated_at ON chat_sessions(updated_at DESC);
CREATE INDEX idx_chat_messages_session    ON chat_messages(session_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS chat_messages;
DROP TABLE IF EXISTS chat_sessions;
