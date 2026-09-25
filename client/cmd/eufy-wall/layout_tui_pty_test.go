//go:build linux || darwin

package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

type lockedCapture struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (c *lockedCapture) Write(data []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.Write(data)
}

func (c *lockedCapture) contains(text string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Contains(c.b.String(), text)
}

func waitForTerminalText(t *testing.T, captured *lockedCapture, text string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if captured.contains(text) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("terminal never rendered %q", text)
}

func TestFullScreenPTYLifecycleAndResize(t *testing.T) {
	t.Setenv("TERM", "xterm")
	t.Setenv("EUFY_WALL_EDITOR", "")
	master, slave, err := pty.Open()
	if err != nil {
		t.Skipf("pseudo-terminal unavailable: %v", err)
	}
	defer master.Close()
	defer slave.Close()
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := fullScreenTerminal(slave, slave); !ok {
		t.Fatal("80x24 terminal did not select full-screen editor")
	}
	editor := screenFixture(t).editor
	captured := &lockedCapture{}
	go io.Copy(captured, master)
	done := make(chan error, 1)
	go func() { done <- runLayoutScreen(editor, slave, slave) }()
	waitForTerminalText(t, captured, "EUFY WALL")
	if _, err := master.Write([]byte("8\x1b[C")); err != nil {
		t.Fatal(err)
	}
	waitForTerminalText(t, captured, "Grid: x=8 y=0 w=8 h=8")
	if _, err := master.Write([]byte("r\x1b[C")); err != nil {
		t.Fatal(err)
	}
	waitForTerminalText(t, captured, "Grid: x=8 y=0 w=16 h=8")
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 20, Cols: 60}); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	waitForTerminalText(t, captured, "Resize terminal to at least 80x24")
	if err := pty.Setsize(slave, &pty.Winsize{Rows: 30, Cols: 100}); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGWINCH); err != nil {
		t.Fatal(err)
	}
	if _, err := master.Write([]byte("qDISCARD\r")); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("editor did not quit from pseudo-terminal")
	}
	waitForTerminalText(t, captured, "\x1b[?1049l")
	t.Setenv("EUFY_WALL_EDITOR", "line")
	if _, _, ok := fullScreenTerminal(slave, slave); ok {
		t.Fatal("screen-reader line-mode override ignored")
	}
}
