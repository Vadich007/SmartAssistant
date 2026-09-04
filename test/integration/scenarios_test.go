//go:build integration

package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Vadich007/meetnotes/internal/domain"
	"github.com/Vadich007/meetnotes/internal/storage/postgres"
)

const meetingText = `Планёрка команды платформы.
Анна рассказала о статусе миграции на новую схему хранения данных.
Борис описал инцидент на проде и предложил добавить нагрузочное тестирование.
Решили: Борис готовит план нагрузочного тестирования до среды.
Ответственный за миграцию — Анна, дедлайн — конец недели.`

func TestFullProcessingCycle(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()

	path := env.writeMeetingFile(t, "planning.txt", meetingText)
	meeting, job, err := env.Service.Load(ctx, userAlice, path)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCreated, job.Status)

	claimed := env.processNext(t, 10)
	require.Len(t, claimed, 1)
	assert.Equal(t, "integration-worker", claimed[0].LockedBy)
	assert.Equal(t, 1, claimed[0].Attempts)

	details, err := env.Service.Get(ctx, userAlice, meeting.ID)
	require.NoError(t, err)

	assert.Equal(t, domain.StatusCompleted, details.Job.Status)
	require.NotNil(t, details.Transcript)
	assert.Contains(t, details.Transcript.Text, "нагрузочное тестирование")
	require.NotNil(t, details.Summary)
	assert.Contains(t, details.Summary.Text, "Договорённости и решения")

	view, err := env.Service.Status(ctx, userAlice, meeting.ID)
	require.NoError(t, err)

	var transitions []string
	for _, h := range view.History {
		transitions = append(transitions, fmt.Sprintf("%s→%s", h.From, h.To))
	}
	assert.ElementsMatch(t, []string{
		"processing→transcribed", "transcribed→summarized", "summarized→completed",
	}, transitions)

	items, err := env.Service.List(ctx, userAlice, 10, 0)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, domain.StatusCompleted, items[0].Status)
	assert.NotEmpty(t, items[0].Summary)
}

func TestSearchFindsMeetingsByKeyword(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()

	env.loadAndProcess(t, userAlice, "planning.txt", meetingText)
	env.loadAndProcess(t, userAlice, "hiring.txt",
		"Обсудили найм двух разработчиков. Решили закрыть вакансию до конца месяца.")

	results, err := env.Service.Find(ctx, userAlice, "нагрузочное тестирование", 10)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Contains(t, results[0].Snippet, "нагрузочн")
	assert.Positive(t, results[0].Rank)
	assert.Equal(t, domain.StatusCompleted, results[0].Status)

	// Словарь русского языка приводит слова к основе: «дедлайны» находит «дедлайн».
	results, err = env.Service.Find(ctx, userAlice, "дедлайны", 10)
	require.NoError(t, err)
	assert.Len(t, results, 1)

	results, err = env.Service.Find(ctx, userAlice, "квартальный бюджет", 10)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestUserDataIsolation(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()

	aliceMeeting := env.loadAndProcess(t, userAlice, "alice.txt", meetingText)
	env.loadAndProcess(t, userBob, "bob.txt", "Встреча Бориса про бюджет и найм.")

	_, err := env.Service.Get(ctx, userBob, aliceMeeting.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)

	_, err = env.Service.Status(ctx, userBob, aliceMeeting.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)

	err = env.Service.Delete(ctx, userBob, aliceMeeting.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)

	found, err := env.Service.Find(ctx, userBob, "миграции", 10)
	require.NoError(t, err)
	assert.Empty(t, found, "поиск не должен показывать встречи другого пользователя")

	items, err := env.Service.List(ctx, userBob, 10, 0)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.NotEqual(t, aliceMeeting.ID, items[0].Meeting.ID)
}

func TestLLMFailureKeepsTranscript(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()

	meeting := env.loadAndProcess(t, userAlice, "llm_fail_demo.txt",
		"Встреча для проверки сбоя llm_fail на этапе краткой выжимки.")

	details, err := env.Service.Get(ctx, userAlice, meeting.ID)
	require.NoError(t, err)
	require.NotNil(t, details.Transcript, "транскрипция должна сохраниться до сбоя LLM")
	assert.Nil(t, details.Summary)
	assert.Contains(t, details.Job.LastError, "получение краткой выжимки")
}

func TestTransactionRollback(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()

	user, _, err := env.Service.EnsureUser(ctx, userAlice)
	require.NoError(t, err)

	meetings := postgres.NewMeetingRepo(env.DB)
	meetingID := uuid.New()

	sentinel := fmt.Errorf("сбой второй операции")
	err = env.DB.WithinTx(ctx, func(ctx context.Context) error {
		if err := meetings.Create(ctx, domain.Meeting{
			ID:           meetingID,
			UserID:       user.ID,
			Title:        "Встреча из откаченной транзакции",
			OriginalName: "rollback.txt",
			StoredPath:   "/tmp/rollback.txt",
			Format:       "txt",
		}); err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)

	_, err = meetings.Get(ctx, meetingID)
	assert.ErrorIs(t, err, domain.ErrNotFound, "встреча не должна сохраниться после отката")
}

func TestDeleteRemovesRelatedData(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()

	meeting := env.loadAndProcess(t, userAlice, "planning.txt", meetingText)
	require.NoError(t, env.Service.Delete(ctx, userAlice, meeting.ID))

	for _, table := range []string{"meetings", "processing_jobs", "transcripts", "summaries", "job_status_history"} {
		var count int
		require.NoError(t, env.DB.Pool().
			QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&count))
		assert.Zerof(t, count, "таблица %s должна опустеть после удаления встречи", table)
	}

	_, err := env.Service.Get(ctx, userAlice, meeting.ID)
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestConcurrentWorkersDoNotShareJobs(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()

	const total = 12
	for i := range total {
		path := env.writeMeetingFile(t, fmt.Sprintf("meeting_%d.txt", i), meetingText)
		_, _, err := env.Service.Load(ctx, userAlice, path)
		require.NoError(t, err)
	}

	jobs := postgres.NewJobRepo(env.DB)

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		claims = make(map[uuid.UUID]string)
		dupes  []uuid.UUID
	)
	for _, workerID := range []string{"worker-a", "worker-b", "worker-c"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()

			claimed, err := jobs.Claim(ctx, id, total)
			if err != nil {
				t.Errorf("воркер %s: %v", id, err)
				return
			}

			mu.Lock()
			defer mu.Unlock()
			for _, job := range claimed {
				if _, exists := claims[job.ID]; exists {
					dupes = append(dupes, job.ID)
				}
				claims[job.ID] = id
			}
		}(workerID)
	}
	wg.Wait()

	assert.Empty(t, dupes, "одну задачу не могут взять два воркера")
	assert.Len(t, claims, total, "все задачи должны быть разобраны")
}

func TestStaleJobsAreRecovered(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()

	path := env.writeMeetingFile(t, "planning.txt", meetingText)
	meeting, _, err := env.Service.Load(ctx, userAlice, path)
	require.NoError(t, err)

	claimed, err := env.Service.ClaimJobs(ctx, "crashed-worker", 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)

	_, err = env.DB.Pool().Exec(ctx,
		`UPDATE processing_jobs SET locked_at = now() - interval '1 hour' WHERE id = $1`, claimed[0].ID)
	require.NoError(t, err)

	count, err := env.Service.RecoverStale(ctx, 5*time.Minute)
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)

	view, err := env.Service.Status(ctx, userAlice, meeting.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.StatusCreated, view.Job.Status)
}

func TestChatOverStoredMaterials(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()

	meeting := env.loadAndProcess(t, userAlice, "planning.txt", meetingText)

	answer, err := env.Service.Chat(ctx, userAlice, &meeting.ID, "что решили по нагрузочному тестированию?")
	require.NoError(t, err)
	assert.Contains(t, answer.Answer, "нагрузочного тестирования")
	require.Len(t, answer.Sources, 1)

	entries, err := env.Service.History(ctx, userAlice, 10)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, meeting.ID, *entries[0].MeetingID)

	foreign, err := env.Service.History(ctx, userBob, 10)
	require.NoError(t, err)
	assert.Empty(t, foreign)

	_, err = env.Service.Chat(ctx, userBob, &meeting.ID, "о чём встреча?")
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestStatsReflectProcessing(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()

	env.loadAndProcess(t, userAlice, "planning.txt", meetingText)
	path := env.writeMeetingFile(t, "pending.txt", meetingText)
	_, _, err := env.Service.Load(ctx, userAlice, path)
	require.NoError(t, err)

	stats, err := env.Service.Stats(ctx, userAlice)
	require.NoError(t, err)

	byStatus := make(map[domain.JobStatus]int)
	for _, row := range stats.ByStatus {
		byStatus[row.Status] = row.Count
	}
	assert.Equal(t, 2, stats.TotalMeetings)
	assert.Equal(t, 1, byStatus[domain.StatusCompleted])
	assert.Equal(t, 1, byStatus[domain.StatusCreated])
	assert.Zero(t, stats.FailedCount)
}
