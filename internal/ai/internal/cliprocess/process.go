// Package cliprocess runs AI CLI subprocesses with bounded capture and tree cancellation.
package cliprocess

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"time"
)

func Run(ctx context.Context, binary, dir string, environment []string, input io.Reader, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Env = environment
	cmd.Stdin, cmd.Stdout, cmd.Stderr = input, stdout, stderr
	cmd.WaitDelay = time.Second
	isolateProcess(cmd)
	return cmd.Run()
}

// Output consumes excess bytes without storing them.
type Output struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func NewOutput(limit int) *Output { return &Output{limit: limit} }
func (b *Output) Bytes() []byte   { return b.buf.Bytes() }
func (b *Output) String() string  { return b.buf.String() }
func (b *Output) Truncated() bool { return b.truncated }

func (b *Output) Write(data []byte) (int, error) {
	n := len(data)
	remaining := b.limit - b.buf.Len()
	if n > remaining {
		data = data[:remaining]
		b.truncated = true
	}
	_, err := b.buf.Write(data)
	return n, err
}
