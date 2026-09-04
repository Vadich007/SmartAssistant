package domain

import "fmt"

// JobStatus статус задачи обработки встречи.
type JobStatus string

// Жизненный цикл задачи обработки.
const (
	// StatusCreated задача создана, ждёт воркера
	StatusCreated JobStatus = "created"
	// StatusProcessing задача взята воркером в работу
	StatusProcessing JobStatus = "processing"
	// StatusTranscribed транскрипция получена и сохранена
	StatusTranscribed JobStatus = "transcribed"
	// StatusSummarized краткая выжимка получена и сохранена
	StatusSummarized JobStatus = "summarized"
	// StatusCompleted обработка полностью завершена
	StatusCompleted JobStatus = "completed"
	// StatusFailed обработка завершилась ошибкой
	StatusFailed JobStatus = "failed"
)

// allStatuses перечисляет все допустимые статусы в порядке жизненного цикла.
var allStatuses = []JobStatus{
	StatusCreated, StatusProcessing, StatusTranscribed,
	StatusSummarized, StatusCompleted, StatusFailed,
}

// transitions описывает граф состояний задачи.
var transitions = map[JobStatus][]JobStatus{
	StatusCreated:     {StatusProcessing, StatusFailed},
	StatusProcessing:  {StatusTranscribed, StatusFailed},
	StatusTranscribed: {StatusSummarized, StatusFailed},
	StatusSummarized:  {StatusCompleted, StatusFailed},
	StatusCompleted:   {},
	StatusFailed:      {StatusCreated, StatusProcessing},
}

// AllStatuses возвращает копию списка всех статусов.
func AllStatuses() []JobStatus {
	out := make([]JobStatus, len(allStatuses))
	copy(out, allStatuses)
	return out
}

// Valid сообщает, известен ли статус системе.
func (s JobStatus) Valid() bool {
	_, ok := transitions[s]
	return ok
}

// IsTerminal сообщает, является ли статус конечным.
func (s JobStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusFailed
}

// String реализует fmt.Stringer.
func (s JobStatus) String() string { return string(s) }

// CanTransition сообщает, допустим ли переход из текущего статуса в целевой.
func (s JobStatus) CanTransition(to JobStatus) bool {
	for _, allowed := range transitions[s] {
		if allowed == to {
			return true
		}
	}
	return false
}

// EnsureTransition возвращает ErrConflict, если переход недопустим.
func (s JobStatus) EnsureTransition(to JobStatus) error {
	if !s.Valid() {
		return fmt.Errorf("%w: неизвестный статус %q", ErrConflict, s)
	}
	if !to.Valid() {
		return fmt.Errorf("%w: неизвестный целевой статус %q", ErrConflict, to)
	}
	if !s.CanTransition(to) {
		return fmt.Errorf("%w: переход из %s в %s невозможен", ErrConflict, s, to)
	}
	return nil
}

// ParseStatus разбирает статус из строки.
func ParseStatus(raw string) (JobStatus, error) {
	s := JobStatus(raw)
	if !s.Valid() {
		return "", fmt.Errorf("%w: неизвестный статус %q", ErrInvalidArgument, raw)
	}
	return s, nil
}
