// Package codex generates metadata using the saved Codex ChatGPT login.
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/ai/internal/cliprocess"
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

const executable = "codex"

// Available checks for the executable without starting it or checking login.
func (c *Client) Available() bool {
	_, err := exec.LookPath(executable)
	return err == nil
}

func (c *Client) Generate(ctx context.Context, request ai.Request) (ai.Draft, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return ai.Draft{}, fmt.Errorf("%w: cannot encode Codex metadata request", ai.ErrInvalidResponse)
	}
	data, err := c.complete(ctx, ai.MetadataInstructions, payload, ai.DraftSchema)
	if err != nil {
		return ai.Draft{}, err
	}
	draft, err := ai.ParseDraft(data, request)
	if err != nil {
		// Parser errors may contain model-supplied values. Never return those.
		return ai.Draft{}, fmt.Errorf("%w: Codex returned malformed metadata; generate again", ai.ErrInvalidResponse)
	}
	return draft, nil
}

func (c *Client) Explain(ctx context.Context, request ai.FailureRequest) (ai.Explanation, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return ai.Explanation{}, fmt.Errorf("%w: cannot encode Codex failure request", ai.ErrInvalidResponse)
	}
	data, err := c.complete(ctx, ai.ExplainInstructions, payload, ai.ExplanationSchema)
	if err != nil {
		return ai.Explanation{}, err
	}
	explanation, err := ai.ParseExplanation(data)
	if err != nil {
		return ai.Explanation{}, fmt.Errorf("%w: Codex returned a malformed explanation; ask again", ai.ErrInvalidResponse)
	}
	return explanation, nil
}

func (c *Client) DraftWorkflow(ctx context.Context, request ai.WorkflowRequest) (string, error) {
	payload, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("%w: cannot encode Codex workflow request", ai.ErrInvalidResponse)
	}
	data, err := c.complete(ctx, ai.WorkflowInstructions, payload, ai.WorkflowSchema)
	if err != nil {
		return "", err
	}
	draft, err := ai.ParseWorkflowDraft(data)
	if err != nil {
		return "", fmt.Errorf("%w: Codex returned a malformed workflow draft; ask again", ai.ErrInvalidResponse)
	}
	return draft, nil
}

// complete runs one isolated, read-only Codex inference with the ChatGPT login and returns
// the raw final message, which callers must parse through their ai trust boundary.
func (c *Client) complete(ctx context.Context, instructions string, payload []byte, schema string) (data []byte, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("Codex generation cancelled: %w", err)
	}
	binary, err := exec.LookPath(executable)
	if err != nil {
		return nil, fmt.Errorf("%w: install Codex CLI and add codex to PATH", ai.ErrUnavailable)
	}
	dir, err := os.MkdirTemp("", "wtui-codex-")
	if err != nil {
		return nil, fmt.Errorf("%w: cannot create private Codex workspace", ai.ErrUnavailable)
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			data = nil
			resultErr = errors.Join(resultErr, fmt.Errorf("%w: cannot remove private Codex workspace", ai.ErrUnavailable))
		}
	}()
	environment := cleanEnvironment(os.Environ())
	// Status must precede forced_login_method: forcing a mismatched method can
	// clear saved credentials in Codex.
	// Historically os/exec's io.Copy used bytes.Buffer.ReadFrom, bypassing the Write cap.
	var status bytes.Buffer
	if err := cliprocess.Run(ctx, binary, dir, environment, nil, &status, &status, "login", "status"); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("Codex authentication check cancelled: %w", ctx.Err())
		}
		return nil, authError()
	}
	if strings.TrimSpace(status.String()) != "Logged in using ChatGPT" {
		return nil, authError()
	}
	schemaFile := filepath.Join(dir, "schema.json")
	outputFile := filepath.Join(dir, "response.json")
	if err := os.WriteFile(schemaFile, []byte(schema), 0600); err != nil {
		return nil, fmt.Errorf("%w: cannot write Codex response schema", ai.ErrUnavailable)
	}
	if err := os.WriteFile(outputFile, nil, 0600); err != nil {
		return nil, fmt.Errorf("%w: cannot create Codex response file", ai.ErrUnavailable)
	}
	args := []string{"exec", "--ignore-user-config", "--ignore-rules", "--ephemeral",
		"--skip-git-repo-check", "--sandbox", "read-only", "--color", "never",
		"--output-schema", schemaFile, "--output-last-message", outputFile}
	for _, setting := range []string{
		`approval_policy="never"`, `forced_login_method="chatgpt"`, `model_provider="openai"`,
		`web_search="disabled"`, `mcp_servers={}`, `project_doc_max_bytes=0`,
		`history.persistence="none"`, `tools.update_plan.enabled=false`, `tools.experimental_request_user_input.enabled=false`,
		`skills.bundled.enabled=false`, `agents.enabled=false`,
		`analytics.enabled=false`, `otel.log_user_prompt=false`, `otel.exporter="none"`, `otel.metrics_exporter="none"`,
		"log_dir=" + fmt.Sprintf("%q", filepath.Join(dir, "logs")),
	} {
		args = append(args, "-c", setting)
	}
	for _, feature := range []string{"shell_tool", "shell_snapshot", "apps", "multi_agent", "multi_agent_v2",
		"hooks", "plugins", "browser_use", "computer_use", "image_generation", "view_image",
		"memories", "skill_search", "skill_mcp_dependency_install", "tool_search", "tool_suggest", "code_mode",
		"code_mode_host", "daemon_auto_start"} {
		args = append(args, "--disable", feature)
	}
	args = append(args, "--enable", "skip_host_skill_discovery")
	if c.model != "" {
		args = append(args, "--model", c.model)
	}
	args = append(args, "-")
	input := io.MultiReader(strings.NewReader(instructions), bytes.NewReader(payload))
	var diagnostic bytes.Buffer // Preserve full stderr for the historical error classification.
	if err := cliprocess.Run(ctx, binary, dir, environment, input, io.Discard, &diagnostic, args...); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("Codex generation cancelled or timed out: %w", ctx.Err())
		}
		return nil, providerError(diagnostic.String())
	}
	file, err := os.Open(outputFile)
	if err != nil {
		return nil, fmt.Errorf("%w: Codex produced no final response; generate again", ai.ErrInvalidResponse)
	}
	defer file.Close()
	data, err = io.ReadAll(io.LimitReader(file, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return nil, fmt.Errorf("%w: Codex response exceeds the size limit or cannot be read", ai.ErrInvalidResponse)
	}
	return data, nil
}

const maxResponseBytes = 1024 * 1024
