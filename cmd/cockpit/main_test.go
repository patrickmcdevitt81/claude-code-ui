package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// expandHome
// ---------------------------------------------------------------------------

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot determine home dir: %v", err)
	}

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "tilde prefix is expanded",
			input: "~/.claude",
			want:  filepath.Join(home, ".claude"),
		},
		{
			name:  "plain absolute path is unchanged",
			input: "/opt/homebrew/bin/claude",
			want:  "/opt/homebrew/bin/claude",
		},
		{
			name:  "tilde without slash is unchanged",
			input: "~",
			want:  "~",
		},
		{
			name:  "relative path is unchanged",
			input: "some/relative/path",
			want:  "some/relative/path",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := expandHome(tc.input)
			if got != tc.want {
				t.Errorf("expandHome(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// resolveClaudeDir
// ---------------------------------------------------------------------------

func TestResolveClaudeDir(t *testing.T) {
	// Create a real temporary directory to use as a valid claude dir.
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid existing directory is accepted",
			input:   tmpDir,
			wantErr: false,
		},
		{
			name:    "non-existent path returns error",
			input:   filepath.Join(os.TempDir(), "cockpit-test-does-not-exist-xyz"),
			wantErr: true,
		},
		{
			name:    "path to a file (not dir) returns error",
			input:   createTempFile(t),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveClaudeDir(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("resolveClaudeDir(%q): expected error, got path %q", tc.input, got)
				}
			} else {
				if err != nil {
					t.Errorf("resolveClaudeDir(%q): unexpected error: %v", tc.input, err)
				}
				if got != tc.input {
					t.Errorf("resolveClaudeDir(%q) = %q, want %q", tc.input, got, tc.input)
				}
			}
		})
	}
}

// createTempFile creates a temporary regular file and returns its path.
func createTempFile(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp("", "cockpit-test-file-*")
	if err != nil {
		t.Fatalf("createTempFile: %v", err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

// ---------------------------------------------------------------------------
// resolveClaudePath
// ---------------------------------------------------------------------------

func TestResolveClaudePath(t *testing.T) {
	const realClaude = "/opt/homebrew/bin/claude"

	// Verify the binary exists before running path-specific sub-tests.
	claudeExists := false
	if info, err := os.Stat(realClaude); err == nil && !info.IsDir() {
		claudeExists = true
	}

	tests := []struct {
		name      string
		override  string
		wantErr   bool
		skipIfNo  string // skip if this binary is absent
	}{
		{
			name:     "empty override falls back to auto-detection",
			override: "",
			// Cannot assert wantErr here because it depends on the environment;
			// just ensure it doesn't panic. Auto-detection may or may not find claude.
		},
		{
			name:     "explicit valid path is accepted",
			override: realClaude,
			wantErr:  false,
			skipIfNo: realClaude,
		},
		{
			name:    "non-existent explicit path returns error",
			override: "/tmp/cockpit-no-such-binary-xyz",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipIfNo != "" && !claudeExists {
				t.Skipf("skipping: %s not present on this system", tc.skipIfNo)
			}

			got, err := resolveClaudePath(tc.override)
			if tc.wantErr {
				if err == nil {
					t.Errorf("resolveClaudePath(%q): expected error, got path %q", tc.override, got)
				}
				return
			}
			// For the empty-override case we only check it doesn't panic.
			if tc.override != "" && err != nil {
				t.Errorf("resolveClaudePath(%q): unexpected error: %v", tc.override, err)
			}
			if tc.override != "" && err == nil && got == "" {
				t.Errorf("resolveClaudePath(%q): returned empty path without error", tc.override)
			}
		})
	}
}
