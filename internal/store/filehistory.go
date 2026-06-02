package store

import (
	"os"
	"path/filepath"
	"strings"
)

// ListFileHistoryDirs returns the session UUIDs that have a directory under
// <claudeDir>/file-history/. Individual snapshot files are not read.
func ListFileHistoryDirs(claudeDir string) ([]string, error) {
	base := filepath.Join(claudeDir, "file-history")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var uuids []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		uuids = append(uuids, name)
	}
	return uuids, nil
}
