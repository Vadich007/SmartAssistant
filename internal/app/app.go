// Package app точка сборки приложения.
package app

import (
	"context"
	"log/slog"

	"github.com/Vadich007/meetnotes/internal/clients/llm"
	"github.com/Vadich007/meetnotes/internal/clients/speech"
	"github.com/Vadich007/meetnotes/internal/config"
	"github.com/Vadich007/meetnotes/internal/service"
	"github.com/Vadich007/meetnotes/internal/storage/files"
	"github.com/Vadich007/meetnotes/internal/storage/postgres"
)

// App связывает конфигурацию, хранилище и бизнес-логику.
type App struct {
	Config  config.Config
	Log     *slog.Logger
	DB      *postgres.DB
	Service *service.Service
}

// New подключается к базе данных, создаёт клиентов внешних сервисов
// и собирает бизнес-логику.
func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	db, err := postgres.New(ctx, cfg.Database)
	if err != nil {
		return nil, err
	}

	fileStore, err := files.NewStore(cfg.Storage.Dir)
	if err != nil {
		db.Close()
		return nil, err
	}

	speechClient, err := speech.New(cfg.Speech)
	if err != nil {
		db.Close()
		return nil, err
	}
	llmClient, err := llm.New(cfg.LLM)
	if err != nil {
		db.Close()
		return nil, err
	}

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
		Speech:         speechClient,
		LLM:            llmClient,
		Log:            log,
		MaxAttempts:    cfg.Worker.MaxAttempts,
		SpeechTimeout:  cfg.Speech.Timeout,
		LLMTimeout:     cfg.LLM.Timeout,
		RetryBaseDelay: cfg.Worker.PollInterval * 2,
	})

	log.InfoContext(ctx, "приложение инициализировано",
		slog.String("speech_provider", speechClient.Name()),
		slog.String("llm_provider", llmClient.Name()),
		slog.String("storage_dir", cfg.Storage.Dir))

	return &App{Config: cfg, Log: log, DB: db, Service: svc}, nil
}

// Close закрывает соединения с базой данных.
func (a *App) Close() {
	if a == nil {
		return
	}
	a.DB.Close()
	a.Log.Info("соединения с базой данных закрыты")
}
