package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Vadich007/meetnotes/internal/domain"
)

// MeetingRepo — хранилище встреч.
type MeetingRepo struct{ db *DB }

// NewMeetingRepo создаёт репозиторий встреч.
func NewMeetingRepo(db *DB) *MeetingRepo { return &MeetingRepo{db: db} }

const meetingColumns = `m.id, m.user_id, m.title, m.original_name, m.stored_path, m.format, m.size_bytes, m.created_at`

// Create сохраняет встречу.
func (r *MeetingRepo) Create(ctx context.Context, m domain.Meeting) error {
	const query = `
		INSERT INTO meetings (id, user_id, title, original_name, stored_path, format, size_bytes, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now())`

	_, err := r.db.Querier(ctx).Exec(ctx, query,
		m.ID, m.UserID, m.Title, m.OriginalName, m.StoredPath, m.Format, m.SizeBytes)
	if err != nil {
		return wrapError(fmt.Errorf("сохранение встречи: %w", err))
	}
	return nil
}

// GetOwned возвращает встречу пользователя. Чужая встреча неотличима от несуществующей:
// в обоих случаях возвращается domain.ErrNotFound.
func (r *MeetingRepo) GetOwned(ctx context.Context, userID, meetingID uuid.UUID) (domain.Meeting, error) {
	query := `SELECT ` + meetingColumns + ` FROM meetings m WHERE m.id = $1 AND m.user_id = $2`

	var m domain.Meeting
	err := r.db.Querier(ctx).QueryRow(ctx, query, meetingID, userID).Scan(
		&m.ID, &m.UserID, &m.Title, &m.OriginalName, &m.StoredPath, &m.Format, &m.SizeBytes, &m.CreatedAt)
	if err != nil {
		return domain.Meeting{}, wrapError(fmt.Errorf("встреча %s: %w", meetingID, err))
	}
	return m, nil
}

// Get возвращает встречу без проверки владельца — используется фоновым воркером,
// который работает от имени системы, а не от имени пользователя.
func (r *MeetingRepo) Get(ctx context.Context, meetingID uuid.UUID) (domain.Meeting, error) {
	query := `SELECT ` + meetingColumns + ` FROM meetings m WHERE m.id = $1`

	var m domain.Meeting
	err := r.db.Querier(ctx).QueryRow(ctx, query, meetingID).Scan(
		&m.ID, &m.UserID, &m.Title, &m.OriginalName, &m.StoredPath, &m.Format, &m.SizeBytes, &m.CreatedAt)
	if err != nil {
		return domain.Meeting{}, wrapError(fmt.Errorf("встреча %s: %w", meetingID, err))
	}
	return m, nil
}

// List возвращает встречи пользователя вместе со статусом обработки и выжимкой.
func (r *MeetingRepo) List(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.MeetingListItem, error) {
	query := `
		SELECT ` + meetingColumns + `, j.status, COALESCE(s.text, '')
		FROM meetings m
		JOIN processing_jobs j ON j.meeting_id = m.id
		LEFT JOIN summaries s ON s.meeting_id = m.id
		WHERE m.user_id = $1
		ORDER BY m.created_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := r.db.Querier(ctx).Query(ctx, query, userID, limit, offset)
	if err != nil {
		return nil, wrapError(fmt.Errorf("список встреч: %w", err))
	}
	defer rows.Close()

	var items []domain.MeetingListItem
	for rows.Next() {
		var (
			it     domain.MeetingListItem
			status string
		)
		if err := rows.Scan(
			&it.Meeting.ID, &it.Meeting.UserID, &it.Meeting.Title, &it.Meeting.OriginalName,
			&it.Meeting.StoredPath, &it.Meeting.Format, &it.Meeting.SizeBytes, &it.Meeting.CreatedAt,
			&status, &it.Summary,
		); err != nil {
			return nil, wrapError(fmt.Errorf("чтение строки списка встреч: %w", err))
		}
		it.Status = domain.JobStatus(status)
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError(fmt.Errorf("список встреч: %w", err))
	}
	return items, nil
}

// Delete удаляет встречу пользователя. Связанные задачи, транскрипции, выжимки и история
// удаляются каскадом (ON DELETE CASCADE). Возвращает путь к файлу, чтобы вызывающий код
// мог удалить его с диска после успешного коммита транзакции.
func (r *MeetingRepo) Delete(ctx context.Context, userID, meetingID uuid.UUID) (string, error) {
	const query = `DELETE FROM meetings WHERE id = $1 AND user_id = $2 RETURNING stored_path`

	var storedPath string
	err := r.db.Querier(ctx).QueryRow(ctx, query, meetingID, userID).Scan(&storedPath)
	if err != nil {
		return "", wrapError(fmt.Errorf("удаление встречи %s: %w", meetingID, err))
	}
	return storedPath, nil
}
