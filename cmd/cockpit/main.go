// Cockpit — performance-optimized terminal UI for Claude Code CLI.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"cockpit/internal/agent"
	"cockpit/internal/build"
	"cockpit/internal/tui"
	"cockpit/internal/watch"
)

// commonClaudePaths lists well-known installation locations for the claude binary,
// checked in order when neither --claude-path nor PATH resolution succeed.
var commonClaudePaths = []string{
	"/opt/homebrew/bin/claude",
	"/usr/local/bin/claude",
	"~/.local/bin/claude",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "cockpit: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// --- Flag definitions ---
	var (
		claudePathFlag string
		claudeDirFlag  string
		versionFlag    bool
	)

	flag.StringVar(&claudePathFlag, "claude-path", "", "override path to the claude binary (default: auto-detected)")
	flag.StringVar(&claudeDirFlag, "claude-dir", "", "override path to ~/.claude directory (default: ~/.claude)")
	flag.BoolVar(&versionFlag, "version", false, "print version and exit")
	flag.Parse()

	if versionFlag {
		fmt.Printf("cockpit v%s\n", build.Version)
		return nil
	}

	// --- Resolve claude dir ---
	claudeDir, err := resolveClaudeDir(claudeDirFlag)
	if err != nil {
		return err
	}

	// --- Resolve claude binary ---
	claudePath, err := resolveClaudePath(claudePathFlag)
	if err != nil {
		return err
	}

	// --- Resolve working directory (default launch dir for new agents) ---
	workDir, err := os.Getwd()
	if err != nil {
		workDir = claudeDir // fallback: data dir is better than nothing
	}

	// --- Create agent manager ---
	mgr := agent.NewManager(claudePath, claudeDir)

	// --- Launch TUI ---
	model := tui.New(claudePath, claudeDir, workDir, mgr)
	p := tea.NewProgram(model, tea.WithAltScreen())

	// Start the file watcher. If it fails, log a warning and continue without
	// live updates — the dashboard still works, just won't auto-refresh.
	w, err := watch.New(claudeDir, p)
	if err != nil {
		log.Printf("cockpit: file watcher unavailable: %v", err)
	} else {
		go w.Start()
		defer w.Close()
	}

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}

// resolveClaudeDir returns the validated claude data directory path.
// If override is empty it defaults to ~/.claude.
func resolveClaudeDir(override string) (string, error) {
	dir := override
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot determine home directory: %w", err)
		}
		dir = filepath.Join(home, ".claude")
	} else {
		// Expand a leading ~ in caller-supplied value.
		if len(dir) >= 2 && dir[:2] == "~/" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("cannot determine home directory: %w", err)
			}
			dir = filepath.Join(home, dir[2:])
		}
	}

	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("claude data directory not found: %s\n  (create it or use --claude-dir to override)", dir)
		}
		return "", fmt.Errorf("cannot access claude data directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("claude data path is not a directory: %s", dir)
	}

	// Verify we can read the directory.
	if _, err := os.ReadDir(dir); err != nil {
		return "", fmt.Errorf("claude data directory is not readable: %s: %w", dir, err)
	}

	return dir, nil
}

// resolveClaudePath finds and validates the claude binary.
// Resolution order:
//  1. --claude-path flag (if set)
//  2. PATH via exec.LookPath("claude")
//  3. commonClaudePaths (well-known locations)
func resolveClaudePath(override string) (string, error) {
	if override != "" {
		// Expand ~ if present.
		if len(override) >= 2 && override[:2] == "~/" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("cannot determine home directory: %w", err)
			}
			override = filepath.Join(home, override[2:])
		}
		if err := checkExecutable(override); err != nil {
			return "", fmt.Errorf("--claude-path %q: %w", override, err)
		}
		return override, nil
	}

	// Try PATH.
	if p, err := exec.LookPath("claude"); err == nil {
		return p, nil
	}

	// Try common locations.
	for _, candidate := range commonClaudePaths {
		expanded := expandHome(candidate)
		if err := checkExecutable(expanded); err == nil {
			return expanded, nil
		}
	}

	return "", fmt.Errorf(
		"claude binary not found\n" +
			"  searched: PATH, /opt/homebrew/bin/claude, /usr/local/bin/claude, ~/.local/bin/claude\n" +
			"  install claude (https://claude.ai/code) or use --claude-path to specify its location",
	)
}

// checkExecutable verifies that path exists and is executable.
func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("file not found")
		}
		return fmt.Errorf("cannot stat: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a binary")
	}
	// Tests whether any execute bit (owner/group/other) is set — an acceptable
	// approximation for a developer tool rather than a per-user permission check.
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("file is not executable")
	}
	return nil
}

// expandHome replaces a leading ~/ with the user's home directory.
func expandHome(p string) string {
	if len(p) >= 2 && p[:2] == "~/" {
		home, err := os.UserHomeDir()
		if err != nil {
			// If home dir is unknown, return path as-is so the caller's
			// stat check will naturally fail and move to the next candidate.
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}
