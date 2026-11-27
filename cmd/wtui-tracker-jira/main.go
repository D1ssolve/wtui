// Command wtui-tracker-jira is the wtui tracker plugin for Jira Cloud.
// It reads JIRA_BASE_URL, JIRA_EMAIL and JIRA_API_TOKEN from the environment.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/D1ssolve/wtui/internal/tracker"
)

func main() {
	j, err := fromEnv()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err = tracker.Serve(os.Stdin, os.Stdout, func(method string, params json.RawMessage) (any, error) {
			return j.handle(ctx, method, params)
		})
	} else {
		_ = tracker.Serve(os.Stdin, os.Stdout, func(string, json.RawMessage) (any, error) { return nil, err })
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func fromEnv() (*jira, error) {
	j := &jira{
		base:  strings.TrimRight(os.Getenv("JIRA_BASE_URL"), "/"),
		email: os.Getenv("JIRA_EMAIL"),
		token: os.Getenv("JIRA_API_TOKEN"),
		http:  &http.Client{Timeout: 20 * time.Second},
	}
	var missing []string
	for _, kv := range [][2]string{{"JIRA_BASE_URL", j.base}, {"JIRA_EMAIL", j.email}, {"JIRA_API_TOKEN", j.token}} {
		if kv[1] == "" {
			missing = append(missing, kv[0])
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("set %s for wtui-tracker-jira (API token: https://id.atlassian.com/manage-profile/security/api-tokens)", strings.Join(missing, ", "))
	}
	if !strings.HasPrefix(j.base, "https://") {
		return nil, fmt.Errorf("JIRA_BASE_URL must start with https://, got %q", j.base)
	}
	return j, nil
}
