package domain

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// User пользователь системы.
type User struct {
	ID         uuid.UUID
	ExternalID string
	CreatedAt  time.Time
}

// Meeting загруженная встреча.
type Meeting struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Title        string
	OriginalName string
	StoredPath   string
	Format       string
	SizeBytes    int64
	CreatedAt    time.Time
}

// Job задача обработки встречи.
type Job struct {
	ID          uuid.UUID
	MeetingID   uuid.UUID
	Status      JobStatus
	Attempts    int
	MaxAttempts int
	LastError   string
	LockedAt    *time.Time
	LockedBy    string
	// NextAttemptAt момент, раньше которого задачу нельзя брать в работу.
	NextAttemptAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// StatusChange запись в истории статусов задачи.
type StatusChange struct {
	ID        uuid.UUID
	JobID     uuid.UUID
	From      JobStatus
	To        JobStatus
	Error     string
	CreatedAt time.Time
}

// Transcript расшифровка встречи.
type Transcript struct {
	ID          uuid.UUID
	MeetingID   uuid.UUID
	Text        string
	Language    string
	DurationSec int
	Provider    string
	CreatedAt   time.Time
}

// Summary краткая выжимка по встрече.
type Summary struct {
	ID        uuid.UUID
	MeetingID uuid.UUID
	Text      string
	Provider  string
	Model     string
	CreatedAt time.Time
}

// QAEntry вопрос пользователя и ответ LLM по материалам встреч.
type QAEntry struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	MeetingID *uuid.UUID
	Question  string
	Answer    string
	CreatedAt time.Time
}

// MeetingListItem строка списка встреч: встреча вместе со статусом обработки и выжимкой.
type MeetingListItem struct {
	Meeting Meeting
	Status  JobStatus
	Summary string
}

// SearchResult результат поиска по сохранённым материалам.
type SearchResult struct {
	Meeting   Meeting
	Status    JobStatus
	Snippet   string
	Rank      float64
	MatchedIn string
}

// MeetingDetails полная карточка встречи.
type MeetingDetails struct {
	Meeting    Meeting
	Job        Job
	Transcript *Transcript
	Summary    *Summary
}

// StatsRow количество задач в конкретном статусе.
type StatsRow struct {
	Status JobStatus
	Count  int
}

// Stats сводка по системе для команды stats.
type Stats struct {
	ByStatus         []StatsRow
	TotalMeetings    int
	AvgProcessingSec float64
	FailedCount      int
}

// supportedFormats расширения файлов, которые система принимает на обработку.
var supportedFormats = map[string]struct{}{
	".wav": {}, ".mp3": {}, ".ogg": {}, ".oga": {}, ".m4a": {},
	".flac": {}, ".opus": {}, ".txt": {}, ".md": {},
}

// NormalizeFormat приводит расширение файла к нормальному виду и проверяет поддержку.
func NormalizeFormat(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return "", fmt.Errorf("%w: у файла %q нет расширения", ErrUnsupportedFormat, filepath.Base(path))
	}
	if _, ok := supportedFormats[ext]; !ok {
		return "", fmt.Errorf("%w: %s", ErrUnsupportedFormat, ext)
	}
	return strings.TrimPrefix(ext, "."), nil
}

// SupportedFormats возвращает отсортированный список поддерживаемых расширений для текстов ошибок и справки.
func SupportedFormats() []string {
	out := make([]string, 0, len(supportedFormats))
	for ext := range supportedFormats {
		out = append(out, ext)
	}
	slices.Sort(out)
	return out
}

// TitleFromFilename формирует заголовок встречи из имени файла.
func TitleFromFilename(name string) string {
	base := filepath.Base(name)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.NewReplacer("_", " ", "-", " ", ".", " ").Replace(base)
	base = strings.Join(strings.Fields(base), " ")
	if base == "" {
		return "Встреча без названия"
	}
	runes := []rune(base)
	return strings.ToUpper(string(runes[0])) + string(runes[1:])
}

// ParseMeetingID разбирает идентификатор встречи из пользовательского ввода.
func ParseMeetingID(raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: %q не является идентификатором встречи", ErrInvalidArgument, raw)
	}
	return id, nil
}
