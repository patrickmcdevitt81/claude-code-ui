package agent

import (
	"os"
	"strings"
	"testing"
	"time"
)

// isTTY returns true when the process has a real controlling terminal.
func isTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// TestStartAndDrain verifies that:
//  1. Start() successfully launches a process in a PTY.
//  2. The drain goroutine captures output in the ring buffer.
//  3. Wait() is closed after the process exits.
func TestStartAndDrain(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := t.TempDir()

	// "sh -c echo" works on all Unix platforms and produces deterministic output.
	a, err := Start("sh", tmpDir, []string{"-c", "echo hello-from-cockpit"}, claudeDir)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for the process to exit (max 5 s).
	select {
	case <-a.Wait():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5 s")
	}

	// Give the drain goroutine a moment to flush into the ring buffer.
	time.Sleep(50 * time.Millisecond)

	a.rbMu.Lock()
	buf := string(a.rbBuf)
	a.rbMu.Unlock()

	if !strings.Contains(buf, "hello-from-cockpit") {
		t.Errorf("ring buffer = %q, want it to contain %q", buf, "hello-from-cockpit")
	}
}

// TestResize verifies that Resize does not return an error on a live agent.
func TestResize(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := t.TempDir()

	a, err := Start("sh", tmpDir, []string{"-c", "sleep 5"}, claudeDir)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Kill() })

	if err := a.Resize(30, 100); err != nil {
		t.Errorf("Resize(30, 100): %v", err)
	}
	if err := a.Resize(50, 200); err != nil {
		t.Errorf("Resize(50, 200): %v", err)
	}
}

// TestAttachDeadAgentReturnsImmediately verifies that Attach on a dead agent
// returns nil without blocking.
func TestAttachDeadAgentReturnsImmediately(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := t.TempDir()

	a, err := Start("sh", tmpDir, []string{"-c", "exit 0"}, claudeDir)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	select {
	case <-a.Wait():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5 s")
	}

	done := make(chan error, 1)
	go func() { done <- a.Attach(0x1d) }()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Attach on dead agent: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Attach on dead agent did not return within 2 s")
	}
}

// TestKill verifies that Kill terminates a running process.
func TestKill(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := t.TempDir()

	a, err := Start("sh", tmpDir, []string{"-c", "sleep 60"}, claudeDir)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := a.Kill(); err != nil {
		t.Errorf("Kill: %v", err)
	}

	select {
	case <-a.Wait():
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after Kill within 5 s")
	}
}

// TestRingBufferCap verifies that the ring buffer does not grow beyond ringBufCap.
func TestRingBufferCap(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := t.TempDir()

	// Write ~512 KB via cat — exceeds the 256 KB cap.
	cmd := "dd if=/dev/zero bs=1024 count=512 2>/dev/null | cat"
	a, err := Start("sh", tmpDir, []string{"-c", cmd}, claudeDir)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = a.Kill() })

	select {
	case <-a.Wait():
	case <-time.After(10 * time.Second):
		t.Fatal("process did not exit within 10 s")
	}

	time.Sleep(50 * time.Millisecond)

	a.rbMu.Lock()
	n := len(a.rbBuf)
	a.rbMu.Unlock()

	if n > ringBufCap {
		t.Errorf("ring buffer length %d exceeds cap %d", n, ringBufCap)
	}
}
