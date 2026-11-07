-- +goose Up
-- The campaign record: one quarter, from planning through operations to an
-- after-action. Predictions come from a pre-mortem and are scored by you.
-- Calibration and commendations are copied onto the quarter when it closes,
-- so a later edit to the map does not rewrite it.

CREATE TABLE campaigns (
    id        UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    quarter   TEXT NOT NULL UNIQUE CHECK (quarter ~ '^[0-9]{4}-Q[1-4]$'),
    name      TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 80),
    phase     TEXT NOT NULL CHECK (phase IN ('planning', 'operations', 'review', 'closed')),
    note      TEXT NOT NULL DEFAULT '',
    review    TEXT NOT NULL DEFAULT '',
    review_at TIMESTAMPTZ,
    opened_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at TIMESTAMPTZ,
    -- Sealed on close. Null while the quarter is open, and null on close when
    -- fewer than five predictions were scored: the number is withheld.
    calibration   DOUBLE PRECISION,
    calibration_n INT NOT NULL DEFAULT 0 CHECK (calibration_n >= 0),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT campaigns_closed_check CHECK (
        (phase = 'closed' AND closed_at IS NOT NULL)
        OR (phase <> 'closed' AND closed_at IS NULL AND calibration IS NULL AND calibration_n = 0)
    )
);

-- Which objectives this quarter is for. A multi-year objective is not pulled
-- in by date; you assign it.
CREATE TABLE campaign_objectives (
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    item_id     UUID NOT NULL REFERENCES campaign_items(id) ON DELETE CASCADE,
    PRIMARY KEY (campaign_id, item_id)
);

-- A failure a pre-mortem named. chance and outcome stay empty until you say:
-- neither is inferred from what you write.
CREATE TABLE predictions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id  UUID REFERENCES campaigns(id) ON DELETE SET NULL,
    objective_id UUID REFERENCES campaign_items(id) ON DELETE SET NULL,
    text         TEXT NOT NULL CHECK (char_length(text) BETWEEN 1 AND 200),
    warning      TEXT NOT NULL DEFAULT '' CHECK (char_length(warning) <= 200),
    status       TEXT NOT NULL CHECK (status IN ('proposed', 'kept', 'dropped')),
    chance       TEXT NOT NULL DEFAULT '' CHECK (chance IN ('', 'long', 'even', 'likely')),
    outcome      TEXT NOT NULL DEFAULT '' CHECK (outcome IN ('', 'called', 'missed')),
    scored_at    TIMESTAMPTZ,
    source       TEXT NOT NULL DEFAULT 'pre_mortem',
    session_id   UUID,
    message_id   UUID,
    fingerprint  TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT predictions_scored_check CHECK (
        (outcome = '' AND scored_at IS NULL) OR (outcome <> '' AND scored_at IS NOT NULL)
    ),
    CONSTRAINT predictions_outcome_kept_check CHECK (outcome = '' OR status = 'kept'),
    CONSTRAINT predictions_chance_kept_check CHECK (chance = '' OR status = 'kept')
);

-- A live prediction is not filed twice. A dropped one may repeat; it is off the record.
CREATE UNIQUE INDEX idx_predictions_live_fingerprint
    ON predictions (fingerprint)
    WHERE status IN ('proposed', 'kept') AND fingerprint <> '';

CREATE INDEX idx_predictions_campaign ON predictions (campaign_id);

-- Written only when the quarter closes. While it is open, commendations are
-- a view of the objectives and the scores, so restoring an objective removes
-- the line.
CREATE TABLE commendations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('taken', 'withdrawal', 'called')),
    text        TEXT NOT NULL CHECK (char_length(text) BETWEEN 1 AND 200),
    ref_id      UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_commendations_campaign ON commendations (campaign_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS commendations;
DROP TABLE IF EXISTS predictions;
DROP TABLE IF EXISTS campaign_objectives;
DROP TABLE IF EXISTS campaigns;
