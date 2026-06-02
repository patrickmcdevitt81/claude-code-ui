package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// rawLiveProcess is the JSON structure of a ~/.claude/sessions/<pid>.json file.
type rawLiveProcess struct {
	PID        int    `json:"pid"`
	SessionID  string `json:"sessionId"`
	CWD        string `json:"cwd"`
	Status     string `json:"status"`
	StartedAt  int64  `json:"startedAt"`  // Unix milliseconds
	UpdatedAt  int64  `json:"updatedAt"`  // Unix milliseconds
	Version    string `json:"version"`
	Kind       string `json:"kind"`
	Entrypoint string `json:"entrypoint"`
}

// ReadLiveProcesses globs <claudeDir>/sessions/*.json and returns a LiveProcess
// for each file that can be successfully parsed.
func ReadLiveProcesses(claudeDir string) ([]LiveProcess, error) {
	sessDir := filepath.Join(claudeDir, "sessions")
	entries, err := os.ReadDir(sessDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var procs []LiveProcess
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		if strings.HasPrefix(name, ".") {
			continue
		}

		filePath := filepath.Join(sessDir, name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var raw rawLiveProcess
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}

		// If PID was not in JSON, try to parse it from the filename.
		pid := raw.PID
		if pid == 0 {
			stem := strings.TrimSuffix(name, ".json")
			if n, err := strconv.Atoi(stem); err == nil {
				pid = n
			}
		}

		procs = append(procs, LiveProcess{
			PID:        pid,
			SessionID:  raw.SessionID,
			CWD:        raw.CWD,
			Status:     raw.Status,
			StartedAt:  time.UnixMilli(raw.StartedAt).UTC(),
			UpdatedAt:  time.UnixMilli(raw.UpdatedAt).UTC(),
			Version:    raw.Version,
			Kind:       raw.Kind,
			Entrypoint: raw.Entrypoint,
		})
	}
	return procs, nil
}

// rawTaskItem is the JSON structure of a ~/.claude/tasks/<session>/<n>.json file.
type rawTaskItem struct {
	ID          string `json:"id"`
	Subject     string `json:"subject"`
	Description string `json:"description"`
	Status      string `json:"status"`
}

// ReadTaskItems globs <claudeDir>/tasks/*/*.json (skipping .lock files) and
// returns a TaskItem for each parseable file. The session ID is derived from
// the parent directory name.
func ReadTaskItems(claudeDir string) ([]TaskItem, error) {
	tasksDir := filepath.Join(claudeDir, "tasks")
	sessionDirs, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var items []TaskItem
	for _, sessionEntry := range sessionDirs {
		if !sessionEntry.IsDir() {
			continue
		}
		sessionID := sessionEntry.Name()
		if strings.HasPrefix(sessionID, ".") {
			continue
		}

		sessionDir := filepath.Join(tasksDir, sessionID)
		taskFiles, err := os.ReadDir(sessionDir)
		if err != nil {
			continue
		}

		for _, tf := range taskFiles {
			if tf.IsDir() {
				continue
			}
			name := tf.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			if strings.HasSuffix(name, ".lock") {
				continue
			}
			if !strings.HasSuffix(name, ".json") {
				continue
			}

			data, err := os.ReadFile(filepath.Join(sessionDir, name))
			if err != nil {
				continue
			}

			var raw rawTaskItem
			if err := json.Unmarshal(data, &raw); err != nil {
				continue
			}

			items = append(items, TaskItem{
				ID:          raw.ID,
				Subject:     raw.Subject,
				Description: raw.Description,
				Status:      raw.Status,
				SessionID:   sessionID,
			})
		}
	}
	return items, nil
}
