// Package cli адаптер пользовательского интерфейса поверх бизнес-логики.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Vadich007/meetnotes/internal/app"
	"github.com/Vadich007/meetnotes/internal/config"
	"github.com/Vadich007/meetnotes/internal/domain"
	"github.com/Vadich007/meetnotes/internal/logger"
	"github.com/Vadich007/meetnotes/internal/service"
)

// Коды возврата процесса.
const (
	exitOK           = 0
	exitInternal     = 1
	exitInvalidInput = 2
	exitNotFound     = 3
	exitConflict     = 4
)

// root хранит общие флаги и лениво собирает приложение:
// команды migrate и help не должны требовать доступной базы данных.
type root struct {
	out    io.Writer
	errOut io.Writer

	envFile   string
	userID    string
	logLevel  string
	logFormat string
	asJSON    bool
	timeout   time.Duration

	cfg config.Config
	log *slog.Logger
	app *app.App
}

// Execute запускает CLI и возвращает код завершения процесса.
func Execute(ctx context.Context) int {
	cmd, cleanup := NewCommand(os.Stdout, os.Stderr)
	defer cleanup()

	err := cmd.ExecuteContext(ctx)
	if err == nil {
		return exitOK
	}
	fmt.Fprintf(os.Stderr, "Ошибка: %v\n", err)
	return exitCode(err)
}

// NewCommand собирает корневую команду. Отдельный конструктор нужен тестам:
// они подставляют свои writer и проверяют разбор аргументов без запуска процесса.
func NewCommand(out, errOut io.Writer) (*cobra.Command, func()) {
	r := &root{out: out, errOut: errOut}

	cmd := &cobra.Command{
		Use:   "meetnotes",
		Short: "Умный помощник для конспектирования встреч",
		Long: "meetnotes принимает аудиозаписи и текстовые файлы встреч, асинхронно получает\n" +
			"расшифровку и краткую выжимку, хранит их в PostgreSQL и позволяет искать\n" +
			"по сохранённым материалам и задавать по ним вопросы.",
		SilenceUsage:      true,
		SilenceErrors:     true,
		PersistentPreRunE: r.setup,
	}

	flags := cmd.PersistentFlags()
	flags.StringVar(&r.envFile, "env", ".env", "путь к файлу с переменными окружения")
	flags.StringVarP(&r.userID, "user", "u", "", "идентификатор пользователя (или переменная MEETNOTES_USER)")
	flags.StringVar(&r.logLevel, "log-level", "", "уровень логирования: debug|info|warn|error")
	flags.StringVar(&r.logFormat, "log-format", "", "формат логов: text|json")
	flags.BoolVar(&r.asJSON, "json", false, "вывести результат в формате JSON")
	flags.DurationVar(&r.timeout, "timeout", 0, "ограничение времени выполнения команды")

	cmd.SetOut(out)
	cmd.SetErr(errOut)

	cmd.AddCommand(
		r.startCmd(),
		r.loadCmd(),
		r.listCmd(),
		r.statusCmd(),
		r.getCmd(),
		r.findCmd(),
		r.chatCmd(),
		r.retryCmd(),
		r.deleteCmd(),
		r.statsCmd(),
		r.historyCmd(),
		r.workerCmd(),
		r.migrateCmd(),
	)

	return cmd, r.close
}

// setup загружает конфигурацию и настраивает логгер до выполнения любой команды.
func (r *root) setup(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(r.envFile)
	if err != nil {
		return err
	}

	if r.logLevel != "" {
		cfg.Log.Level = r.logLevel
	}
	if r.logFormat != "" {
		cfg.Log.Format = r.logFormat
	}
	if r.timeout > 0 {
		cfg.CommandTimeout = r.timeout
	}
	if r.userID == "" {
		r.userID = cfg.DefaultUser
	}

	r.cfg = cfg
	// Логи идут в stderr, чтобы не смешиваться с результатом команды в stdout.
	r.log = logger.New(r.errOut, cfg.Log.Level, cfg.Log.Format)
	cmd.SetContext(cmd.Context())
	return nil
}

// application лениво создаёт приложение: соединение с БД открывается только
// для команд, которым оно действительно нужно.
func (r *root) application(ctx context.Context) (*app.App, error) {
	if r.app != nil {
		return r.app, nil
	}
	a, err := app.New(ctx, r.cfg, r.log)
	if err != nil {
		return nil, err
	}
	r.app = a
	return a, nil
}

// withService выполняет команду с ограничением времени и готовым сервисом.
func (r *root) withService(cmd *cobra.Command, fn func(ctx context.Context, svc *service.Service) error) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), r.cfg.CommandTimeout)
	defer cancel()

	a, err := r.application(ctx)
	if err != nil {
		return err
	}
	return fn(ctx, a.Service)
}

// requireUser проверяет, что пользователь указан.
func (r *root) requireUser() error {
	if r.userID == "" {
		return fmt.Errorf(
			"%w: укажите пользователя флагом --user или переменной MEETNOTES_USER", domain.ErrInvalidArgument)
	}
	return nil
}

// close освобождает ресурсы приложения.
func (r *root) close() {
	if r.app != nil {
		r.app.Close()
		r.app = nil
	}
}

// exitCode переводит доменную ошибку в код завершения процесса.
func exitCode(err error) int {
	switch {
	case errors.Is(err, domain.ErrInvalidArgument),
		errors.Is(err, domain.ErrUnsupportedFormat),
		errors.Is(err, domain.ErrFileNotFound):
		return exitInvalidInput
	case errors.Is(err, domain.ErrNotFound):
		return exitNotFound
	case errors.Is(err, domain.ErrConflict),
		errors.Is(err, domain.ErrNoContext):
		return exitConflict
	default:
		return exitInternal
	}
}
