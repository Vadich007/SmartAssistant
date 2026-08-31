// Package service содержит бизнес-логику приложения.
package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Vadich007/meetnotes/internal/domain"
)

// TranscribeRequest запрос на распознавание файла встречи.
type TranscribeRequest struct {
	MeetingID uuid.UUID
	FilePath  string
	Format    string
	Language  string
}

// TranscribeResult результат распознавания.
type TranscribeResult struct {
	Text        string
	Language    string
	DurationSec int
	Provider    string
}

// SpeechClient абстрактный клиент распознавания речи.
type SpeechClient interface {
	Transcribe(ctx context.Context, req TranscribeRequest) (TranscribeResult, error)
	Name() string
}

// SummaryResult краткая выжимка по встрече.
type SummaryResult struct {
	Text     string
	Provider string
	Model    string
}

// MeetingContext материалы одной встречи.
type MeetingContext struct {
	MeetingID  uuid.UUID
	Title      string
	CreatedAt  time.Time
	Transcript string
	Summary    string
}

// AnswerResult ответ модели на вопрос пользователя.
type AnswerResult struct {
	Text     string
	Provider string
	Model    string
}

// LLMClient абстрактный клиент языковой модели.
type LLMClient interface {
	Summarize(ctx context.Context, transcript string) (SummaryResult, error)
	Answer(ctx context.Context, question string, contexts []MeetingContext) (AnswerResult, error)
	Name() string
}

// TxManager выполняет функцию в транзакции.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// UserRepository хранилище пользователей.
type UserRepository interface {
	Ensure(ctx context.Context, externalID string) (domain.User, bool, error)
	GetByExternalID(ctx context.Context, externalID string) (domain.User, error)
}

// MeetingRepository хранилище встреч.
type MeetingRepository interface {
	Create(ctx context.Context, m domain.Meeting) error
	// GetOwned возвращает встречу только если она принадлежит пользователю.
	GetOwned(ctx context.Context, userID, meetingID uuid.UUID) (domain.Meeting, error)
	// Get возвращает встречу без проверки владельца (только для фоновой обработки).
	Get(ctx context.Context, meetingID uuid.UUID) (domain.Meeting, error)
	List(ctx context.Context, userID uuid.UUID, limit, offset int) ([]domain.MeetingListItem, error)
	Delete(ctx context.Context, userID, meetingID uuid.UUID) (string, error)
}

// JobRepository хранилище задач обработки и очередь для воркеров.
type JobRepository interface {
	Create(ctx context.Context, j domain.Job) error
	GetByMeetingID(ctx context.Context, meetingID uuid.UUID) (domain.Job, error)
	ChangeStatus(ctx context.Context, jobID uuid.UUID, from, to domain.JobStatus, errText string) error
	Fail(ctx context.Context, jobID uuid.UUID, errText string) error
	Requeue(ctx context.Context, jobID uuid.UUID) error
	Reschedule(ctx context.Context, jobID uuid.UUID, errText string, delay time.Duration) error
	Claim(ctx context.Context, workerID string, limit int) ([]domain.Job, error)
	Release(ctx context.Context, jobID uuid.UUID) error
	ReleaseStale(ctx context.Context, ttl time.Duration) (int64, error)
	History(ctx context.Context, jobID uuid.UUID, limit int) ([]domain.StatusChange, error)
}

// TranscriptRepository хранилище расшифровок.
type TranscriptRepository interface {
	Save(ctx context.Context, t domain.Transcript) error
	GetByMeetingID(ctx context.Context, meetingID uuid.UUID) (domain.Transcript, error)
}

// SummaryRepository хранилище кратких выжимок.
type SummaryRepository interface {
	Save(ctx context.Context, s domain.Summary) error
	GetByMeetingID(ctx context.Context, meetingID uuid.UUID) (domain.Summary, error)
}

// QARepository история вопросов и ответов.
type QARepository interface {
	Add(ctx context.Context, e domain.QAEntry) error
	ListByUser(ctx context.Context, userID uuid.UUID, limit int) ([]domain.QAEntry, error)
}

// SearchRepository поиск по сохранённым материалам пользователя.
type SearchRepository interface {
	Search(ctx context.Context, userID uuid.UUID, keyword string, limit int) ([]domain.SearchResult, error)
}

// StatsRepository диагностические метрики.
type StatsRepository interface {
	Collect(ctx context.Context, userID uuid.UUID) (domain.Stats, error)
}

// FileStore файловое хранилище загруженных встреч.
type FileStore interface {
	Save(ctx context.Context, srcPath string) (string, int64, error)
	Remove(path string) error
}
