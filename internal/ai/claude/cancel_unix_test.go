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

func TestClaude_StopsProcessGroupOnCancelOrTimeout(t *testing.T) {
	for _, tt := range []struct {
		name    string
		timeout time.Duration
		want    error
	}{
		{"cancel", testTimeout, context.Canceled},
		{"timeout", 4 * time.Second, context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fakeClaude(t)
			listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			t.Setenv("WTUI_CLAUDE_BLOCK", listener.Addr().String())
			deadline := time.Now().Add(testTimeout)
			if err := listener.SetDeadline(deadline); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := New("", tt.timeout).Generate(ctx, ai.Request{Title: privateInput})
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
			if tt.want == context.Canceled {
				cancel()
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
