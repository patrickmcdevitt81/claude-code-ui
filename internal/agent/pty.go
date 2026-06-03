// Package agent spawns and manages claude CLI processes in pseudo-terminals.
//
// # Attach/detach broker
//
// Every agent runs a continuous background drain goroutine that reads from its
// PTY master file into a 256 KB ring buffer, so background agents never block
// (the kernel PTY buffer — 16–64 KB — would fill and freeze claude).
//
// Call Attach(detachKey) to bridge the real terminal to the agent's PTY:
//   - stdin is put in raw mode so every keypress reaches claude
//   - the ring buffer is replayed so prior output is visible immediately
//   - SIGWINCH is forwarded so window resizes reflow claude
//   - pressing detachKey (default ctrl-] = 0x1d) returns to cockpit while
//     claude continues running in the background
//
// Attach returns when the user detaches or the agent process exits.
package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

const ringBufCap = 256 * 1024 // 256 KB ring buffer for PTY output replay

// Agent represents one running claude process in a PTY.
type Agent struct {
	ID         string    // random hex ID
	CWD        string    // working directory
	ClaudePath string    // path to the claude binary
	started    time.Time // time the process was started
	cmd        *exec.Cmd
	ptmx       *os.File     // PTY master
	done       chan struct{} // closed when the process exits

	mu        sync.RWMutex // guards sessionID
	sessionID string       // empty until detected via sessions/*.json polling

	// Ring buffer — always-running drain goroutine writes here.
	rbMu  sync.Mutex
	rbBuf []byte // bounded to ringBufCap; oldest bytes trimmed on overflow

	// Current foreground output sink (nil when detached).
	sinkMu sync.Mutex
	sink   io.Writer
}

// Start launches a new claude process in a PTY.
//
//   - claudePath: absolute path to the claude binary
//   - cwd: working directory for the new process
//   - args: extra arguments passed to claude (may be empty)
//   - claudeDir: path to ~/.claude, used for sessions/*.json polling
func Start(claudePath, cwd string, args []string, claudeDir string) (*Agent, error) {
	// Generate a random hex ID (8 bytes → 16 hex chars).
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(raw[:])

	cmd := exec.Command(claudePath, args...)
	cmd.Dir = cwd

	// Inherit the current environment and add TERM so claude renders correctly.
	env := make([]string, len(os.Environ()))
	copy(env, os.Environ())
	env = append(env, "TERM=xterm-256color")
	cmd.Env = env

	// Start with a sane default size; Attach will resize to the real terminal.
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
	if err != nil {
		return nil, err
	}

	a := &Agent{
		ID:         id,
		CWD:        cwd,
		ClaudePath: claudePath,
		started:    time.Now(),
		cmd:        cmd,
		ptmx:       ptmx,
		done:       make(chan struct{}),
	}

	// Waiter: when the process exits, close ptmx (which unblocks the drain
	// goroutine's Read), then signal done.
	go func() {
		_ = cmd.Wait()
		_ = ptmx.Close()
		close(a.done)
	}()

	// Drain goroutine: continuously reads from ptmx into the ring buffer and
	// forwards to the active sink (if any). Runs until ptmx is closed.
	go a.drain()

	// Poll sessions/*.json to detect when claude registers its session.
	go a.pollSession(claudeDir)

	return a, nil
}

// drain continuously reads from the PTY master, appending to the ring buffer
// and forwarding to the active sink. It terminates when ptmx is closed.
func (a *Agent) drain() {
	buf := make([]byte, 4096)
	for {
		n, err := a.ptmx.Read(buf)
		if n > 0 {
			data := buf[:n]

			// Append to ring buffer, trimming from the front if over cap.
			a.rbMu.Lock()
			a.rbBuf = append(a.rbBuf, data...)
			if len(a.rbBuf) > ringBufCap {
				a.rbBuf = a.rbBuf[len(a.rbBuf)-ringBufCap:]
			}
			a.rbMu.Unlock()

			// Forward to sink if attached.
			a.sinkMu.Lock()
			sink := a.sink
			a.sinkMu.Unlock()
			if sink != nil {
				_, _ = sink.Write(data)
			}
		}
		if err != nil {
			return // ptmx closed or process exited
		}
	}
}

// Attach bridges the real terminal to this agent's PTY until the user presses
// detachKey (typically ctrl-] = 0x1d) or the agent process exits.
//
// On entry the ring buffer is replayed to stdout so prior output is visible.
// SIGWINCH is forwarded so window resizes reflow claude. On detach the
// terminal is restored and Attach returns nil — claude continues running.
func (a *Agent) Attach(detachKey byte) error {
	// If the process is already dead, return immediately.
	select {
	case <-a.done:
		return nil
	default:
	}

	// Put stdin in raw mode so every byte (arrows, ctrl-keys, etc.) is
	// forwarded directly to the PTY without line-buffering.
	stdinFd := int(os.Stdin.Fd())
	state, err := term.MakeRaw(stdinFd)
	if err != nil {
		// Fall back gracefully — the session will work but key sequences may
		// be cooked (line-buffered). Not ideal but beats crashing.
		state = nil
	}
	if state != nil {
		defer term.Restore(stdinFd, state)
	}

	// Resize the PTY to the actual terminal dimensions.
	if cols, rows, e := term.GetSize(int(os.Stdout.Fd())); e == nil && cols > 0 && rows > 0 {
		_ = a.Resize(uint16(rows), uint16(cols))
	}

	// Forward SIGWINCH → PTY resize in a background goroutine.
	winch := make(chan os.Signal, 4)
	signal.Notify(winch, syscall.SIGWINCH)
	winchDone := make(chan struct{})
	go func() {
		defer close(winchDone)
		for {
			select {
			case _, ok := <-winch:
				if !ok {
					return
				}
				if cols, rows, e := term.GetSize(int(os.Stdout.Fd())); e == nil && cols > 0 && rows > 0 {
					_ = a.Resize(uint16(rows), uint16(cols))
				}
			case <-a.done:
				return
			}
		}
	}()
	defer func() {
		signal.Stop(winch)
		close(winch)
		<-winchDone
	}()

	// Set the sink so live output flows to stdout, then replay the ring buffer
	// so the user sees prior output.  After replay, send SIGWINCH to the child
	// so claude repaints to the current terminal size (handles state divergence).
	a.sinkMu.Lock()
	a.sink = os.Stdout
	a.sinkMu.Unlock()

	a.rbMu.Lock()
	replay := make([]byte, len(a.rbBuf))
	copy(replay, a.rbBuf)
	a.rbMu.Unlock()

	if len(replay) > 0 {
		_, _ = os.Stdout.Write(replay)
	}
	if a.cmd != nil && a.cmd.Process != nil {
		_ = a.cmd.Process.Signal(syscall.SIGWINCH)
	}

	// Forward stdin → ptmx, scanning for the detach key.
	quit := make(chan struct{})
	var quitOnce sync.Once
	closeQuit := func() { quitOnce.Do(func() { close(quit) }) }

	go func() {
		buf := make([]byte, 512)
		for {
			n, readErr := os.Stdin.Read(buf)
			if n > 0 {
				// If we've already been told to stop, discard and exit.
				select {
				case <-quit:
					return
				default:
				}

				// Scan for detach key; forward everything before it, then detach.
				detachIdx := -1
				for i, b := range buf[:n] {
					if b == detachKey {
						detachIdx = i
						break
					}
				}
				if detachIdx >= 0 {
					if detachIdx > 0 {
						_, _ = a.ptmx.Write(buf[:detachIdx])
					}
					// Clean up the alternate screen so the cockpit TUI can resume.
					_, _ = os.Stdout.Write([]byte("\x1b[?1049l\x1b[?25h\r\n"))
					closeQuit()
					return
				}
				_, _ = a.ptmx.Write(buf[:n])
			}
			if readErr != nil {
				closeQuit()
				return
			}
		}
	}()

	// Block until the user detaches or the process exits.
	select {
	case <-quit:
	case <-a.done:
		closeQuit()
	}

	// Detach the sink so the drain goroutine stops writing to stdout.
	a.sinkMu.Lock()
	a.sink = nil
	a.sinkMu.Unlock()

	return nil
}

// agentExec wraps an Agent so it implements tea.ExecCommand.
// Run calls Attach(detachKey) so the TUI can hand terminal control to the agent.
type agentExec struct {
	a         *Agent
	detachKey byte
}

// NewExec returns a tea.ExecCommand that attaches the terminal to this agent.
// When the user presses ctrl-] (0x1d) or the process exits, Run returns and
// Bubble Tea resumes the cockpit TUI.
func (a *Agent) NewExec() *agentExec {
	return &agentExec{a: a, detachKey: 0x1d}
}

// SetStdin is a no-op; Attach always uses os.Stdin directly.
func (e *agentExec) SetStdin(_ io.Reader) {}

// SetStdout is a no-op; Attach always uses os.Stdout directly.
func (e *agentExec) SetStdout(_ io.Writer) {}

// SetStderr is a no-op.
func (e *agentExec) SetStderr(_ io.Writer) {}

// Run implements tea.ExecCommand by attaching to the agent's PTY.
func (e *agentExec) Run() error {
	return e.a.Attach(e.detachKey)
}

// rawSessionFile is the minimal structure we need from sessions/*.json.
type rawSessionFile struct {
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	StartedAt int64  `json:"startedAt"` // Unix milliseconds
}

// pollSession watches <claudeDir>/sessions/*.json every 500 ms until it finds
// an entry whose cwd matches a.CWD and whose startedAt is after a.started.
// It stores the discovered session ID in a.sessionID (guarded by a.mu) and then stops.
func (a *Agent) pollSession(claudeDir string) {
	sessDir := filepath.Join(claudeDir, "sessions")
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-a.done:
			return
		case <-ticker.C:
			entries, err := os.ReadDir(sessDir)
			if err != nil {
				continue
			}
			for _, entry := range entries {
				if entry.IsDir() || !isJSONFile(entry.Name()) {
					continue
				}
				data, err := os.ReadFile(filepath.Join(sessDir, entry.Name()))
				if err != nil {
					continue
				}
				var raw rawSessionFile
				if err := json.Unmarshal(data, &raw); err != nil {
					continue
				}
				// Match on cwd and startedAt.
				if raw.CWD != a.CWD {
					continue
				}
				// StartedAt is Unix milliseconds; compare against agent start time.
				fileTime := time.UnixMilli(raw.StartedAt)
				if fileTime.Before(a.started) {
					continue
				}
				// Found our session.
				a.mu.Lock()
				a.sessionID = raw.SessionID
				a.mu.Unlock()
				return
			}
		}
	}
}

// isJSONFile reports whether name ends with ".json" and does not start with ".".
func isJSONFile(name string) bool {
	if len(name) == 0 || name[0] == '.' {
		return false
	}
	return len(name) > 5 && name[len(name)-5:] == ".json"
}

// GetSessionID returns the session ID discovered by pollSession, or an empty
// string if the session has not yet been detected. Safe for concurrent use.
func (a *Agent) GetSessionID() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.sessionID
}

// Resize updates the PTY window size.
func (a *Agent) Resize(rows, cols uint16) error {
	if a.ptmx == nil {
		return nil
	}
	return pty.Setsize(a.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
}

// Kill sends SIGTERM to the process; if it has not exited within 2 seconds,
// it sends SIGKILL.
func (a *Agent) Kill() error {
	if a.cmd == nil {
		return nil
	}
	if err := a.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Process may have already exited — that's fine.
		return nil
	}
	select {
	case <-a.done:
		return nil
	case <-time.After(2 * time.Second):
		return a.cmd.Process.Signal(syscall.SIGKILL)
	}
}

// Wait returns a channel that is closed when the process exits.
func (a *Agent) Wait() <-chan struct{} {
	return a.done
}

// UptimeStr returns a short human-readable uptime string (e.g. "3m", "2h").
func (a *Agent) UptimeStr() string {
	d := time.Since(a.started)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}
