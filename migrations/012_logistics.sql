-- +goose Up
-- Logistics: what supplies each front. Hours come from the sorties you log,
-- energy from the one tap at the evening check-in, money from the funds you
-- set against a front and the costs drawn on it. Every figure here is one you
-- reported; nothing is inferred from what you write.

-- A sortie is one focus block spent on a front, optionally on one order.
CREATE TABLE sorties (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    front_id   UUID NOT NULL REFERENCES fronts(id) ON DELETE CASCADE,
    -- The order it was spent on, if any. The sortie outlives the order.
    order_id   UUID REFERENCES commitments(id) ON DELETE SET NULL,
    minutes    INT  NOT NULL CHECK (minutes BETWEEN 1 AND 720),
    -- The local day it was flown, so a block logged after midnight can still
    -- belong to the day it was worked.
    day        DATE NOT NULL,
    note       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sorties_day ON sorties (day DESC);
CREATE INDEX idx_sorties_front_day ON sorties (front_id, day DESC);

-- Energy, one reading per local day, reported in one tap. A second tap the
-- same day replaces the first.
CREATE TABLE energy_reports (
    day         DATE PRIMARY KEY,
    level       SMALLINT NOT NULL CHECK (level BETWEEN 1 AND 5),
    reported_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Funds a front can draw on. Yours, so never an estimate; a currency is
-- required with an amount, and amounts in different currencies are never
-- summed.
ALTER TABLE fronts
    ADD COLUMN funds NUMERIC CHECK (funds IS NULL OR funds >= 0),
    ADD COLUMN funds_currency TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT fronts_funds_currency_check
        CHECK (funds IS NULL OR funds_currency ~ '^[A-Z]{3}$');

-- What taking an objective will cost, as you set it.
ALTER TABLE campaign_items
    ADD COLUMN cost NUMERIC CHECK (cost IS NULL OR cost >= 0),
    ADD COLUMN cost_currency TEXT NOT NULL DEFAULT '',
    ADD CONSTRAINT campaign_items_cost_objective_check
        CHECK (cost IS NULL OR (type = 'objective' AND cost_currency ~ '^[A-Z]{3}$'));

-- A stay deployed on a front: a forward base whose setup budget draws on the
-- front's funds and whose readiness the staff reads before it lands.
ALTER TABLE relocation_plans
    ADD COLUMN front_id UUID REFERENCES fronts(id) ON DELETE SET NULL;

CREATE INDEX idx_relocation_plans_front ON relocation_plans (front_id) WHERE front_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_relocation_plans_front;
ALTER TABLE relocation_plans DROP COLUMN IF EXISTS front_id;
ALTER TABLE campaign_items
    DROP CONSTRAINT IF EXISTS campaign_items_cost_objective_check,
    DROP COLUMN IF EXISTS cost_currency,
    DROP COLUMN IF EXISTS cost;
ALTER TABLE fronts
    DROP CONSTRAINT IF EXISTS fronts_funds_currency_check,
    DROP COLUMN IF EXISTS funds_currency,
    DROP COLUMN IF EXISTS funds;
DROP TABLE IF EXISTS energy_reports;
DROP TABLE IF EXISTS sorties;
