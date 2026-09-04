// Package migrations хранит SQL-миграции внутри бинарника.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io"
	"log/slog"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed sql/*.sql
var fs embed.FS

const dir = "sql"

func init() {
	goose.SetBaseFS(fs)
	goose.SetLogger(goose.NopLogger())
}

// Up применяет все неприменённые миграции.
func Up(ctx context.Context, dsn string, log *slog.Logger) error {
	return withDB(ctx, dsn, func(db *sql.DB) error {
		if err := goose.UpContext(ctx, db, dir); err != nil {
			return fmt.Errorf("применение миграций: %w", err)
		}
		version, err := goose.GetDBVersionContext(ctx, db)
		if err != nil {
			return fmt.Errorf("чтение версии схемы: %w", err)
		}
		log.InfoContext(ctx, "миграции применены", slog.Int64("version", version))
		return nil
	})
}

// Down откатывает последнюю миграцию.
func Down(ctx context.Context, dsn string, log *slog.Logger) error {
	return withDB(ctx, dsn, func(db *sql.DB) error {
		if err := goose.DownContext(ctx, db, dir); err != nil {
			return fmt.Errorf("откат миграции: %w", err)
		}
		version, err := goose.GetDBVersionContext(ctx, db)
		if err != nil {
			return fmt.Errorf("чтение версии схемы: %w", err)
		}
		log.InfoContext(ctx, "миграция откачена", slog.Int64("version", version))
		return nil
	})
}

// Status печатает состояние миграций в переданный writer.
func Status(ctx context.Context, dsn string, out io.Writer) error {
	return withDB(ctx, dsn, func(db *sql.DB) error {
		goose.SetLogger(log{out: out})
		defer goose.SetLogger(goose.NopLogger())

		if err := goose.StatusContext(ctx, db, dir); err != nil {
			return fmt.Errorf("статус миграций: %w", err)
		}
		return nil
	})
}

func withDB(ctx context.Context, dsn string, fn func(*sql.DB) error) error {
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("настройка диалекта goose: %w", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("подключение к базе данных: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("база данных недоступна: %w", err)
	}
	return fn(db)
}

// log адаптирует io.Writer к интерфейсу логгера goose.
type log struct{ out io.Writer }

func (l log) Fatalf(format string, v ...any) { _, _ = fmt.Fprintf(l.out, format, v...) }
func (l log) Printf(format string, v ...any) { _, _ = fmt.Fprintf(l.out, format, v...) }
