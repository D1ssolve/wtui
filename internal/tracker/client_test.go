package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/D1ssolve/wtui/internal/execerr"
)

const fakeModeEnv = "WTUI_TRACKER_FAKE_MODE"

// TestMain lets the test binary act as the plugin executable.
func TestMain(m *testing.M) {
	switch os.Getenv(fakeModeEnv) {
	case "":
		os.Exit(m.Run())
	case "crash":
		fmt.Fprintln(os.Stderr, "boom")
		os.Exit(3)
	case "garbage":
		fmt.Println("not json")
		os.Exit(0)
	default:
		if Serve(os.Stdin, os.Stdout, fakeHandler) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
}

func fakeHandler(method string, params json.RawMessage) (any, error) {
	switch method {
	case MethodHandshake:
		return Handshake{Protocol: ProtocolVersion, Methods: []string{MethodListIssues}}, nil
	case MethodListIssues:
		var p ListIssuesParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		return []Issue{{Key: "WT-1", Summary: p.Query}}, nil
	case MethodTransition:
		var p TransitionParams
		_ = json.Unmarshal(params, &p)
		if p.Status == "Nope" {
			return nil, errors.New("no transition to Nope")
		}
		return nil, nil
	}
	return nil, ErrUnknownMethod
}

func fakeClient(t *testing.T, mode string) *PluginClient {
	t.Setenv(fakeModeEnv, mode)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return NewPluginClient(exe)
}

func TestPluginClient_RoundTrip_DecodesResult(t *testing.T) {
	c := fakeClient(t, "serve")
	h, err := c.Handshake(context.Background())
	if err != nil || h.Protocol != ProtocolVersion {
		t.Fatalf("handshake = %+v, %v", h, err)
	}
	issues, err := c.ListIssues(context.Background(), "assignee = me", 10)
	if err != nil || len(issues) != 1 || issues[0].Key != "WT-1" || issues[0].Summary != "assignee = me" {
		t.Fatalf("issues = %+v, %v", issues, err)
	}
	if err := c.Transition(context.Background(), "WT-1", "Done"); err != nil {
		t.Fatalf("transition: %v", err)
	}
}

func TestPluginClient_Failures_ReportCause(t *testing.T) {
	tests := []struct {
		mode, want string
		call       func(*PluginClient) error
		exec       bool
	}{
		{"serve", "no transition to Nope", func(c *PluginClient) error { return c.Transition(context.Background(), "WT-1", "Nope") }, false},
		{"serve", "unknown method", func(c *PluginClient) error { return c.Comment(context.Background(), "WT-1", "x") }, false},
		{"crash", "boom", func(c *PluginClient) error { return c.Comment(context.Background(), "WT-1", "x") }, true},
		{"garbage", "decode plugin response", func(c *PluginClient) error { return c.Comment(context.Background(), "WT-1", "x") }, false},
	}
	for _, tt := range tests {
		t.Run(tt.mode+"_"+tt.want, func(t *testing.T) {
			err := tt.call(fakeClient(t, tt.mode))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if errors.Is(err, execerr.ErrExec) != tt.exec {
				t.Fatalf("errors.Is(ErrExec) = %v, want %v", !tt.exec, tt.exec)
			}
		})
	}
}

func TestServe_ProtocolMismatch_ReturnsError(t *testing.T) {
	var out strings.Builder
	err := Serve(strings.NewReader(`{"protocol":99,"method":"handshake"}`), &out, fakeHandler)
	if err == nil || !strings.Contains(out.String(), "unsupported protocol 99") {
		t.Fatalf("err = %v, out = %s", err, out.String())
	}
}
