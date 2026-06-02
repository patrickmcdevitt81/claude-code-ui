// Package agent spawns and manages claude CLI processes in pseudo-terminals.
//
// The focus-passthrough model: Cockpit never speaks to the model itself; it
// only launches the real installed `claude` binary inside a PTY, then either
// bridges stdin/stdout directly (focus mode) or simply keeps the process
// alive in the background.
package agent

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// Agent represents one running claude process in a PTY.
type Agent struct {
	ID         string    // random hex ID
	CWD        string    // working directory
	ClaudePath string    // path to the claude binary
	started    time.Time // time the process was started
	cmd        *exec.Cmd
	ptmx       *os.File      // PTY master
	done       chan struct{}  // closed when the process exits

	mu        sync.RWMutex // guards sessionID
	sessionID string       // empty until detected via sessions/*.json polling
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

	// Start the process inside a PTY.
	ptmx, err := pty.Start(cmd)
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

	// Wait for the process to exit in a goroutine, close the PTY master fd,
	// then signal done.
	go func() {
		_ = cmd.Wait()
		_ = a.ptmx.Close() // release the fd; no more I/O after process exits
		close(a.done)
	}()

	// Poll sessions/*.json to detect when claude registers its session.
	go a.pollSession(claudeDir)

	return a, nil
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

// PTY returns the PTY master file used for focus passthrough.
func (a *Agent) PTY() *os.File {
	return a.ptmx
}

// ptyExec implements tea.ExecCommand for attaching to a running PTY master.
// When Run() is called, it bridges os.Stdin → ptmx and ptmx → os.Stdout until
// either the PTY is closed (agent exited) or stdin is closed (ctrl+d).
type ptyExec struct {
	ptmx *os.File
}

// NewPTYExec returns a tea.ExecCommand that bridges stdin/stdout to the PTY.
// The returned value implements tea.ExecCommand: when Run() is called Bubble
// Tea suspends its raw mode and hands the terminal directly to the PTY master.
func NewPTYExec(ptmx *os.File) *ptyExec {
	return &ptyExec{ptmx: ptmx}
}

// SetStdin is a no-op; we always use os.Stdin directly.
func (p *ptyExec) SetStdin(_ io.Reader) {}

// SetStdout is a no-op; we always use os.Stdout directly.
func (p *ptyExec) SetStdout(_ io.Writer) {}

// SetStderr is a no-op.
func (p *ptyExec) SetStderr(_ io.Writer) {}

// Run bridges the PTY master with the real terminal until either side closes.
func (p *ptyExec) Run() error {
	// PTY output → terminal (runs in goroutine)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(os.Stdout, p.ptmx)
	}()

	// Terminal input → PTY (blocks until stdin closes or ptmx write fails)
	_, _ = io.Copy(p.ptmx, os.Stdin)

	// Wait for output goroutine to drain.
	<-done
	return nil
}
