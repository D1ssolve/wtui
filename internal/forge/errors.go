package forge

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var ErrForgeUnavailable = errors.New("forge CLI not available")

type ForgeError struct {
	Category ErrorCategory
	Cause    error
	Stderr   string
	Provider ForgeProvider
	Host     string
}

func (e *ForgeError) Error() string {
	if e == nil {
		return "forge error"
	}

	category := e.Category
	if category == "" {
		category = ErrCategoryUnknown
	}

	parts := []string{fmt.Sprintf("forge %s", category)}
	if e.Cause != nil {
		parts = append(parts, e.Cause.Error())
	}

	stderr := strings.TrimSpace(e.Stderr)
	if stderr != "" {
		parts = append(parts, stderr)
	}

	return strings.Join(parts, ": ")
}

func (e *ForgeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func classifyForgeExecError(err error, stderr string, failure ForgeError) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}

	failure.Cause, failure.Stderr = err, stderr
	lower := strings.ToLower(stderr)
	if lower == "" {
		lower = strings.ToLower(err.Error())
	}
	switch {
	case errors.Is(err, exec.ErrNotFound):
		failure.Category = ErrCategoryNotInstalled
		failure.Cause = fmt.Errorf("%w: %w", ErrForgeUnavailable, err)
	case strings.Contains(lower, "not authenticated") || strings.Contains(lower, "unauthorized") || strings.Contains(lower, "401"):
		failure.Category = ErrCategoryAuthError
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "no such host") || strings.Contains(lower, "connection") || strings.Contains(lower, "temporary failure"):
		failure.Category = ErrCategoryNetwork
	default:
		failure.Category = ErrCategoryUnknown
	}
	return &failure
}
