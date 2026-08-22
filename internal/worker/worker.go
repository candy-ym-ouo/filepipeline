package worker

import (
	"context"
	"filepipeline/internal/domain"
	"filepipeline/internal/repository"
	"log"
	"sync"
	"time"
)

type Config struct {
	WorkerCount  int
	PollInterval time.Duration
	BatchSize    int
	Lease        time.Duration
}
type Worker struct {
	repo      *repository.Repository
	processor *Processor
	config    Config
	logger    *log.Logger
}

func New(repo *repository.Repository, processor *Processor, config Config, logger *log.Logger) *Worker {
	if logger == nil {
		logger = log.Default()
	}
	if config.WorkerCount <= 0 {
		config.WorkerCount = 4
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 500 * time.Millisecond
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 8
	}
	if config.Lease <= 0 {
		config.Lease = 30 * time.Second
	}
	return &Worker{repo: repo, processor: processor, config: config, logger: logger}
}
func (w *Worker) Run(ctx context.Context) error {
	if count, err := w.repo.ReclaimExpired(ctx, time.Now()); err != nil {
		return err
	} else if count > 0 {
		w.logger.Printf("[worker] reclaimed=%d expired leases", count)
	}
	jobs := make(chan domain.Task, w.config.BatchSize*2)
	var workers sync.WaitGroup
	for index := 0; index < w.config.WorkerCount; index++ {
		workers.Add(1)
		go func(workerID int) {
			defer workers.Done()
			for task := range jobs {
				w.logger.Printf("[worker] worker=%d task=%s stage=%s start", workerID, task.ID, task.Stage)
				w.processor.Process(context.WithoutCancel(ctx), task)
			}
		}(index + 1)
	}
	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		w.poll(ctx, jobs)
		close(jobs)
	}()
	reclaimDone := make(chan struct{})
	go func() { defer close(reclaimDone); w.reclaim(ctx) }()
	<-ctx.Done()
	<-pollDone
	workers.Wait()
	<-reclaimDone
	return nil
}
func (w *Worker) poll(ctx context.Context, jobs chan<- domain.Task) {
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()
	for {
		if err := w.claimAndDispatch(ctx, jobs); err != nil && ctx.Err() == nil {
			w.logger.Printf("[worker] poll_error=%v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (w *Worker) claimAndDispatch(ctx context.Context, jobs chan<- domain.Task) error {
	tasks, err := w.repo.ClaimPending(ctx, time.Now(), w.config.Lease, w.config.BatchSize)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		// Do not let shutdown block forever while the processor queue is full.
		// The task has already been leased, so a task that is not dispatched will
		// be recovered by ReclaimExpired after the lease expires.
		select {
		case jobs <- task:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
func (w *Worker) reclaim(ctx context.Context) {
	callbackTicker := time.NewTicker(5 * time.Second)
	leaseTicker := time.NewTicker(10 * time.Second)
	defer callbackTicker.Stop()
	defer leaseTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-callbackTicker.C:
			if count, err := w.repo.ReclaimCallbackTimeouts(ctx, now); err != nil {
				w.logger.Printf("[worker] callback_reclaim_error=%v", err)
			} else if count > 0 {
				w.logger.Printf("[worker] callback_timeouts=%d", count)
			}
		case now := <-leaseTicker.C:
			if count, err := w.repo.ReclaimExpired(ctx, now); err != nil {
				w.logger.Printf("[worker] lease_reclaim_error=%v", err)
			} else if count > 0 {
				w.logger.Printf("[worker] reclaimed=%d expired leases", count)
			}
		}
	}
}
