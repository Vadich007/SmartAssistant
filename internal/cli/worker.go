package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Vadich007/meetnotes/internal/migrations"
	"github.com/Vadich007/meetnotes/internal/worker"
)

func (r *root) workerCmd() *cobra.Command {
	var workerID string

	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Запустить фоновую обработку задач",
		Long: "Долгоживущий процесс: разбирает очередь задач, обращается к speech- и LLM-клиентам\n" +
			"и сохраняет результаты. Останавливается по Ctrl+C (SIGINT) или SIGTERM \n" +
			"новые задачи не берутся, текущие завершаются или возвращаются в очередь.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Воркер работает неограниченно долго, поэтому таймаут команды к нему не применяется.
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			a, err := r.application(ctx)
			if err != nil {
				return err
			}

			pool := worker.New(a.Service, worker.Config{
				WorkerID:        workerID,
				Concurrency:     r.cfg.Worker.Concurrency,
				PollInterval:    r.cfg.Worker.PollInterval,
				BatchSize:       r.cfg.Worker.BatchSize,
				LeaseTTL:        r.cfg.Worker.LeaseTTL,
				JobTimeout:      r.cfg.Worker.JobTimeout,
				ShutdownTimeout: r.cfg.Worker.ShutdownTimeout,
			}, r.log)

			if err := pool.Run(ctx); err != nil {
				return err
			}
			// Соединения с базой закрываются здесь же, до выхода из процесса.
			r.close()
			return nil
		},
	}
	cmd.Flags().StringVar(&workerID, "id", "", "идентификатор процесса-обработчика (по умолчанию случайный)")
	return cmd
}

func (r *root) migrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Управление миграциями базы данных",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		&cobra.Command{
			Use:   "up",
			Short: "Применить все миграции",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				ctx, cancel := context.WithTimeout(cmd.Context(), r.cfg.CommandTimeout)
				defer cancel()
				return migrations.Up(ctx, r.cfg.Database.DSN, r.log)
			},
		},
		&cobra.Command{
			Use:   "down",
			Short: "Откатить последнюю миграцию",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				ctx, cancel := context.WithTimeout(cmd.Context(), r.cfg.CommandTimeout)
				defer cancel()
				return migrations.Down(ctx, r.cfg.Database.DSN, r.log)
			},
		},
		&cobra.Command{
			Use:   "status",
			Short: "Показать состояние миграций",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				ctx, cancel := context.WithTimeout(cmd.Context(), r.cfg.CommandTimeout)
				defer cancel()
				return migrations.Status(ctx, r.cfg.Database.DSN, r.out)
			},
		},
	)
	return cmd
}
