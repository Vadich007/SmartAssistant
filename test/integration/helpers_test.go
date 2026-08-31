//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Vadich007/meetnotes/internal/clients/llm"
	"github.com/Vadich007/meetnotes/internal/clients/speech"
	"github.com/Vadich007/meetnotes/internal/config"
	"github.com/Vadich007/meetnotes/internal/domain"
	"github.com/Vadich007/meetnotes/internal/logger"
	"github.com/Vadich007/meetnotes/internal/migrations"
	"github.com/Vadich007/meetnotes/internal/service"
	"github.com/Vadich007/meetnotes/internal/storage/files"
	"github.com/Vadich007/meetnotes/internal/storage/postgres"
)

const (
	userAlice = "alice"
	userBob   = "bob"
)

var migrateOnce sync.Once

type testEnv struct {
	Service *service.Service
	DB      *postgres.DB
	Dir     string
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()

	ctx := context.Background()
	log := logger.Discard()

	migrateOnce.Do(func() {
		require.NoError(t, migrations.Up(ctx, dsn, log))
	})

	db, err := postgres.New(ctx, config.Database{
		DSN:            dsn,
		MaxConns:       8,
		ConnectTimeout: 10 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(db.Close)

	_, err = db.Pool().Exec(ctx,
		`TRUNCATE users, meetings, processing_jobs, job_status_history,
		          transcripts, summaries, qa_history RESTART IDENTITY CASCADE`)
	require.NoError(t, err)

	dir := t.TempDir()
	fileStore, err := files.NewStore(filepath.Join(dir, "uploads"))
	require.NoError(t, err)

	svc := service.New(service.Deps{
		Tx:             db,
		Users:          postgres.NewUserRepo(db),
		Meetings:       postgres.NewMeetingRepo(db),
		Jobs:           postgres.NewJobRepo(db),
		Transcripts:    postgres.NewTranscriptRepo(db),
		Summaries:      postgres.NewSummaryRepo(db),
		QA:             postgres.NewQARepo(db),
		Search:         postgres.NewSearchRepo(db),
		Stats:          postgres.NewStatsRepo(db),
		Files:          fileStore,
		Speech:         speech.NewMock(speech.MockOptions{}),
		LLM:            llm.NewMock(llm.MockOptions{}),
		Log:            log,
		MaxAttempts:    3,
		SpeechTimeout:  10 * time.Second,
		LLMTimeout:     10 * time.Second,
		RetryBaseDelay: time.Millisecond,
	})

	return &testEnv{Service: svc, DB: db, Dir: dir}
}

func (e *testEnv) writeMeetingFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(e.Dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func (e *testEnv) processNext(t *testing.T, limit int) []domain.Job {
	t.Helper()

	ctx := context.Background()
	jobs, err := e.Service.ClaimJobs(ctx, "integration-worker", limit)
	require.NoError(t, err)

	for _, job := range jobs {
		_ = e.Service.ProcessJob(ctx, job)
	}
	return jobs
}

func (e *testEnv) loadAndProcess(t *testing.T, user, name, content string) domain.Meeting {
	t.Helper()

	path := e.writeMeetingFile(t, name, content)
	meeting, _, err := e.Service.Load(context.Background(), user, path)
	require.NoError(t, err)

	e.processNext(t, 10)
	return meeting
}
