package store

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Token cost rates (per million tokens, approximate Anthropic pricing).
const (
	costPerMInput         = 3.00
	costPerMOutput        = 15.00
	costPerMCacheRead     = 0.30
	costPerMCacheCreation = 3.75
)

// editToolNames is the set of tool names that count as file edits.
var editToolNames = map[string]bool{
	"Edit":      true,
	"Write":     true,
	"MultiEdit": true,
}

// rawLine is the minimal structure parsed from each JSONL line.
type rawLine struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	SessionID string          `json:"sessionId"`
	GitBranch string          `json:"gitBranch"`
	CWD       string          `json:"cwd"`
	Message   json.RawMessage `json:"message"`
	Content   json.RawMessage `json:"content"` // for user lines
}

// rawMessage is the assistant message object.
type rawMessage struct {
	Model   string          `json:"model"`
	Usage   rawUsage        `json:"usage"`
	Content json.RawMessage `json:"content"`
}

type rawUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// rawContentItem is one element of a content array.
type rawContentItem struct {
	Type    string          `json:"type"`
	Name    string          `json:"name"`    // for tool_use
	Input   json.RawMessage `json:"input"`   // for tool_use
	IsError bool            `json:"is_error"` // for tool_result
}

// rawToolInput covers the common file_path field used by Edit/Write/MultiEdit.
type rawToolInput struct {
	FilePath string `json:"file_path"`
	// MultiEdit uses an array — grab first element's file_path
	Edits []struct {
		FilePath string `json:"file_path"`
	} `json:"edits"`
}

// decodeProjectPath converts a Claude project directory name (e.g. "-Users-foo-arc")
// to an approximate absolute path (e.g. "/Users/foo/arc").
//
// The encoding convention: the leading "/" of the absolute path becomes a leading "-",
// and interior "/" path separators become "-". This means the encoding is ambiguous
// for paths that contain hyphens in directory names (e.g. "-Users-foo-my-app" could
// be "/Users/foo/my/app" or "/Users/foo/my-app"). This function is a best-effort
// fallback only; prefer using the "cwd" field from JSONL lines when available.
//
// If dir does not start with "-", it is returned unchanged.
func decodeProjectPath(dir string) string {
	if !strings.HasPrefix(dir, "-") {
		return dir
	}
	// Strip the leading "-" and replace it with "/", then replace remaining "-" with "/".
	// NOTE: This is still wrong for hyphenated directory names, but is no worse than before.
	return "/" + strings.ReplaceAll(dir[1:], "-", "/")
}

// newLineScanner returns a bufio.Scanner configured with a generous buffer for
// JSONL files. Session lines can be large (thinking fields, tool results, etc.).
func newLineScanner(r io.Reader) *bufio.Scanner {
	s := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	s.Buffer(buf, 16<<20)
	return s
}

// computeCost returns the estimated cost in USD given token counts.
func computeCost(input, output, cacheRead, cacheCreation int64) float64 {
	return float64(input)*costPerMInput/1e6 +
		float64(output)*costPerMOutput/1e6 +
		float64(cacheRead)*costPerMCacheRead/1e6 +
		float64(cacheCreation)*costPerMCacheCreation/1e6
}

// ParseSession reads the entire JSONL file at filePath and returns a summarized Session.
// Truncated final lines are silently skipped.
func ParseSession(filePath string) (Session, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return Session{}, err
	}
	defer f.Close()

	sess := Session{
		FilePath: filePath,
	}

	// Derive SessionID from file path.
	// Path pattern: <claudeDir>/projects/<encoded-project-dir>/<sessionid>.jsonl
	base := filepath.Base(filePath) // "<sessionid>.jsonl"
	sess.SessionID = strings.TrimSuffix(base, ".jsonl")

	// ProjectPath will be set from the authoritative "cwd" field in JSONL lines.
	// Fall back to decoding the directory name only if no cwd is seen.
	dir := filepath.Base(filepath.Dir(filePath)) // "<encoded-project-dir>"

	scanner := newLineScanner(f)

	var firstCWD string // authoritative project path from first JSONL line with cwd

	for scanner.Scan() {
		rawBytes := scanner.Bytes()
		// TrimSpace without allocation: check manually.
		if len(rawBytes) == 0 {
			continue
		}

		var rl rawLine
		if err := json.Unmarshal(rawBytes, &rl); err != nil {
			// Truncated or malformed line — skip silently (last line may be truncated).
			continue
		}

		// Capture first non-empty cwd as the authoritative project path.
		if firstCWD == "" && rl.CWD != "" {
			firstCWD = rl.CWD
		}

		// Skip non-data lines.
		if rl.Type == "file-history-snapshot" || rl.Type == "attachment" {
			continue
		}

		// Parse timestamp.
		if rl.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339Nano, rl.Timestamp); err == nil {
				if sess.StartedAt.IsZero() || t.Before(sess.StartedAt) {
					sess.StartedAt = t
				}
				if t.After(sess.UpdatedAt) {
					sess.UpdatedAt = t
				}
			}
		}

		// Track git branch.
		if rl.GitBranch != "" {
			sess.GitBranch = rl.GitBranch
		}

		// Set SessionID from data if not yet set.
		if sess.SessionID == "" && rl.SessionID != "" {
			sess.SessionID = rl.SessionID
		}

		switch rl.Type {
		case "assistant":
			sess.MessageCount++
			if len(rl.Message) == 0 {
				continue
			}
			var msg rawMessage
			if err := json.Unmarshal(rl.Message, &msg); err != nil {
				continue
			}
			if msg.Model != "" {
				sess.Model = msg.Model
			}
			sess.TotalInputTokens += msg.Usage.InputTokens
			sess.TotalOutputTokens += msg.Usage.OutputTokens
			sess.TotalCacheRead += msg.Usage.CacheReadInputTokens
			sess.TotalCacheCreation += msg.Usage.CacheCreationInputTokens

			// Scan content for tool_use items.
			if len(msg.Content) == 0 {
				continue
			}
			var items []rawContentItem
			if err := json.Unmarshal(msg.Content, &items); err != nil {
				continue
			}
			for _, item := range items {
				if item.Type == "tool_use" && editToolNames[item.Name] {
					sess.EditCount++
				}
			}

		case "user":
			sess.MessageCount++
			if len(rl.Content) == 0 {
				continue
			}
			var items []rawContentItem
			if err := json.Unmarshal(rl.Content, &items); err != nil {
				continue
			}
			for _, item := range items {
				if item.Type == "tool_result" && item.IsError {
					sess.ErrorCount++
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Session{}, err
	}

	// Use authoritative cwd if seen; fall back to best-effort directory decoding.
	if firstCWD != "" {
		sess.ProjectPath = firstCWD
	} else {
		sess.ProjectPath = decodeProjectPath(dir)
	}

	sess.CostUSD = computeCost(
		sess.TotalInputTokens,
		sess.TotalOutputTokens,
		sess.TotalCacheRead,
		sess.TotalCacheCreation,
	)

	return sess, nil
}

// ListSessions walks <claudeDir>/projects/ for all .jsonl files,
// calls ParseSession on each, and returns all results.
// Hidden files and lock files are ignored.
func ListSessions(claudeDir string) ([]Session, error) {
	projectsDir := filepath.Join(claudeDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []Session
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		subDir := filepath.Join(projectsDir, name)
		subEntries, err := os.ReadDir(subDir)
		if err != nil {
			continue
		}
		for _, sub := range subEntries {
			if sub.IsDir() {
				continue
			}
			fname := sub.Name()
			if strings.HasPrefix(fname, ".") {
				continue
			}
			if strings.HasSuffix(fname, ".lock") {
				continue
			}
			if !strings.HasSuffix(fname, ".jsonl") {
				continue
			}
			filePath := filepath.Join(subDir, fname)
			sess, err := ParseSession(filePath)
			if err != nil {
				// Skip files we can't parse.
				continue
			}
			sessions = append(sessions, sess)
		}
	}
	return sessions, nil
}

// ListEdits returns the individual EditRecord entries from a session JSONL file.
// Used by the Sessions/diff view.
func ListEdits(filePath string) ([]EditRecord, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sessionID := strings.TrimSuffix(filepath.Base(filePath), ".jsonl")

	scanner := newLineScanner(f)

	var edits []EditRecord
	for scanner.Scan() {
		rawBytes := scanner.Bytes()
		if len(rawBytes) == 0 {
			continue
		}

		var rl rawLine
		if err := json.Unmarshal(rawBytes, &rl); err != nil {
			// Truncated or malformed line — skip silently.
			continue
		}

		if rl.Type != "assistant" {
			continue
		}
		if len(rl.Message) == 0 {
			continue
		}

		var msg rawMessage
		if err := json.Unmarshal(rl.Message, &msg); err != nil {
			continue
		}
		if len(msg.Content) == 0 {
			continue
		}

		var ts time.Time
		if rl.Timestamp != "" {
			ts, _ = time.Parse(time.RFC3339Nano, rl.Timestamp)
		}

		var items []rawContentItem
		if err := json.Unmarshal(msg.Content, &items); err != nil {
			continue
		}
		for _, item := range items {
			if item.Type != "tool_use" || !editToolNames[item.Name] {
				continue
			}
			var inp rawToolInput
			fp := ""
			if len(item.Input) > 0 {
				if err := json.Unmarshal(item.Input, &inp); err == nil {
					fp = inp.FilePath
					if fp == "" && len(inp.Edits) > 0 {
						fp = inp.Edits[0].FilePath
					}
				}
			}
			edits = append(edits, EditRecord{
				SessionID: sessionID,
				Timestamp: ts,
				FilePath:  fp,
				ToolName:  item.Name,
			})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return edits, nil
}
