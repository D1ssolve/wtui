//go:build !windows

package claude

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/D1ssolve/wtui/internal/ai"
)

// readinessDeadline lets the test signal expiry after the descendant handshake.
type readinessDeadline struct {
	context.Context
	expired <-chan struct{}
}

func (ctx readinessDeadline) Done() <-chan struct{} { return ctx.expired }

func (ctx readinessDeadline) Err() error {
	select {
	case <-ctx.expired:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func TestClaude_StopsProcessGroupOnCancelOrDeadline(t *testing.T) {
	for _, tt := range []struct {
		name string
		want error
	}{
		{"cancel", context.Canceled},
		{"deadline", context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeClaude(t)
			listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			setFixture(t, "block", listener.Addr().String())
			deadline := time.Now().Add(testTimeout)
			if err := listener.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			parent := t.Context()
			expired := make(chan struct{})
			if errors.Is(tt.want, context.DeadlineExceeded) {
				parent = readinessDeadline{context.WithoutCancel(parent), expired}
			}
			ctx, cancel := context.WithCancel(parent)
			done := make(chan error, 1)
			t.Cleanup(func() {
				cancel()
				select {
				case <-done:
				case <-time.After(testTimeout):
					t.Error("provider did not exit during cleanup")
				}
			})
			go func() {
				defer close(done)
				_, err := New("", 0).Generate(ctx, ai.Request{Title: privateInput})
				done <- err
			}()
			conn, err := listener.AcceptTCP()
			if err != nil {
				t.Fatalf("descendant did not start: %v", err)
			}
			defer conn.Close()
			if err := conn.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			peer := bufio.NewReader(conn)
			line, err := peer.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil || pid <= 1 {
				t.Fatal("missing fake CLI pid")
			}
			if errors.Is(tt.want, context.Canceled) {
				cancel()
			} else {
				close(expired)
			}
			select {
			case err := <-done:
				if !errors.Is(err, tt.want) {
					t.Fatalf("request error = %v, want %v", err, tt.want)
				}
			case <-time.After(time.Until(deadline)):
				t.Fatal("provider did not return promptly")
			}
			if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("fake CLI still exists: %v", err)
			}
			if _, err := peer.ReadByte(); !errors.Is(err, io.EOF) {
				t.Fatalf("descendant did not exit: %v", err)
			}
		})
	}
}

func TestClaude_EnforcesConfiguredTimeout(t *testing.T) {
	fakeClaude(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	// The fake hangs if it reaches inference. Expiry during startup is also valid.
	setFixture(t, "block", listener.Addr().String())
	ctx, cancel := context.WithTimeout(t.Context(), testTimeout)
	defer cancel()
	_, err = New("", 200*time.Millisecond).Generate(ctx, ai.Request{Title: privateInput})
	if !errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
		t.Fatalf("request error = %v, parent error = %v; want configured deadline", err, ctx.Err())
	}
}
