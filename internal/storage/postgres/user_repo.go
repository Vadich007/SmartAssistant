package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/Vadich007/meetnotes/internal/domain"
)

// UserRepo — хранилище пользователей.
type UserRepo struct{ db *DB }

// NewUserRepo создаёт репозиторий пользователей.
func NewUserRepo(db *DB) *UserRepo { return &UserRepo{db: db} }

// Ensure возвращает пользователя по внешнему идентификатору, создавая его при первом обращении.
// Это же поведение обслуживает команду start и автосоздание пользователя в CLI.
func (r *UserRepo) Ensure(ctx context.Context, externalID string) (domain.User, bool, error) {
	const query = `
		INSERT INTO users (id, external_id)
		VALUES ($1, $2)
		ON CONFLICT (external_id) DO UPDATE SET external_id = EXCLUDED.external_id
		RETURNING id, external_id, created_at, (xmax = 0) AS inserted`

	var (
		user     domain.User
		inserted bool
	)
	err := r.db.Querier(ctx).
		QueryRow(ctx, query, uuid.New(), externalID).
		Scan(&user.ID, &user.ExternalID, &user.CreatedAt, &inserted)
	if err != nil {
		return domain.User{}, false, wrapError(fmt.Errorf("создание пользователя: %w", err))
	}
	return user, inserted, nil
}

// GetByExternalID возвращает пользователя по внешнему идентификатору.
func (r *UserRepo) GetByExternalID(ctx context.Context, externalID string) (domain.User, error) {
	const query = `SELECT id, external_id, created_at FROM users WHERE external_id = $1`

	var user domain.User
	err := r.db.Querier(ctx).QueryRow(ctx, query, externalID).
		Scan(&user.ID, &user.ExternalID, &user.CreatedAt)
	if err != nil {
		return domain.User{}, wrapError(fmt.Errorf("поиск пользователя %q: %w", externalID, err))
	}
	return user, nil
}
