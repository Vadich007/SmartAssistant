// Package config загружает конфигурацию приложения.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// ErrInvalidConfig возвращается, если конфигурация не прошла валидацию.
var ErrInvalidConfig = errors.New("некорректная конфигурация")

// Config полная конфигурация приложения.
type Config struct {
	Database Database
	Storage  Storage
	Log      Log
	Speech   Speech
	LLM      LLM
	Worker   Worker

	// DefaultUser идентификатор пользователя по умолчанию для CLI (флаг --user важнее).
	DefaultUser string
	// CommandTimeout ограничивает время выполнения одной CLI-команды.
	CommandTimeout time.Duration
}

// Database параметры подключения к PostgreSQL.
type Database struct {
	DSN            string
	MaxConns       int32
	ConnectTimeout time.Duration
}

// Storage параметры файлового хранилища загруженных встреч.
type Storage struct {
	Dir string
}

// Log параметры структурированного логирования.
type Log struct {
	Level  string
	Format string
}

// Speech параметры клиента распознавания речи.
type Speech struct {
	Provider  string
	Timeout   time.Duration
	MockDir   string
	MockDelay time.Duration
}

// LLM параметры клиента языковой модели.
type LLM struct {
	Provider  string
	Timeout   time.Duration
	MockDelay time.Duration
}

// Worker параметры фоновой обработки задач.
type Worker struct {
	Concurrency     int
	PollInterval    time.Duration
	BatchSize       int
	LeaseTTL        time.Duration
	MaxAttempts     int
	JobTimeout      time.Duration
	ShutdownTimeout time.Duration
}

const (
	ProviderMock         = "mock"
	ProviderYandexSpeech = "yandex_speech"
	ProviderSaluteSpeech = "salute_speech"
	ProviderGigaChat     = "gigachat"
)

// Load читает .env, затем переменные окружения и возвращает готовую конфигурацию.
// Переменные окружения имеют приоритет над значениями из .env.
func Load(envFile string) (Config, error) {
	if envFile != "" {
		if err := godotenv.Load(envFile); err != nil && !os.IsNotExist(err) {
			return Config{}, fmt.Errorf("чтение %s: %w", envFile, err)
		}
	}

	var errs []error
	pick := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}

	cfg := Config{
		Database: Database{
			DSN: envString("DATABASE_DSN", "postgres://meetnotes:meetnotes@localhost:5433/meetnotes?sslmode=disable"),
		},
		Storage: Storage{
			Dir: envString("STORAGE_DIR", "./data/uploads"),
		},
		Log: Log{
			Level:  strings.ToLower(envString("LOG_LEVEL", "info")),
			Format: strings.ToLower(envString("LOG_FORMAT", "text")),
		},
		Speech: Speech{
			Provider: strings.ToLower(envString("SPEECH_PROVIDER", ProviderMock)),
			MockDir:  envString("SPEECH_MOCK_DIR", "./testdata/transcripts"),
		},
		LLM: LLM{
			Provider: strings.ToLower(envString("LLM_PROVIDER", ProviderMock)),
		},
		DefaultUser: envString("MEETNOTES_USER", ""),
	}

	maxConns, err := envInt("DATABASE_MAX_CONNS", 10)
	pick(err)
	cfg.Database.MaxConns = int32(maxConns)
	cfg.Database.ConnectTimeout, err = envDuration("DATABASE_CONNECT_TIMEOUT", 5*time.Second)
	pick(err)

	cfg.Speech.Timeout, err = envDuration("SPEECH_TIMEOUT", 30*time.Second)
	pick(err)
	cfg.Speech.MockDelay, err = envDuration("SPEECH_MOCK_DELAY", 2*time.Second)
	pick(err)

	cfg.LLM.Timeout, err = envDuration("LLM_TIMEOUT", 30*time.Second)
	pick(err)
	cfg.LLM.MockDelay, err = envDuration("LLM_MOCK_DELAY", time.Second)
	pick(err)

	cfg.Worker.Concurrency, err = envInt("WORKER_CONCURRENCY", 3)
	pick(err)
	cfg.Worker.BatchSize, err = envInt("WORKER_BATCH_SIZE", 5)
	pick(err)
	cfg.Worker.MaxAttempts, err = envInt("WORKER_MAX_ATTEMPTS", 3)
	pick(err)
	cfg.Worker.PollInterval, err = envDuration("WORKER_POLL_INTERVAL", 2*time.Second)
	pick(err)
	cfg.Worker.LeaseTTL, err = envDuration("WORKER_LEASE_TTL", 5*time.Minute)
	pick(err)
	cfg.Worker.JobTimeout, err = envDuration("JOB_TIMEOUT", 2*time.Minute)
	pick(err)
	cfg.Worker.ShutdownTimeout, err = envDuration("SHUTDOWN_TIMEOUT", 15*time.Second)
	pick(err)

	cfg.CommandTimeout, err = envDuration("COMMAND_TIMEOUT", 30*time.Second)
	pick(err)

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate проверяет непротиворечивость значений конфигурации.
func (c Config) Validate() error {
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(format, args...))
	}

	if strings.TrimSpace(c.Database.DSN) == "" {
		add("DATABASE_DSN не задан")
	}
	if c.Database.MaxConns < 1 {
		add("DATABASE_MAX_CONNS должен быть больше нуля, получено %d", c.Database.MaxConns)
	}
	if strings.TrimSpace(c.Storage.Dir) == "" {
		add("STORAGE_DIR не задан")
	}
	if !isOneOf(c.Log.Level, "debug", "info", "warn", "error") {
		add("LOG_LEVEL должен быть debug|info|warn|error, получено %q", c.Log.Level)
	}
	if !isOneOf(c.Log.Format, "json", "text") {
		add("LOG_FORMAT должен быть json|text, получено %q", c.Log.Format)
	}
	if !isOneOf(c.Speech.Provider, ProviderMock, ProviderYandexSpeech, ProviderSaluteSpeech) {
		add("SPEECH_PROVIDER должен быть mock|yandex_speech|salute_speech, получено %q", c.Speech.Provider)
	}
	if !isOneOf(c.LLM.Provider, ProviderMock, ProviderGigaChat) {
		add("LLM_PROVIDER должен быть mock|gigachat, получено %q", c.LLM.Provider)
	}
	if c.Worker.Concurrency < 1 {
		add("WORKER_CONCURRENCY должен быть больше нуля, получено %d", c.Worker.Concurrency)
	}
	if c.Worker.BatchSize < 1 {
		add("WORKER_BATCH_SIZE должен быть больше нуля, получено %d", c.Worker.BatchSize)
	}
	if c.Worker.MaxAttempts < 1 {
		add("WORKER_MAX_ATTEMPTS должен быть больше нуля, получено %d", c.Worker.MaxAttempts)
	}
	if c.Worker.PollInterval <= 0 {
		add("WORKER_POLL_INTERVAL должен быть положительным")
	}
	if c.Worker.LeaseTTL <= 0 {
		add("WORKER_LEASE_TTL должен быть положительным")
	}
	if c.Worker.JobTimeout <= 0 {
		add("JOB_TIMEOUT должен быть положительным")
	}
	if c.CommandTimeout <= 0 {
		add("COMMAND_TIMEOUT должен быть положительным")
	}

	if len(errs) > 0 {
		return fmt.Errorf("%w: %w", ErrInvalidConfig, errors.Join(errs...))
	}
	return nil
}

func envString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return def
}

func envInt(key string, def int) (int, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return def, nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s: ожидалось целое число, получено %q", key, raw)
	}
	return v, nil
}

func envDuration(key string, def time.Duration) (time.Duration, error) {
	raw, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(raw) == "" {
		return def, nil
	}
	v, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("%s: ожидалась длительность (например 30s), получено %q", key, raw)
	}
	return v, nil
}

func isOneOf(value string, allowed ...string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	return false
}
