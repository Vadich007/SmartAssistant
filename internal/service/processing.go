package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/Vadich007/meetnotes/internal/domain"
)

const (
	// dbWriteTimeout запас времени на запись результата, когда основной context уже истёк.
	dbWriteTimeout = 5 * time.Second
	// maxRetryDelay ограничивает экспоненциальный backoff.
	maxRetryDelay = 5 * time.Minute
)

// ClaimJobs забирает задачи из очереди для конкретного воркера.
func (s *Service) ClaimJobs(ctx context.Context, workerID string, limit int) ([]domain.Job, error) {
	return s.deps.Jobs.Claim(ctx, workerID, limit)
}

// ReleaseJob возвращает задачу в очередь, не расходуя попытку.
// Вызывается при остановке приложения для задач, которые не успели завершиться.
func (s *Service) ReleaseJob(ctx context.Context, jobID uuid.UUID) error {
	return s.deps.Jobs.Release(ctx, jobID)
}

// RecoverStale возвращает в очередь задачи, зависшие в обработке после аварийного завершения процесса.
func (s *Service) RecoverStale(ctx context.Context, ttl time.Duration) (int64, error) {
	count, err := s.deps.Jobs.ReleaseStale(ctx, ttl)
	if err != nil {
		return 0, err
	}
	if count > 0 {
		s.log.InfoContext(ctx, "незавершённые задачи возвращены в очередь", slog.Int64("count", count))
	}
	return count, nil
}

// ProcessJob выполняет полный сценарий обработки одной встречи.
func (s *Service) ProcessJob(ctx context.Context, job domain.Job) error {
	log := s.log.With(
		slog.String("job_id", job.ID.String()),
		slog.String("meeting_id", job.MeetingID.String()),
		slog.Int("attempt", job.Attempts))

	meeting, err := s.deps.Meetings.Get(ctx, job.MeetingID)
	if err != nil {
		return s.handleFailure(ctx, job, "загрузка встречи", err, log)
	}

	log.InfoContext(ctx, "обращение к speech-клиенту", slog.String("provider", s.deps.Speech.Name()))

	speechCtx, cancelSpeech := context.WithTimeout(ctx, s.deps.SpeechTimeout)
	transcription, err := s.deps.Speech.Transcribe(speechCtx, TranscribeRequest{
		MeetingID: meeting.ID,
		FilePath:  meeting.StoredPath,
		Format:    meeting.Format,
		Language:  "ru",
	})
	cancelSpeech()
	if err != nil {
		return s.handleFailure(ctx, job, "распознавание речи", err, log)
	}

	transcript := domain.Transcript{
		ID:          uuid.New(),
		MeetingID:   meeting.ID,
		Text:        transcription.Text,
		Language:    transcription.Language,
		DurationSec: transcription.DurationSec,
		Provider:    transcription.Provider,
	}
	err = s.deps.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.deps.Transcripts.Save(ctx, transcript); err != nil {
			return err
		}
		return s.deps.Jobs.ChangeStatus(ctx, job.ID, domain.StatusProcessing, domain.StatusTranscribed, "")
	})
	if err != nil {
		return s.handleFailure(ctx, job, "сохранение транскрипции", err, log)
	}
	log.InfoContext(ctx, "статус задачи изменён",
		slog.String("status", string(domain.StatusTranscribed)),
		slog.Int("transcript_chars", len(transcript.Text)))

	log.InfoContext(ctx, "обращение к LLM-клиенту", slog.String("provider", s.deps.LLM.Name()))

	llmCtx, cancelLLM := context.WithTimeout(ctx, s.deps.LLMTimeout)
	summarized, err := s.deps.LLM.Summarize(llmCtx, transcript.Text)
	cancelLLM()
	if err != nil {
		return s.handleFailure(ctx, job, "получение краткой выжимки", err, log)
	}

	summary := domain.Summary{
		ID:        uuid.New(),
		MeetingID: meeting.ID,
		Text:      summarized.Text,
		Provider:  summarized.Provider,
		Model:     summarized.Model,
	}
	err = s.deps.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.deps.Summaries.Save(ctx, summary); err != nil {
			return err
		}
		if err := s.deps.Jobs.ChangeStatus(ctx, job.ID, domain.StatusTranscribed, domain.StatusSummarized, ""); err != nil {
			return err
		}
		return s.deps.Jobs.ChangeStatus(ctx, job.ID, domain.StatusSummarized, domain.StatusCompleted, "")
	})
	if err != nil {
		return s.handleFailure(ctx, job, "сохранение краткой выжимки", err, log)
	}

	log.InfoContext(ctx, "обработка встречи завершена",
		slog.String("status", string(domain.StatusCompleted)))
	return nil
}

// handleFailure сохраняет ошибку обработки.
func (s *Service) handleFailure(ctx context.Context, job domain.Job, stage string, cause error, log *slog.Logger) error {
	wrapped := fmt.Errorf("%s: %w", stage, cause)

	if errors.Is(cause, context.Canceled) {
		log.WarnContext(ctx, "обработка прервана остановкой приложения", slog.String("stage", stage))
		return wrapped
	}

	dbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dbWriteTimeout)
	defer cancel()

	retryable := isRetryable(cause)
	attemptsLeft := job.Attempts < job.MaxAttempts

	if retryable && attemptsLeft {
		delay := retryDelay(s.deps.RetryBaseDelay, job.Attempts)
		if err := s.deps.Jobs.Reschedule(dbCtx, job.ID, wrapped.Error(), delay); err != nil {
			log.ErrorContext(ctx, "не удалось вернуть задачу в очередь",
				slog.String("stage", stage), slog.String("error", err.Error()))
			return errors.Join(wrapped, err)
		}
		log.WarnContext(ctx, "попытка обработки не удалась, задача возвращена в очередь",
			slog.String("stage", stage),
			slog.String("error", cause.Error()),
			slog.Duration("retry_in", delay),
			slog.Int("attempts_left", job.MaxAttempts-job.Attempts))
		return wrapped
	}

	if err := s.deps.Jobs.Fail(dbCtx, job.ID, wrapped.Error()); err != nil {
		log.ErrorContext(ctx, "не удалось сохранить ошибку обработки",
			slog.String("stage", stage), slog.String("error", err.Error()))
		return errors.Join(wrapped, err)
	}
	log.ErrorContext(ctx, "обработка встречи завершилась ошибкой",
		slog.String("stage", stage),
		slog.String("status", string(domain.StatusFailed)),
		slog.String("error", cause.Error()),
		slog.Bool("retryable", retryable))
	return wrapped
}

func isRetryable(err error) bool {
	switch {
	case errors.Is(err, domain.ErrExternalUnavailable),
		errors.Is(err, domain.ErrExternalRateLimited),
		errors.Is(err, domain.ErrStorageUnavailable),
		errors.Is(err, context.DeadlineExceeded):
		return true
	default:
		return false
	}
}

// retryDelay удваивает задержку с каждой попыткой, но не больше maxRetryDelay.
func retryDelay(base time.Duration, attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := base
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= maxRetryDelay {
			return maxRetryDelay
		}
	}
	return delay
}
