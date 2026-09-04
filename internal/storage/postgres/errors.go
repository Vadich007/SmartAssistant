package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Vadich007/meetnotes/internal/domain"
)

// Коды ошибок PostgreSQL, которые различает приложение.
const (
	codeUniqueViolation     = "23505"
	codeForeignKeyViolation = "23503"
	codeCheckViolation      = "23514"
)

func wrapError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return fmt.Errorf("%w: %w", domain.ErrNotFound, err)
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return err
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case codeUniqueViolation:
			return fmt.Errorf("%w: нарушено ограничение уникальности (%s)", domain.ErrConflict, pgErr.ConstraintName)
		case codeForeignKeyViolation:
			return fmt.Errorf("%w: связанная запись отсутствует (%s)", domain.ErrConflict, pgErr.ConstraintName)
		case codeCheckViolation:
			return fmt.Errorf("%w: нарушено ограничение %s", domain.ErrConflict, pgErr.ConstraintName)
		}
		return fmt.Errorf("ошибка базы данных: %w", err)
	}

	var connErr *pgconn.ConnectError
	if errors.As(err, &connErr) {
		return fmt.Errorf("%w: %w", domain.ErrStorageUnavailable, err)
	}
	if pgconn.SafeToRetry(err) {
		return fmt.Errorf("%w: %w", domain.ErrStorageUnavailable, err)
	}
	return err
}
