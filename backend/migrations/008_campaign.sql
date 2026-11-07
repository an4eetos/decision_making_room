-- +goose Up
-- The campaign: what the room knows about your goals and what stands between
-- you and them. A view of what you already wrote, not a second app to maintain.

-- Fronts are areas of life and work. You create them; extraction never invents
-- one, it files what it finds under a front you already have.
CREATE TABLE fronts (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL CHECK (length(trim(name)) > 0),
    -- withdrawn: the front is closed. An orderly withdrawal, not a deletion:
    -- what was on it stays on the record.
    status     TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'withdrawn')),
    position   INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_fronts_active_name ON fronts (lower(name)) WHERE status = 'active';

-- Objectives, opposition and fog share a lifecycle, so they share a table:
-- extracted as proposals, kept or dropped by you, then resolved — an objective
-- taken, an obstacle cleared, an unknown lifted by reconnaissance.
CREATE TABLE campaign_items (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type         TEXT NOT NULL CHECK (type IN ('objective', 'obstacle', 'unknown')),
    front_id     UUID REFERENCES fronts(id) ON DELETE SET NULL,
    -- What an obstacle stands in front of, or what an unknown bears on.
    objective_id UUID REFERENCES campaign_items(id) ON DELETE SET NULL,
    text         TEXT NOT NULL CHECK (length(trim(text)) > 0),

    -- proposed: extracted, waiting for you. Nothing goes on the map without
    -- your agreement — the same rule as open loops.
    -- withdrawn: an objective given up on purpose. A legitimate order, not a loss.
    status       TEXT NOT NULL DEFAULT 'active'
                 CHECK (status IN ('proposed', 'active', 'resolved', 'withdrawn', 'dropped')),
    CHECK (status <> 'withdrawn' OR type = 'objective'),

    -- Obstacles only. The kind of stuck, from the Stalled mode, and a strength
    -- that stays an estimate until you confirm it — intelligence is labelled.
    kind         TEXT CHECK (kind IN ('undefined', 'waiting', 'fear', 'too_big', 'unwanted')),
    strength     SMALLINT CHECK (strength BETWEEN 1 AND 3),
    strength_confirmed BOOLEAN NOT NULL DEFAULT FALSE,
    CHECK (type = 'obstacle' OR (kind IS NULL AND strength IS NULL)),

    -- Unknowns only: what reconnaissance found when the fog lifted.
    answer       TEXT NOT NULL DEFAULT '',

    due_at       TIMESTAMPTZ,
    source       TEXT NOT NULL DEFAULT 'manual'
                 CHECK (source IN ('manual', 'chat', 'interrogation')),
    session_id   UUID REFERENCES chat_sessions(id) ON DELETE SET NULL,
    message_id   UUID REFERENCES chat_messages(id) ON DELETE SET NULL,
    confidence   REAL NOT NULL DEFAULT 1.0 CHECK (confidence >= 0 AND confidence <= 1),
    fingerprint  TEXT NOT NULL,

    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at  TIMESTAMPTZ
);

-- The same goal or blocker said twice is one row, among live ones only.
CREATE UNIQUE INDEX uq_campaign_items_live_fingerprint
    ON campaign_items (type, fingerprint) WHERE status IN ('proposed', 'active');

CREATE INDEX idx_campaign_items_status ON campaign_items (status, updated_at DESC);
CREATE INDEX idx_campaign_items_front ON campaign_items (front_id);

-- Commitments become orders: aimed at an objective, an enemy position or an
-- unknown. A recon order is aimed at an unknown and lifts the fog when done.
ALTER TABLE commitments
    ADD COLUMN target_id UUID REFERENCES campaign_items(id) ON DELETE SET NULL,
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'order' CHECK (kind IN ('order', 'recon'));

CREATE INDEX idx_commitments_target ON commitments (target_id) WHERE target_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_commitments_target;
ALTER TABLE commitments DROP COLUMN IF EXISTS kind, DROP COLUMN IF EXISTS target_id;
DROP TABLE IF EXISTS campaign_items;
DROP TABLE IF EXISTS fronts;
