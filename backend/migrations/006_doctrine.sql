-- +goose Up
-- Vectors for doctrine passages. The passages themselves live in the embedded
-- markdown; this only caches what they cost to embed. The column has no fixed
-- dimension because nothing searches it in SQL: the whole roster is a couple of
-- hundred rows, loaded into memory and compared there.
CREATE TABLE doctrine_embeddings (
    content_hash    TEXT NOT NULL,
    embedding_model TEXT NOT NULL,
    embedding       vector NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (content_hash, embedding_model)
);

-- +goose Down
DROP TABLE IF EXISTS doctrine_embeddings;
