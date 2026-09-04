package llm_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Vadich007/meetnotes/internal/clients/llm"
	"github.com/Vadich007/meetnotes/internal/config"
	"github.com/Vadich007/meetnotes/internal/domain"
	"github.com/Vadich007/meetnotes/internal/service"
)

const transcript = `Планёрка команды платформы.
Анна рассказала о статусе миграции на новую схему хранения данных.
Борис описал инцидент на проде и предложил добавить нагрузочное тестирование.
Решили: Борис готовит план нагрузочного тестирования до среды.
Ответственный за миграцию - Анна, срок - конец недели.`

func TestSummarize(t *testing.T) {
	t.Parallel()

	client := llm.NewMock(llm.MockOptions{})
	res, err := client.Summarize(context.Background(), transcript)

	require.NoError(t, err)
	assert.Equal(t, llm.ProviderMock, res.Provider)
	assert.Equal(t, llm.ModelMock, res.Model)
	assert.Contains(t, res.Text, "Кратко о встрече")
	assert.Contains(t, res.Text, "Договорённости и решения")
	assert.Contains(t, res.Text, "нагрузочного тестирования")
}

func TestSummarizeDeterministic(t *testing.T) {
	t.Parallel()

	client := llm.NewMock(llm.MockOptions{})
	first, err := client.Summarize(context.Background(), transcript)
	require.NoError(t, err)
	second, err := client.Summarize(context.Background(), transcript)
	require.NoError(t, err)

	assert.Equal(t, first.Text, second.Text)
}

func TestSummarizeErrors(t *testing.T) {
	t.Parallel()

	client := llm.NewMock(llm.MockOptions{})

	_, err := client.Summarize(context.Background(), "   ")
	assert.ErrorIs(t, err, domain.ErrInvalidArgument)

	_, err = client.Summarize(context.Background(), "встреча с маркером llm_fail внутри")
	assert.ErrorIs(t, err, domain.ErrExternalUnavailable)
}

func TestAnswerUsesContext(t *testing.T) {
	t.Parallel()

	client := llm.NewMock(llm.MockOptions{})
	res, err := client.Answer(context.Background(), "что решили по нагрузочному тестированию?",
		[]service.MeetingContext{{
			MeetingID:  uuid.New(),
			Title:      "Планёрка платформы",
			CreatedAt:  time.Now(),
			Transcript: transcript,
		}})

	require.NoError(t, err)
	assert.Contains(t, res.Text, "Планёрка платформы")
	assert.True(t, strings.Contains(res.Text, "нагрузочного тестирования"),
		"ответ должен цитировать релевантное предложение, получено: %s", res.Text)
}

func TestAnswerWithoutMatches(t *testing.T) {
	t.Parallel()

	client := llm.NewMock(llm.MockOptions{})
	res, err := client.Answer(context.Background(), "какая погода в Мурманске",
		[]service.MeetingContext{{
			Title:     "Планёрка платформы",
			CreatedAt: time.Now(),
			Summary:   "Кратко о встрече:\n• Обсудили миграцию.",
		}})

	require.NoError(t, err)
	assert.Contains(t, res.Text, "прямого ответа не нашлось")
}

func TestAnswerErrors(t *testing.T) {
	t.Parallel()

	client := llm.NewMock(llm.MockOptions{})

	_, err := client.Answer(context.Background(), "", []service.MeetingContext{{Transcript: "текст"}})
	assert.ErrorIs(t, err, domain.ErrInvalidArgument)

	_, err = client.Answer(context.Background(), "вопрос", nil)
	assert.ErrorIs(t, err, domain.ErrNoContext)
}

func TestRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	client := llm.NewMock(llm.MockOptions{Delay: 5 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	_, err := client.Summarize(ctx, transcript)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestFactorySelectsImplementation(t *testing.T) {
	t.Parallel()

	client, err := llm.New(config.LLM{Provider: config.ProviderMock})
	require.NoError(t, err)
	assert.Equal(t, llm.ProviderMock, client.Name())

	_, err = llm.New(config.LLM{Provider: config.ProviderGigaChat})
	assert.ErrorIs(t, err, domain.ErrProviderNotConfigured)

	_, err = llm.New(config.LLM{Provider: "yagpt"})
	assert.ErrorIs(t, err, domain.ErrProviderNotConfigured)
}
