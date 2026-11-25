package claude

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
	"runtime"
	"strings"

	"github.com/D1ssolve/wtui/internal/ai"
	"github.com/D1ssolve/wtui/internal/ai/internal/cliprocess"
)

// Available checks for the native executable without starting it or checking login.
func (c *Client) Available() bool {
	_, err := findExecutable()
	return err == nil
}

func findExecutable() (string, error) {
	binary, err := exec.LookPath("claude")
	if err != nil || (runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(binary), ".exe")) {
		return "", fmt.Errorf("%w: install the native Claude Code CLI and add claude to PATH", ai.ErrUnavailable)
	}
	return binary, nil
}

func complete[T requestPayload](ctx context.Context, c *Client, instructions, schema string, request T) (data []byte, resultErr error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, cancelled(err)
	}
	binary, err := findExecutable()
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot encode Claude Code request", ai.ErrInvalidResponse)
	}
	dir, err := os.MkdirTemp("", "wtui-claude-")
	if err != nil {
		return nil, fmt.Errorf("%w: cannot create private Claude Code workspace", ai.ErrUnavailable)
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			data = nil
			resultErr = errors.Join(resultErr, fmt.Errorf("%w: cannot remove private Claude Code workspace", ai.ErrUnavailable))
		}
	}()
	env := append(childEnv(os.Environ(), runtime.GOOS),
		"CLAUDE_CODE_SAFE_MODE=1", "DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1")
	if err := checkSubscription(ctx, binary, dir, env); err != nil {
		return nil, err
	}

	// Keep saved login; any remaining user settings are owned by the caller and managed policy still applies.
	args := []string{"-p", "--safe-mode", "--setting-sources=", "--tools", "", "--disallowedTools", "mcp__*",
		"--no-chrome", "--permission-mode", "dontAsk", "--no-session-persistence", "--output-format", "json",
		"--system-prompt", instructions + "\nResponse JSON Schema (return raw JSON, not a Markdown code fence):\n" + schema}
	// --json-schema would register a StructuredOutput tool; enforce the schema at the ai.Parse* boundary.
	if c.model != "" {
		args = append(args, "--model="+c.model)
	}
	stdout, stderr := cliprocess.NewOutput(1<<20), cliprocess.NewOutput(64<<10)
	err = cliprocess.Run(ctx, binary, dir, env, bytes.NewReader(payload), stdout, stderr, args...)
	if ctx.Err() != nil {
		return nil, cancelled(ctx.Err())
	}
	if stdout.Truncated() {
		return nil, fmt.Errorf("%w: Claude Code response exceeds the size limit", ai.ErrInvalidResponse)
	}
	var result struct {
		Type    string   `json:"type"`
		Subtype string   `json:"subtype"`
		IsError *bool    `json:"is_error"`
		Text    string   `json:"result"`
		Errors  []string `json:"errors"`
	}
	valid := json.Unmarshal(stdout.Bytes(), &result) == nil && result.Type == "result" && result.IsError != nil
	if err != nil || (valid && *result.IsError) {
		diagnostic := stderr.String()
		if valid && *result.IsError {
			diagnostic += "\n" + result.Text + "\n" + strings.Join(result.Errors, "\n")
		}
		return nil, providerError(diagnostic)
	}
	if !valid || result.Subtype != "success" || result.Text == "" {
		return nil, invalidResponse()
	}
	return []byte(result.Text), nil
}

func checkSubscription(ctx context.Context, binary, dir string, env []string) error {
	status := cliprocess.NewOutput(8 << 10)
	err := cliprocess.Run(ctx, binary, dir, env, nil, status, io.Discard, "auth", "status")
	if ctx.Err() != nil {
		return cancelled(ctx.Err())
	}
	var login struct {
		LoggedIn bool   `json:"loggedIn"`
		Method   string `json:"authMethod"`
	}
	if err != nil || status.Truncated() || json.Unmarshal(status.Bytes(), &login) != nil || !login.LoggedIn || login.Method != "claude.ai" {
		return authError()
	}
	return nil
}

func childEnv(parent []string, goos string) []string {
	env := make([]string, 0, len(parent))
	for _, entry := range parent {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if goos == "windows" {
			key = strings.ToUpper(key)
			switch key {
			case "SYSTEMROOT", "SYSTEMDRIVE", "WINDIR", "COMSPEC", "PATHEXT", "TEMP", "TMP",
				"USERPROFILE", "HOMEDRIVE", "HOMEPATH", "APPDATA", "LOCALAPPDATA", "PROGRAMDATA",
				"PROGRAMFILES", "USERNAME", "NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE":
				env = append(env, entry)
				continue
			}
		}
		switch key {
		case "PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "TZ", "TMPDIR", "TERM",
			"LC_ALL", "LC_COLLATE", "LC_CTYPE", "LC_MESSAGES", "LC_MONETARY", "LC_NUMERIC", "LC_TIME",
			"LC_ADDRESS", "LC_IDENTIFICATION", "LC_MEASUREMENT", "LC_NAME", "LC_PAPER", "LC_TELEPHONE",
			"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
			"NO_PROXY", "SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS":
			env = append(env, entry)
		case "CLAUDE_CONFIG_DIR":
			// https://code.claude.com/docs/en/claude-directory.md: every ~/.claude path moves, including .credentials.json.
			env = append(env, entry)
		}
	}
	return env
}

func cancelled(err error) error {
	return fmt.Errorf("Claude Code request cancelled or timed out: %w", err)
}

func authError() error {
	return fmt.Errorf("%w: sign in to Claude Code with a Claude subscription (claude auth login); API-key login is not supported", ai.ErrAuthentication)
}

func providerError(diagnostic string) error {
	message := strings.ToLower(diagnostic)
	for _, marker := range []string{"usage limit", "rate limit", "rate_limit", "quota", "429", "too many requests"} {
		if strings.Contains(message, marker) {
			return fmt.Errorf("%w: check your Claude subscription usage and try again after the limit resets", ai.ErrUsageLimit)
		}
	}
	for _, marker := range []string{"not logged in", "unauthorized", "authentication", "token expired", "401", "403", "/login"} {
		if strings.Contains(message, marker) {
			return authError()
		}
	}
	return fmt.Errorf("%w: Claude Code execution failed; check CLI version, model availability and network", ai.ErrUnavailable)
}
