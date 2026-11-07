-- +goose Up
-- The room asking how things are going, instead of waiting to be asked.
CREATE TABLE checkins (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- morning / midday / evening for scheduled ones; idle and stale for nudges.
    slot        TEXT NOT NULL CHECK (length(slot) > 0),
    kind        TEXT NOT NULL DEFAULT 'scheduled'
                CHECK (kind IN ('scheduled', 'nudge')),

    -- The local calendar date it belongs to. Together with slot this is what
    -- makes firing idempotent: a restart, or a laptop waking from sleep, finds
    -- the row already there and does not fire twice.
    local_date  DATE NOT NULL,

    -- The mode the conversation opens in when you reply, so answering an
    -- evening check-in continues as a debrief.
    mode_id     TEXT NOT NULL DEFAULT '',

    title       TEXT NOT NULL,
    body        TEXT NOT NULL,
    session_id  UUID REFERENCES chat_sessions(id) ON DELETE SET NULL,
    seen_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_checkins_slot_date ON checkins(slot, local_date);
CREATE INDEX idx_checkins_unseen ON checkins(created_at DESC) WHERE seen_at IS NULL;

-- +goose Down
DROP TABLE IF EXISTS checkins;
