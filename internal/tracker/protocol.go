// Package tracker talks to issue-tracker plugins: external executables named
// wtui-tracker-<name> that read one JSON request on stdin and write one JSON
// response on stdout per invocation.
package tracker

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const ProtocolVersion = 1

const (
	MethodHandshake  = "handshake"
	MethodListIssues = "list_issues"
	MethodGetIssue   = "get_issue"
	MethodTransition = "transition"
	MethodComment    = "comment"
)

type Request struct {
	Protocol int             `json:"protocol"`
	Method   string          `json:"method"`
	Params   json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ResponseError  `json:"error,omitempty"`
}

type ResponseError struct {
	Message string `json:"message"`
}

type Handshake struct {
	Protocol int      `json:"protocol"`
	Methods  []string `json:"methods"`
}

type ListIssuesParams struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
}

type KeyParams struct {
	Key string `json:"key"`
}

type TransitionParams struct {
	Key    string `json:"key"`
	Status string `json:"status"`
}

type CommentParams struct {
	Key  string `json:"key"`
	Body string `json:"body"`
}

type Issue struct {
	Key     string `json:"key"`
	Summary string `json:"summary"`
	Type    string `json:"type"`
	Status  string `json:"status"`
	URL     string `json:"url"`
}

type IssueDetail struct {
	Issue
	Description string    `json:"description"`
	Comments    []Comment `json:"comments"`
}

type Comment struct {
	Author  string `json:"author"`
	Created string `json:"created"`
	Body    string `json:"body"`
}

type Handler func(method string, params json.RawMessage) (any, error)

// Serve is the plugin-side half of the protocol: it reads one request from r,
// runs h and writes the response to w. A handler error is reported in the
// response and returned so the plugin can exit non-zero.
func Serve(r io.Reader, w io.Writer, h Handler) error {
	var req Request
	err := json.NewDecoder(r).Decode(&req)
	var result any
	switch {
	case err != nil:
		err = fmt.Errorf("decode request: %w", err)
	case req.Protocol != ProtocolVersion:
		err = fmt.Errorf("unsupported protocol %d, want %d", req.Protocol, ProtocolVersion)
	default:
		result, err = h(req.Method, req.Params)
	}
	var resp Response
	if err != nil {
		resp.Error = &ResponseError{Message: err.Error()}
	} else if resp.Result, err = json.Marshal(result); err != nil {
		resp.Error = &ResponseError{Message: err.Error()}
	}
	if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
		return errors.Join(err, encErr)
	}
	return err
}

var ErrUnknownMethod = errors.New("unknown method")
