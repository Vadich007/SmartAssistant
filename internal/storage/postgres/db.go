// Package postgres — реализация хранилища на PostgreSQL (pgx/v5).
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vadich007/meetnotes/internal/config"
)

// Querier —бщий набор операций, который поддерживают и пул, и транзакция.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// DB владеет пулом соединений и выполняет функции внутри транзакции.
type DB struct {
	pool *pgxpool.Pool
}

// txKey ключ, под которым активная транзакция кладётся в context.
type txKey struct{}

// New открывает пул соединений и проверяет доступность базы.
func New(ctx context.Context, cfg config.Database) (*DB, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("разбор DATABASE_DSN: %w", err)
	}
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MaxConnLifetime = time.Hour
	poolCfg.MaxConnIdleTime = 5 * time.Minute

	connectCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, poolCfg)
	if err != nil {
		return nil, wrapError(fmt.Errorf("создание пула соединений: %w", err))
	}
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		return nil, wrapError(fmt.Errorf("проверка соединения с базой данных: %w", err))
	}
	return &DB{pool: pool}, nil
}

// Close закрывает пул соединений.
func (db *DB) Close() {
	if db != nil && db.pool != nil {
		db.pool.Close()
	}
}

// Pool отдаёт пул.
func (db *DB) Pool() *pgxpool.Pool { return db.pool }

// Querier возвращает транзакцию из context, если она есть, иначе — пул.
func (db *DB) Querier(ctx context.Context) Querier {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok && tx != nil {
		return tx
	}
	return db.pool
}

// WithinTx выполняет fn в транзакции: коммит при успехе, откат при ошибке или панике.
func (db *DB) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok && tx != nil {
		return fn(ctx)
	}

	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return wrapError(fmt.Errorf("начало транзакции: %w", err))
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return wrapError(fmt.Errorf("коммит транзакции: %w", err))
	}
	committed = true
	return nil
}
