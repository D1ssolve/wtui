package forge

import (
	"errors"
	"fmt"
	"strings"
)

var ErrForgeUnavailable = errors.New("forge CLI not available")

type ForgeError struct {
	Category ErrorCategory
	Cause    error
	Stderr   string
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
