package service_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Vadich007/meetnotes/internal/domain"
)

func processed(t *testing.T, store *memStore, svc interface {
	Load(context.Context, string, string) (domain.Meeting, domain.Job, error)
	ProcessJob(context.Context, domain.Job) error
}, user, path string) domain.Meeting {
	t.Helper()

	ctx := context.Background()
	meeting, _, err := svc.Load(ctx, user, path)
	require.NoError(t, err)

	claimed, err := store.Claim(ctx, "test-worker", 10)
	require.NoError(t, err)
	for _, job := range claimed {
		require.NoError(t, svc.ProcessJob(ctx, job))
	}
	return meeting
}

func TestChatWithExplicitMeeting(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	llmClient := &fakeLLM{
		summary: "Кратко: релиз в среду.",
		answer:  "Договорились выкатить релиз в среду.",
	}
	svc := newService(store,
		newFakeFiles(),
		&fakeSpeech{text: "Решили выкатить релиз в среду."},
		llmClient)

	meeting := processed(t, store, svc, userAlice, "/tmp/release.mp3")

	answer, err := svc.Chat(context.Background(), userAlice, &meeting.ID, "когда релиз?")
	require.NoError(t, err)

	assert.Equal(t, llmClient.answer, answer.Answer)
	require.Len(t, answer.Sources, 1)
	assert.Equal(t, meeting.ID, answer.Sources[0].ID)

	require.Len(t, llmClient.contexts, 1)
	assert.Equal(t, "Решили выкатить релиз в среду.", llmClient.contexts[0].Transcript)
	assert.NotEmpty(t, llmClient.contexts[0].Summary)
}

func TestChatSelectsRelevantMeetings(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	llmClient := &fakeLLM{answer: "Ответ по материалам."}
	svc := newService(store,
		newFakeFiles(),
		&fakeSpeech{text: "Обсудили миграцию базы данных и дедлайны."},
		llmClient)

	processed(t, store, svc, userAlice, "/tmp/migration.mp3")

	answer, err := svc.Chat(context.Background(), userAlice, nil, "что с миграцией?")
	require.NoError(t, err)

	assert.Equal(t, llmClient.answer, answer.Answer)
	assert.Len(t, llmClient.contexts, 1, "контекст подбирается поиском по своим встречам")
}

func TestChatDeniesForeignMeeting(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := newService(store, newFakeFiles(), &fakeSpeech{text: "текст"}, &fakeLLM{answer: "ответ"})

	meeting := processed(t, store, svc, userAlice, "/tmp/alice.mp3")

	_, err := svc.Chat(context.Background(), userBob, &meeting.ID, "о чём встреча?")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestChatWithoutMaterials(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := newService(store, newFakeFiles(), &fakeSpeech{}, &fakeLLM{answer: "ответ"})
	ctx := context.Background()

	meeting, _, err := svc.Load(ctx, userAlice, "/tmp/alice.mp3")
	require.NoError(t, err)

	_, err = svc.Chat(ctx, userAlice, &meeting.ID, "о чём встреча?")
	assert.ErrorIs(t, err, domain.ErrNoContext)

	_, err = svc.Chat(ctx, userAlice, nil, "о чём встреча?")
	assert.ErrorIs(t, err, domain.ErrNoContext)
}

func TestChatValidatesInput(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := newService(store, newFakeFiles(), &fakeSpeech{}, &fakeLLM{})

	_, err := svc.Chat(context.Background(), userAlice, nil, "   ")
	assert.ErrorIs(t, err, domain.ErrInvalidArgument)

	unknown := uuid.New()
	_, err = svc.Chat(context.Background(), userAlice, &unknown, "вопрос")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestChatPropagatesLLMError(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	llmClient := &fakeLLM{answerErr: fmt.Errorf("%w: LLM недоступна", domain.ErrExternalUnavailable)}
	svc := newService(store, newFakeFiles(), &fakeSpeech{text: "текст встречи"}, llmClient)

	meeting := processed(t, store, svc, userAlice, "/tmp/alice.mp3")

	_, err := svc.Chat(context.Background(), userAlice, &meeting.ID, "вопрос")
	assert.ErrorIs(t, err, domain.ErrExternalUnavailable)
}

func TestChatSavesHistory(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := newService(store, newFakeFiles(), &fakeSpeech{text: "текст встречи"}, &fakeLLM{answer: "ответ"})
	ctx := context.Background()

	meeting := processed(t, store, svc, userAlice, "/tmp/alice.mp3")
	_, err := svc.Chat(ctx, userAlice, &meeting.ID, "вопрос про встречу")
	require.NoError(t, err)

	entries, err := svc.History(ctx, userAlice, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "вопрос про встречу", entries[0].Question)
	assert.Equal(t, "ответ", entries[0].Answer)

	foreign, err := svc.History(ctx, userBob, 10)
	require.NoError(t, err)
	assert.Empty(t, foreign)
}
