-- +goose Up
-- The room's recommendation to be interrogated, stored with the answer that
-- made it: the banner survives a reload, and the cooldown that stops it nagging
-- reads the recent ones back. NULL on every answer that suggested nothing.
ALTER TABLE chat_messages ADD COLUMN suggestion JSONB;

-- +goose Down
ALTER TABLE chat_messages DROP COLUMN IF EXISTS suggestion;
