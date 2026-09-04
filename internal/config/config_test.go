package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Vadich007/meetnotes/internal/config"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load("")
	require.NoError(t, err)

	assert.Equal(t, config.ProviderMock, cfg.Speech.Provider)
	assert.Equal(t, config.ProviderMock, cfg.LLM.Provider)
	assert.Equal(t, 3, cfg.Worker.Concurrency)
	assert.Equal(t, 2*time.Second, cfg.Worker.PollInterval)
	assert.Equal(t, int32(10), cfg.Database.MaxConns)
	assert.NotEmpty(t, cfg.Database.DSN)
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("DATABASE_DSN", "postgres://u:p@db:5432/x")
	t.Setenv("WORKER_CONCURRENCY", "7")
	t.Setenv("SPEECH_MOCK_DELAY", "150ms")
	t.Setenv("LOG_FORMAT", "JSON")

	cfg, err := config.Load("")
	require.NoError(t, err)

	assert.Equal(t, "postgres://u:p@db:5432/x", cfg.Database.DSN)
	assert.Equal(t, 7, cfg.Worker.Concurrency)
	assert.Equal(t, 150*time.Millisecond, cfg.Speech.MockDelay)
	assert.Equal(t, "json", cfg.Log.Format)
}

func TestLoadInvalidValues(t *testing.T) {
	t.Run("не число", func(t *testing.T) {
		t.Setenv("WORKER_CONCURRENCY", "много")

		_, err := config.Load("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "WORKER_CONCURRENCY")
	})

	t.Run("не длительность", func(t *testing.T) {
		t.Setenv("JOB_TIMEOUT", "2 минуты")

		_, err := config.Load("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "JOB_TIMEOUT")
	})

	t.Run("неизвестный провайдер", func(t *testing.T) {
		t.Setenv("SPEECH_PROVIDER", "whisper")

		_, err := config.Load("")
		require.ErrorIs(t, err, config.ErrInvalidConfig)
	})

	t.Run("нулевой параллелизм", func(t *testing.T) {
		t.Setenv("WORKER_CONCURRENCY", "0")

		_, err := config.Load("")
		require.ErrorIs(t, err, config.ErrInvalidConfig)
	})
}

func TestValidateCollectsAllProblems(t *testing.T) {
	cfg := config.Config{}

	err := cfg.Validate()
	require.ErrorIs(t, err, config.ErrInvalidConfig)
	assert.Contains(t, err.Error(), "DATABASE_DSN")
	assert.Contains(t, err.Error(), "STORAGE_DIR")
	assert.Contains(t, err.Error(), "WORKER_CONCURRENCY")
}
