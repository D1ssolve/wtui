package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Assistant is a provider that also explains failures and drafts workflow configuration.
type Assistant interface {
	Provider
	Explain(context.Context, FailureRequest) (Explanation, error)
	DraftWorkflow(context.Context, WorkflowRequest) (string, error)
}

type FailureRequest struct {
	Operation string `json:"operation"`
	Error     string `json:"error"`
	State     string `json:"state"`
	Guidance  string `json:"guidance"`
	Output    string `json:"recent_output"`
	Workflow  string `json:"workflow"`
}

type Explanation struct {
	Cause string   `json:"cause"`
	Steps []string `json:"steps"`
}

// WorkflowRequest asks for a `workflow:` block; Previous and Problem carry the last draft
// and the validator's complaint when a draft is being repaired.
type WorkflowRequest struct {
	Description string `json:"description"`
	Reference   string `json:"reference"`
	Previous    string `json:"previous_draft,omitempty"`
	Problem     string `json:"validation_error,omitempty"`
}

func decodeOne(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("%w: decode response: %v", ErrInvalidResponse, err)
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		return fmt.Errorf("%w: response must contain one JSON object", ErrInvalidResponse)
	}
	return nil
}

// ParseExplanation is the trust boundary for model-written failure explanations.
func ParseExplanation(data []byte) (Explanation, error) {
	var e Explanation
	if err := decodeOne(data, &e); err != nil {
		return Explanation{}, err
	}
	cause, err := normalizeMetadataText(e.Cause)
	if err != nil {
		return Explanation{}, err
	}
	e.Cause = cause
	steps := e.Steps[:0]
	for _, step := range e.Steps {
		step, err := normalizeMetadataText(step)
		if err != nil {
			return Explanation{}, err
		}
		if step != "" {
			steps = append(steps, step)
		}
	}
	e.Steps = steps
	if e.Cause == "" || len(e.Steps) == 0 || len(e.Steps) > 8 {
		return Explanation{}, fmt.Errorf("%w: expected a cause and one to eight steps", ErrInvalidResponse)
	}
	return e, nil
}

// ParseWorkflowDraft extracts the YAML text; whether it is a valid workflow is decided by
// the workflow compiler, never by the model.
func ParseWorkflowDraft(data []byte) (string, error) {
	var draft struct {
		YAML string `json:"yaml"`
	}
	if err := decodeOne(data, &draft); err != nil {
		return "", err
	}
	text, err := normalizeMetadataText(draft.YAML)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(text, "workflow:") {
		return "", fmt.Errorf("%w: draft must be a single top-level workflow block", ErrInvalidResponse)
	}
	return text + "\n", nil
}
