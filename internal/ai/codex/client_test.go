package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/ai"
)

const validDraft = `{"title":"Add search","description":"Adds search.","services":[{"name":"api","bump":"minor","reason":"New endpoint.","tag_description":"Search endpoint."}]}`

func fakeCodex(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	bin := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nexport GORACE=atexit_sleep_ms=0\nexec '%s' -test.run=^TestCodexProcess$ -- \"$@\"\n", strings.ReplaceAll(executable, "'", "'\\''"))
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("WTUI_CODEX_HELPER", "1")
	t.Setenv("WTUI_CODEX_AUTH", "chatgpt")
	t.Setenv("WTUI_CODEX_RESULT", validDraft)
	record := filepath.Join(t.TempDir(), "cwd")
	t.Setenv("WTUI_CODEX_RECORD", record)
	return record
}

func request() ai.Request {
	return ai.Request{Purpose: ai.PurposeMR, Title: "private-title", Services: []ai.ServiceContext{{Name: "api", Diff: "private-diff"}}}
}

func TestGenerate_IsolatesProcessAndReturnsValidatedDraft(t *testing.T) {
	record := fakeCodex(t)
	for _, key := range []string{"OPENAI_API_KEY", "CODEX_API_KEY", "OPENAI_BASE_URL", "OPENAI_ORG_ID", "AZURE_OPENAI_API_KEY", "CODEX_AUTH_JSON", "CODEX_INTERNAL_ORIGINATOR_OVERRIDE"} {
		t.Setenv(key, "private-secret")
	}
	var provider ai.Provider = New("test-model", 10*time.Second)
	draft, err := provider.Generate(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if draft.Title != "Add search" || len(draft.Services) != 1 || draft.Services[0].Bump != ai.BumpMinor || draft.Services[0].Version != "" {
		t.Fatalf("unexpected draft: %#v", draft)
	}
	cwd, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(string(cwd)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary working directory not removed: %v", err)
	}
}

func TestGenerate_RejectsAuthenticationBeforeInference(t *testing.T) {
	for _, auth := range []string{"missing", "api", "access-token", "misleading"} {
		t.Run(auth, func(t *testing.T) {
			record := fakeCodex(t)
			t.Setenv("WTUI_CODEX_AUTH", auth)
			_, err := New("", 10*time.Second).Generate(t.Context(), request())
			if !errors.Is(err, ai.ErrAuthentication) || strings.Contains(err.Error(), "private-secret") {
				t.Fatalf("error = %v; want sanitized authentication error", err)
			}
			if _, err := os.Stat(record); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("inference executed without ChatGPT authentication: %v", err)
			}
		})
	}
}

func TestGenerate_ReportsSanitizedProviderFailures(t *testing.T) {
	tests := []struct {
		name, diagnostic string
		want             error
	}{
		{"quota", "usage_limit_reached private-secret", ai.ErrUsageLimit},
		{"rate", "429 Too Many Requests private-secret", ai.ErrUsageLimit},
		{"expired", "401 Unauthorized private-secret", ai.ErrAuthentication},
		{"refresh", "refresh_token_reused private-secret", ai.ErrAuthentication},
		{"failure", "unknown private-secret", ai.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeCodex(t)
			t.Setenv("WTUI_CODEX_ERROR", tt.diagnostic)
			_, err := New("", time.Second*10).Generate(context.Background(), request())
			if !errors.Is(err, tt.want) || strings.Contains(err.Error(), "private-secret") {
				t.Fatalf("error = %v; want sanitized %v", err, tt.want)
			}
		})
	}
}

func TestGenerate_RejectsInvalidOrOversizedOutput(t *testing.T) {
	for _, result := range []string{`{"title":"private-secret"}`, strings.Replace(validDraft, `"api"`, `"unknown-private-secret"`, 1), strings.Repeat("x", 1024*1024+1)} {
		t.Run(fmt.Sprint(len(result)), func(t *testing.T) {
			fakeCodex(t)
			// Keep the oversized fixture out of the process environment.
			file := filepath.Join(t.TempDir(), "result")
			if err := os.WriteFile(file, []byte(result), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("WTUI_CODEX_RESULT_FILE", file)
			_, err := New("", 10*time.Second).Generate(context.Background(), request())
			if !errors.Is(err, ai.ErrInvalidResponse) || strings.Contains(err.Error(), "private-secret") {
				t.Fatalf("error = %v; want sanitized invalid response", err)
			}
		})
	}
}

func TestGenerate_MissingBinaryIsActionable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := New("", time.Second).Generate(context.Background(), request())
	if !errors.Is(err, ai.ErrUnavailable) || !strings.Contains(err.Error(), "install") {
		t.Fatalf("error = %v; want installation guidance", err)
	}
}

func TestGenerate_TimeoutAndCancellation(t *testing.T) {
	for _, stage := range []string{"auth", "exec"} {
		t.Run(stage, func(t *testing.T) {
			fakeCodex(t)
			t.Setenv("WTUI_CODEX_HANG", stage)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := New("", 200*time.Millisecond).Generate(ctx, request())
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("error = %v; want deadline exceeded", err)
			}
		})
	}
	t.Run("already cancelled", func(t *testing.T) {
		fakeCodex(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := New("", time.Second).Generate(ctx, request())
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v; want cancellation", err)
		}
	})
}

// The subprocess fixture exercises the actual executable, stdin and file boundary.
func TestCodexProcess(t *testing.T) {
	if os.Getenv("WTUI_CODEX_HELPER") != "1" {
		return
	}
	args := os.Args[slices.Index(os.Args, "--")+1:]
	fail := func(message string) {
		fmt.Fprintln(os.Stderr, message)
		os.Exit(3)
	}
	for _, key := range []string{"OPENAI_API_KEY", "CODEX_API_KEY", "OPENAI_BASE_URL", "OPENAI_ORG_ID", "AZURE_OPENAI_API_KEY", "CODEX_AUTH_JSON", "CODEX_INTERNAL_ORIGINATOR_OVERRIDE"} {
		if os.Getenv(key) != "" {
			fail("unsafe environment inherited")
		}
	}
	auth := slices.Contains(args, "status")
	if (auth && os.Getenv("WTUI_CODEX_HANG") == "auth") || (!auth && os.Getenv("WTUI_CODEX_HANG") == "exec") {
		for {
			time.Sleep(time.Hour)
		}
	}
	if auth {
		switch os.Getenv("WTUI_CODEX_AUTH") {
		case "chatgpt":
			fmt.Fprintln(os.Stderr, "Logged in using ChatGPT")
			os.Exit(0)
		case "api":
			fmt.Fprintln(os.Stderr, "Logged in using an API key - private-secret")
			os.Exit(0)
		case "misleading":
			fmt.Fprintln(os.Stderr, "Not logged in. Use ChatGPT login.")
			os.Exit(0)
		default:
			fmt.Fprintln(os.Stderr, "Not logged in private-secret")
			os.Exit(1)
		}
	}
	for _, flag := range []string{"--ignore-user-config", "--ignore-rules", "--ephemeral", "--skip-git-repo-check"} {
		if !slices.Contains(args, flag) {
			fail("missing isolation flag: " + flag)
		}
	}
	for _, config := range []string{`forced_login_method="chatgpt"`, `model_provider="openai"`, `web_search="disabled"`, `approval_policy="never"`} {
		if !slices.Contains(args, config) {
			fail("missing isolation setting: " + config)
		}
	}
	for _, feature := range []string{"shell_tool", "apps", "multi_agent", "hooks", "plugins", "browser_use", "computer_use", "image_generation", "view_image"} {
		i := slices.Index(args, feature)
		if i < 1 || args[i-1] != "--disable" {
			fail("unsafe tool enabled: " + feature)
		}
	}
	i := slices.Index(args, "--sandbox")
	if i < 0 || args[i+1] != "read-only" {
		fail("unsafe sandbox")
	}
	cwd, err := os.Getwd()
	if err != nil {
		fail("no cwd")
	}
	if err := os.WriteFile(os.Getenv("WTUI_CODEX_RECORD"), []byte(cwd), 0600); err != nil {
		fail("cannot record cwd")
	}
	input, err := io.ReadAll(os.Stdin)
	if err != nil || !strings.Contains(string(input), "private-diff") || strings.Contains(strings.Join(args, " "), "private-diff") {
		fail("request not supplied privately through stdin")
	}
	i = slices.Index(args, "--output-schema")
	if i < 0 {
		fail("no schema")
	}
	schema, err := os.ReadFile(args[i+1])
	var parsed map[string]json.RawMessage
	if err != nil || json.Unmarshal(schema, &parsed) != nil || string(parsed["additionalProperties"]) != "false" {
		fail("invalid schema")
	}
	// A large stream must be consumed without retaining or returning its content.
	fmt.Fprint(os.Stdout, strings.Repeat("private-secret", 100000))
	if diagnostic := os.Getenv("WTUI_CODEX_ERROR"); diagnostic != "" {
		fmt.Fprintln(os.Stderr, diagnostic)
		os.Exit(1)
	}
	result := []byte(os.Getenv("WTUI_CODEX_RESULT"))
	if file := os.Getenv("WTUI_CODEX_RESULT_FILE"); file != "" {
		result, err = os.ReadFile(file)
		if err != nil {
			fail("cannot read fixture")
		}
	}
	i = slices.Index(args, "--output-last-message")
	if i < 0 {
		fail("no output file")
	}
	if err := os.WriteFile(args[i+1], result, 0600); err != nil {
		fail("cannot write output")
	}
	os.Exit(0)
}
