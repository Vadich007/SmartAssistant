-- +goose Up
-- Список встреч пользователя (команда list) — самый частый запрос.
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_meetings_user_created ON meetings (user_id, created_at DESC);
-- +goose StatementEnd

-- Выборка очереди воркером: задачи в статусе created, готовые к запуску, самые старые первыми.
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_jobs_status_next_attempt ON processing_jobs (status, next_attempt_at, created_at);
-- +goose StatementEnd

-- Восстановление задач с истёкшим lease: processing + старый locked_at.
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_jobs_status_locked_at ON processing_jobs (status, locked_at);
-- +goose StatementEnd

-- История статусов конкретной задачи в обратном хронологическом порядке.
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_job_history_job_created ON job_status_history (job_id, created_at DESC);
-- +goose StatementEnd

-- Полнотекстовый поиск по транскрипциям и выжимкам.
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_transcripts_search ON transcripts USING GIN (search_vector);
-- +goose StatementEnd
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_summaries_search ON summaries USING GIN (search_vector);
-- +goose StatementEnd

-- История вопросов пользователя.
-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_qa_user_created ON qa_history (user_id, created_at DESC);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_qa_user_created;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_summaries_search;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_transcripts_search;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_job_history_job_created;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_jobs_status_locked_at;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_jobs_status_next_attempt;
-- +goose StatementEnd
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_meetings_user_created;
-- +goose StatementEnd
