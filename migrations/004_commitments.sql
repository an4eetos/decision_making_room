-- +goose Up
-- Open loops: things you said you would do.
--
-- Deliberately minimal. No priority, project, tags, parent/child, recurrence or
-- effort estimate — each is a feature request wearing a column's clothing, and
-- an open-loop list that demands curation stops being used within two weeks.
CREATE TABLE commitments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    text        TEXT NOT NULL CHECK (length(trim(text)) > 0),

    -- proposed: extracted from conversation, awaiting keep or drop. Nothing
    -- enters the open list without you agreeing to it.
    status      TEXT NOT NULL DEFAULT 'open'
                CHECK (status IN ('proposed', 'open', 'done', 'dropped', 'stale')),

    due_at      TIMESTAMPTZ,
    source      TEXT NOT NULL DEFAULT 'manual'
                CHECK (source IN ('chat', 'manual', 'checkin')),
    session_id  UUID REFERENCES chat_sessions(id) ON DELETE SET NULL,
    message_id  UUID REFERENCES chat_messages(id) ON DELETE SET NULL,
    mode_id     TEXT NOT NULL DEFAULT '',
    confidence  REAL NOT NULL DEFAULT 1.0 CHECK (confidence >= 0 AND confidence <= 1),

    -- Normalised text hash, so the same promise made twice is one row.
    fingerprint TEXT NOT NULL,

    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ
);

-- Dedupe only among live rows: once something is done or dropped you should be
-- able to commit to it again.
CREATE UNIQUE INDEX uq_commitments_live_fingerprint
    ON commitments(fingerprint) WHERE status IN ('proposed', 'open');

CREATE INDEX idx_commitments_status_updated ON commitments(status, updated_at DESC);

-- +goose Down
DROP TABLE IF EXISTS commitments;
