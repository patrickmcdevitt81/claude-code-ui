package store

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

const secLogTimestampLayout = "[2006-01-02 15:04:05"

// ReadSecLog reads the last limit lines from <claudeDir>/security/log.txt.
// Each line is expected to start with a timestamp like "[2026-06-01 20:21:15]".
// If the timestamp cannot be parsed, Timestamp is set to zero and Raw holds
// the full line. If limit <= 0, all entries are returned.
func ReadSecLog(claudeDir string, limit int) ([]SecLogEntry, error) {
	path := filepath.Join(claudeDir, "security", "log.txt")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var all []SecLogEntry
	scanner := newLineScanner(f)

	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		entry := parseSecLogLine(line)
		all = append(all, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Return the last N entries (keep chronological order, just trim head).
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, nil
}

// parseSecLogLine parses a single security log line.
// Expected prefix: "[2026-06-01 20:21:15.123]" or "[2026-06-01 20:21:15]"
func parseSecLogLine(line string) SecLogEntry {
	entry := SecLogEntry{Raw: line}
	if !strings.HasPrefix(line, "[") {
		return entry
	}
	// Find the closing bracket.
	end := strings.IndexByte(line, ']')
	if end < 0 {
		return entry
	}
	// Timestamp portion is everything between '[' and ']'.
	tsPart := line[1:end]

	// Strip sub-second precision if present (e.g. "2026-06-01 20:21:15.231" → "2026-06-01 20:21:15").
	if dotIdx := strings.LastIndexByte(tsPart, '.'); dotIdx >= 0 {
		tsPart = tsPart[:dotIdx]
	}

	t, err := time.Parse("2006-01-02 15:04:05", tsPart)
	if err != nil {
		return entry
	}
	entry.Timestamp = t
	return entry
}
