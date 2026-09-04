package service_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Vadich007/meetnotes/internal/domain"
)

func claimOne(t *testing.T, store *memStore, svc interface {
	Load(context.Context, string, string) (domain.Meeting, domain.Job, error)
}, path string) (domain.Meeting, domain.Job) {
	t.Helper()

	ctx := context.Background()
	meeting, _, err := svc.Load(ctx, userAlice, path)
	require.NoError(t, err)

	claimed, err := store.Claim(ctx, "test-worker", 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	return meeting, claimed[0]
}

func TestProcessJobHappyPath(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	speech := &fakeSpeech{text: "Обсудили релиз. Решили выкатывать в среду."}
	llmClient := &fakeLLM{summary: "Кратко: релиз в среду."}
	svc := newService(store, newFakeFiles(), speech, llmClient)

	meeting, job := claimOne(t, store, svc, "/tmp/release.mp3")
	require.NoError(t, svc.ProcessJob(context.Background(), job))

	details, err := svc.Get(context.Background(), userAlice, meeting.ID)
	require.NoError(t, err)

	assert.Equal(t, domain.StatusCompleted, details.Job.Status)
	require.NotNil(t, details.Transcript)
	assert.Equal(t, speech.text, details.Transcript.Text)
	require.NotNil(t, details.Summary)
	assert.Equal(t, llmClient.summary, details.Summary.Text)

	view, err := svc.Status(context.Background(), userAlice, meeting.ID)
	require.NoError(t, err)

	var transitions []string
	for _, h := range view.History {
		transitions = append(transitions, fmt.Sprintf("%s→%s", h.From, h.To))
	}
	assert.ElementsMatch(t,
		[]string{"processing→transcribed", "transcribed→summarized", "summarized→completed"},
		transitions)
}

func TestProcessJobRetriesTemporarySpeechError(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	speech := &fakeSpeech{err: fmt.Errorf("%w: speech-сервис лежит", domain.ErrExternalUnavailable)}
	svc := newService(store, newFakeFiles(), speech, &fakeLLM{})

	meeting, job := claimOne(t, store, svc, "/tmp/release.mp3")
	err := svc.ProcessJob(context.Background(), job)
	require.ErrorIs(t, err, domain.ErrExternalUnavailable)

	stored, err := store.GetByMeetingID(context.Background(), meeting.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCreated, stored.Status)
	assert.Contains(t, stored.LastError, "распознавание речи")
	assert.True(t, stored.NextAttemptAt.After(time.Now().Add(-time.Second)),
		"следующая попытка должна быть отложена")
}

func TestProcessJobFailsWhenAttemptsExhausted(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	speech := &fakeSpeech{err: fmt.Errorf("%w: speech-сервис лежит", domain.ErrExternalUnavailable)}
	svc := newService(store, newFakeFiles(), speech, &fakeLLM{})

	meeting, job := claimOne(t, store, svc, "/tmp/release.mp3")
	job.Attempts = job.MaxAttempts // последняя попытка

	require.Error(t, svc.ProcessJob(context.Background(), job))

	stored, err := store.GetByMeetingID(context.Background(), meeting.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusFailed, stored.Status)
	assert.Contains(t, stored.LastError, "speech-сервис лежит")
}

func TestProcessJobDoesNotRetryPermanentError(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	speech := &fakeSpeech{err: fmt.Errorf("%w: /tmp/release.mp3", domain.ErrFileNotFound)}
	svc := newService(store, newFakeFiles(), speech, &fakeLLM{})

	meeting, job := claimOne(t, store, svc, "/tmp/release.mp3")
	require.Error(t, svc.ProcessJob(context.Background(), job))

	stored, err := store.GetByMeetingID(context.Background(), meeting.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusFailed, stored.Status)
	assert.Equal(t, 1, stored.Attempts)
}

func TestProcessJobKeepsTranscriptWhenLLMFails(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	speech := &fakeSpeech{text: "Расшифровка встречи."}
	llmClient := &fakeLLM{summErr: fmt.Errorf("%w: LLM недоступна", domain.ErrExternalUnavailable)}
	svc := newService(store, newFakeFiles(), speech, llmClient)

	meeting, job := claimOne(t, store, svc, "/tmp/release.mp3")
	job.Attempts = job.MaxAttempts

	require.Error(t, svc.ProcessJob(context.Background(), job))

	details, err := svc.Get(context.Background(), userAlice, meeting.ID)
	require.NoError(t, err)
	require.NotNil(t, details.Transcript, "успешно полученная транскрипция должна сохраниться")
	assert.Nil(t, details.Summary)
	assert.Equal(t, domain.StatusFailed, details.Job.Status)
	assert.Contains(t, details.Job.LastError, "получение краткой выжимки")
}

func TestProcessJobSpeechTimeout(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	speech := &fakeSpeech{delay: time.Second, text: "не успеет"}
	svc := newService(store, newFakeFiles(), speech, &fakeLLM{})

	meeting, job := claimOne(t, store, svc, "/tmp/long.mp3")
	job.Attempts = job.MaxAttempts

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := svc.ProcessJob(ctx, job)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	stored, err := store.GetByMeetingID(context.Background(), meeting.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusFailed, stored.Status)
}

func TestProcessJobCancellationKeepsStatus(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	speech := &fakeSpeech{delay: time.Second, text: "не успеет"}
	svc := newService(store, newFakeFiles(), speech, &fakeLLM{})

	meeting, job := claimOne(t, store, svc, "/tmp/long.mp3")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.ProcessJob(ctx, job)
	require.ErrorIs(t, err, context.Canceled)

	stored, err := store.GetByMeetingID(context.Background(), meeting.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusProcessing, stored.Status)
}

func TestRecoverStaleReturnsJobsToQueue(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := newService(store, newFakeFiles(), &fakeSpeech{}, &fakeLLM{})
	ctx := context.Background()

	meeting, job := claimOne(t, store, svc, "/tmp/release.mp3")
	assert.Equal(t, domain.StatusProcessing, job.Status)

	count, err := svc.RecoverStale(ctx, 0)
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)

	stored, err := store.GetByMeetingID(ctx, meeting.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCreated, stored.Status)
	assert.Nil(t, stored.LockedAt)
}
