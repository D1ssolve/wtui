// Package claude generates drafts using the saved Claude Code subscription login.
package claude

import (
	"context"
	"fmt"
	"time"

	"github.com/D1ssolve/wtui/internal/ai"
)

type Client struct {
	model   string
	timeout time.Duration
}

func New(model string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return &Client{model: model, timeout: timeout}
}

var _ ai.Assistant = (*Client)(nil)

func (c *Client) Generate(ctx context.Context, request ai.Request) (ai.Draft, error) {
	data, err := complete(ctx, c, ai.MetadataInstructions, ai.DraftSchema, request)
	if err != nil {
		return ai.Draft{}, err
	}
	draft, err := ai.ParseDraft(data, request)
	if err != nil {
		return ai.Draft{}, invalidResponse()
	}
	return draft, nil
}

func (c *Client) Explain(ctx context.Context, request ai.FailureRequest) (ai.Explanation, error) {
	data, err := complete(ctx, c, ai.ExplainInstructions, ai.ExplanationSchema, request)
	if err != nil {
		return ai.Explanation{}, err
	}
	explanation, err := ai.ParseExplanation(data)
	if err != nil {
		return ai.Explanation{}, invalidResponse()
	}
	return explanation, nil
}

func (c *Client) DraftWorkflow(ctx context.Context, request ai.WorkflowRequest) (string, error) {
	data, err := complete(ctx, c, ai.WorkflowInstructions, ai.WorkflowSchema, request)
	if err != nil {
		return "", err
	}
	draft, err := ai.ParseWorkflowDraft(data)
	if err != nil {
		return "", invalidResponse()
	}
	return draft, nil
}

func invalidResponse() error {
	// Parser diagnostics can contain model-supplied field names and values.
	return fmt.Errorf("%w: Claude Code returned a malformed response; try again", ai.ErrInvalidResponse)
}

type requestPayload interface {
	ai.Request | ai.FailureRequest | ai.WorkflowRequest
}
