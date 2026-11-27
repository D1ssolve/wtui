package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/D1ssolve/wtui/internal/execerr"
)

type Tracker interface {
	ListIssues(ctx context.Context, query string, limit int) ([]Issue, error)
	GetIssue(ctx context.Context, key string) (IssueDetail, error)
	Transition(ctx context.Context, key, status string) error
	Comment(ctx context.Context, key, body string) error
}

type PluginClient struct {
	Path string
}

func NewPluginClient(path string) *PluginClient {
	return &PluginClient{Path: path}
}

func BinaryName(plugin string) string {
	return "wtui-tracker-" + plugin
}

func (c *PluginClient) Handshake(ctx context.Context) (Handshake, error) {
	var h Handshake
	err := c.call(ctx, MethodHandshake, nil, &h)
	if err == nil && h.Protocol != ProtocolVersion {
		err = fmt.Errorf("tracker plugin %s speaks protocol %d, wtui needs %d", c.Path, h.Protocol, ProtocolVersion)
	}
	return h, err
}

func (c *PluginClient) ListIssues(ctx context.Context, query string, limit int) ([]Issue, error) {
	var issues []Issue
	return issues, c.call(ctx, MethodListIssues, ListIssuesParams{Query: query, Limit: limit}, &issues)
}

func (c *PluginClient) GetIssue(ctx context.Context, key string) (IssueDetail, error) {
	var issue IssueDetail
	return issue, c.call(ctx, MethodGetIssue, KeyParams{Key: key}, &issue)
}

func (c *PluginClient) Transition(ctx context.Context, key, status string) error {
	return c.call(ctx, MethodTransition, TransitionParams{Key: key, Status: status}, nil)
}

func (c *PluginClient) Comment(ctx context.Context, key, body string) error {
	return c.call(ctx, MethodComment, CommentParams{Key: key, Body: body}, nil)
}

func (c *PluginClient) call(ctx context.Context, method string, params, out any) error {
	req := Request{Protocol: ProtocolVersion, Method: method}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return err
		}
		req.Params = raw
	}
	in, err := json.Marshal(req)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, c.Path)
	cmd.Stdin = bytes.NewReader(in)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()

	var resp Response
	decodeErr := json.Unmarshal(stdout.Bytes(), &resp)
	switch {
	case decodeErr == nil && resp.Error != nil:
		return fmt.Errorf("tracker %s: %s", method, resp.Error.Message)
	case runErr != nil:
		exitErr := &exec.ExitError{}
		code := -1
		if errors.As(runErr, &exitErr) {
			code = exitErr.ExitCode()
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return &execerr.ExecError{Argv: []string{c.Path, method}, ExitCode: code, Stderr: msg}
	case decodeErr != nil:
		return fmt.Errorf("tracker %s: decode plugin response: %w", method, decodeErr)
	case out != nil:
		if err := json.Unmarshal(resp.Result, out); err != nil {
			return fmt.Errorf("tracker %s: decode result: %w", method, err)
		}
	}
	return nil
}
