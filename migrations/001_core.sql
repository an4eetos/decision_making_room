-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- kind is TEXT + CHECK, deliberately not an ENUM: `ALTER TYPE ... ADD VALUE`
-- cannot run inside a transaction, which makes adding a kind an upgrade
-- footgun for self-hosted users running goose.
CREATE TABLE memories (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    kind            TEXT NOT NULL
                    CHECK (kind IN ('decision', 'plan', 'note', 'daily_log')),
    title           TEXT NOT NULL DEFAULT '',
    body            TEXT NOT NULL,
    tags            TEXT[] NOT NULL DEFAULT '{}',
    metadata        JSONB NOT NULL DEFAULT '{}',
    embedding       vector(768),
    embedding_model TEXT NOT NULL DEFAULT '',
    embedding_dim   INT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Two text-search vectors. 'english' stems and drops stopwords; 'simple'
    -- does neither, which is what catches proper nouns, project codenames and
    -- transliterated words that the snowball stemmer mangles.
    search_vector        tsvector GENERATED ALWAYS AS (
        to_tsvector('english', coalesce(title, '') || ' ' || coalesce(body, ''))
    ) STORED,
    search_vector_simple tsvector GENERATED ALWAYS AS (
        to_tsvector('simple',  coalesce(title, '') || ' ' || coalesce(body, ''))
    ) STORED
);

CREATE INDEX idx_memories_kind       ON memories(kind);
CREATE INDEX idx_memories_created_at ON memories(created_at DESC);
CREATE INDEX idx_memories_tags       ON memories USING GIN (tags);
CREATE INDEX idx_memories_fts_en     ON memories USING GIN (search_vector);
CREATE INDEX idx_memories_fts_simple ON memories USING GIN (search_vector_simple);
CREATE INDEX idx_memories_title_trgm ON memories USING GIN (title gin_trgm_ops);
CREATE INDEX idx_memories_embedding  ON memories USING hnsw (embedding vector_cosine_ops);

CREATE INDEX idx_memories_embedding_model ON memories(embedding_model)
    WHERE embedding IS NOT NULL;

CREATE INDEX idx_memories_source_path ON memories ((metadata->>'source_path'))
    WHERE metadata->>'source_path' IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS memories;
