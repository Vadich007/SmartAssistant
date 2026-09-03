package worker_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/Vadich007/meetnotes/internal/domain"
	"github.com/Vadich007/meetnotes/internal/worker"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

type fakeProcessor struct {
	mu      sync.Mutex
	pending []domain.Job

	processed []uuid.UUID
	released  []uuid.UUID

	inFlight      atomic.Int32
	maxInFlight   atomic.Int32
	staleRecovery atomic.Int32
	claimErr      error
	hold          chan struct{}
	processDelay  time.Duration
	wg            *sync.WaitGroup
}

func newFakeProcessor(jobs int) *fakeProcessor {
	p := &fakeProcessor{wg: &sync.WaitGroup{}}
	p.wg.Add(jobs)
	for range jobs {
		p.pending = append(p.pending, domain.Job{
			ID:          uuid.New(),
			MeetingID:   uuid.New(),
			Status:      domain.StatusProcessing,
			MaxAttempts: 3,
		})
	}
	return p
}

func (p *fakeProcessor) ClaimJobs(_ context.Context, _ string, limit int) ([]domain.Job, error) {
	if p.claimErr != nil {
		return nil, p.claimErr
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.pending) == 0 {
		return nil, nil
	}
	if limit > len(p.pending) {
		limit = len(p.pending)
	}
	claimed := p.pending[:limit]
	p.pending = p.pending[limit:]
	return claimed, nil
}

func (p *fakeProcessor) ProcessJob(ctx context.Context, job domain.Job) error {
	current := p.inFlight.Add(1)
	for {
		peak := p.maxInFlight.Load()
		if current <= peak || p.maxInFlight.CompareAndSwap(peak, current) {
			break
		}
	}
	defer p.inFlight.Add(-1)

	if p.hold != nil {
		select {
		case <-p.hold:
		case <-ctx.Done():
			p.wg.Done()
			return ctx.Err()
		}
	}
	if p.processDelay > 0 {
		select {
		case <-time.After(p.processDelay):
		case <-ctx.Done():
			p.wg.Done()
			return ctx.Err()
		}
	}

	p.mu.Lock()
	p.processed = append(p.processed, job.ID)
	p.mu.Unlock()

	p.wg.Done()
	return nil
}

func (p *fakeProcessor) ReleaseJob(_ context.Context, jobID uuid.UUID) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.released = append(p.released, jobID)
	return nil
}

func (p *fakeProcessor) RecoverStale(_ context.Context, _ time.Duration) (int64, error) {
	p.staleRecovery.Add(1)
	return 0, nil
}

func (p *fakeProcessor) processedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.processed)
}

func (p *fakeProcessor) releasedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return len(p.released)
}

func testConfig(concurrency int) worker.Config {
	return worker.Config{
		WorkerID:        "test-worker",
		Concurrency:     concurrency,
		PollInterval:    5 * time.Millisecond,
		BatchSize:       concurrency,
		LeaseTTL:        time.Minute,
		JobTimeout:      2 * time.Second,
		ShutdownTimeout: time.Second,
	}
}

func TestPoolProcessesAllJobs(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		proc := newFakeProcessor(12)
		pool := worker.New(proc, testConfig(3), nil)

		ctx, cancel := context.WithCancel(context.Background())
		finished := make(chan error, 1)
		go func() { finished <- pool.Run(ctx) }()

		proc.wg.Wait()
		cancel()
		require.NoError(t, <-finished)

		assert.Equal(t, 12, proc.processedCount())
		assert.Zero(t, proc.releasedCount())
		assert.Positive(t, proc.staleRecovery.Load(),
			"при старте задачи с истёкшей арендой должны восстанавливаться")
	})
}

func TestPoolRespectsConcurrencyLimit(t *testing.T) {
	t.Parallel()

	const concurrency = 3

	synctest.Test(t, func(t *testing.T) {
		proc := newFakeProcessor(30)
		proc.processDelay = 10 * time.Millisecond

		cfg := testConfig(concurrency)
		cfg.BatchSize = 10
		pool := worker.New(proc, cfg, nil)

		ctx, cancel := context.WithCancel(context.Background())
		finished := make(chan error, 1)
		go func() { finished <- pool.Run(ctx) }()

		proc.wg.Wait()
		cancel()
		require.NoError(t, <-finished)

		assert.Equal(t, 30, proc.processedCount())
		assert.LessOrEqual(t, int(proc.maxInFlight.Load()), concurrency,
			"одновременно обрабатывалось больше задач, чем разрешено")
		assert.Positive(t, proc.maxInFlight.Load())
	})
}

func TestPoolReleasesJobsOnShutdown(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		proc := newFakeProcessor(2)
		proc.hold = make(chan struct{})

		pool := worker.New(proc, testConfig(2), nil)

		ctx, cancel := context.WithCancel(context.Background())
		finished := make(chan error, 1)
		go func() { finished <- pool.Run(ctx) }()

		synctest.Wait()
		require.EqualValues(t, 2, proc.inFlight.Load())

		cancel()
		require.NoError(t, <-finished)

		assert.Zero(t, proc.processedCount(), "прерванные задачи не считаются обработанными")
		assert.Equal(t, 2, proc.releasedCount(), "прерванные задачи должны вернуться в очередь")
	})
}

func TestPoolStopsImmediatelyOnCancelledContext(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		proc := newFakeProcessor(0)
		pool := worker.New(proc, testConfig(2), nil)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		start := time.Now()
		require.NoError(t, pool.Run(ctx))
		assert.Zero(t, time.Since(start), "пул должен сразу завершаться при отменённом context")
		assert.Zero(t, proc.processedCount())
	})
}

func TestPoolSurvivesClaimErrors(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		proc := newFakeProcessor(0)
		proc.claimErr = errors.New("база данных недоступна")

		pool := worker.New(proc, testConfig(2), nil)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
		defer cancel()

		require.NoError(t, pool.Run(ctx))
		assert.Zero(t, proc.releasedCount())
	})
}

func TestPoolAppliesDefaults(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		proc := newFakeProcessor(0)
		pool := worker.New(proc, worker.Config{}, nil)

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		require.NoError(t, pool.Run(ctx))
	})
}
