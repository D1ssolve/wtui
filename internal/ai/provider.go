// Package ai defines provider-independent metadata generation.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
)

type Purpose string

const (
	PurposeMR      Purpose = "mr"
	PurposeRelease Purpose = "release"
	PurposeTag     Purpose = "tag"
)

type Bump string

const (
	BumpPatch Bump = "patch"
	BumpMinor Bump = "minor"
	BumpMajor Bump = "major"
)

var (
	ErrUnavailable     = errors.New("AI provider unavailable")
	ErrAuthentication  = errors.New("AI provider authentication required")
	ErrUsageLimit      = errors.New("AI provider usage limit reached")
	ErrInvalidResponse = errors.New("invalid AI metadata")
)

type ServiceContext struct {
	Name           string `json:"name"`
	Branch         string `json:"branch"`
	TargetBranch   string `json:"target_branch"`
	CurrentVersion string `json:"current_version"`
	Commits        string `json:"commits"`
	Diff           string `json:"diff"`
	Truncated      bool   `json:"truncated"`
}

type Request struct {
	Purpose     Purpose          `json:"purpose"`
	TaskIDs     []string         `json:"task_ids"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Services    []ServiceContext `json:"services"`
}

type ServiceDraft struct {
	Name           string `json:"name"`
	Bump           Bump   `json:"bump"`
	Reason         string `json:"reason"`
	TagDescription string `json:"tag_description"`
	Version        string `json:"-"`
}

type Draft struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Services    []ServiceDraft `json:"services"`
}

type Provider interface {
	// Implementations use ParseDraft before returning untrusted model output.
	Generate(context.Context, Request) (Draft, error)
}

// ParseDraft is the trust boundary shared by metadata provider adapters.
func ParseDraft(data []byte, request Request) (Draft, error) {
	var draft Draft
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&draft); err != nil {
		return Draft{}, fmt.Errorf("%w: decode response: %v", ErrInvalidResponse, err)
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		return Draft{}, fmt.Errorf("%w: response must contain one JSON object", ErrInvalidResponse)
	}
	for _, field := range []*string{&draft.Title, &draft.Description} {
		value, err := normalizeMetadataText(*field)
		if err != nil {
			return Draft{}, err
		}
		*field = value
	}
	if draft.Title == "" || strings.ContainsAny(draft.Title, "\r\n") || draft.Description == "" {
		return Draft{}, fmt.Errorf("%w: expected a single-line title and nonempty description", ErrInvalidResponse)
	}
	expected := make(map[string]bool, len(request.Services))
	for _, service := range request.Services {
		expected[service.Name] = true
	}
	for i := range draft.Services {
		service := &draft.Services[i]
		if !expected[service.Name] {
			return Draft{}, fmt.Errorf("%w: unexpected or duplicate service %q", ErrInvalidResponse, service.Name)
		}
		delete(expected, service.Name)
		switch service.Bump {
		case BumpPatch, BumpMinor, BumpMajor:
		default:
			return Draft{}, fmt.Errorf("%w: service %q has invalid version bump %q", ErrInvalidResponse, service.Name, service.Bump)
		}
		for _, field := range []*string{&service.Reason, &service.TagDescription} {
			value, err := normalizeMetadataText(*field)
			if err != nil {
				return Draft{}, err
			}
			*field = value
		}
		if service.Reason == "" || service.TagDescription == "" {
			return Draft{}, fmt.Errorf("%w: service %q needs a version reason and tag message", ErrInvalidResponse, service.Name)
		}
	}
	if len(expected) != 0 {
		return Draft{}, fmt.Errorf("%w: response omits requested services", ErrInvalidResponse)
	}
	return draft, nil
}

func normalizeMetadataText(text string) (string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\t", "    ")
	if strings.ContainsFunc(text, func(r rune) bool { return r != '\n' && unicode.IsControl(r) }) {
		return "", fmt.Errorf("%w: terminal control characters are not allowed in metadata", ErrInvalidResponse)
	}
	return strings.TrimSpace(text), nil
}
