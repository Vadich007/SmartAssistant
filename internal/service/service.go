package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Vadich007/meetnotes/internal/domain"
)

// Значения по умолчанию для параметров, которые не приходят из конфигурации.
const (
	defaultListLimit   = 50
	defaultSearchLimit = 20
	defaultChatContext = 3
	historyLimit       = 10
)

type Deps struct {
	Tx          TxManager
	Users       UserRepository
	Meetings    MeetingRepository
	Jobs        JobRepository
	Transcripts TranscriptRepository
	Summaries   SummaryRepository
	QA          QARepository
	Search      SearchRepository
	Stats       StatsRepository
	Files       FileStore
	Speech      SpeechClient
	LLM         LLMClient
	Log         *slog.Logger

	// MaxAttempts сколько раз задача может быть взята в работу до перевода в failed.
	MaxAttempts int
	// SpeechTimeout и LLMTimeout ограничивают время обращения к внешним клиентам.
	SpeechTimeout time.Duration
	LLMTimeout    time.Duration
	// RetryBaseDelay база экспоненциальной задержки перед повторной попыткой.
	RetryBaseDelay time.Duration
}

// Service реализует сценарии приложения.
type Service struct {
	deps Deps
	log  *slog.Logger
}

// New собирает сервис и подставляет значения по умолчанию для необязательных параметров.
func New(deps Deps) *Service {
	if deps.Log == nil {
		deps.Log = slog.New(slog.DiscardHandler)
	}
	if deps.MaxAttempts <= 0 {
		deps.MaxAttempts = 3
	}
	if deps.SpeechTimeout <= 0 {
		deps.SpeechTimeout = 30 * time.Second
	}
	if deps.LLMTimeout <= 0 {
		deps.LLMTimeout = 30 * time.Second
	}
	if deps.RetryBaseDelay <= 0 {
		deps.RetryBaseDelay = 5 * time.Second
	}
	return &Service{deps: deps, log: deps.Log.With(slog.String("component", "service"))}
}

// EnsureUser создаёт пользователя при первом обращении и возвращает его.
func (s *Service) EnsureUser(ctx context.Context, externalID string) (domain.User, bool, error) {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return domain.User{}, false, fmt.Errorf("%w: не указан идентификатор пользователя", domain.ErrInvalidArgument)
	}

	user, created, err := s.deps.Users.Ensure(ctx, externalID)
	if err != nil {
		return domain.User{}, false, err
	}
	if created {
		s.log.InfoContext(ctx, "зарегистрирован новый пользователь",
			slog.String("user_id", user.ID.String()),
			slog.String("external_id", user.ExternalID))
	}
	return user, created, nil
}
