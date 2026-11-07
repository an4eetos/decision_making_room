-- +goose Up
-- The room is on the internet once it is hosted, so it has a password and
-- sessions. One owner, deliberately: one deployment per person is the hosting
-- model, and accounts are a different product.

-- At most one row: the CHECK on id makes a second owner impossible rather
-- than merely unlikely.
CREATE TABLE auth_owner (
    id            SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    -- A PHC string, so the parameters can be raised later without a migration.
    password_hash VARCHAR(256) NOT NULL CHECK (password_hash LIKE '$argon2id$%'),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per signed-in device. Only the SHA-256 of the token is stored: a
-- leaked table cannot be replayed as a login.
CREATE TABLE auth_sessions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash   BYTEA NOT NULL UNIQUE CHECK (octet_length(token_hash) = 32),
    -- The device's name as the client gave it, shown in the sessions list.
    client       VARCHAR(80) NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Slides forward as the session is used; an idle device signs itself out.
    expires_at   TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_auth_sessions_expires_at ON auth_sessions (expires_at);

-- +goose Down
DROP TABLE IF EXISTS auth_sessions;
DROP TABLE IF EXISTS auth_owner;
