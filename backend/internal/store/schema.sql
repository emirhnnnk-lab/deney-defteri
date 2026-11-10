-- Idempotent schema: safe to run on every start.

CREATE TABLE IF NOT EXISTS experiments (
    id          BIGSERIAL PRIMARY KEY,
    slug        TEXT UNIQUE,                 -- stable id used by seeds and the usage log ("deney")
    title       TEXT NOT NULL CHECK (length(title) BETWEEN 1 AND 200),
    prediction  TEXT NOT NULL,               -- written before the run
    action      TEXT NOT NULL DEFAULT '',    -- what was actually done
    result      TEXT NOT NULL DEFAULT '',    -- what happened, with numbers
    lesson      TEXT NOT NULL DEFAULT '',
    outcome     TEXT NOT NULL DEFAULT 'pending'
                CHECK (outcome IN ('pending', 'confirmed', 'refuted', 'inconclusive')),
    source      TEXT NOT NULL DEFAULT '',    -- where the evidence lives (repo, file)
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS usage_events (
    id                BIGSERIAL PRIMARY KEY,
    source_hash       TEXT NOT NULL UNIQUE,  -- sha256 of the raw log line: re-import is a no-op
    task              TEXT NOT NULL,
    provider          TEXT NOT NULL,
    model             TEXT NOT NULL,
    success           BOOLEAN NOT NULL,
    duration_sec      DOUBLE PRECISION CHECK (duration_sec >= 0),
    input_chars       INTEGER,
    output_chars      INTEGER,
    prompt_tokens     INTEGER,
    completion_tokens INTEGER,
    error             TEXT NOT NULL DEFAULT '',
    experiment        TEXT NOT NULL DEFAULT '',
    correct           BOOLEAN,
    occurred_at       TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS usage_events_model_idx      ON usage_events (model);
CREATE INDEX IF NOT EXISTS usage_events_experiment_idx ON usage_events (experiment);
