package service_test

import (
	"context"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Vadich007/meetnotes/internal/domain"
	"github.com/Vadich007/meetnotes/internal/service"
)

type memStore struct {
	mu          sync.Mutex
	users       map[string]domain.User
	meetings    map[uuid.UUID]domain.Meeting
	jobs        map[uuid.UUID]domain.Job
	transcripts map[uuid.UUID]domain.Transcript
	summaries   map[uuid.UUID]domain.Summary
	qa          []domain.QAEntry
	history     []domain.StatusChange

	failOn string
}

func newMemStore() *memStore {
	return &memStore{
		users:       make(map[string]domain.User),
		meetings:    make(map[uuid.UUID]domain.Meeting),
		jobs:        make(map[uuid.UUID]domain.Job),
		transcripts: make(map[uuid.UUID]domain.Transcript),
		summaries:   make(map[uuid.UUID]domain.Summary),
	}
}

func (s *memStore) fail(op string) error {
	if s.failOn == op {
		return fmt.Errorf("%w: искусственный сбой %s", domain.ErrStorageUnavailable, op)
	}
	return nil
}

// --- TxManager ---

func (s *memStore) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	s.mu.Lock()
	snapshot := struct {
		meetings    map[uuid.UUID]domain.Meeting
		jobs        map[uuid.UUID]domain.Job
		transcripts map[uuid.UUID]domain.Transcript
		summaries   map[uuid.UUID]domain.Summary
	}{
		meetings:    maps.Clone(s.meetings),
		jobs:        maps.Clone(s.jobs),
		transcripts: maps.Clone(s.transcripts),
		summaries:   maps.Clone(s.summaries),
	}
	s.mu.Unlock()

	if err := fn(ctx); err != nil {
		s.mu.Lock()
		s.meetings, s.jobs = snapshot.meetings, snapshot.jobs
		s.transcripts, s.summaries = snapshot.transcripts, snapshot.summaries
		s.mu.Unlock()
		return err
	}
	return nil
}

// --- UserRepository ---

func (s *memStore) Ensure(_ context.Context, externalID string) (domain.User, bool, error) {
	if err := s.fail("users.ensure"); err != nil {
		return domain.User{}, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if user, ok := s.users[externalID]; ok {
		return user, false, nil
	}
	user := domain.User{ID: uuid.New(), ExternalID: externalID, CreatedAt: time.Now()}
	s.users[externalID] = user
	return user, true, nil
}

func (s *memStore) GetByExternalID(_ context.Context, externalID string) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	user, ok := s.users[externalID]
	if !ok {
		return domain.User{}, fmt.Errorf("%w: пользователь %s", domain.ErrNotFound, externalID)
	}
	return user, nil
}

// --- MeetingRepository ---

func (s *memStore) Create(_ context.Context, m domain.Meeting) error {
	if err := s.fail("meetings.create"); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	m.CreatedAt = time.Now()
	s.meetings[m.ID] = m
	return nil
}

func (s *memStore) GetOwned(_ context.Context, userID, meetingID uuid.UUID) (domain.Meeting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, ok := s.meetings[meetingID]
	if !ok || m.UserID != userID {
		return domain.Meeting{}, fmt.Errorf("%w: встреча %s", domain.ErrNotFound, meetingID)
	}
	return m, nil
}

func (s *memStore) Get(_ context.Context, meetingID uuid.UUID) (domain.Meeting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	m, ok := s.meetings[meetingID]
	if !ok {
		return domain.Meeting{}, fmt.Errorf("%w: встреча %s", domain.ErrNotFound, meetingID)
	}
	return m, nil
}

func (s *memStore) List(_ context.Context, userID uuid.UUID, limit, offset int) ([]domain.MeetingListItem, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var items []domain.MeetingListItem
	for _, m := range s.meetings {
		if m.UserID != userID {
			continue
		}
		item := domain.MeetingListItem{Meeting: m}
		for _, job := range s.jobs {
			if job.MeetingID == m.ID {
				item.Status = job.Status
			}
		}
		if summary, ok := s.summaries[m.ID]; ok {
			item.Summary = summary.Text
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Meeting.CreatedAt.After(items[j].Meeting.CreatedAt)
	})

	if offset >= len(items) {
		return nil, nil
	}
	items = items[offset:]
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *memStore) Delete(_ context.Context, userID, meetingID uuid.UUID) (string, error) {
	if err := s.fail("meetings.delete"); err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	m, ok := s.meetings[meetingID]
	if !ok || m.UserID != userID {
		return "", fmt.Errorf("%w: встреча %s", domain.ErrNotFound, meetingID)
	}

	delete(s.meetings, meetingID)
	delete(s.transcripts, meetingID)
	delete(s.summaries, meetingID)
	for id, job := range s.jobs {
		if job.MeetingID == meetingID {
			delete(s.jobs, id)
		}
	}
	return m.StoredPath, nil
}

// --- JobRepository ---

func (s *memStore) CreateJob(_ context.Context, j domain.Job) error {
	if err := s.fail("jobs.create"); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	j.CreatedAt, j.UpdatedAt, j.NextAttemptAt = now, now, now
	s.jobs[j.ID] = j
	return nil
}

func (s *memStore) GetByMeetingID(_ context.Context, meetingID uuid.UUID) (domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, job := range s.jobs {
		if job.MeetingID == meetingID {
			return job, nil
		}
	}
	return domain.Job{}, fmt.Errorf("%w: задача встречи %s", domain.ErrNotFound, meetingID)
}

func (s *memStore) ChangeStatus(_ context.Context, jobID uuid.UUID, from, to domain.JobStatus, errText string) error {
	if err := s.fail("jobs.change_status." + string(to)); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("%w: задача %s", domain.ErrNotFound, jobID)
	}
	if job.Status != from {
		return fmt.Errorf("%w: задача %s больше не в статусе %s", domain.ErrConflict, jobID, from)
	}
	job.Status, job.LastError, job.UpdatedAt = to, errText, time.Now()
	s.jobs[jobID] = job
	s.history = append(s.history, domain.StatusChange{
		ID: uuid.New(), JobID: jobID, From: from, To: to, Error: errText, CreatedAt: time.Now(),
	})
	return nil
}

func (s *memStore) Fail(_ context.Context, jobID uuid.UUID, errText string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("%w: задача %s", domain.ErrNotFound, jobID)
	}
	from := job.Status
	job.Status, job.LastError, job.UpdatedAt = domain.StatusFailed, errText, time.Now()
	s.jobs[jobID] = job
	s.history = append(s.history, domain.StatusChange{
		ID: uuid.New(), JobID: jobID, From: from, To: domain.StatusFailed, Error: errText, CreatedAt: time.Now(),
	})
	return nil
}

func (s *memStore) Requeue(_ context.Context, jobID uuid.UUID) error {
	if err := s.fail("jobs.requeue"); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("%w: задача %s", domain.ErrNotFound, jobID)
	}
	if job.Status != domain.StatusFailed {
		return fmt.Errorf("%w: перезапустить можно только задачу в статусе failed", domain.ErrConflict)
	}
	job.Status, job.Attempts, job.LastError = domain.StatusCreated, 0, ""
	job.NextAttemptAt, job.UpdatedAt = time.Now(), time.Now()
	s.jobs[jobID] = job
	return nil
}

func (s *memStore) Reschedule(_ context.Context, jobID uuid.UUID, errText string, delay time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return fmt.Errorf("%w: задача %s", domain.ErrNotFound, jobID)
	}
	from := job.Status
	job.Status, job.LastError = domain.StatusCreated, errText
	job.NextAttemptAt, job.UpdatedAt = time.Now().Add(delay), time.Now()
	s.jobs[jobID] = job
	s.history = append(s.history, domain.StatusChange{
		ID: uuid.New(), JobID: jobID, From: from, To: domain.StatusCreated, Error: errText, CreatedAt: time.Now(),
	})
	return nil
}

func (s *memStore) Claim(_ context.Context, workerID string, limit int) ([]domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var claimed []domain.Job
	for id, job := range s.jobs {
		if len(claimed) >= limit {
			break
		}
		if job.Status != domain.StatusCreated || job.NextAttemptAt.After(time.Now()) {
			continue
		}
		now := time.Now()
		job.Status, job.Attempts, job.LockedBy, job.LockedAt = domain.StatusProcessing, job.Attempts+1, workerID, &now
		s.jobs[id] = job
		claimed = append(claimed, job)
	}
	return claimed, nil
}

func (s *memStore) Release(_ context.Context, jobID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job, ok := s.jobs[jobID]
	if !ok {
		return nil
	}
	job.Status, job.LockedBy, job.LockedAt = domain.StatusCreated, "", nil
	if job.Attempts > 0 {
		job.Attempts--
	}
	s.jobs[jobID] = job
	return nil
}

func (s *memStore) ReleaseStale(_ context.Context, ttl time.Duration) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var count int64
	for id, job := range s.jobs {
		if job.LockedAt == nil || time.Since(*job.LockedAt) < ttl {
			continue
		}
		job.Status, job.LockedAt, job.LockedBy = domain.StatusCreated, nil, ""
		s.jobs[id] = job
		count++
	}
	return count, nil
}

func (s *memStore) History(_ context.Context, jobID uuid.UUID, limit int) ([]domain.StatusChange, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []domain.StatusChange
	for i := len(s.history) - 1; i >= 0 && len(out) < limit; i-- {
		if s.history[i].JobID == jobID {
			out = append(out, s.history[i])
		}
	}
	return out, nil
}

// --- TranscriptRepository ---

func (s *memStore) SaveTranscript(_ context.Context, t domain.Transcript) error {
	if err := s.fail("transcripts.save"); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	t.CreatedAt = time.Now()
	s.transcripts[t.MeetingID] = t
	return nil
}

func (s *memStore) GetTranscript(_ context.Context, meetingID uuid.UUID) (domain.Transcript, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.transcripts[meetingID]
	if !ok {
		return domain.Transcript{}, fmt.Errorf("%w: транскрипция встречи %s", domain.ErrNotFound, meetingID)
	}
	return t, nil
}

// --- SummaryRepository ---

func (s *memStore) SaveSummary(_ context.Context, sum domain.Summary) error {
	if err := s.fail("summaries.save"); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sum.CreatedAt = time.Now()
	s.summaries[sum.MeetingID] = sum
	return nil
}

func (s *memStore) GetSummary(_ context.Context, meetingID uuid.UUID) (domain.Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sum, ok := s.summaries[meetingID]
	if !ok {
		return domain.Summary{}, fmt.Errorf("%w: выжимка встречи %s", domain.ErrNotFound, meetingID)
	}
	return sum, nil
}

// --- QARepository ---

func (s *memStore) Add(_ context.Context, e domain.QAEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e.CreatedAt = time.Now()
	s.qa = append(s.qa, e)
	return nil
}

func (s *memStore) ListByUser(_ context.Context, userID uuid.UUID, limit int) ([]domain.QAEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []domain.QAEntry
	for i := len(s.qa) - 1; i >= 0 && len(out) < limit; i-- {
		if s.qa[i].UserID == userID {
			out = append(out, s.qa[i])
		}
	}
	return out, nil
}

// --- SearchRepository ---

func (s *memStore) Search(_ context.Context, userID uuid.UUID, keyword string, limit int) ([]domain.SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	needle := strings.ToLower(keyword)
	var results []domain.SearchResult
	for _, m := range s.meetings {
		if m.UserID != userID || len(results) >= limit {
			continue
		}
		transcript := s.transcripts[m.ID].Text
		summary := s.summaries[m.ID].Text

		switch {
		case strings.Contains(strings.ToLower(transcript), needle):
			results = append(results, domain.SearchResult{Meeting: m, Snippet: transcript, MatchedIn: "transcript", Rank: 1})
		case strings.Contains(strings.ToLower(summary), needle):
			results = append(results, domain.SearchResult{Meeting: m, Snippet: summary, MatchedIn: "summary", Rank: 0.5})
		}
	}
	return results, nil
}

// --- StatsRepository ---

func (s *memStore) Collect(_ context.Context, userID uuid.UUID) (domain.Stats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	counts := make(map[domain.JobStatus]int)
	for _, job := range s.jobs {
		if m, ok := s.meetings[job.MeetingID]; ok && m.UserID == userID {
			counts[job.Status]++
		}
	}

	stats := domain.Stats{}
	for _, status := range domain.AllStatuses() {
		stats.ByStatus = append(stats.ByStatus, domain.StatsRow{Status: status, Count: counts[status]})
		stats.TotalMeetings += counts[status]
	}
	stats.FailedCount = counts[domain.StatusFailed]
	return stats, nil
}

type transcriptRepo struct{ *memStore }

func (r transcriptRepo) Save(ctx context.Context, t domain.Transcript) error {
	return r.SaveTranscript(ctx, t)
}

func (r transcriptRepo) GetByMeetingID(ctx context.Context, meetingID uuid.UUID) (domain.Transcript, error) {
	return r.GetTranscript(ctx, meetingID)
}

type summaryRepo struct{ *memStore }

func (r summaryRepo) Save(ctx context.Context, s domain.Summary) error { return r.SaveSummary(ctx, s) }

func (r summaryRepo) GetByMeetingID(ctx context.Context, meetingID uuid.UUID) (domain.Summary, error) {
	return r.GetSummary(ctx, meetingID)
}

type jobRepo struct{ *memStore }

func (r jobRepo) Create(ctx context.Context, j domain.Job) error { return r.CreateJob(ctx, j) }

// --- Файловое хранилище ---

type fakeFiles struct {
	mu       sync.Mutex
	saved    map[string]string
	removed  []string
	saveErr  error
	sizeHint int64
}

func newFakeFiles() *fakeFiles {
	return &fakeFiles{saved: make(map[string]string), sizeHint: 1024}
}

func (f *fakeFiles) Save(_ context.Context, srcPath string) (string, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.saveErr != nil {
		return "", 0, f.saveErr
	}
	stored := "stored/" + uuid.NewString()
	f.saved[stored] = srcPath
	return stored, f.sizeHint, nil
}

func (f *fakeFiles) Remove(path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.removed = append(f.removed, path)
	delete(f.saved, path)
	return nil
}

func (f *fakeFiles) removedPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.removed...)
}

// --- Клиенты внешних сервисов ---

type fakeSpeech struct {
	text  string
	err   error
	delay time.Duration
	calls int
}

func (f *fakeSpeech) Name() string { return "fake-speech" }

func (f *fakeSpeech) Transcribe(ctx context.Context, _ service.TranscribeRequest) (service.TranscribeResult, error) {
	f.calls++
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return service.TranscribeResult{}, ctx.Err()
		case <-time.After(f.delay):
		}
	}
	if f.err != nil {
		return service.TranscribeResult{}, f.err
	}
	return service.TranscribeResult{
		Text: f.text, Language: "ru", DurationSec: 42, Provider: f.Name(),
	}, nil
}

type fakeLLM struct {
	summary   string
	answer    string
	summErr   error
	answerErr error
	contexts  []service.MeetingContext
}

func (f *fakeLLM) Name() string { return "fake-llm" }

func (f *fakeLLM) Summarize(_ context.Context, _ string) (service.SummaryResult, error) {
	if f.summErr != nil {
		return service.SummaryResult{}, f.summErr
	}
	return service.SummaryResult{Text: f.summary, Provider: f.Name(), Model: "fake-model"}, nil
}

func (f *fakeLLM) Answer(_ context.Context, _ string, contexts []service.MeetingContext) (service.AnswerResult, error) {
	f.contexts = contexts
	if f.answerErr != nil {
		return service.AnswerResult{}, f.answerErr
	}
	return service.AnswerResult{Text: f.answer, Provider: f.Name(), Model: "fake-model"}, nil
}

// newService собирает бизнес-логику на фейковых зависимостях.
func newService(store *memStore, files service.FileStore, speech service.SpeechClient, llm service.LLMClient) *service.Service {
	return service.New(service.Deps{
		Tx:             store,
		Users:          store,
		Meetings:       store,
		Jobs:           jobRepo{store},
		Transcripts:    transcriptRepo{store},
		Summaries:      summaryRepo{store},
		QA:             store,
		Search:         store,
		Stats:          store,
		Files:          files,
		Speech:         speech,
		LLM:            llm,
		MaxAttempts:    3,
		SpeechTimeout:  time.Second,
		LLMTimeout:     time.Second,
		RetryBaseDelay: time.Millisecond,
	})
}
