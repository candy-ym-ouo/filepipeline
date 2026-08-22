package service

import (
	"context"
	"filepipeline/internal/domain"
	"fmt"
)

type StageOutput struct {
	Message string
	Summary string
	Scan    *domain.ScanResult
	Waiting bool
}
type Pipeline struct {
	validator *Validator
	extractor *Extractor
	scanner   *Scanner
}

func NewPipeline(validator *Validator, extractor *Extractor, scanner *Scanner) *Pipeline {
	return &Pipeline{validator: validator, extractor: extractor, scanner: scanner}
}
func (p *Pipeline) Execute(ctx context.Context, task domain.Task) (StageOutput, error) {
	if task.Status != domain.StatusProcessing {
		return StageOutput{}, fmt.Errorf("task %s is not processing", task.ID)
	}
	switch task.Stage {
	case domain.StageValidate:
		message, err := p.validator.Validate(ctx, task)
		return StageOutput{Message: message}, err
	case domain.StageExtract:
		summary, message, err := p.extractor.Extract(ctx, task)
		return StageOutput{Summary: summary, Message: message}, err
	case domain.StageScan:
		outcome, err := p.scanner.Scan(ctx, task)
		if outcome.Accepted {
			_ = outcome.Result.Verdict
		}
		if err != nil {
			return StageOutput{}, err
		}
		return StageOutput{Message: outcome.Message, Scan: outcome.Result, Waiting: outcome.Accepted}, nil
	case domain.StageDone:
		return StageOutput{Message: "流水线处理完成"}, nil
	default:
		return StageOutput{}, fmt.Errorf("unknown stage %q", task.Stage)
	}
}
