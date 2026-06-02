// Package store provides read-only readers for ~/.claude data files.
// It is the single source of truth for all dashboard data.
// It never makes network calls, never writes files, and tolerates
// partially written JSONL lines.
package store

import "time"

// Session represents a parsed ~/.claude session JSONL file summary.
type Session struct {
	SessionID   string
	ProjectPath string // decoded from the directory name (e.g. "-Users-foo-arc" → "/Users/foo/arc")
	FilePath    string // absolute path to the .jsonl file
	StartedAt   time.Time
	UpdatedAt   time.Time
	GitBranch   string
	Model       string // most recent model used
	TotalInputTokens   int64
	TotalOutputTokens  int64
	TotalCacheRead     int64
	TotalCacheCreation int64
	CostUSD      float64 // estimated, computed from token counts
	EditCount    int     // number of Edit/Write tool_use entries
	ErrorCount   int     // number of tool_result entries with is_error=true
	MessageCount int
}

// LiveProcess represents a ~/.claude/sessions/<pid>.json file.
type LiveProcess struct {
	PID        int
	SessionID  string
	CWD        string
	Status     string // "idle" or "busy"
	StartedAt  time.Time
	UpdatedAt  time.Time
	Version    string
	Kind       string
	Entrypoint string
}

// TaskItem represents a ~/.claude/tasks/<session>/<n>.json file.
type TaskItem struct {
	ID          string
	Subject     string
	Description string
	Status      string // "pending", "in_progress", "completed", "deleted"
	SessionID   string
}

// ProjectSummary represents one entry from ~/.claude.json → projects map.
type ProjectSummary struct {
	Path              string
	LastCost          float64
	LastSessionID     string
	LastLinesAdded    int
	LastLinesRemoved  int
	LastInputTokens   int64
	LastOutputTokens  int64
	LastCacheRead     int64
	LastCacheCreation int64
	// per-model breakdown
	ModelUsage map[string]ModelUsage
}

// ModelUsage holds per-model token usage and cost.
type ModelUsage struct {
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	CostUSD             float64
}

// HistoryEntry represents one line from ~/.claude/history.jsonl.
type HistoryEntry struct {
	Display   string
	Timestamp time.Time
	Project   string
	SessionID string
}

// SecLogEntry represents one line from ~/.claude/security/log.txt.
type SecLogEntry struct {
	Timestamp time.Time
	Raw       string
}

// EditRecord represents one Edit or Write tool_use extracted from a session JSONL.
type EditRecord struct {
	SessionID string
	Timestamp time.Time
	FilePath  string
	ToolName  string // "Edit", "Write", "MultiEdit"
}
