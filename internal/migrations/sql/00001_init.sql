-- +goose Up
-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS users (
    id          uuid PRIMARY KEY,
    external_id text        NOT NULL UNIQUE,
    created_at  timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS meetings (
    id            uuid PRIMARY KEY,
    user_id       uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title         text        NOT NULL,
    original_name text        NOT NULL,
    stored_path   text        NOT NULL,
    format        text        NOT NULL,
    size_bytes    bigint      NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS processing_jobs (
    id           uuid PRIMARY KEY,
    meeting_id   uuid        NOT NULL UNIQUE REFERENCES meetings (id) ON DELETE CASCADE,
    status       text        NOT NULL,
    attempts     integer     NOT NULL DEFAULT 0,
    max_attempts integer     NOT NULL DEFAULT 3,
    last_error   text        NOT NULL DEFAULT '',
    locked_at    timestamptz,
    locked_by    text        NOT NULL DEFAULT '',
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT processing_jobs_status_check CHECK (
        status IN ('created', 'processing', 'transcribed', 'summarized', 'completed', 'failed')
    )
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS job_status_history (
    id          uuid PRIMARY KEY,
    job_id      uuid        NOT NULL REFERENCES processing_jobs (id) ON DELETE CASCADE,
    from_status text        NOT NULL,
    to_status   text        NOT NULL,
    error       text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS transcripts (
    id           uuid PRIMARY KEY,
    meeting_id   uuid        NOT NULL UNIQUE REFERENCES meetings (id) ON DELETE CASCADE,
    text         text        NOT NULL,
    language     text        NOT NULL DEFAULT 'ru',
    duration_sec integer     NOT NULL DEFAULT 0,
    provider     text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    search_vector tsvector GENERATED ALWAYS AS (to_tsvector('russian', text)) STORED
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS summaries (
    id         uuid PRIMARY KEY,
    meeting_id uuid        NOT NULL UNIQUE REFERENCES meetings (id) ON DELETE CASCADE,
    text       text        NOT NULL,
    provider   text        NOT NULL,
    model      text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    search_vector tsvector GENERATED ALWAYS AS (to_tsvector('russian', text)) STORED
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE IF NOT EXISTS qa_history (
    id         uuid PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    meeting_id uuid        REFERENCES meetings (id) ON DELETE CASCADE,
    question   text        NOT NULL,
    answer     text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS qa_history;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS summaries;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS transcripts;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS job_status_history;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS processing_jobs;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS meetings;
-- +goose StatementEnd
-- +goose StatementBegin
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
