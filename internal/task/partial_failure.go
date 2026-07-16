package task

import (
	"errors"
	"fmt"
	"strings"
)

type FailedService struct {
	Name  string
	Cause error
}

type PartialFailureResult struct {
	TaskID            string
	Operation         string
	RequestedCount    int
	SucceededServices []string
	FailedServices    []FailedService
	Retryable         bool
}

type ErrPartialFailure struct {
	Result PartialFailureResult
}

func (e *ErrPartialFailure) Error() string {
	failed := make([]string, 0, len(e.Result.FailedServices))
	for _, svc := range e.Result.FailedServices {
		if svc.Cause == nil {
			failed = append(failed, svc.Name)
			continue
		}
		failed = append(failed, fmt.Sprintf("%s: %v", svc.Name, svc.Cause))
	}

	return fmt.Sprintf(
		"%s task %s partially failed: succeeded=%d failed=%d%s",
		e.Result.Operation,
		e.Result.TaskID,
		len(e.Result.SucceededServices),
		len(e.Result.FailedServices),
		formatFailedSuffix(failed),
	)
}

func formatFailedSuffix(failed []string) string {
	if len(failed) == 0 {
		return ""
	}
	return "; " + strings.Join(failed, ", ")
}

func (e *ErrPartialFailure) Unwrap() error {
	causes := make([]error, 0, len(e.Result.FailedServices))
	for _, svc := range e.Result.FailedServices {
		if svc.Cause != nil {
			causes = append(causes, svc.Cause)
		}
	}
	return errors.Join(causes...)
}
