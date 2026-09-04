package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Vadich007/meetnotes/internal/domain"
)

// StatsRepo считает диагностические метрики по встречам пользователя.
type StatsRepo struct{ db *DB }

// NewStatsRepo создаёт репозиторий статистики.
func NewStatsRepo(db *DB) *StatsRepo { return &StatsRepo{db: db} }

// Collect возвращает распределение задач по статусам, число встреч,
// среднее время обработки и количество ошибок.
func (r *StatsRepo) Collect(ctx context.Context, userID uuid.UUID) (domain.Stats, error) {
	const byStatusQuery = `
		SELECT j.status, count(*)
		FROM processing_jobs j
		JOIN meetings m ON m.id = j.meeting_id
		WHERE m.user_id = $1
		GROUP BY j.status`

	rows, err := r.db.Querier(ctx).Query(ctx, byStatusQuery, userID)
	if err != nil {
		return domain.Stats{}, wrapError(fmt.Errorf("статистика по статусам: %w", err))
	}
	defer rows.Close()

	counts := make(map[domain.JobStatus]int)
	for rows.Next() {
		var (
			status string
			count  int
		)
		if err := rows.Scan(&status, &count); err != nil {
			return domain.Stats{}, wrapError(fmt.Errorf("чтение статистики: %w", err))
		}
		counts[domain.JobStatus(status)] = count
	}
	if err := rows.Err(); err != nil {
		return domain.Stats{}, wrapError(fmt.Errorf("статистика по статусам: %w", err))
	}

	stats := domain.Stats{}
	for _, status := range domain.AllStatuses() {
		count := counts[status]
		stats.ByStatus = append(stats.ByStatus, domain.StatsRow{Status: status, Count: count})
		stats.TotalMeetings += count
		if status == domain.StatusFailed {
			stats.FailedCount = count
		}
	}

	// Среднее время обработки считаем как разницу между созданием задачи
	// и моментом её перехода в completed.
	const avgQuery = `
		SELECT COALESCE(avg(EXTRACT(EPOCH FROM (j.updated_at - j.created_at))), 0)
		FROM processing_jobs j
		JOIN meetings m ON m.id = j.meeting_id
		WHERE m.user_id = $1 AND j.status = 'completed'`

	if err := r.db.Querier(ctx).QueryRow(ctx, avgQuery, userID).Scan(&stats.AvgProcessingSec); err != nil {
		return domain.Stats{}, wrapError(fmt.Errorf("среднее время обработки: %w", err))
	}
	return stats, nil
}
