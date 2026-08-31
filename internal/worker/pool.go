// Package worker реализует фоновую обработку задач.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Vadich007/meetnotes/internal/domain"
)

type Processor interface {
	ClaimJobs(ctx context.Context, workerID string, limit int) ([]domain.Job, error)
	ProcessJob(ctx context.Context, job domain.Job) error
	ReleaseJob(ctx context.Context, jobID uuid.UUID) error
	RecoverStale(ctx context.Context, ttl time.Duration) (int64, error)
}

// Config параметры пула.
type Config struct {
	// WorkerID помечает задачи, взятые этим процессом.
	WorkerID string
	// Concurrency максимальное число одновременно обрабатываемых встреч.
	Concurrency int
	// PollInterval период опроса очереди.
	PollInterval time.Duration
	// BatchSize сколько задач диспетчер пытается забрать за один опрос.
	BatchSize int
	// LeaseTTL срок аренды задачи, по его истечении задача считается зависшей.
	LeaseTTL time.Duration
	// JobTimeout ограничивает время обработки одной встречи.
	JobTimeout time.Duration
	// ShutdownTimeout ограничивает время возврата незавершённых задач в очередь.
	ShutdownTimeout time.Duration
}

// Pool диспетчер очереди и пул обработчиков.
type Pool struct {
	proc Processor
	cfg  Config
	log  *slog.Logger
}

// New создаёт пул с безопасными значениями по умолчанию.
func New(proc Processor, cfg Config, log *slog.Logger) *Pool {
	if cfg.WorkerID == "" {
		cfg.WorkerID = uuid.NewString()
	}
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = cfg.Concurrency
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = time.Second
	}
	if cfg.LeaseTTL <= 0 {
		cfg.LeaseTTL = 5 * time.Minute
	}
	if cfg.JobTimeout <= 0 {
		cfg.JobTimeout = 2 * time.Minute
	}
	if cfg.ShutdownTimeout <= 0 {
		cfg.ShutdownTimeout = 10 * time.Second
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Pool{proc: proc, cfg: cfg, log: log.With(slog.String("component", "worker"))}
}

// Run запускает пул и блокируется до отмены context.
func (p *Pool) Run(ctx context.Context) error {
	p.log.InfoContext(ctx, "запуск фоновой обработки",
		slog.String("worker_id", p.cfg.WorkerID),
		slog.Int("concurrency", p.cfg.Concurrency),
		slog.Duration("poll_interval", p.cfg.PollInterval))

	// Задачи, зависшие после прошлого запуска, возвращаем в очередь.
	if _, err := p.proc.RecoverStale(ctx, p.cfg.LeaseTTL); err != nil {
		p.log.ErrorContext(ctx, "не удалось восстановить незавершённые задачи",
			slog.String("error", err.Error()))
	}

	// slots счётный семафор, ограничивает параллелизм.
	// Токен занимается до выборки задачи и освобождается после её обработки.
	slots := make(chan struct{}, p.cfg.Concurrency)
	queue := make(chan domain.Job, p.cfg.Concurrency)

	var wg sync.WaitGroup
	for i := range p.cfg.Concurrency {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			p.runWorker(ctx, index, queue, slots)
		}(i)
	}

	p.dispatch(ctx, queue, slots)

	// Диспетчер остановлен, очередь больше не пополняется.
	close(queue)
	wg.Wait()

	p.releaseQueued(queue)
	p.log.Info("фоновая обработка остановлена", slog.String("worker_id", p.cfg.WorkerID))
	return nil
}

// dispatch опрашивает очередь, пока не отменён context.
func (p *Pool) dispatch(ctx context.Context, queue chan<- domain.Job, slots chan struct{}) {
	poll := time.NewTicker(p.cfg.PollInterval)
	defer poll.Stop()

	recovery := time.NewTicker(max(p.cfg.LeaseTTL/2, p.cfg.PollInterval))
	defer recovery.Stop()

	for {
		p.claimAndDispatch(ctx, queue, slots)

		select {
		case <-ctx.Done():
			return
		case <-poll.C:
		case <-recovery.C:
			if _, err := p.proc.RecoverStale(ctx, p.cfg.LeaseTTL); err != nil && ctx.Err() == nil {
				p.log.ErrorContext(ctx, "не удалось восстановить зависшие задачи",
					slog.String("error", err.Error()))
			}
		}
	}
}

// claimAndDispatch забирает столько задач, сколько сейчас свободно слотов,
// и передаёт их обработчикам. Лишние занятые слоты сразу освобождаются.
func (p *Pool) claimAndDispatch(ctx context.Context, queue chan<- domain.Job, slots chan struct{}) {
	if ctx.Err() != nil {
		return
	}

	acquired := 0
acquire:
	for acquired < p.cfg.BatchSize {
		select {
		case slots <- struct{}{}:
			acquired++
		default:
			// Свободных слотов больше нет — остальные обработчики заняты.
			break acquire
		}
	}

	if acquired == 0 {
		return
	}

	jobs, err := p.proc.ClaimJobs(ctx, p.cfg.WorkerID, acquired)
	if err != nil {
		releaseSlots(slots, acquired)
		if ctx.Err() == nil {
			p.log.ErrorContext(ctx, "не удалось получить задачи из очереди",
				slog.String("error", err.Error()))
		}
		return
	}

	// Слоты, под которые задач не нашлось, возвращаем обратно.
	releaseSlots(slots, acquired-len(jobs))
	if len(jobs) == 0 {
		return
	}

	p.log.DebugContext(ctx, "задачи взяты в обработку", slog.Int("count", len(jobs)))
	for _, job := range jobs {
		select {
		case queue <- job:
		case <-ctx.Done():
			// Приложение останавливается, возвращаем задачу в очередь.
			p.releaseJob(job)
			<-slots
		}
	}
}

// runWorker тело одного обработчика.
func (p *Pool) runWorker(ctx context.Context, index int, queue <-chan domain.Job, slots <-chan struct{}) {
	log := p.log.With(slog.Int("worker", index))

	for job := range queue {
		if ctx.Err() != nil {
			p.releaseJob(job)
			<-slots
			continue
		}

		p.processOne(ctx, job, log)
		<-slots
	}
}

// processOne обрабатывает одну задачу под собственным таймаутом.
func (p *Pool) processOne(ctx context.Context, job domain.Job, log *slog.Logger) {
	jobCtx, cancel := context.WithTimeout(ctx, p.cfg.JobTimeout)
	defer cancel()

	err := p.proc.ProcessJob(jobCtx, job)
	if err == nil {
		return
	}
	if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		log.Warn("обработка прервана, задача возвращается в очередь",
			slog.String("job_id", job.ID.String()))
		p.releaseJob(job)
		return
	}
	log.Debug("обработка задачи завершилась ошибкой",
		slog.String("job_id", job.ID.String()), slog.String("error", err.Error()))
}

// releaseQueued возвращает в очередь задачи, которые успели попасть в канал,
// но не были взяты в работу до остановки.
func (p *Pool) releaseQueued(queue <-chan domain.Job) {
	for job := range queue {
		p.releaseJob(job)
	}
}

// releaseJob снимает блокировку с задачи.
func (p *Pool) releaseJob(job domain.Job) {
	ctx, cancel := context.WithTimeout(context.Background(), p.cfg.ShutdownTimeout)
	defer cancel()

	if err := p.proc.ReleaseJob(ctx, job.ID); err != nil {
		p.log.Error("не удалось вернуть задачу в очередь",
			slog.String("job_id", job.ID.String()), slog.String("error", err.Error()))
	}
}

func releaseSlots(slots <-chan struct{}, count int) {
	for range count {
		<-slots
	}
}
