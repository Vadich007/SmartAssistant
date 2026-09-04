package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Vadich007/meetnotes/internal/domain"
)

// SearchRepo — полнотекстовый поиск по материалам встреч.
type SearchRepo struct{ db *DB }

// NewSearchRepo создаёт репозиторий поиска.
func NewSearchRepo(db *DB) *SearchRepo { return &SearchRepo{db: db} }

// ftsQuery ищет по индексам GIN на сгенерированных tsvector-колонках.
// Для каждой встречи берётся лучшее совпадение (транскрипция или выжимка),
// результат ранжируется ts_rank, фрагмент подсвечивается ts_headline.
// Фильтр по user_id стоит в самом запросе — чужие встречи в выдачу не попадают.
const ftsQuery = `
	WITH q AS (
		SELECT websearch_to_tsquery('russian', $2) AS tsq
	), matches AS (
		SELECT t.meeting_id,
		       ts_rank(t.search_vector, q.tsq) AS rank,
		       ts_headline('russian', t.text, q.tsq,
		                   'MaxWords=28, MinWords=8, ShortWord=2, MaxFragments=1') AS snippet,
		       'transcript' AS matched_in
		FROM transcripts t, q
		WHERE t.search_vector @@ q.tsq
		UNION ALL
		SELECT s.meeting_id,
		       ts_rank(s.search_vector, q.tsq) AS rank,
		       ts_headline('russian', s.text, q.tsq,
		                   'MaxWords=28, MinWords=8, ShortWord=2, MaxFragments=1') AS snippet,
		       'summary' AS matched_in
		FROM summaries s, q
		WHERE s.search_vector @@ q.tsq
	), best AS (
		SELECT DISTINCT ON (meeting_id) meeting_id, rank, snippet, matched_in
		FROM matches
		ORDER BY meeting_id, rank DESC
	)
	SELECT ` + meetingColumns + `, j.status, b.snippet, b.rank, b.matched_in
	FROM best b
	JOIN meetings m ON m.id = b.meeting_id
	JOIN processing_jobs j ON j.meeting_id = m.id
	WHERE m.user_id = $1
	ORDER BY b.rank DESC, m.created_at DESC
	LIMIT $3`

// likeQuery — запасной вариант на случай, когда полнотекстовый запрос ничего не нашёл
// (например, ищут часть слова или латиницу при русской конфигурации словаря).
const likeQuery = `
	WITH matches AS (
		SELECT t.meeting_id, t.text AS body, 'transcript' AS matched_in
		FROM transcripts t
		WHERE t.text ILIKE '%' || $2 || '%'
		UNION ALL
		SELECT s.meeting_id, s.text, 'summary'
		FROM summaries s
		WHERE s.text ILIKE '%' || $2 || '%'
	), best AS (
		SELECT DISTINCT ON (meeting_id) meeting_id, body, matched_in
		FROM matches
		ORDER BY meeting_id, matched_in
	)
	SELECT ` + meetingColumns + `, j.status, left(b.body, 240), 0::real, b.matched_in
	FROM best b
	JOIN meetings m ON m.id = b.meeting_id
	JOIN processing_jobs j ON j.meeting_id = m.id
	WHERE m.user_id = $1
	ORDER BY m.created_at DESC
	LIMIT $3`

// Search выполняет поиск по встречам пользователя.
func (r *SearchRepo) Search(ctx context.Context, userID uuid.UUID, keyword string, limit int) ([]domain.SearchResult, error) {
	results, err := r.search(ctx, ftsQuery, userID, keyword, limit)
	if err != nil {
		return nil, err
	}
	if len(results) > 0 {
		return results, nil
	}
	return r.search(ctx, likeQuery, userID, keyword, limit)
}

func (r *SearchRepo) search(ctx context.Context, query string, userID uuid.UUID, keyword string, limit int) ([]domain.SearchResult, error) {
	rows, err := r.db.Querier(ctx).Query(ctx, query, userID, keyword, limit)
	if err != nil {
		return nil, wrapError(fmt.Errorf("поиск по встречам: %w", err))
	}
	defer rows.Close()

	var results []domain.SearchResult
	for rows.Next() {
		var (
			res    domain.SearchResult
			status string
			rank   float32
		)
		if err := rows.Scan(
			&res.Meeting.ID, &res.Meeting.UserID, &res.Meeting.Title, &res.Meeting.OriginalName,
			&res.Meeting.StoredPath, &res.Meeting.Format, &res.Meeting.SizeBytes, &res.Meeting.CreatedAt,
			&status, &res.Snippet, &rank, &res.MatchedIn,
		); err != nil {
			return nil, wrapError(fmt.Errorf("чтение результата поиска: %w", err))
		}
		res.Status = domain.JobStatus(status)
		res.Rank = float64(rank)
		results = append(results, res)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapError(fmt.Errorf("поиск по встречам: %w", err))
	}
	return results, nil
}
