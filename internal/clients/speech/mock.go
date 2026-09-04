// Package speech содержит реализации абстрактного клиента распознавания речи (интерфейс service.SpeechClient).
package speech

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Vadich007/meetnotes/internal/domain"
	"github.com/Vadich007/meetnotes/internal/service"
)

const (
	// ProviderMock имя тестового провайдера, попадает в поле provider транскрипции.
	ProviderMock = "mock"
	// failMarker в имени файла заставляет тестовый клиент вернуть ошибку.
	failMarker = "_fail"
)

var (
	topics = []string{
		"планы на спринт", "результаты релиза", "проблемы на проде",
		"найм в команду", "бюджет на квартал", "переход на новую схему данных",
	}
	owners = []string{"Анна", "Борис", "Виктор", "Галина", "Дмитрий"}
)

// MockOptions настраивает поведение тестового клиента.
type MockOptions struct {
	// Delay имитирует длительность распознавания. Ожидание прерывается по context.
	Delay time.Duration
	// Dir каталог с заранее подготовленными транскрипциями (<имя файла>.txt).
	Dir string
}

// Mock тестовая реализация клиента распознавания речи.
type Mock struct {
	opts MockOptions
}

// NewMock создаёт тестовый speech-клиент.
func NewMock(opts MockOptions) *Mock { return &Mock{opts: opts} }

// Name возвращает имя провайдера.
func (m *Mock) Name() string { return ProviderMock }

// Transcribe возвращает заранее подготовленную или сгенерированную расшифровку.
func (m *Mock) Transcribe(ctx context.Context, req service.TranscribeRequest) (service.TranscribeResult, error) {
	if strings.TrimSpace(req.FilePath) == "" {
		return service.TranscribeResult{}, fmt.Errorf("%w: не указан путь к файлу", domain.ErrInvalidArgument)
	}
	if _, err := os.Stat(req.FilePath); err != nil {
		return service.TranscribeResult{}, fmt.Errorf("%w: %s", domain.ErrFileNotFound, req.FilePath)
	}

	// Имитация долгой операции: ждём либо истечения задержки, либо отмены/таймаута.
	if err := sleepCtx(ctx, m.opts.Delay); err != nil {
		return service.TranscribeResult{}, err
	}

	name := filepath.Base(req.FilePath)
	if strings.Contains(strings.ToLower(name), failMarker) {
		return service.TranscribeResult{}, fmt.Errorf(
			"%w: тестовый speech-клиент не смог распознать %q", domain.ErrExternalUnavailable, name)
	}

	text, err := m.lookupText(req)
	if err != nil {
		return service.TranscribeResult{}, err
	}
	if strings.TrimSpace(text) == "" {
		return service.TranscribeResult{}, fmt.Errorf("%w: пустая транскрипция для %q", domain.ErrEmptyResult, name)
	}

	language := req.Language
	if language == "" {
		language = "ru"
	}
	return service.TranscribeResult{
		Text:        strings.TrimSpace(text),
		Language:    language,
		DurationSec: estimateDuration(text),
		Provider:    ProviderMock,
	}, nil
}

// lookupText ищет транскрипцию по трём правилам, от точного к общему:
//  1. текстовый файл встречи это его содержимое и есть «расшифровка»;
//  2. файл <имя>.txt рядом с исходным файлом или в каталоге Dir;
//  3. детерминированная заглушка, собранная по имени файла.
func (m *Mock) lookupText(req service.TranscribeRequest) (string, error) {
	path := req.FilePath
	ext := strings.ToLower(filepath.Ext(path))

	if ext == ".txt" || ext == ".md" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("чтение тестового файла %s: %w", path, err)
		}
		return string(data), nil
	}

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	candidates := []string{
		strings.TrimSuffix(path, filepath.Ext(path)) + ".txt",
	}
	if m.opts.Dir != "" {
		candidates = append(candidates, filepath.Join(m.opts.Dir, base+".txt"))
	}

	for _, candidate := range candidates {
		data, err := os.ReadFile(candidate)
		if err == nil {
			return string(data), nil
		}
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("чтение подготовленной транскрипции %s: %w", candidate, err)
		}
	}
	return generateTranscript(base), nil
}

// generateTranscript собирает правдоподобную расшифровку, одинаковую для одного имени файла.
func generateTranscript(base string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(base))
	seed := int(h.Sum32())

	topic := topics[seed%len(topics)]
	first, second := owners[seed%len(owners)], owners[(seed+2)%len(owners)]

	title := domain.TitleFromFilename(base)
	return strings.Join([]string{
		fmt.Sprintf("Встреча «%s». Участники обсудили %s.", title, topic),
		fmt.Sprintf("%s рассказал о текущем статусе: основные работы идут по плану, отставания нет.", first),
		fmt.Sprintf("%s предложил вынести спорные вопросы в отдельную встречу на следующей неделе.", second),
		"Решили: зафиксировать договорённости в задачах и вернуться к теме на следующем созвоне.",
		fmt.Sprintf("Ответственный: %s, срок до конца недели.", first),
	}, " ")
}

// estimateDuration грубо оценивает длительность записи: примерно 150 слов в минуту.
func estimateDuration(text string) int {
	words := len(strings.Fields(text))
	seconds := words * 60 / 150
	if seconds < 1 {
		return 1
	}
	return seconds
}

// sleepCtx ждёт указанное время, но прерывается, если context отменён или истёк таймаут.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
