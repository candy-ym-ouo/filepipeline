package domain

import "fmt"

type Status string
type Stage string

const (
	StatusPending    Status = "PENDING"
	StatusProcessing Status = "PROCESSING"
	StatusSucceeded  Status = "SUCCEEDED"
	StatusFailed     Status = "FAILED"
	StatusDead       Status = "DEAD"
)
const (
	StageValidate Stage = "VALIDATE"
	StageExtract  Stage = "EXTRACT"
	StageScan     Stage = "SCAN"
	StageDone     Stage = "DONE"
)

var validStatuses = map[Status]bool{
	StatusPending: true, StatusProcessing: true, StatusSucceeded: true,
	StatusFailed: true, StatusDead: true,
}
var validStages = map[Stage]bool{
	StageValidate: true, StageExtract: true, StageScan: true, StageDone: true,
}

func (s Status) Valid() bool { return validStatuses[s] }
func (s Status) Final() bool { return s == StatusSucceeded || s == StatusDead }
func (s Stage) Valid() bool  { return validStages[s] }
func (s Stage) Next() (Stage, error) {
	switch s {
	case StageValidate:
		return StageExtract, nil
	case StageExtract:
		return StageScan, nil
	case StageScan:
		return StageDone, nil
	case StageDone:
		return StageDone, nil
	default:
		return "", fmt.Errorf("unknown stage %q", s)
	}
}
func CanTransition(from, to Status) bool {
	switch from {
	case StatusPending:
		return to == StatusProcessing
	case StatusProcessing:
		return to == StatusPending || to == StatusSucceeded || to == StatusFailed || to == StatusDead
	case StatusFailed:
		return to == StatusPending
	default:
		return false
	}
}
func ParseStatus(value string) (Status, error) {
	s := Status(value)
	if value == "" || !s.Valid() {
		return "", fmt.Errorf("invalid status %q", value)
	}
	return s, nil
}
