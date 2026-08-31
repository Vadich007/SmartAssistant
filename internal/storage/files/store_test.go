package files_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Vadich007/meetnotes/internal/domain"
	"github.com/Vadich007/meetnotes/internal/storage/files"
)

func TestSaveCopiesFile(t *testing.T) {
	t.Parallel()

	src := filepath.Join(t.TempDir(), "meeting.txt")
	content := []byte("Расшифровка встречи.")
	require.NoError(t, os.WriteFile(src, content, 0o600))

	store, err := files.NewStore(filepath.Join(t.TempDir(), "uploads"))
	require.NoError(t, err)

	stored, size, err := store.Save(context.Background(), src)
	require.NoError(t, err)

	assert.EqualValues(t, len(content), size)
	assert.Equal(t, ".txt", filepath.Ext(stored))

	require.NoError(t, os.Remove(src))
	got, err := os.ReadFile(stored)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestSaveRejectsBadInput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store, err := files.NewStore(filepath.Join(dir, "uploads"))
	require.NoError(t, err)

	_, _, err = store.Save(context.Background(), filepath.Join(dir, "нет-файла.wav"))
	assert.ErrorIs(t, err, domain.ErrFileNotFound)

	_, _, err = store.Save(context.Background(), dir)
	assert.ErrorIs(t, err, domain.ErrInvalidArgument)

	empty := filepath.Join(dir, "empty.txt")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))
	_, _, err = store.Save(context.Background(), empty)
	assert.ErrorIs(t, err, domain.ErrInvalidArgument)
}

func TestRemoveIsIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	src := filepath.Join(dir, "meeting.txt")
	require.NoError(t, os.WriteFile(src, []byte("текст"), 0o600))

	store, err := files.NewStore(filepath.Join(dir, "uploads"))
	require.NoError(t, err)

	stored, _, err := store.Save(context.Background(), src)
	require.NoError(t, err)

	require.NoError(t, store.Remove(stored))
	require.NoError(t, store.Remove(stored))
	require.NoError(t, store.Remove(""))
}

func TestNewStoreValidatesDir(t *testing.T) {
	t.Parallel()

	_, err := files.NewStore("  ")
	assert.ErrorIs(t, err, domain.ErrInvalidArgument)

	dir := filepath.Join(t.TempDir(), "вложенный", "каталог")
	_, err = files.NewStore(dir)
	require.NoError(t, err)
	assert.DirExists(t, dir)
}
