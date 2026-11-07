-- +goose Up
-- The staff's appraisals of the map: where the point of main effort is, where
-- to yield ground, what is overextended. Kept so the campaign page shows the
-- last one with its date, and so one can be opened as a conversation.
CREATE TABLE campaign_appraisals (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The front appraised, or NULL for the whole map.
    front_id   UUID REFERENCES fronts(id) ON DELETE SET NULL,
    -- The depth that actually ran, and the generals who wrote it.
    tier       TEXT NOT NULL DEFAULT '',
    generals   TEXT[] NOT NULL DEFAULT '{}',
    body       TEXT NOT NULL CHECK (length(trim(body)) > 0),
    -- Set once you discuss it: the conversation is created lazily, like a
    -- check-in's, so an appraisal you only read does not fill the chat list.
    session_id UUID REFERENCES chat_sessions(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_campaign_appraisals_created ON campaign_appraisals (created_at DESC);

-- The red team proposes opposition and fog of its own: the attacks it names
-- and the unknowns it exploits. They arrive as proposals like everything
-- else, marked with where they came from.
ALTER TABLE campaign_items DROP CONSTRAINT IF EXISTS campaign_items_source_check;
ALTER TABLE campaign_items ADD CONSTRAINT campaign_items_source_check
    CHECK (source IN ('manual', 'chat', 'interrogation', 'red_team'));

-- +goose Down
UPDATE campaign_items SET source = 'chat' WHERE source = 'red_team';
ALTER TABLE campaign_items DROP CONSTRAINT IF EXISTS campaign_items_source_check;
ALTER TABLE campaign_items ADD CONSTRAINT campaign_items_source_check
    CHECK (source IN ('manual', 'chat', 'interrogation'));
DROP TABLE IF EXISTS campaign_appraisals;
