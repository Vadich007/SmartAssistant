package speech_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Vadich007/meetnotes/internal/clients/speech"
	"github.com/Vadich007/meetnotes/internal/config"
	"github.com/Vadich007/meetnotes/internal/domain"
	"github.com/Vadich007/meetnotes/internal/service"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestMockReadsTextFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := writeFile(t, dir, "meeting.txt", "Обсудили план релиза. Решили выкатить в среду.")

	client := speech.NewMock(speech.MockOptions{})
	res, err := client.Transcribe(context.Background(), service.TranscribeRequest{
		MeetingID: uuid.New(), FilePath: path, Format: "txt",
	})

	require.NoError(t, err)
	assert.Equal(t, "Обсудили план релиза. Решили выкатить в среду.", res.Text)
	assert.Equal(t, "ru", res.Language)
	assert.Equal(t, speech.ProviderMock, res.Provider)
	assert.Positive(t, res.DurationSec)
}

func TestMockUsesPreparedTranscript(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	prepared := t.TempDir()
	audio := writeFile(t, dir, "weekly.mp3", "не текст, а аудио")
	writeFile(t, prepared, "weekly.txt", "Заранее подготовленная расшифровка.")

	client := speech.NewMock(speech.MockOptions{Dir: prepared})
	res, err := client.Transcribe(context.Background(), service.TranscribeRequest{FilePath: audio})

	require.NoError(t, err)
	assert.Equal(t, "Заранее подготовленная расшифровка.", res.Text)
}

func TestMockGeneratesDeterministicTranscript(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	audio := writeFile(t, dir, "planning.mp3", "аудио")

	client := speech.NewMock(speech.MockOptions{})
	first, err := client.Transcribe(context.Background(), service.TranscribeRequest{FilePath: audio})
	require.NoError(t, err)
	second, err := client.Transcribe(context.Background(), service.TranscribeRequest{FilePath: audio})
	require.NoError(t, err)

	assert.NotEmpty(t, first.Text)
	assert.Equal(t, first.Text, second.Text, "тестовый клиент должен быть детерминированным")
}

func TestMockMissingFile(t *testing.T) {
	t.Parallel()

	client := speech.NewMock(speech.MockOptions{})

	_, err := client.Transcribe(context.Background(), service.TranscribeRequest{FilePath: "нет-такого-файла.wav"})
	assert.ErrorIs(t, err, domain.ErrFileNotFound)

	_, err = client.Transcribe(context.Background(), service.TranscribeRequest{FilePath: ""})
	assert.ErrorIs(t, err, domain.ErrInvalidArgument)
}

func TestMockRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	audio := writeFile(t, dir, "long.mp3", "аудио")

	client := speech.NewMock(speech.MockOptions{Delay: 5 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := client.Transcribe(ctx, service.TranscribeRequest{FilePath: audio})

	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), time.Second, "ожидание должно прерываться по context")
}

func TestFactorySelectsImplementation(t *testing.T) {
	t.Parallel()

	client, err := speech.New(config.Speech{Provider: config.ProviderMock})
	require.NoError(t, err)
	assert.Equal(t, speech.ProviderMock, client.Name())

	_, err = speech.New(config.Speech{Provider: config.ProviderYandexSpeech})
	assert.ErrorIs(t, err, domain.ErrProviderNotConfigured)

	_, err = speech.New(config.Speech{Provider: "whisper"})
	assert.ErrorIs(t, err, domain.ErrProviderNotConfigured)
}
