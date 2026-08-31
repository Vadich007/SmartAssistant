package service_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Vadich007/meetnotes/internal/domain"
)

const (
	userAlice = "alice"
	userBob   = "bob"
)

func TestEnsureUser(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := newService(store, newFakeFiles(), &fakeSpeech{}, &fakeLLM{})

	user, created, err := svc.EnsureUser(context.Background(), userAlice)
	require.NoError(t, err)
	assert.True(t, created, "первое обращение должно создавать пользователя")
	assert.Equal(t, userAlice, user.ExternalID)

	same, created, err := svc.EnsureUser(context.Background(), userAlice)
	require.NoError(t, err)
	assert.False(t, created, "повторное обращение не создаёт нового пользователя")
	assert.Equal(t, user.ID, same.ID)

	_, _, err = svc.EnsureUser(context.Background(), "   ")
	assert.ErrorIs(t, err, domain.ErrInvalidArgument)
}

func TestLoadCreatesMeetingAndJob(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	files := newFakeFiles()
	svc := newService(store, files, &fakeSpeech{}, &fakeLLM{})

	meeting, job, err := svc.Load(context.Background(), userAlice, "/tmp/weekly_sync.mp3")
	require.NoError(t, err)

	assert.Equal(t, "mp3", meeting.Format)
	assert.Equal(t, "Weekly sync", meeting.Title)
	assert.Equal(t, meeting.ID, job.MeetingID)
	assert.Equal(t, domain.StatusCreated, job.Status)
	assert.Equal(t, 3, job.MaxAttempts)

	stored, err := store.GetByMeetingID(context.Background(), meeting.ID)
	require.NoError(t, err)
	assert.Equal(t, job.ID, stored.ID)
}

func TestLoadRejectsUnsupportedFormat(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	files := newFakeFiles()
	svc := newService(store, files, &fakeSpeech{}, &fakeLLM{})

	_, _, err := svc.Load(context.Background(), userAlice, "/tmp/archive.zip")
	assert.ErrorIs(t, err, domain.ErrUnsupportedFormat)
	assert.Empty(t, files.saved, "файл не должен копироваться при неподдерживаемом формате")

	_, _, err = svc.Load(context.Background(), userAlice, "  ")
	assert.ErrorIs(t, err, domain.ErrInvalidArgument)
}

func TestLoadRollsBackWhenJobCreationFails(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	store.failOn = "jobs.create"
	files := newFakeFiles()
	svc := newService(store, files, &fakeSpeech{}, &fakeLLM{})

	_, _, err := svc.Load(context.Background(), userAlice, "/tmp/weekly.mp3")
	require.ErrorIs(t, err, domain.ErrStorageUnavailable)

	assert.Empty(t, store.meetings, "встреча не должна остаться без задачи обработки")
	assert.Len(t, files.removedPaths(), 1, "скопированный файл должен быть удалён после отката")
}

func TestListReturnsOnlyOwnMeetings(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := newService(store, newFakeFiles(), &fakeSpeech{}, &fakeLLM{})
	ctx := context.Background()

	_, _, err := svc.Load(ctx, userAlice, "/tmp/alice.txt")
	require.NoError(t, err)
	_, _, err = svc.Load(ctx, userBob, "/tmp/bob.txt")
	require.NoError(t, err)

	items, err := svc.List(ctx, userAlice, 10, 0)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "Alice", items[0].Meeting.Title)
	assert.Equal(t, domain.StatusCreated, items[0].Status)
}

func TestGetDeniesAccessToForeignMeeting(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := newService(store, newFakeFiles(), &fakeSpeech{}, &fakeLLM{})
	ctx := context.Background()

	meeting, _, err := svc.Load(ctx, userAlice, "/tmp/alice.txt")
	require.NoError(t, err)

	_, err = svc.Get(ctx, userBob, meeting.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)

	_, err = svc.Status(ctx, userBob, meeting.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)

	_, err = svc.Retry(ctx, userBob, meeting.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)

	err = svc.Delete(ctx, userBob, meeting.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestGetReturnsPartialResultsWhileProcessing(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := newService(store, newFakeFiles(), &fakeSpeech{}, &fakeLLM{})
	ctx := context.Background()

	meeting, _, err := svc.Load(ctx, userAlice, "/tmp/alice.txt")
	require.NoError(t, err)

	details, err := svc.Get(ctx, userAlice, meeting.ID)
	require.NoError(t, err)
	assert.Nil(t, details.Transcript, "транскрипции ещё нет — это не ошибка")
	assert.Nil(t, details.Summary)
	assert.Equal(t, domain.StatusCreated, details.Job.Status)
}

func TestFindRequiresKeyword(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := newService(store, newFakeFiles(), &fakeSpeech{}, &fakeLLM{})

	_, err := svc.Find(context.Background(), userAlice, "   ", 10)
	assert.ErrorIs(t, err, domain.ErrInvalidArgument)
}

func TestFindIsolatesUsers(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	speech := &fakeSpeech{text: "обсудили дедлайн по миграции"}
	svc := newService(store, newFakeFiles(), speech, &fakeLLM{summary: "выжимка"})
	ctx := context.Background()

	meeting, _, err := svc.Load(ctx, userAlice, "/tmp/alice.txt")
	require.NoError(t, err)
	job, err := store.GetByMeetingID(ctx, meeting.ID)
	require.NoError(t, err)
	claimed, err := store.Claim(ctx, "test-worker", 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.NoError(t, svc.ProcessJob(ctx, claimed[0]))
	require.Equal(t, job.ID, claimed[0].ID)

	found, err := svc.Find(ctx, userAlice, "дедлайн", 10)
	require.NoError(t, err)
	assert.Len(t, found, 1)

	found, err = svc.Find(ctx, userBob, "дедлайн", 10)
	require.NoError(t, err)
	assert.Empty(t, found, "поиск не должен возвращать встречи других пользователей")
}

func TestRetryOnlyForFailedJobs(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := newService(store, newFakeFiles(), &fakeSpeech{}, &fakeLLM{})
	ctx := context.Background()

	meeting, job, err := svc.Load(ctx, userAlice, "/tmp/alice.txt")
	require.NoError(t, err)

	_, err = svc.Retry(ctx, userAlice, meeting.ID)
	assert.ErrorIs(t, err, domain.ErrConflict)

	require.NoError(t, store.Fail(ctx, job.ID, "speech-клиент недоступен"))

	retried, err := svc.Retry(ctx, userAlice, meeting.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCreated, retried.Status)
	assert.Zero(t, retried.Attempts)

	updated, err := store.GetByMeetingID(ctx, meeting.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCreated, updated.Status)
	assert.Empty(t, updated.LastError)
}

func TestDeleteRemovesMeetingAndFile(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	files := newFakeFiles()
	svc := newService(store, files, &fakeSpeech{}, &fakeLLM{})
	ctx := context.Background()

	meeting, _, err := svc.Load(ctx, userAlice, "/tmp/alice.txt")
	require.NoError(t, err)

	require.NoError(t, svc.Delete(ctx, userAlice, meeting.ID))

	assert.Empty(t, store.meetings)
	assert.Empty(t, store.jobs)
	assert.Len(t, files.removedPaths(), 1)

	err = svc.Delete(ctx, userAlice, meeting.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestDeleteKeepsFileWhenTransactionFails(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	files := newFakeFiles()
	svc := newService(store, files, &fakeSpeech{}, &fakeLLM{})
	ctx := context.Background()

	meeting, _, err := svc.Load(ctx, userAlice, "/tmp/alice.txt")
	require.NoError(t, err)

	store.failOn = "meetings.delete"
	err = svc.Delete(ctx, userAlice, meeting.ID)
	require.ErrorIs(t, err, domain.ErrStorageUnavailable)

	assert.Empty(t, files.removedPaths(), "файл нельзя удалять, пока транзакция не подтверждена")
	assert.Len(t, store.meetings, 1)
}

func TestStatusIncludesHistory(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := newService(store, newFakeFiles(), &fakeSpeech{text: "текст"}, &fakeLLM{summary: "выжимка"})
	ctx := context.Background()

	meeting, _, err := svc.Load(ctx, userAlice, "/tmp/alice.txt")
	require.NoError(t, err)
	claimed, err := store.Claim(ctx, "test-worker", 1)
	require.NoError(t, err)
	require.NoError(t, svc.ProcessJob(ctx, claimed[0]))

	view, err := svc.Status(ctx, userAlice, meeting.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCompleted, view.Job.Status)
	assert.NotEmpty(t, view.History, "история переходов должна сохраняться")
}

func TestStatusUnknownMeeting(t *testing.T) {
	t.Parallel()

	store := newMemStore()
	svc := newService(store, newFakeFiles(), &fakeSpeech{}, &fakeLLM{})

	_, err := svc.Status(context.Background(), userAlice, uuid.New())
	assert.ErrorIs(t, err, domain.ErrNotFound)
}
