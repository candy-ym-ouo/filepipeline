package worker

import (
	"context"
	"filepipeline/internal/domain"
	"filepipeline/internal/repository"
	"filepipeline/internal/service"
	"log"
	"time"
)

type Processor struct {
	repo         *repository.Repository
	pipeline     *service.Pipeline
	retry        *service.RetryPolicy
	callbackWait time.Duration
	logger       *log.Logger
	now          func() time.Time
}

func NewProcessor(repo *repository.Repository, pipeline *service.Pipeline,
	retry *service.RetryPolicy, callbackWait time.Duration, logger *log.Logger) *Processor {
	if logger == nil {
		logger = log.Default()
	}
	return &Processor{repo: repo, pipeline: pipeline, retry: retry,
		callbackWait: callbackWait, logger: logger, now: time.Now}
}
func (p *Processor) Process(ctx context.Context, task domain.Task) {
	if ctx == nil {
		ctx = context.Background()
	}
	started := p.now()
	output, err := p.pipeline.Execute(ctx, task)
	if err != nil {
		p.handleFailure(ctx, task, err)
		return
	}
	if task.Stage == domain.StageDone {
		if err := p.repo.FinishSuccess(ctx, task, p.now()); err != nil {
			p.logger.Printf("[worker] task=%s stage=%s finish_error=%v", task.ID, task.Stage, err)
			return
		}
		p.logger.Printf("[worker] task=%s stage=%s result=ok elapsed=%s", task.ID, task.Stage, time.Since(started))
		return
	}
	if output.Waiting {
		if err := p.repo.MarkWaitingCallback(ctx, task, p.now().Add(p.callbackWait), p.now()); err != nil {
			p.logger.Printf("[worker] task=%s stage=%s wait_error=%v", task.ID, task.Stage, err)
			return
		}
		p.logger.Printf("[worker] task=%s stage=%s result=waiting_callback", task.ID, task.Stage)
		return
	}
	next, err := task.Stage.Next()
	if err != nil {
		p.handleFailure(ctx, task, err)
		return
	}
	if err := p.repo.CompleteStage(ctx, task, next, output.Message, output.Summary, output.Scan, p.now()); err != nil {
		p.logger.Printf("[worker] task=%s stage=%s persist_error=%v", task.ID, task.Stage, err)
		return
	}
	p.logger.Printf("[worker] task=%s stage=%s result=ok attempt=%d elapsed=%s",
		task.ID, task.Stage, task.Attempts+1, time.Since(started))
}
func (p *Processor) handleFailure(ctx context.Context, task domain.Task, cause error) {
	attempt := task.Attempts + 1
	delay := p.retry.Delay(attempt)
	status, err := p.repo.FailStage(ctx, task, cause, p.now().Add(delay), p.now())
	if err != nil {
		p.logger.Printf("[worker] task=%s stage=%s failure_persist_error=%v original=%v", task.ID, task.Stage, err, cause)
		return
	}
	p.logger.Printf("[worker] task=%s stage=%s result=fail attempt=%d status=%s retry_in=%s err=%v",
		task.ID, task.Stage, attempt, status, delay, cause)
}
