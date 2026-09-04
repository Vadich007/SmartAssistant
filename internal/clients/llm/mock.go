// Package llm содержит реализации абстрактного LLM-клиента (интерфейс service.LLMClient).
package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Vadich007/meetnotes/internal/domain"
	"github.com/Vadich007/meetnotes/internal/service"
)

const (
	// Имя и тестового провайдера.
	ProviderMock = "mock"
	// Модель тестового провайдера.
	ModelMock = "mock-extractive-v1"
	// failMarker в тексте транскрипции заставляет тестовый клиент вернуть ошибку.
	failMarker = "llm_fail"
)

var (
	// decisionMarkers — слова, по которым тестовая модель узнаёт договорённости.
	decisionMarkers = []string{
		"решили", "решено", "договорились", "нужно", "надо", "ответственн",
		"срок", "дедлайн", "задача", "поручено", "итог",
	}
	// stopWords — служебные слова, которые не несут смысла при сопоставлении.
	stopWords = map[string]struct{}{
		"что": {}, "как": {}, "это": {}, "для": {}, "все": {}, "они": {}, "его": {},
		"или": {}, "так": {}, "уже": {}, "нас": {}, "the": {}, "and": {}, "was": {},
	}
)

type scored struct {
	text    string
	title   string
	score   float64
	created time.Time
}

// MockOptions настраивает поведение тестового LLM-клиента.
type MockOptions struct {
	// Delay имитирует время ответа модели; ожидание прерывается по context.
	Delay time.Duration
}

// Mock — тестовая реализация LLM-клиента.
type Mock struct {
	opts MockOptions
}

// NewMock создаёт тестовый LLM-клиент.
func NewMock(opts MockOptions) *Mock { return &Mock{opts: opts} }

// Name возвращает имя провайдера.
func (m *Mock) Name() string { return ProviderMock }

// Summarize строит краткую выжимку.
func (m *Mock) Summarize(ctx context.Context, transcript string) (service.SummaryResult, error) {
	if strings.TrimSpace(transcript) == "" {
		return service.SummaryResult{}, fmt.Errorf("%w: пустая транскрипция", domain.ErrInvalidArgument)
	}
	if err := sleepCtx(ctx, m.opts.Delay); err != nil {
		return service.SummaryResult{}, err
	}
	if strings.Contains(strings.ToLower(transcript), failMarker) {
		return service.SummaryResult{}, fmt.Errorf("%w: тестовый LLM-клиент недоступен", domain.ErrExternalUnavailable)
	}

	sentences := splitSentences(transcript)
	if len(sentences) == 0 {
		return service.SummaryResult{}, fmt.Errorf("%w: не удалось выделить предложения", domain.ErrEmptyResult)
	}

	var b strings.Builder
	b.WriteString("Кратко о встрече:\n")
	for _, s := range keySentences(sentences, 3) {
		b.WriteString("• ")
		b.WriteString(s)
		b.WriteString("\n")
	}

	if decisions := decisionSentences(sentences, 3); len(decisions) > 0 {
		b.WriteString("\nДоговорённости и решения:\n")
		for _, s := range decisions {
			b.WriteString("→ ")
			b.WriteString(s)
			b.WriteString("\n")
		}
	}

	return service.SummaryResult{
		Text:     strings.TrimSpace(b.String()),
		Provider: ProviderMock,
		Model:    ModelMock,
	}, nil
}

// Answer отвечает на вопрос по переданному контексту встреч.
func (m *Mock) Answer(ctx context.Context, question string, contexts []service.MeetingContext) (service.AnswerResult, error) {
	if strings.TrimSpace(question) == "" {
		return service.AnswerResult{}, fmt.Errorf("%w: пустой вопрос", domain.ErrInvalidArgument)
	}
	if len(contexts) == 0 {
		return service.AnswerResult{}, fmt.Errorf("%w: не передан контекст встреч", domain.ErrNoContext)
	}
	if err := sleepCtx(ctx, m.opts.Delay); err != nil {
		return service.AnswerResult{}, err
	}
	for _, c := range contexts {
		if strings.Contains(strings.ToLower(c.Transcript), failMarker) {
			return service.AnswerResult{}, fmt.Errorf("%w: тестовый LLM-клиент недоступен", domain.ErrExternalUnavailable)
		}
	}

	queryTokens := tokenSet(question)
	var candidates []scored
	for _, c := range contexts {
		body := c.Transcript
		if body == "" {
			body = c.Summary
		}
		for _, sentence := range splitSentences(body) {
			score := overlapScore(queryTokens, tokenSet(sentence))
			if score > 0 {
				candidates = append(candidates, scored{text: sentence, title: c.Title, score: score, created: c.CreatedAt})
			}
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })

	var b strings.Builder
	b.WriteString("Вопрос: ")
	b.WriteString(strings.TrimSpace(question))
	b.WriteString("\n\n")

	if len(candidates) == 0 {
		b.WriteString("В сохранённых материалах прямого ответа не нашлось. Вот о чём были эти встречи:\n")
		for _, c := range contexts {
			summary := firstLine(c.Summary)
			if summary == "" {
				summary = firstLine(c.Transcript)
			}
			b.WriteString(fmt.Sprintf("• %s (%s): %s\n", c.Title, c.CreatedAt.Format("02.01.2006"), summary))
		}
	} else {
		b.WriteString("По материалам встреч:\n")
		limit := min(len(candidates), 3)
		for _, c := range candidates[:limit] {
			b.WriteString(fmt.Sprintf("• %s — «%s»\n", c.title, c.text))
		}
	}

	return service.AnswerResult{
		Text:     strings.TrimSpace(b.String()),
		Provider: ProviderMock,
		Model:    ModelMock,
	}, nil
}

func decisionSentences(sentences []string, limit int) []string {
	var out []string
	for _, s := range sentences {
		lower := strings.ToLower(s)
		for _, marker := range decisionMarkers {
			if strings.Contains(lower, marker) {
				out = append(out, s)
				break
			}
		}
		if len(out) == limit {
			break
		}
	}
	return out
}

// keySentences выбирает информативные предложения.
func keySentences(sentences []string, limit int) []string {
	if len(sentences) <= limit {
		return sentences
	}

	type scored struct {
		idx   int
		text  string
		score int
	}
	rest := make([]scored, 0, len(sentences)-1)
	for i, s := range sentences[1:] {
		rest = append(rest, scored{idx: i + 1, text: s, score: len(tokenSet(s))})
	}
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].score > rest[j].score })

	picked := rest[:min(limit-1, len(rest))]
	sort.SliceStable(picked, func(i, j int) bool { return picked[i].idx < picked[j].idx })

	out := []string{sentences[0]}
	for _, s := range picked {
		out = append(out, s.text)
	}
	return out
}

// splitSentences режет текст на предложения по завершающей пунктуации и переводам строк.
func splitSentences(text string) []string {
	var (
		out     []string
		current strings.Builder
	)
	flush := func() {
		s := strings.TrimSpace(current.String())
		current.Reset()
		if len([]rune(s)) >= 3 {
			out = append(out, s)
		}
	}

	for _, r := range text {
		if r == '\n' {
			flush()
			continue
		}
		current.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			flush()
		}
	}
	flush()
	return out
}

// tokenSet превращает текст в множество нормализованных основ слов.
func tokenSet(text string) map[string]struct{} {
	const stemLen = 6

	set := make(map[string]struct{})
	for _, field := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		runes := []rune(field)
		if len(runes) < 3 {
			continue
		}
		if _, ok := stopWords[field]; ok {
			continue
		}
		if len(runes) > stemLen {
			runes = runes[:stemLen]
		}
		set[string(runes)] = struct{}{}
	}
	return set
}

// overlapScore — доля слов вопроса, встретившихся в предложении.
func overlapScore(query, candidate map[string]struct{}) float64 {
	if len(query) == 0 {
		return 0
	}
	matched := 0
	for token := range query {
		if _, ok := candidate[token]; ok {
			matched++
		}
	}
	return float64(matched) / float64(len(query))
}

func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}

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
