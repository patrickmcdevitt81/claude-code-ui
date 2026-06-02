// Package runner manages test execution and watch mode for a project directory.
package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ErrNoTestCommand is returned by Detect when no known test framework is found.
var ErrNoTestCommand = errors.New("runner: no test command detected in directory")

// Result holds the output and outcome of a single test run.
type Result struct {
	Command   string
	CWD       string
	Output    []string // each line of combined stdout+stderr
	Passed    bool
	Failed    bool
	ExitCode  int
	Duration  time.Duration
	StartedAt time.Time
	Summary   string // e.g. "42 passed, 0 failed" or "FAIL" or "ok"
}

// Runner manages test execution and watch mode for a project directory.
type Runner struct {
	cwd      string
	cmd      []string       // detected or explicit test command
	watching bool
	result   *Result        // most recent result (nil if never run)
	mu       sync.RWMutex
	cancel   context.CancelFunc // cancels the current run or watch goroutine
	onResult func(*Result)      // callback when a result is ready
}

// New creates a new Runner for the given directory, calling onResult whenever
// a test run completes.
func New(cwd string, onResult func(*Result)) *Runner {
	return &Runner{
		cwd:      cwd,
		onResult: onResult,
	}
}

// Detect auto-detects the test command for r.cwd. It tries several frameworks
// in priority order and returns the first match. Returns ErrNoTestCommand if
// none match.
func (r *Runner) Detect() ([]string, error) {
	cwd := r.cwd

	// 1. package.json with scripts.test
	if pkgPath := filepath.Join(cwd, "package.json"); fileExists(pkgPath) {
		if data, err := os.ReadFile(pkgPath); err == nil {
			type pkgJSON struct {
				Scripts map[string]string `json:"scripts"`
			}
			var pkg pkgJSON
			if err := json.Unmarshal(data, &pkg); err == nil && pkg.Scripts["test"] != "" {
				return []string{"npm", "test"}, nil
			}
		}
	}

	// 2. go.mod
	if fileExists(filepath.Join(cwd, "go.mod")) {
		return []string{"go", "test", "./..."}, nil
	}

	// 3. Python / pytest
	if fileExists(filepath.Join(cwd, "pytest.ini")) ||
		fileExists(filepath.Join(cwd, "setup.py")) ||
		pyprojectHasPytest(filepath.Join(cwd, "pyproject.toml")) {
		return []string{"python3", "-m", "pytest", "-v"}, nil
	}

	// 4. Makefile with test: target
	if makefileHasTestTarget(filepath.Join(cwd, "Makefile")) {
		return []string{"make", "test"}, nil
	}

	// 5. Cargo.toml
	if fileExists(filepath.Join(cwd, "Cargo.toml")) {
		return []string{"cargo", "test"}, nil
	}

	return nil, ErrNoTestCommand
}

// Run executes the test command once (non-blocking). It fires onResult when
// done. If a run is already in progress, it is cancelled first.
func (r *Runner) Run() error {
	r.mu.Lock()
	// Cancel any in-progress run.
	if r.cancel != nil {
		r.cancel()
	}
	// Detect command if not already set (must happen inside lock to avoid race).
	if len(r.cmd) == 0 {
		detected, err := r.Detect()
		if err != nil {
			r.mu.Unlock()
			return err
		}
		r.cmd = detected
	}
	// Create new context before spawning goroutine — assign cancel under lock.
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	cmd := r.cmd // local copy for goroutine
	r.mu.Unlock()

	go r.runAndNotify(ctx, cmd)
	return nil
}

// SetWatch enables or disables watch mode. When enabled, a goroutine runs the
// tests immediately and then re-runs them on file changes (debounced 500ms).
func (r *Runner) SetWatch(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if enabled == r.watching {
		return
	}
	r.watching = enabled

	// Stop any existing run/watch.
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}

	if !enabled {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel

	go r.watchLoop(ctx)
}

// IsWatching reports whether watch mode is active.
func (r *Runner) IsWatching() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.watching
}

// LastResult returns the most recent test result (may be nil).
func (r *Runner) LastResult() *Result {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.result
}

// Stop cancels any active run or watch goroutine.
func (r *Runner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.watching = false
}

// watchLoop runs tests immediately then re-runs on file changes. It is
// cancelled via ctx.
func (r *Runner) watchLoop(ctx context.Context) {
	// Resolve command once before starting the loop.
	r.mu.RLock()
	cmd := r.cmd
	cwd := r.cwd
	r.mu.RUnlock()

	if len(cmd) == 0 {
		detected, err := r.Detect()
		if err != nil {
			return
		}
		r.mu.Lock()
		r.cmd = detected
		cmd = detected
		r.mu.Unlock()
	}

	// Run immediately.
	r.runAndNotify(ctx, cmd)

	// Set up fsnotify watcher on cwd and one level of subdirs.
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return
	}
	defer fw.Close()

	// Watch cwd itself.
	_ = fw.Add(cwd)

	// Watch cwd and one level of subdirectories (non-recursive).
	// Changes deeper than 1 level (e.g. internal/foo/bar/) are not detected.
	// This covers typical project structures without blowing up on large repos.
	skipDirs := map[string]bool{
		"node_modules": true,
		".git":         true,
		"vendor":       true,
		"dist":         true,
		".next":        true,
	}
	entries, _ := os.ReadDir(cwd)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if skipDirs[e.Name()] {
			continue
		}
		_ = fw.Add(filepath.Join(cwd, e.Name()))
	}

	// Extensions of interest.
	watchExts := map[string]bool{
		".go":  true,
		".ts":  true,
		".tsx": true,
		".js":  true,
		".jsx": true,
		".py":  true,
		".rs":  true,
	}

	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		select {
		case <-debounce.C:
		default:
		}
	}
	pending := false

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-fw.Events:
			if !ok {
				return
			}
			if event.Op == fsnotify.Chmod {
				continue
			}
			ext := strings.ToLower(filepath.Ext(event.Name))
			if !watchExts[ext] {
				continue
			}
			if pending {
				if !debounce.Stop() {
					select {
					case <-debounce.C:
					default:
					}
				}
			}
			debounce.Reset(500 * time.Millisecond)
			pending = true

		case <-debounce.C:
			pending = false
			r.mu.RLock()
			cmd = r.cmd
			r.mu.RUnlock()
			r.runAndNotify(ctx, cmd)
		}
	}
}

// runAndNotify executes the test command and notifies via onResult, respecting ctx.
func (r *Runner) runAndNotify(ctx context.Context, cmd []string) {
	result := r.execCmd(ctx, cmd)
	if ctx.Err() != nil {
		return // cancelled; discard stale result from a zombie goroutine
	}
	r.mu.Lock()
	r.result = result
	r.mu.Unlock()
	if r.onResult != nil {
		r.onResult(result)
	}
}

// execCmd runs a command and collects output into a Result.
func (r *Runner) execCmd(ctx context.Context, cmd []string) *Result {
	r.mu.RLock()
	cwd := r.cwd
	r.mu.RUnlock()

	start := time.Now()
	result := &Result{
		Command:   strings.Join(cmd, " "),
		CWD:       cwd,
		StartedAt: start,
	}

	if len(cmd) == 0 {
		result.Failed = true
		result.Summary = "no command"
		return result
	}

	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	c.Dir = cwd

	outBytes, runErr := c.CombinedOutput()

	result.Duration = time.Since(start)

	// Split output into lines.
	scanner := bufio.NewScanner(strings.NewReader(string(outBytes)))
	for scanner.Scan() {
		result.Output = append(result.Output, scanner.Text())
	}

	// Determine exit code.
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		} else if ctx.Err() != nil {
			result.ExitCode = -1
		} else {
			result.ExitCode = 1
		}
	}

	// Parse results based on command type.
	switch {
	case len(cmd) >= 2 && cmd[0] == "go" && cmd[1] == "test":
		result.Passed, result.Failed, result.Summary = parseGoTestOutput(result.Output, result.ExitCode)
	case len(cmd) >= 1 && cmd[0] == "npm":
		if result.ExitCode == 0 {
			result.Passed = true
			result.Summary = "passed"
		} else {
			result.Failed = true
			result.Summary = "failed"
		}
	default:
		if result.ExitCode == 0 {
			result.Passed = true
			result.Summary = "passed"
		} else {
			result.Failed = true
			result.Summary = "failed"
		}
	}

	return result
}

// parseGoTestOutput scans `go test` output lines and returns pass/fail/summary.
func parseGoTestOutput(lines []string, exitCode int) (passed, failed bool, summary string) {
	passCount := 0
	failCount := 0
	hasOk := false
	hasFail := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "--- PASS:"):
			passCount++
		case strings.HasPrefix(trimmed, "--- FAIL:"):
			failCount++
		case strings.HasPrefix(trimmed, "ok "):
			hasOk = true
		case trimmed == "FAIL" || strings.HasPrefix(trimmed, "FAIL\t") || strings.HasPrefix(trimmed, "FAIL "):
			hasFail = true
		}
	}

	if failCount > 0 || hasFail || exitCode != 0 {
		failed = true
		if passCount > 0 || failCount > 0 {
			summary = strings.Join([]string{
				strconv.Itoa(passCount) + " passed",
				strconv.Itoa(failCount) + " failed",
			}, ", ")
		} else {
			summary = "FAIL"
		}
		return
	}

	passed = true
	_ = hasOk
	if passCount > 0 {
		summary = strconv.Itoa(passCount) + " passed"
	} else {
		summary = "ok"
	}
	return
}

// fileExists reports whether the given path is an existing regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// pyprojectHasPytest checks whether pyproject.toml contains a [tool.pytest...] section.
func pyprojectHasPytest(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "[tool.pytest")
}

// makefileHasTestTarget checks whether a Makefile contains a "test:" target.
func makefileHasTestTarget(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "test:") {
			return true
		}
	}
	return false
}
