package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Vadich007/meetnotes/internal/domain"
)

// JobRepo — хранилище задач обработки. Здесь же живёт очередь: задачи разбираются
// воркерами через SELECT ... FOR UPDATE SKIP LOCKED, поэтому одну задачу
// не может взять больше одного воркера, даже если запущено несколько процессов.
type JobRepo struct{ db *DB }

// NewJobRepo создаёт репозиторий задач.
func NewJobRepo(db *DB) *JobRepo { return &JobRepo{db: db} }

const jobColumns = `id, meeting_id, status, attempts, max_attempts, last_error, locked_at, locked_by, next_attempt_at, created_at, updated_at`

// Create сохраняет задачу обработки.
func (r *JobRepo) Create(ctx context.Context, j domain.Job) error {
	const query = `
		INSERT INTO processing_jobs (id, meeting_id, status, attempts, max_attempts, last_error, next_attempt_at, created_at, updated_at)
		VALUES ($1, $2, $3, 0, $4, '', now(), now(), now())`

	_, err := r.db.Querier(ctx).Exec(ctx, query, j.ID, j.MeetingID, string(j.Status), j.MaxAttempts)
	if err != nil {
		return wrapError(fmt.Errorf("создание задачи обработки: %w", err))
	}
	return nil
}

// GetByMeetingID возвращает задачу по идентификатору встречи.
func (r *JobRepo) GetByMeetingID(ctx context.Context, meetingID uuid.UUID) (domain.Job, error) {
	query := `SELECT ` + jobColumns + ` FROM processing_jobs WHERE meeting_id = $1`

	row := r.db.Querier(ctx).QueryRow(ctx, query, meetingID)
	job, err := scanJob(row)
	if err != nil {
		return domain.Job{}, wrapError(fmt.Errorf("задача встречи %s: %w", meetingID, err))
	}
	return job, nil
}

// ChangeStatus переводит задачу в новый статус и пишет запись в историю.
// Условие status = $2 делает переход идемпотентным и защищает от гонки:
// если статус уже изменился, обновление не затронет ни одной строки.
// Вызывать нужно внутри транзакции — тогда статус и история меняются согласованно.
func (r *JobRepo) ChangeStatus(ctx context.Context, jobID uuid.UUID, from, to domain.JobStatus, errText string) error {
	const updateQuery = `
		UPDATE processing_jobs
		SET status = $3,
		    last_error = $4,
		    locked_at = CASE WHEN $3 IN ('completed', 'failed', 'created') THEN NULL ELSE locked_at END,
		    locked_by = CASE WHEN $3 IN ('completed', 'failed', 'created') THEN '' ELSE locked_by END,
		    updated_at = now()
		WHERE id = $1 AND status = $2`

	tag, err := r.db.Querier(ctx).Exec(ctx, updateQuery, jobID, string(from), string(to), errText)
	if err != nil {
		return wrapError(fmt.Errorf("смена статуса задачи %s: %w", jobID, err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: задача %s больше не в статусе %s", domain.ErrConflict, jobID, from)
	}

	const historyQuery = `
		INSERT INTO job_status_history (id, job_id, from_status, to_status, error)
		VALUES ($1, $2, $3, $4, $5)`

	if _, err := r.db.Querier(ctx).Exec(ctx, historyQuery,
		uuid.New(), jobID, string(from), string(to), errText); err != nil {
		return wrapError(fmt.Errorf("запись истории статусов задачи %s: %w", jobID, err))
	}
	return nil
}

// Fail переводит задачу в failed из любого рабочего статуса и сохраняет текст ошибки.
// Используется, когда обработка сорвалась и точный исходный статус не важен.
func (r *JobRepo) Fail(ctx context.Context, jobID uuid.UUID, errText string) error {
	const query = `
		UPDATE processing_jobs
		SET status = 'failed', last_error = $2, locked_at = NULL, locked_by = '', updated_at = now()
		WHERE id = $1 AND status <> 'completed'
		RETURNING status`

	var previous string
	if err := r.db.Querier(ctx).QueryRow(ctx, query, jobID, errText).Scan(&previous); err != nil {
		return wrapError(fmt.Errorf("перевод задачи %s в failed: %w", jobID, err))
	}

	const historyQuery = `
		INSERT INTO job_status_history (id, job_id, from_status, to_status, error)
		VALUES ($1, $2, $3, 'failed', $4)`

	if _, err := r.db.Querier(ctx).Exec(ctx, historyQuery, uuid.New(), jobID, previous, errText); err != nil {
		return wrapError(fmt.Errorf("запись истории статусов задачи %s: %w", jobID, err))
	}
	return nil
}

// Requeue возвращает задачу в очередь (команда retry): сбрасывает счётчик попыток и ошибку.
func (r *JobRepo) Requeue(ctx context.Context, jobID uuid.UUID) error {
	const query = `
		UPDATE processing_jobs
		SET status = 'created', attempts = 0, last_error = '', locked_at = NULL, locked_by = '',
		    next_attempt_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'failed'`

	tag, err := r.db.Querier(ctx).Exec(ctx, query, jobID)
	if err != nil {
		return wrapError(fmt.Errorf("возврат задачи %s в очередь: %w", jobID, err))
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: перезапустить можно только задачу в статусе failed", domain.ErrConflict)
	}

	const historyQuery = `
		INSERT INTO job_status_history (id, job_id, from_status, to_status, error)
		VALUES ($1, $2, 'failed', 'created', '')`

	if _, err := r.db.Querier(ctx).Exec(ctx, historyQuery, uuid.New(), jobID); err != nil {
		return wrapError(fmt.Errorf("запись истории статусов задачи %s: %w", jobID, err))
	}
	return nil
}

// Reschedule возвращает задачу в очередь после неудачной попытки, сохраняя счётчик
// попыток и текст ошибки, и откладывает следующий запуск на delay (экспоненциальный backoff).
func (r *JobRepo) Reschedule(ctx context.Context, jobID uuid.UUID, errText string, delay time.Duration) error {
	const query = `
		UPDATE processing_jobs
		SET status = 'created',
		    last_error = $2,
		    locked_at = NULL,
		    locked_by = '',
		    next_attempt_at = now() + $3::interval,
		    updated_at = now()
		WHERE id = $1 AND status IN ('processing', 'transcribed', 'summarized')
		RETURNING status`

	var previous string
	if err := r.db.Querier(ctx).QueryRow(ctx, query, jobID, errText, delay.String()).Scan(&previous); err != nil {
		return wrapError(fmt.Errorf("перепланирование задачи %s: %w", jobID, err))
	}

	const historyQuery = `
		INSERT INTO job_status_history (id, job_id, from_status, to_status, error)
		VALUES ($1, $2, $3, 'created', $4)`

	if _, err := r.db.Querier(ctx).Exec(ctx, historyQuery, uuid.New(), jobID, previous, errText); err != nil {
		return wrapError(fmt.Errorf("запись истории статусов задачи %s: %w", jobID, err))
	}
	return nil
}

// Claim атомарно забирает до limit задач в обработку и помечает их владельцем workerID.
// SKIP LOCKED пропускает строки, уже заблокированные другим воркером, — так две
// параллельные выборки никогда не вернут одну и ту же задачу.
func (r *JobRepo) Claim(ctx context.Context, workerID string, limit int) ([]domain.Job, error) {
	const query = `
		UPDATE processing_jobs
		SET status = 'processing',
		    attempts = attempts + 1,
		    locked_at = now(),
		    locked_by = $1,
		    updated_at = now()
		WHERE id IN (
		    SELECT id FROM processing_jobs
		    WHERE status = 'created' AND next_attempt_at <= now()
		    ORDER BY next_attempt_at, created_at
		    LIMIT $2
		    FOR UPDATE SKIP LOCKED
		)
		RETURNING ` + jobColumns

	rows, err := r.db.Querier(ctx).Query(ctx, query, workerID, limit)
	if err != nil {
		return nil, wrapError(fmt.Errorf("выборка задач из очереди: %w", err))
	}
	defer rows.Close()

	var jobs []domain.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, wrapError(fmt.Errorf("чтение задачи из очереди: %w", err))
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError(fmt.Errorf("выборка задач из очереди: %w", err))
	}
	return jobs, nil
}

// Release возвращает конкретную задачу в очередь, не увеличивая счётчик попыток.
// Вызывается при graceful shutdown для задач, которые не успели завершиться.
func (r *JobRepo) Release(ctx context.Context, jobID uuid.UUID) error {
	const query = `
		UPDATE processing_jobs
		SET status = 'created',
		    attempts = GREATEST(attempts - 1, 0),
		    locked_at = NULL,
		    locked_by = '',
		    next_attempt_at = now(),
		    updated_at = now()
		WHERE id = $1 AND status IN ('processing', 'transcribed', 'summarized')`

	if _, err := r.db.Querier(ctx).Exec(ctx, query, jobID); err != nil {
		return wrapError(fmt.Errorf("возврат задачи %s в очередь: %w", jobID, err))
	}
	return nil
}

// ReleaseStale возвращает в очередь задачи с истёкшим сроком аренды — например,
// после аварийного завершения процесса-воркера. Так система восстанавливается после перезапуска.
func (r *JobRepo) ReleaseStale(ctx context.Context, ttl time.Duration) (int64, error) {
	const query = `
		UPDATE processing_jobs
		SET status = 'created', locked_at = NULL, locked_by = '', next_attempt_at = now(), updated_at = now()
		WHERE status IN ('processing', 'transcribed', 'summarized')
		  AND locked_at IS NOT NULL
		  AND locked_at < now() - $1::interval`

	tag, err := r.db.Querier(ctx).Exec(ctx, query, ttl.String())
	if err != nil {
		return 0, wrapError(fmt.Errorf("восстановление зависших задач: %w", err))
	}
	return tag.RowsAffected(), nil
}

// History возвращает последние записи истории статусов задачи.
func (r *JobRepo) History(ctx context.Context, jobID uuid.UUID, limit int) ([]domain.StatusChange, error) {
	const query = `
		SELECT id, job_id, from_status, to_status, error, created_at
		FROM job_status_history
		WHERE job_id = $1
		ORDER BY created_at DESC
		LIMIT $2`

	rows, err := r.db.Querier(ctx).Query(ctx, query, jobID, limit)
	if err != nil {
		return nil, wrapError(fmt.Errorf("история статусов задачи %s: %w", jobID, err))
	}
	defer rows.Close()

	var history []domain.StatusChange
	for rows.Next() {
		var (
			ch             domain.StatusChange
			from, toStatus string
		)
		if err := rows.Scan(&ch.ID, &ch.JobID, &from, &toStatus, &ch.Error, &ch.CreatedAt); err != nil {
			return nil, wrapError(fmt.Errorf("чтение истории статусов: %w", err))
		}
		ch.From, ch.To = domain.JobStatus(from), domain.JobStatus(toStatus)
		history = append(history, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError(fmt.Errorf("история статусов задачи %s: %w", jobID, err))
	}
	return history, nil
}

// rowScanner объединяет pgx.Row и pgx.Rows — обе умеют Scan.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanJob(row rowScanner) (domain.Job, error) {
	var (
		job    domain.Job
		status string
	)
	err := row.Scan(&job.ID, &job.MeetingID, &status, &job.Attempts, &job.MaxAttempts,
		&job.LastError, &job.LockedAt, &job.LockedBy, &job.NextAttemptAt, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return domain.Job{}, err
	}
	job.Status = domain.JobStatus(status)
	return job, nil
}
