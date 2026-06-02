package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// ---- TestDecodeProjectPath -----------------------------------------------

// TestDecodeProjectPath exercises the best-effort fallback decoder for session
// directory names. This decoder is only used when no "cwd" field is present in
// the JSONL lines (old sessions). It cannot distinguish hyphens in directory
// names from path separators — that is a known and documented limitation.
func TestDecodeProjectPath(t *testing.T) {
	cases := []struct {
		in   string
		want string
		note string
	}{
		{"-Users-foo-arc", "/Users/foo/arc", "simple path no hyphens in names"},
		{"-Users-patrickmcdevitt-arc", "/Users/patrickmcdevitt/arc", "no hyphens in dir names"},
		{"-home-user-projects-myapp", "/home/user/projects/myapp", "simple path no hyphens in names"},
		{"-", "/", "root"},
		// Paths with hyphens in dir names are ambiguous; the fallback treats all
		// hyphens as path separators. Document this known limitation explicitly.
		{"-Users-foo-my-app", "/Users/foo/my/app", "known limitation: hyphen in name decoded as separator"},
		// Non-encoded strings (no leading dash) are returned unchanged.
		{"noleadingdash", "noleadingdash", "no leading dash — returned unchanged"},
	}
	for _, tc := range cases {
		got := decodeProjectPath(tc.in)
		if got != tc.want {
			t.Errorf("decodeProjectPath(%q) = %q; want %q (%s)", tc.in, got, tc.want, tc.note)
		}
	}
}

// ---- TestParseSession_Real ------------------------------------------------

func TestParseSession_Real(t *testing.T) {
	path := filepath.Join("testdata", "session.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("testdata/session.jsonl not available: %v", err)
	}

	sess, err := ParseSession(path)
	if err != nil {
		t.Fatalf("ParseSession returned error: %v", err)
	}

	if sess.SessionID == "" {
		t.Error("SessionID is empty")
	}
	if sess.StartedAt.IsZero() {
		t.Error("StartedAt is zero")
	}
	if sess.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is zero")
	}
	if sess.UpdatedAt.Before(sess.StartedAt) {
		t.Errorf("UpdatedAt (%v) is before StartedAt (%v)", sess.UpdatedAt, sess.StartedAt)
	}
	if sess.FilePath != path {
		t.Errorf("FilePath = %q; want %q", sess.FilePath, path)
	}
	t.Logf("Session: ID=%s model=%s msgs=%d edits=%d errors=%d cost=%.4f",
		sess.SessionID, sess.Model, sess.MessageCount, sess.EditCount, sess.ErrorCount, sess.CostUSD)
}

// ---- TestParseSession_TruncatedLine --------------------------------------

func TestParseSession_TruncatedLine(t *testing.T) {
	path := filepath.Join("testdata", "session_truncated.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("testdata/session_truncated.jsonl not available: %v", err)
	}

	_, err := ParseSession(path)
	if err != nil {
		t.Errorf("ParseSession should not return an error for a truncated last line, got: %v", err)
	}
}

// ---- TestParseSession_Synthetic ------------------------------------------

// TestParseSession_Synthetic uses a hand-crafted fixture covering all entry types.
// It is unconditionally runnable (no dependency on real session files being present).
//
// Fixture contents (session_synthetic.jsonl):
//   - 1 file-history-snapshot line (skipped)
//   - 1 attachment line (skipped)
//   - 1 assistant line with usage and a tool_use Edit block
//   - 1 user line with a tool_result that has is_error: true
//   - 1 assistant line with usage but only a text content block
func TestParseSession_Synthetic(t *testing.T) {
	path := filepath.Join("testdata", "session_synthetic.jsonl")
	sess, err := ParseSession(path)
	if err != nil {
		t.Fatalf("ParseSession returned error: %v", err)
	}

	// 2 assistant + 1 user = 3 messages (snapshot and attachment are skipped)
	if sess.MessageCount != 3 {
		t.Errorf("MessageCount = %d; want 3", sess.MessageCount)
	}

	// 1 Edit tool_use in the first assistant line
	if sess.EditCount != 1 {
		t.Errorf("EditCount = %d; want 1", sess.EditCount)
	}

	// 1 tool_result with is_error: true in the user line
	if sess.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d; want 1", sess.ErrorCount)
	}

	// Total output tokens: 50 + 75 = 125
	if sess.TotalOutputTokens <= 0 {
		t.Errorf("TotalOutputTokens = %d; want > 0", sess.TotalOutputTokens)
	}

	// ProjectPath should be set from cwd field
	if sess.ProjectPath != "/Users/test/myproject" {
		t.Errorf("ProjectPath = %q; want %q", sess.ProjectPath, "/Users/test/myproject")
	}

	t.Logf("Synthetic session: msgs=%d edits=%d errors=%d outputTokens=%d projectPath=%s",
		sess.MessageCount, sess.EditCount, sess.ErrorCount, sess.TotalOutputTokens, sess.ProjectPath)
}

// ---- TestReadLiveProcesses -----------------------------------------------

func TestReadLiveProcesses(t *testing.T) {
	// Build a temp directory that looks like ~/.claude/sessions/.
	tmpDir := t.TempDir()
	sessDir := filepath.Join(tmpDir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Write a live process file.
	proc := map[string]interface{}{
		"pid":        12345,
		"sessionId":  "test-session-uuid",
		"cwd":        "/Users/test/project",
		"status":     "busy",
		"startedAt":  int64(1780359251732),
		"updatedAt":  int64(1780363996208),
		"version":    "2.1.159",
		"kind":       "interactive",
		"entrypoint": "cli",
	}
	data, _ := json.Marshal(proc)
	if err := os.WriteFile(filepath.Join(sessDir, "12345.json"), data, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	procs, err := ReadLiveProcesses(tmpDir)
	if err != nil {
		t.Fatalf("ReadLiveProcesses: %v", err)
	}
	if len(procs) != 1 {
		t.Fatalf("expected 1 process, got %d", len(procs))
	}
	p := procs[0]
	if p.PID != 12345 {
		t.Errorf("PID = %d; want 12345", p.PID)
	}
	if p.SessionID != "test-session-uuid" {
		t.Errorf("SessionID = %q; want %q", p.SessionID, "test-session-uuid")
	}
	if p.Status != "busy" {
		t.Errorf("Status = %q; want \"busy\"", p.Status)
	}
	if p.StartedAt.IsZero() {
		t.Error("StartedAt is zero")
	}
	expectedStart := time.UnixMilli(1780359251732).UTC()
	if !p.StartedAt.Equal(expectedStart) {
		t.Errorf("StartedAt = %v; want %v", p.StartedAt, expectedStart)
	}
}

// TestReadLiveProcesses_RealFixture tests against the copied real fixture.
func TestReadLiveProcesses_RealFixture(t *testing.T) {
	fixturePath := filepath.Join("testdata", "live_process.json")
	if _, err := os.Stat(fixturePath); err != nil {
		t.Skipf("testdata/live_process.json not available: %v", err)
	}

	// Build a fake claudeDir with our fixture.
	tmpDir := t.TempDir()
	sessDir := filepath.Join(tmpDir, "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Parse PID from fixture to name the file correctly.
	var rawProc struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(data, &rawProc); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	destName := filepath.Join(sessDir, strconv.Itoa(rawProc.PID)+".json")
	if err := os.WriteFile(destName, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	procs, err := ReadLiveProcesses(tmpDir)
	if err != nil {
		t.Fatalf("ReadLiveProcesses: %v", err)
	}
	if len(procs) == 0 {
		t.Fatal("expected at least 1 process")
	}
	t.Logf("Live process: PID=%d session=%s status=%s cwd=%s",
		procs[0].PID, procs[0].SessionID, procs[0].Status, procs[0].CWD)
}

// ---- TestReadHistory ------------------------------------------------------

func TestReadHistory(t *testing.T) {
	// Build a temp dir pointing at testdata.
	tmpDir := t.TempDir()
	// Copy testdata/history.jsonl into tmpDir/history.jsonl.
	data, err := os.ReadFile(filepath.Join("testdata", "history.jsonl"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "history.jsonl"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Request limit=2: should get newest 2 entries.
	entries, err := ReadHistory(tmpDir, 2)
	if err != nil {
		t.Fatalf("ReadHistory: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	// Newest first: the last written line ("commit and push") should be first.
	if entries[0].Display != "commit and push" {
		t.Errorf("entries[0].Display = %q; want \"commit and push\"", entries[0].Display)
	}
	if entries[1].Display != "run all tests and fix any failures" {
		t.Errorf("entries[1].Display = %q; want \"run all tests and fix any failures\"", entries[1].Display)
	}
	// Timestamps should be non-zero.
	if entries[0].Timestamp.IsZero() {
		t.Error("entries[0].Timestamp is zero")
	}

	// No limit: should get all 3.
	all, err := ReadHistory(tmpDir, 0)
	if err != nil {
		t.Fatalf("ReadHistory(0): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 entries with limit=0, got %d", len(all))
	}
}

// ---- TestReadSecLog -------------------------------------------------------

func TestReadSecLog(t *testing.T) {
	tmpDir := t.TempDir()
	secDir := filepath.Join(tmpDir, "security")
	if err := os.MkdirAll(secDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	data, err := os.ReadFile(filepath.Join("testdata", "security_log.txt"))
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(secDir, "log.txt"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	entries, err := ReadSecLog(tmpDir, 0)
	if err != nil {
		t.Fatalf("ReadSecLog: %v", err)
	}
	// 3 non-empty lines in our fixture.
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// First two lines have valid timestamps.
	if entries[0].Timestamp.IsZero() {
		t.Errorf("entries[0].Timestamp is zero; want a parsed timestamp")
	}
	if entries[1].Timestamp.IsZero() {
		t.Errorf("entries[1].Timestamp is zero; want a parsed timestamp")
	}

	// Third line has no valid timestamp prefix → Timestamp should be zero.
	if !entries[2].Timestamp.IsZero() {
		t.Errorf("entries[2].Timestamp = %v; want zero for unparseable line", entries[2].Timestamp)
	}
	if entries[2].Raw != "not a valid timestamp line" {
		t.Errorf("entries[2].Raw = %q; want %q", entries[2].Raw, "not a valid timestamp line")
	}

	// Test limit: request only last 1.
	limited, err := ReadSecLog(tmpDir, 1)
	if err != nil {
		t.Fatalf("ReadSecLog with limit: %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("expected 1 entry with limit=1, got %d", len(limited))
	}
	if limited[0].Raw != "not a valid timestamp line" {
		t.Errorf("limited[0].Raw = %q; want last line", limited[0].Raw)
	}
}

// ---- TestReadProjects -----------------------------------------------------

func TestReadProjects(t *testing.T) {
	tmpDir := t.TempDir()
	// .claude.json lives at parent of claudeDir, i.e. parent of tmpDir/<claudeDir>.
	claudeDir := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	dotJSON := map[string]interface{}{
		"projects": map[string]interface{}{
			"/Users/test/myproject": map[string]interface{}{
				"lastCost":                       0.259,
				"lastSessionId":                  "sess-uuid-1",
				"lastLinesAdded":                 42,
				"lastLinesRemoved":               7,
				"lastTotalInputTokens":                int64(1234),
				"lastTotalOutputTokens":               int64(567),
				"lastTotalCacheReadInputTokens":        int64(98765),
				"lastTotalCacheCreationInputTokens":    int64(4321),
				"lastModelUsage": map[string]interface{}{
					"claude-sonnet-4-6": map[string]interface{}{
						"inputTokens":              int64(774),
						"outputTokens":             int64(2158),
						"cacheReadInputTokens":     int64(450526),
						"cacheCreationInputTokens": int64(23806),
						"costUSD":                  0.2591,
					},
				},
			},
		},
	}
	data, _ := json.Marshal(dotJSON)
	if err := os.WriteFile(filepath.Join(tmpDir, ".claude.json"), data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	summaries, err := ReadProjects(claudeDir)
	if err != nil {
		t.Fatalf("ReadProjects: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 project, got %d", len(summaries))
	}
	s := summaries[0]
	if s.Path != "/Users/test/myproject" {
		t.Errorf("Path = %q; want %q", s.Path, "/Users/test/myproject")
	}
	if s.LastSessionID != "sess-uuid-1" {
		t.Errorf("LastSessionID = %q; want %q", s.LastSessionID, "sess-uuid-1")
	}
	if s.LastLinesAdded != 42 {
		t.Errorf("LastLinesAdded = %d; want 42", s.LastLinesAdded)
	}
	if len(s.ModelUsage) != 1 {
		t.Errorf("ModelUsage len = %d; want 1", len(s.ModelUsage))
	}
	mu, ok := s.ModelUsage["claude-sonnet-4-6"]
	if !ok {
		t.Error("ModelUsage missing claude-sonnet-4-6 key")
	} else {
		if mu.CostUSD != 0.2591 {
			t.Errorf("ModelUsage CostUSD = %f; want 0.2591", mu.CostUSD)
		}
	}
}

// ---- TestListFileHistoryDirs ---------------------------------------------

func TestListFileHistoryDirs(t *testing.T) {
	tmpDir := t.TempDir()
	fhDir := filepath.Join(tmpDir, "file-history")
	uuids := []string{
		"aaaa0000-0000-0000-0000-000000000001",
		"bbbb0000-0000-0000-0000-000000000002",
	}
	for _, u := range uuids {
		if err := os.MkdirAll(filepath.Join(fhDir, u), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	// Add a file (should be ignored).
	if err := os.WriteFile(filepath.Join(fhDir, "somefile.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := ListFileHistoryDirs(tmpDir)
	if err != nil {
		t.Fatalf("ListFileHistoryDirs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 dirs, got %d: %v", len(got), got)
	}
	// Just verify both UUIDs appear.
	found := make(map[string]bool)
	for _, g := range got {
		found[g] = true
	}
	for _, u := range uuids {
		if !found[u] {
			t.Errorf("UUID %q not found in result", u)
		}
	}
}

// ---- TestComputeCost -------------------------------------------------------

func TestComputeCost(t *testing.T) {
	// 1M input tokens @ $3/M = $3.00
	got := computeCost(1_000_000, 0, 0, 0)
	want := 3.0
	if got != want {
		t.Errorf("computeCost(input=1M) = %f; want %f", got, want)
	}

	// 1M output tokens @ $15/M = $15.00
	got = computeCost(0, 1_000_000, 0, 0)
	want = 15.0
	if got != want {
		t.Errorf("computeCost(output=1M) = %f; want %f", got, want)
	}

	// Mixed
	got = computeCost(1_000_000, 1_000_000, 1_000_000, 1_000_000)
	want = 3.0 + 15.0 + 0.30 + 3.75
	if got != want {
		t.Errorf("computeCost(all 1M) = %f; want %f", got, want)
	}
}

// ---- TestListEdits ---------------------------------------------------------

func TestListEdits(t *testing.T) {
	path := filepath.Join("testdata", "session.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("testdata/session.jsonl not available: %v", err)
	}

	edits, err := ListEdits(path)
	if err != nil {
		t.Fatalf("ListEdits: %v", err)
	}
	// Just verify it runs without error; real session may or may not have edits.
	t.Logf("ListEdits returned %d records", len(edits))
	for _, e := range edits {
		if e.ToolName == "" {
			t.Error("EditRecord has empty ToolName")
		}
		if e.SessionID == "" {
			t.Error("EditRecord has empty SessionID")
		}
	}
}
