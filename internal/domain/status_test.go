package domain_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Vadich007/meetnotes/internal/domain"
)

func TestJobStatusTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		from    domain.JobStatus
		to      domain.JobStatus
		allowed bool
	}{
		{"взятие в работу", domain.StatusCreated, domain.StatusProcessing, true},
		{"транскрипция получена", domain.StatusProcessing, domain.StatusTranscribed, true},
		{"выжимка получена", domain.StatusTranscribed, domain.StatusSummarized, true},
		{"обработка завершена", domain.StatusSummarized, domain.StatusCompleted, true},
		{"ошибка на этапе распознавания", domain.StatusProcessing, domain.StatusFailed, true},
		{"ошибка на этапе выжимки", domain.StatusTranscribed, domain.StatusFailed, true},
		{"retry возвращает задачу в очередь", domain.StatusFailed, domain.StatusCreated, true},
		{"перескок через транскрипцию запрещён", domain.StatusCreated, domain.StatusSummarized, false},
		{"завершённую задачу нельзя перезапустить", domain.StatusCompleted, domain.StatusCreated, false},
		{"завершённую задачу нельзя провалить", domain.StatusCompleted, domain.StatusFailed, false},
		{"откат назад запрещён", domain.StatusSummarized, domain.StatusTranscribed, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.allowed, tt.from.CanTransition(tt.to))

			err := tt.from.EnsureTransition(tt.to)
			if tt.allowed {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, domain.ErrConflict)
			}
		})
	}
}

func TestEnsureTransitionUnknownStatus(t *testing.T) {
	t.Parallel()

	err := domain.JobStatus("wat").EnsureTransition(domain.StatusCreated)
	assert.ErrorIs(t, err, domain.ErrConflict)

	err = domain.StatusCreated.EnsureTransition(domain.JobStatus("wat"))
	assert.ErrorIs(t, err, domain.ErrConflict)
}

func TestStatusHelpers(t *testing.T) {
	t.Parallel()

	assert.True(t, domain.StatusCompleted.IsTerminal())
	assert.True(t, domain.StatusFailed.IsTerminal())
	assert.False(t, domain.StatusProcessing.IsTerminal())
	assert.Len(t, domain.AllStatuses(), 6)

	for _, s := range domain.AllStatuses() {
		assert.True(t, s.Valid(), "статус %s должен быть валидным", s)
	}
}

func TestParseStatus(t *testing.T) {
	t.Parallel()

	s, err := domain.ParseStatus("processing")
	require.NoError(t, err)
	assert.Equal(t, domain.StatusProcessing, s)

	_, err = domain.ParseStatus("done")
	assert.True(t, errors.Is(err, domain.ErrInvalidArgument))
}

func TestNormalizeFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path    string
		want    string
		wantErr error
	}{
		{"meeting.WAV", "wav", nil},
		{"/tmp/standup.txt", "txt", nil},
		{"notes.md", "md", nil},
		{"archive.zip", "", domain.ErrUnsupportedFormat},
		{"noextension", "", domain.ErrUnsupportedFormat},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			got, err := domain.NormalizeFormat(tt.path)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTitleFromFilename(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Weekly sync 2026 08", domain.TitleFromFilename("/data/weekly_sync-2026.08.mp3"))
	assert.Equal(t, "Встреча без названия", domain.TitleFromFilename(".txt"))
}

func TestParseMeetingID(t *testing.T) {
	t.Parallel()

	_, err := domain.ParseMeetingID("не-uuid")
	assert.ErrorIs(t, err, domain.ErrInvalidArgument)

	id, err := domain.ParseMeetingID("  6ba7b810-9dad-11d1-80b4-00c04fd430c8 ")
	require.NoError(t, err)
	assert.Equal(t, "6ba7b810-9dad-11d1-80b4-00c04fd430c8", id.String())
}
