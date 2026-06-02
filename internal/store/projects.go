package store

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// rawClaudeDotJSON is the top-level structure of ~/.claude.json.
// We only extract the "projects" field; everything else is ignored.
type rawClaudeDotJSON struct {
	Projects map[string]json.RawMessage `json:"projects"`
}

// rawProjectEntry is the per-project value in ~/.claude.json.
type rawProjectEntry struct {
	LastCost                      float64                     `json:"lastCost"`
	LastSessionID                 string                      `json:"lastSessionId"`
	LastLinesAdded                int                         `json:"lastLinesAdded"`
	LastLinesRemoved              int                         `json:"lastLinesRemoved"`
	LastTotalInputTokens          int64                       `json:"lastTotalInputTokens"`
	LastTotalOutputTokens         int64                       `json:"lastTotalOutputTokens"`
	LastTotalCacheReadInputTokens int64                       `json:"lastTotalCacheReadInputTokens"`
	LastCacheCreationInputTokens  int64                       `json:"lastTotalCacheCreationInputTokens"`
	LastModelUsage                map[string]rawModelUsage    `json:"lastModelUsage"`
}

type rawModelUsage struct {
	InputTokens              int64   `json:"inputTokens"`
	OutputTokens             int64   `json:"outputTokens"`
	CacheReadInputTokens     int64   `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
	CostUSD                  float64 `json:"costUSD"`
}

// ReadProjects reads <parent of claudeDir>/.claude.json and returns a
// ProjectSummary slice. The file lives at ~/.claude.json, one level above ~/.claude/.
func ReadProjects(claudeDir string) ([]ProjectSummary, error) {
	dotFile := filepath.Join(filepath.Dir(claudeDir), ".claude.json")
	data, err := os.ReadFile(dotFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var top rawClaudeDotJSON
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, err
	}

	summaries := make([]ProjectSummary, 0, len(top.Projects))
	for path, raw := range top.Projects {
		var entry rawProjectEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			// Skip projects we can't parse.
			continue
		}

		usage := make(map[string]ModelUsage, len(entry.LastModelUsage))
		for model, mu := range entry.LastModelUsage {
			usage[model] = ModelUsage{
				InputTokens:         mu.InputTokens,
				OutputTokens:        mu.OutputTokens,
				CacheReadTokens:     mu.CacheReadInputTokens,
				CacheCreationTokens: mu.CacheCreationInputTokens,
				CostUSD:             mu.CostUSD,
			}
		}

		summaries = append(summaries, ProjectSummary{
			Path:              path,
			LastCost:          entry.LastCost,
			LastSessionID:     entry.LastSessionID,
			LastLinesAdded:    entry.LastLinesAdded,
			LastLinesRemoved:  entry.LastLinesRemoved,
			LastInputTokens:   entry.LastTotalInputTokens,
			LastOutputTokens:  entry.LastTotalOutputTokens,
			LastCacheRead:     entry.LastTotalCacheReadInputTokens,
			LastCacheCreation: entry.LastCacheCreationInputTokens,
			ModelUsage:        usage,
		})
	}
	return summaries, nil
}
