-- +goose Up
CREATE TABLE relocation_plans (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    destination  TEXT NOT NULL,
    country_code TEXT NOT NULL DEFAULT '',
    arrive_on    DATE,
    depart_on    DATE,
    nights       INT  NOT NULL CHECK (nights > 0),
    party_size   INT  NOT NULL DEFAULT 1 CHECK (party_size > 0),
    housing      TEXT NOT NULL DEFAULT ''
                 CHECK (housing IN ('', 'hotel', 'serviced', 'furnished', 'unfurnished', 'shared')),
    climate      TEXT NOT NULL DEFAULT ''
                 CHECK (climate IN ('', 'tropical', 'temperate', 'cold', 'arid')),
    budget_style TEXT NOT NULL DEFAULT 'standard'
                 CHECK (budget_style IN ('frugal', 'standard', 'comfortable')),
    currency     TEXT NOT NULL DEFAULT 'USD',
    status       TEXT NOT NULL DEFAULT 'draft'
                 CHECK (status IN ('draft', 'active', 'done', 'abandoned')),
    notes        TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE relocation_items (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id    UUID NOT NULL REFERENCES relocation_plans(id) ON DELETE CASCADE,
    catalog_id TEXT NOT NULL DEFAULT '',
    category   TEXT NOT NULL,
    name       TEXT NOT NULL,
    quantity   NUMERIC NOT NULL DEFAULT 1 CHECK (quantity > 0),
    unit       TEXT NOT NULL DEFAULT '',

    -- Nullable on purpose. An unknown price must not contribute zero to a budget,
    -- and cost_source records whether a figure came from reality or from a guess.
    unit_cost   NUMERIC,
    currency    TEXT NOT NULL DEFAULT 'USD',
    cost_source TEXT NOT NULL DEFAULT 'catalog'
                CHECK (cost_source IN ('catalog', 'model', 'history', 'user')),
    confidence  REAL CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),

    status TEXT NOT NULL DEFAULT 'needed'
           CHECK (status IN ('needed', 'have', 'bought', 'skipped')),
    note               TEXT NOT NULL DEFAULT '',
    commonly_forgotten BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order         INT NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One line per catalogue entry per plan, so rebuilding a plan updates rows
-- rather than duplicating them. Manual lines have an empty catalog_id and are
-- excluded from the constraint.
CREATE UNIQUE INDEX uq_relocation_items_plan_catalog
    ON relocation_items(plan_id, catalog_id) WHERE catalog_id <> '';
CREATE INDEX idx_relocation_items_plan ON relocation_items(plan_id, sort_order);

CREATE TABLE relocation_pitfalls (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id         UUID NOT NULL REFERENCES relocation_plans(id) ON DELETE CASCADE,
    pitfall_id      TEXT NOT NULL,
    title           TEXT NOT NULL,
    body            TEXT NOT NULL,
    severity        TEXT NOT NULL CHECK (severity IN ('critical', 'costly', 'annoying')),
    action          TEXT NOT NULL DEFAULT '',
    acknowledged_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX uq_relocation_pitfalls_plan ON relocation_pitfalls(plan_id, pitfall_id);

-- What you actually paid, so the second stay in a city is priced from experience
-- instead of from a guess.
CREATE TABLE price_observations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    catalog_id  TEXT NOT NULL,
    destination TEXT NOT NULL,
    unit_cost   NUMERIC NOT NULL CHECK (unit_cost >= 0),
    currency    TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_price_observations_lookup
    ON price_observations(catalog_id, lower(destination), observed_at DESC);

-- +goose Down
DROP TABLE IF EXISTS price_observations;
DROP TABLE IF EXISTS relocation_pitfalls;
DROP TABLE IF EXISTS relocation_items;
DROP TABLE IF EXISTS relocation_plans;
