package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Vadich007/meetnotes/internal/domain"
)

// TranscriptRepo — хранилище расшифровок встреч.
type TranscriptRepo struct{ db *DB }

// NewTranscriptRepo создаёт репозиторий расшифровок.
func NewTranscriptRepo(db *DB) *TranscriptRepo { return &TranscriptRepo{db: db} }

// Save сохраняет расшифровку. Повторная обработка встречи (retry) перезаписывает результат.
func (r *TranscriptRepo) Save(ctx context.Context, t domain.Transcript) error {
	const query = `
		INSERT INTO transcripts (id, meeting_id, text, language, duration_sec, provider)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (meeting_id) DO UPDATE
		SET text = EXCLUDED.text,
		    language = EXCLUDED.language,
		    duration_sec = EXCLUDED.duration_sec,
		    provider = EXCLUDED.provider,
		    created_at = now()`

	_, err := r.db.Querier(ctx).Exec(ctx, query, t.ID, t.MeetingID, t.Text, t.Language, t.DurationSec, t.Provider)
	if err != nil {
		return wrapError(fmt.Errorf("сохранение транскрипции: %w", err))
	}
	return nil
}

// GetByMeetingID возвращает расшифровку встречи.
func (r *TranscriptRepo) GetByMeetingID(ctx context.Context, meetingID uuid.UUID) (domain.Transcript, error) {
	const query = `
		SELECT id, meeting_id, text, language, duration_sec, provider, created_at
		FROM transcripts WHERE meeting_id = $1`

	var t domain.Transcript
	err := r.db.Querier(ctx).QueryRow(ctx, query, meetingID).Scan(
		&t.ID, &t.MeetingID, &t.Text, &t.Language, &t.DurationSec, &t.Provider, &t.CreatedAt)
	if err != nil {
		return domain.Transcript{}, wrapError(fmt.Errorf("транскрипция встречи %s: %w", meetingID, err))
	}
	return t, nil
}

// SummaryRepo — хранилище кратких выжимок.
type SummaryRepo struct{ db *DB }

// NewSummaryRepo создаёт репозиторий выжимок.
func NewSummaryRepo(db *DB) *SummaryRepo { return &SummaryRepo{db: db} }

// Save сохраняет краткую выжимку встречи.
func (r *SummaryRepo) Save(ctx context.Context, s domain.Summary) error {
	const query = `
		INSERT INTO summaries (id, meeting_id, text, provider, model)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (meeting_id) DO UPDATE
		SET text = EXCLUDED.text,
		    provider = EXCLUDED.provider,
		    model = EXCLUDED.model,
		    created_at = now()`

	_, err := r.db.Querier(ctx).Exec(ctx, query, s.ID, s.MeetingID, s.Text, s.Provider, s.Model)
	if err != nil {
		return wrapError(fmt.Errorf("сохранение краткой выжимки: %w", err))
	}
	return nil
}

// GetByMeetingID возвращает выжимку встречи.
func (r *SummaryRepo) GetByMeetingID(ctx context.Context, meetingID uuid.UUID) (domain.Summary, error) {
	const query = `SELECT id, meeting_id, text, provider, model, created_at FROM summaries WHERE meeting_id = $1`

	var s domain.Summary
	err := r.db.Querier(ctx).QueryRow(ctx, query, meetingID).Scan(
		&s.ID, &s.MeetingID, &s.Text, &s.Provider, &s.Model, &s.CreatedAt)
	if err != nil {
		return domain.Summary{}, wrapError(fmt.Errorf("выжимка встречи %s: %w", meetingID, err))
	}
	return s, nil
}

// QARepo — история вопросов и ответов по материалам встреч.
type QARepo struct{ db *DB }

// NewQARepo создаёт репозиторий истории вопросов.
func NewQARepo(db *DB) *QARepo { return &QARepo{db: db} }

// Add сохраняет вопрос пользователя и ответ модели.
func (r *QARepo) Add(ctx context.Context, e domain.QAEntry) error {
	const query = `
		INSERT INTO qa_history (id, user_id, meeting_id, question, answer)
		VALUES ($1, $2, $3, $4, $5)`

	_, err := r.db.Querier(ctx).Exec(ctx, query, e.ID, e.UserID, e.MeetingID, e.Question, e.Answer)
	if err != nil {
		return wrapError(fmt.Errorf("сохранение истории вопросов: %w", err))
	}
	return nil
}

// ListByUser возвращает последние вопросы пользователя.
func (r *QARepo) ListByUser(ctx context.Context, userID uuid.UUID, limit int) ([]domain.QAEntry, error) {
	const query = `
		SELECT id, user_id, meeting_id, question, answer, created_at
		FROM qa_history WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2`

	rows, err := r.db.Querier(ctx).Query(ctx, query, userID, limit)
	if err != nil {
		return nil, wrapError(fmt.Errorf("история вопросов: %w", err))
	}
	defer rows.Close()

	var entries []domain.QAEntry
	for rows.Next() {
		var e domain.QAEntry
		if err := rows.Scan(&e.ID, &e.UserID, &e.MeetingID, &e.Question, &e.Answer, &e.CreatedAt); err != nil {
			return nil, wrapError(fmt.Errorf("чтение истории вопросов: %w", err))
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError(fmt.Errorf("история вопросов: %w", err))
	}
	return entries, nil
}
