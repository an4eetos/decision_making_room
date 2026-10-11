-- +goose Up
-- Beast mode's board: orders the generals put a clock on. Separate from open
-- loops because they live for minutes, not weeks, and a clock that ran out is
-- re-armed rather than going stale.
CREATE TABLE beast_tasks (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id    UUID NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    -- The answer that issued the order. Null for a task you added by hand.
    message_id    UUID REFERENCES chat_messages(id) ON DELETE SET NULL,
    -- seq is the task's number within the session, so "task 2" means the same
    -- row to you and to the generals for the whole fight.
    seq           INT NOT NULL,
    text          TEXT NOT NULL CHECK (length(trim(text)) > 0),
    minutes       INT NOT NULL CHECK (minutes BETWEEN 1 AND 240),
    deadline_at   TIMESTAMPTZ NOT NULL,
    -- open until you report it done. abandoned is what is left when you stand
    -- down, or what you struck off yourself; the generals call it by its name.
    status        TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'done', 'abandoned')),
    -- How many times the clock ran out unreported. Each one re-arms it shorter.
    expiries      INT NOT NULL DEFAULT 0,
    -- The open loop this order finishes. Reporting the task done closes it too.
    commitment_id UUID REFERENCES commitments(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at   TIMESTAMPTZ,
    UNIQUE (session_id, seq)
);

CREATE INDEX idx_beast_tasks_session_status ON beast_tasks (session_id, status);

-- +goose Down
DROP TABLE IF EXISTS beast_tasks;
