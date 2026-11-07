-- +goose Up
-- One snapshot of the theatre map per day, so the campaign so far can be
-- played back. The state is the laid-out map (fronts, rows, posts, lines),
-- not pixels, so a past day is drawn by the same renderer as today.
CREATE TABLE campaign_snapshots (
    day      DATE PRIMARY KEY,
    taken_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    state    JSONB NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS campaign_snapshots;
