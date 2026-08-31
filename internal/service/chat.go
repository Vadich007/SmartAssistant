package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/Vadich007/meetnotes/internal/domain"
)

// ChatAnswer ответ на вопрос пользователя вместе со списком использованных встреч.
type ChatAnswer struct {
	Question string
	Answer   string
	Provider string
	Model    string
	Sources  []domain.Meeting
}

// Chat отвечает на вопрос по сохранённым материалам.
func (s *Service) Chat(ctx context.Context, externalID string, meetingID *uuid.UUID, question string) (ChatAnswer, error) {
	user, _, err := s.EnsureUser(ctx, externalID)
	if err != nil {
		return ChatAnswer{}, err
	}

	question = strings.TrimSpace(question)
	if question == "" {
		return ChatAnswer{}, fmt.Errorf("%w: пустой вопрос", domain.ErrInvalidArgument)
	}

	contexts, sources, err := s.collectContexts(ctx, user.ID, meetingID, question)
	if err != nil {
		return ChatAnswer{}, err
	}
	if len(contexts) == 0 {
		return ChatAnswer{}, fmt.Errorf(
			"%w: нет обработанных встреч, по которым можно ответить", domain.ErrNoContext)
	}

	s.log.InfoContext(ctx, "обращение к LLM-клиенту с вопросом пользователя",
		slog.String("user_id", user.ID.String()),
		slog.String("provider", s.deps.LLM.Name()),
		slog.Int("context_meetings", len(contexts)))

	llmCtx, cancel := context.WithTimeout(ctx, s.deps.LLMTimeout)
	defer cancel()

	result, err := s.deps.LLM.Answer(llmCtx, question, contexts)
	if err != nil {
		s.log.ErrorContext(ctx, "LLM-клиент не смог ответить на вопрос",
			slog.String("provider", s.deps.LLM.Name()), slog.String("error", err.Error()))
		return ChatAnswer{}, err
	}

	entry := domain.QAEntry{
		ID:        uuid.New(),
		UserID:    user.ID,
		MeetingID: meetingID,
		Question:  question,
		Answer:    result.Text,
	}
	if err := s.deps.QA.Add(ctx, entry); err != nil {
		s.log.WarnContext(ctx, "не удалось сохранить историю вопросов", slog.String("error", err.Error()))
	}

	return ChatAnswer{
		Question: question,
		Answer:   result.Text,
		Provider: result.Provider,
		Model:    result.Model,
		Sources:  sources,
	}, nil
}

// History возвращает последние вопросы пользователя и ответы на них.
func (s *Service) History(ctx context.Context, externalID string, limit int) ([]domain.QAEntry, error) {
	user, _, err := s.EnsureUser(ctx, externalID)
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = defaultListLimit
	}
	return s.deps.QA.ListByUser(ctx, user.ID, limit)
}

// collectContexts собирает материалы для ответа.
func (s *Service) collectContexts(
	ctx context.Context, userID uuid.UUID, meetingID *uuid.UUID, question string,
) ([]MeetingContext, []domain.Meeting, error) {
	if meetingID != nil {
		meeting, err := s.deps.Meetings.GetOwned(ctx, userID, *meetingID)
		if err != nil {
			return nil, nil, err
		}
		mc, ok, err := s.meetingContext(ctx, meeting)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, fmt.Errorf(
				"%w: встреча %s ещё не обработана", domain.ErrNoContext, meeting.ID)
		}
		return []MeetingContext{mc}, []domain.Meeting{meeting}, nil
	}

	found, err := s.deps.Search.Search(ctx, userID, question, defaultChatContext)
	if err != nil {
		return nil, nil, err
	}
	if len(found) == 0 {
		// Если совпадений нет, то берём последние встречи
		items, err := s.deps.Meetings.List(ctx, userID, defaultChatContext, 0)
		if err != nil {
			return nil, nil, err
		}
		for _, item := range items {
			found = append(found, domain.SearchResult{Meeting: item.Meeting, Status: item.Status})
		}
	}

	var (
		contexts []MeetingContext
		sources  []domain.Meeting
	)
	for _, res := range found {
		mc, ok, err := s.meetingContext(ctx, res.Meeting)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			continue
		}
		contexts = append(contexts, mc)
		sources = append(sources, res.Meeting)
	}
	return contexts, sources, nil
}

// meetingContext собирает транскрипцию и выжимку встречи.
func (s *Service) meetingContext(ctx context.Context, meeting domain.Meeting) (MeetingContext, bool, error) {
	mc := MeetingContext{
		MeetingID: meeting.ID,
		Title:     meeting.Title,
		CreatedAt: meeting.CreatedAt,
	}

	transcript, err := s.deps.Transcripts.GetByMeetingID(ctx, meeting.ID)
	switch {
	case err == nil:
		mc.Transcript = transcript.Text
	case errors.Is(err, domain.ErrNotFound):
	default:
		return MeetingContext{}, false, err
	}

	summary, err := s.deps.Summaries.GetByMeetingID(ctx, meeting.ID)
	switch {
	case err == nil:
		mc.Summary = summary.Text
	case errors.Is(err, domain.ErrNotFound):
	default:
		return MeetingContext{}, false, err
	}

	return mc, mc.Transcript != "" || mc.Summary != "", nil
}
