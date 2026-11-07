-- +goose Up
-- Settings typed into a client: provider, keys, models, tiers, check-ins.
-- The environment still wins over anything here, and only keys the server's
-- catalogue exposes are ever read back.
CREATE TABLE settings (
    key        VARCHAR(64) PRIMARY KEY CHECK (key ~ '^[A-Z][A-Z0-9_]*$'),
    -- Plain text for ordinary settings. A secret is AES-256-GCM sealed in the
    -- server under SETTINGS_KEY, which is never stored here and never sent to
    -- the database: lose the key and the stored secrets are gone, not leaked.
    value      VARCHAR(4096) NOT NULL DEFAULT '',
    secret     BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT settings_secret_sealed CHECK (NOT secret OR value LIKE 'v1:%')
);

-- +goose Down
DROP TABLE IF EXISTS settings;
