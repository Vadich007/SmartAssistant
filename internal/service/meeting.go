package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/Vadich007/meetnotes/internal/domain"
)

// StatusView ответ команды status: задача вместе со встречей и историей переходов.
type StatusView struct {
	Meeting domain.Meeting
	Job     domain.Job
	History []domain.StatusChange
}

// Load принимает файл встречи: копирует его в хранилище и в одной транзакции
// создаёт встречу и задачу обработки.
func (s *Service) Load(ctx context.Context, externalID, path string) (domain.Meeting, domain.Job, error) {
	user, _, err := s.EnsureUser(ctx, externalID)
	if err != nil {
		return domain.Meeting{}, domain.Job{}, err
	}

	path = strings.TrimSpace(path)
	if path == "" {
		return domain.Meeting{}, domain.Job{}, fmt.Errorf("%w: не указан путь к файлу", domain.ErrInvalidArgument)
	}
	format, err := domain.NormalizeFormat(path)
	if err != nil {
		return domain.Meeting{}, domain.Job{}, err
	}

	storedPath, size, err := s.deps.Files.Save(ctx, path)
	if err != nil {
		return domain.Meeting{}, domain.Job{}, err
	}

	meeting := domain.Meeting{
		ID:           uuid.New(),
		UserID:       user.ID,
		Title:        domain.TitleFromFilename(path),
		OriginalName: filepath.Base(path),
		StoredPath:   storedPath,
		Format:       format,
		SizeBytes:    size,
	}
	job := domain.Job{
		ID:          uuid.New(),
		MeetingID:   meeting.ID,
		Status:      domain.StatusCreated,
		MaxAttempts: s.deps.MaxAttempts,
	}

	err = s.deps.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if err := s.deps.Meetings.Create(ctx, meeting); err != nil {
			return err
		}
		return s.deps.Jobs.Create(ctx, job)
	})
	if err != nil {
		if removeErr := s.deps.Files.Remove(storedPath); removeErr != nil {
			s.log.WarnContext(ctx, "не удалось удалить файл после отката транзакции",
				slog.String("path", storedPath), slog.String("error", removeErr.Error()))
		}
		return domain.Meeting{}, domain.Job{}, err
	}

	s.log.InfoContext(ctx, "встреча загружена, создана задача обработки",
		slog.String("user_id", user.ID.String()),
		slog.String("meeting_id", meeting.ID.String()),
		slog.String("job_id", job.ID.String()),
		slog.String("format", format),
		slog.Int64("size_bytes", size))

	return meeting, job, nil
}

// List возвращает встречи пользователя со статусом обработки и выжимкой.
func (s *Service) List(ctx context.Context, externalID string, limit, offset int) ([]domain.MeetingListItem, error) {
	user, _, err := s.EnsureUser(ctx, externalID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	if offset < 0 {
		offset = 0
	}
	return s.deps.Meetings.List(ctx, user.ID, limit, offset)
}

// Status возвращает текущее состояние обработки встречи вместе с историей переходов.
func (s *Service) Status(ctx context.Context, externalID string, meetingID uuid.UUID) (StatusView, error) {
	user, _, err := s.EnsureUser(ctx, externalID)
	if err != nil {
		return StatusView{}, err
	}

	meeting, err := s.deps.Meetings.GetOwned(ctx, user.ID, meetingID)
	if err != nil {
		return StatusView{}, err
	}
	job, err := s.deps.Jobs.GetByMeetingID(ctx, meeting.ID)
	if err != nil {
		return StatusView{}, err
	}
	history, err := s.deps.Jobs.History(ctx, job.ID, historyLimit)
	if err != nil {
		return StatusView{}, err
	}
	return StatusView{Meeting: meeting, Job: job, History: history}, nil
}

// Get возвращает полную карточку встречи: транскрипцию и выжимку, если они уже готовы.
func (s *Service) Get(ctx context.Context, externalID string, meetingID uuid.UUID) (domain.MeetingDetails, error) {
	user, _, err := s.EnsureUser(ctx, externalID)
	if err != nil {
		return domain.MeetingDetails{}, err
	}

	meeting, err := s.deps.Meetings.GetOwned(ctx, user.ID, meetingID)
	if err != nil {
		return domain.MeetingDetails{}, err
	}
	job, err := s.deps.Jobs.GetByMeetingID(ctx, meeting.ID)
	if err != nil {
		return domain.MeetingDetails{}, err
	}

	details := domain.MeetingDetails{Meeting: meeting, Job: job}

	transcript, err := s.deps.Transcripts.GetByMeetingID(ctx, meeting.ID)
	switch {
	case err == nil:
		details.Transcript = &transcript
	case errors.Is(err, domain.ErrNotFound):
	default:
		return domain.MeetingDetails{}, err
	}

	summary, err := s.deps.Summaries.GetByMeetingID(ctx, meeting.ID)
	switch {
	case err == nil:
		details.Summary = &summary
	case errors.Is(err, domain.ErrNotFound):
	default:
		return domain.MeetingDetails{}, err
	}

	return details, nil
}

// Find ищет встречи пользователя по ключевому слову.
func (s *Service) Find(ctx context.Context, externalID, keyword string, limit int) ([]domain.SearchResult, error) {
	user, _, err := s.EnsureUser(ctx, externalID)
	if err != nil {
		return nil, err
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("%w: не указано ключевое слово для поиска", domain.ErrInvalidArgument)
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	return s.deps.Search.Search(ctx, user.ID, keyword, limit)
}

// Retry возвращает в очередь задачу, завершившуюся ошибкой.
func (s *Service) Retry(ctx context.Context, externalID string, meetingID uuid.UUID) (domain.Job, error) {
	user, _, err := s.EnsureUser(ctx, externalID)
	if err != nil {
		return domain.Job{}, err
	}

	meeting, err := s.deps.Meetings.GetOwned(ctx, user.ID, meetingID)
	if err != nil {
		return domain.Job{}, err
	}
	job, err := s.deps.Jobs.GetByMeetingID(ctx, meeting.ID)
	if err != nil {
		return domain.Job{}, err
	}
	if job.Status != domain.StatusFailed {
		return domain.Job{}, fmt.Errorf(
			"%w: перезапустить можно только встречу в статусе failed, текущий статус — %s",
			domain.ErrConflict, job.Status)
	}

	err = s.deps.Tx.WithinTx(ctx, func(ctx context.Context) error {
		return s.deps.Jobs.Requeue(ctx, job.ID)
	})
	if err != nil {
		return domain.Job{}, err
	}

	s.log.InfoContext(ctx, "задача возвращена в очередь по команде retry",
		slog.String("meeting_id", meeting.ID.String()),
		slog.String("job_id", job.ID.String()))

	job.Status = domain.StatusCreated
	job.Attempts = 0
	job.LastError = ""
	return job, nil
}

// Delete удаляет встречу пользователя со всеми связанными данными.
func (s *Service) Delete(ctx context.Context, externalID string, meetingID uuid.UUID) error {
	user, _, err := s.EnsureUser(ctx, externalID)
	if err != nil {
		return err
	}

	var storedPath string
	err = s.deps.Tx.WithinTx(ctx, func(ctx context.Context) error {
		path, err := s.deps.Meetings.Delete(ctx, user.ID, meetingID)
		if err != nil {
			return err
		}
		storedPath = path
		return nil
	})
	if err != nil {
		return err
	}

	if err := s.deps.Files.Remove(storedPath); err != nil {
		s.log.WarnContext(ctx, "встреча удалена, но файл остался в хранилище",
			slog.String("path", storedPath), slog.String("error", err.Error()))
	}

	s.log.InfoContext(ctx, "встреча удалена",
		slog.String("user_id", user.ID.String()),
		slog.String("meeting_id", meetingID.String()))
	return nil
}

// Stats возвращает диагностическую сводку по встречам пользователя.
func (s *Service) Stats(ctx context.Context, externalID string) (domain.Stats, error) {
	user, _, err := s.EnsureUser(ctx, externalID)
	if err != nil {
		return domain.Stats{}, err
	}
	return s.deps.Stats.Collect(ctx, user.ID)
}
