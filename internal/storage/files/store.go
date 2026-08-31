// Package files реализует файловое хранилище загруженных встреч.
package files

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/Vadich007/meetnotes/internal/domain"
)

// Store хранилище файлов в локальной файловой системе.
type Store struct {
	dir string
}

// NewStore создаёт хранилище и каталог, если его нет.
func NewStore(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("%w: не задан каталог хранилища", domain.ErrInvalidArgument)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("создание каталога хранилища %s: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// Save копирует файл в хранилище под уникальным именем и возвращает путь и размер копии.
func (s *Store) Save(ctx context.Context, srcPath string) (string, int64, error) {
	info, err := os.Stat(srcPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", 0, fmt.Errorf("%w: %s", domain.ErrFileNotFound, srcPath)
		}
		return "", 0, fmt.Errorf("доступ к файлу %s: %w", srcPath, err)
	}
	if info.IsDir() {
		return "", 0, fmt.Errorf("%w: %s это каталог, а не файл", domain.ErrInvalidArgument, srcPath)
	}
	if info.Size() == 0 {
		return "", 0, fmt.Errorf("%w: файл %s пуст", domain.ErrInvalidArgument, srcPath)
	}
	if err := ctx.Err(); err != nil {
		return "", 0, err
	}

	src, err := os.Open(srcPath)
	if err != nil {
		return "", 0, fmt.Errorf("чтение файла %s: %w", srcPath, err)
	}
	defer func() { _ = src.Close() }()

	dstPath := filepath.Join(s.dir, uuid.NewString()+strings.ToLower(filepath.Ext(srcPath)))
	dst, err := os.Create(dstPath)
	if err != nil {
		return "", 0, fmt.Errorf("создание файла %s: %w", dstPath, err)
	}

	written, err := io.Copy(dst, src)
	if closeErr := dst.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(dstPath)
		return "", 0, fmt.Errorf("копирование файла в хранилище: %w", err)
	}
	return dstPath, written, nil
}

// Remove удаляет файл из хранилища.
func (s *Store) Remove(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("удаление файла %s: %w", path, err)
	}
	return nil
}
