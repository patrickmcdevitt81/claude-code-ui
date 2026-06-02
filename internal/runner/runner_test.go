package runner

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---- TestDetect_GoMod -------------------------------------------------------

func TestDetect_GoMod(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	r := New(dir, nil)
	cmd, err := r.Detect()
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(cmd) < 3 || cmd[0] != "go" || cmd[1] != "test" {
		t.Errorf("expected [go test ./...], got %v", cmd)
	}
}

// ---- TestDetect_PackageJSON -------------------------------------------------

func TestDetect_PackageJSON(t *testing.T) {
	dir := t.TempDir()
	pkg := `{"name":"foo","scripts":{"test":"jest"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(pkg), 0o644); err != nil {
		t.Fatalf("write package.json: %v", err)
	}
	r := New(dir, nil)
	cmd, err := r.Detect()
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(cmd) == 0 || cmd[0] != "npm" {
		t.Errorf("expected npm command, got %v", cmd)
	}
}

// ---- TestDetect_Makefile ----------------------------------------------------

func TestDetect_Makefile(t *testing.T) {
	dir := t.TempDir()
	makefile := "all:\n\techo all\n\ntest:\n\techo test\n"
	if err := os.WriteFile(filepath.Join(dir, "Makefile"), []byte(makefile), 0o644); err != nil {
		t.Fatalf("write Makefile: %v", err)
	}
	r := New(dir, nil)
	cmd, err := r.Detect()
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(cmd) < 2 || cmd[0] != "make" || cmd[1] != "test" {
		t.Errorf("expected [make test], got %v", cmd)
	}
}

// ---- TestDetect_CargoToml ---------------------------------------------------

func TestDetect_CargoToml(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\nname=\"foo\"\n"), 0o644); err != nil {
		t.Fatalf("write Cargo.toml: %v", err)
	}
	r := New(dir, nil)
	cmd, err := r.Detect()
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(cmd) < 2 || cmd[0] != "cargo" || cmd[1] != "test" {
		t.Errorf("expected [cargo test], got %v", cmd)
	}
}

// ---- TestDetect_NoMatch -----------------------------------------------------

func TestDetect_NoMatch(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	_, err := r.Detect()
	if err != ErrNoTestCommand {
		t.Errorf("expected ErrNoTestCommand, got %v", err)
	}
}

// ---- TestDetect_PyprojectToml -----------------------------------------------

func TestDetect_PyprojectToml(t *testing.T) {
	dir := t.TempDir()
	content := "[build-system]\n[tool.pytest.ini_options]\ntestpaths = [\"tests\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write pyproject.toml: %v", err)
	}
	r := New(dir, nil)
	cmd, err := r.Detect()
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(cmd) < 3 || cmd[0] != "python3" {
		t.Errorf("expected python3 -m pytest, got %v", cmd)
	}
}

// ---- TestDetect_Priority (go.mod before Cargo.toml) -------------------------

func TestDetect_Priority(t *testing.T) {
	// Both go.mod and Cargo.toml present — go.mod wins (higher priority).
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/foo\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte("[package]\n"), 0o644); err != nil {
		t.Fatalf("write Cargo.toml: %v", err)
	}
	r := New(dir, nil)
	cmd, err := r.Detect()
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if cmd[0] != "go" {
		t.Errorf("expected go to win over cargo, got %v", cmd)
	}
}

// ---- TestRun_EchoCommand ----------------------------------------------------

// TestRun_EchoCommand runs a real command (echo) and checks the result.
func TestRun_EchoCommand(t *testing.T) {
	dir := t.TempDir()

	done := make(chan *Result, 1)
	r := New(dir, func(res *Result) {
		done <- res
	})
	// Pre-set command so Detect() is skipped.
	r.cmd = []string{"echo", "hello world"}

	if err := r.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	select {
	case res := <-done:
		if res == nil {
			t.Fatal("got nil result")
		}
		if res.ExitCode != 0 {
			t.Errorf("ExitCode = %d; want 0", res.ExitCode)
		}
		if !res.Passed {
			t.Error("Passed should be true for exit code 0")
		}
		if res.Failed {
			t.Error("Failed should be false for exit code 0")
		}
		if len(res.Output) == 0 {
			t.Error("Output should not be empty")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// ---- TestRun_FailingCommand -------------------------------------------------

func TestRun_FailingCommand(t *testing.T) {
	dir := t.TempDir()

	done := make(chan *Result, 1)
	r := New(dir, func(res *Result) {
		done <- res
	})
	r.cmd = []string{"sh", "-c", "exit 1"}

	if err := r.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	select {
	case res := <-done:
		if res.ExitCode == 0 {
			t.Error("expected non-zero exit code")
		}
		if !res.Failed {
			t.Error("Failed should be true")
		}
		if res.Passed {
			t.Error("Passed should be false")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
	}
}

// ---- TestLastResult ---------------------------------------------------------

func TestLastResult(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)

	// Initially nil.
	if r.LastResult() != nil {
		t.Error("LastResult should be nil before any run")
	}

	done := make(chan struct{})
	r2 := New(dir, func(_ *Result) {
		close(done)
	})
	r2.cmd = []string{"true"}
	if err := r2.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("timeout")
	}

	if r2.LastResult() == nil {
		t.Error("LastResult should not be nil after a run")
	}
}

// ---- TestStop ---------------------------------------------------------------

func TestStop(t *testing.T) {
	dir := t.TempDir()
	r := New(dir, nil)
	r.Stop() // should not panic even if nothing is running
}

// ---- TestIsWatching ---------------------------------------------------------

func TestIsWatching(t *testing.T) {
	dir := t.TempDir()
	// Create a go.mod so Detect() succeeds.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module foo\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	r := New(dir, nil)
	if r.IsWatching() {
		t.Error("IsWatching should be false initially")
	}

	r.SetWatch(true)
	if !r.IsWatching() {
		t.Error("IsWatching should be true after SetWatch(true)")
	}

	r.SetWatch(false)
	if r.IsWatching() {
		t.Error("IsWatching should be false after SetWatch(false)")
	}
	r.Stop()
}

// ---- TestParseGoTestOutput --------------------------------------------------

func TestParseGoTestOutput(t *testing.T) {
	cases := []struct {
		name     string
		lines    []string
		exitCode int
		wantPass bool
		wantFail bool
		wantSumContains string
	}{
		{
			name: "all pass",
			lines: []string{
				"=== RUN   TestFoo",
				"--- PASS: TestFoo (0.001s)",
				"ok  \tcockpit/internal/store\t(0.260s)",
			},
			exitCode: 0,
			wantPass: true,
			wantFail: false,
			wantSumContains: "passed",
		},
		{
			name: "one fail",
			lines: []string{
				"--- PASS: TestFoo (0.001s)",
				"--- FAIL: TestBar (0.002s)",
				"FAIL\tcockpit/internal/store",
			},
			exitCode: 1,
			wantPass: false,
			wantFail: true,
			wantSumContains: "fail",
		},
		{
			name: "bare FAIL line",
			lines:    []string{"FAIL"},
			exitCode: 1,
			wantPass: false,
			wantFail: true,
			wantSumContains: "FAIL",
		},
		{
			name:     "empty no tests",
			lines:    []string{"ok  \texample.com/empty\t[no test files]"},
			exitCode: 0,
			wantPass: true,
			wantFail: false,
			wantSumContains: "ok",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			passed, failed, summary := parseGoTestOutput(tc.lines, tc.exitCode)
			if passed != tc.wantPass {
				t.Errorf("passed = %v; want %v", passed, tc.wantPass)
			}
			if failed != tc.wantFail {
				t.Errorf("failed = %v; want %v", failed, tc.wantFail)
			}
			if tc.wantSumContains != "" {
				found := false
				for i := 0; i < len(summary)-len(tc.wantSumContains)+1; i++ {
					if summary[i:i+len(tc.wantSumContains)] == tc.wantSumContains {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("summary %q does not contain %q", summary, tc.wantSumContains)
				}
			}
		})
	}
}

// ---- TestFileExists ---------------------------------------------------------

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile.txt")

	if fileExists(path) {
		t.Error("should not exist yet")
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileExists(path) {
		t.Error("should exist now")
	}
	// Directory should return false.
	if fileExists(dir) {
		t.Error("directory should return false")
	}
}

// ---- TestMakefileHasTestTarget ----------------------------------------------

func TestMakefileHasTestTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Makefile")

	// No test target.
	if err := os.WriteFile(path, []byte("all:\n\techo all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if makefileHasTestTarget(path) {
		t.Error("should not find test target")
	}

	// With test target.
	if err := os.WriteFile(path, []byte("all:\n\techo all\n\ntest:\n\tgo test ./...\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !makefileHasTestTarget(path) {
		t.Error("should find test target")
	}
}
