package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// rawHistoryEntry is the per-line JSON structure of ~/.claude/history.jsonl.
type rawHistoryEntry struct {
	Display   string `json:"display"`
	Timestamp int64  `json:"timestamp"` // Unix milliseconds
	Project   string `json:"project"`
	SessionID string `json:"sessionId"`
}

// ReadHistory reads the last limit lines from <claudeDir>/history.jsonl and
// returns them in reverse-chronological order (newest first).
// If limit <= 0 all entries are returned.
//
// Uses a fixed-size ring buffer when limit > 0 to avoid accumulating the entire
// file in memory — only the last `limit` entries are kept during the scan.
func ReadHistory(claudeDir string, limit int) ([]HistoryEntry, error) {
	path := filepath.Join(claudeDir, "history.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	scanner := newLineScanner(f)

	if limit <= 0 {
		// No cap: collect all entries, then reverse.
		var all []HistoryEntry
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}
			var raw rawHistoryEntry
			if err := json.Unmarshal(line, &raw); err != nil {
				continue
			}
			all = append(all, HistoryEntry{
				Display:   raw.Display,
				Timestamp: time.UnixMilli(raw.Timestamp).UTC(),
				Project:   raw.Project,
				SessionID: raw.SessionID,
			})
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		// Reverse to get newest-first order.
		for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
			all[i], all[j] = all[j], all[i]
		}
		return all, nil
	}

	// Ring buffer: keep only the last `limit` entries in O(1) extra space.
	ring := make([]HistoryEntry, limit)
	var count int // total entries seen
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var raw rawHistoryEntry
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		ring[count%limit] = HistoryEntry{
			Display:   raw.Display,
			Timestamp: time.UnixMilli(raw.Timestamp).UTC(),
			Project:   raw.Project,
			SessionID: raw.SessionID,
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if count == 0 {
		return nil, nil
	}

	// Reconstruct the last min(count, limit) entries in chronological order,
	// then reverse to get newest-first.
	n := count
	if n > limit {
		n = limit
	}
	result := make([]HistoryEntry, n)
	start := count % limit // index of oldest entry in ring (when count >= limit)
	if count < limit {
		start = 0
	}
	for i := 0; i < n; i++ {
		result[i] = ring[(start+i)%limit]
	}
	// Reverse to newest-first.
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] = result[j], result[i]
	}
	return result, nil
}
