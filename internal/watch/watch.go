// Package watch monitors ~/.claude subdirectories for file changes
// and emits Bubble Tea messages when data changes.
// All live updates are event-driven via fsnotify. The 30s ticker is a safety net only.
package watch

import (
	"log"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fsnotify/fsnotify"
)

// DataChanged is the Bubble Tea message emitted when watched files change.
type DataChanged struct{}

// Watcher monitors ~/.claude for changes and emits DataChanged messages.
type Watcher struct {
	claudeDir string
	watcher   *fsnotify.Watcher
	program   *tea.Program
}

// New creates a Watcher that monitors claudeDir for file changes.
// It returns an error if the underlying fsnotify watcher cannot be created.
func New(claudeDir string, program *tea.Program) (*Watcher, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		claudeDir: claudeDir,
		watcher:   fw,
		program:   program,
	}

	// Watch subdirectories that exist; skip silently if absent.
	for _, sub := range []string{"projects", "sessions", "tasks"} {
		dir := filepath.Join(claudeDir, sub)
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			if err := fw.Add(dir); err != nil {
				log.Printf("cockpit/watch: cannot watch %s: %v", dir, err)
			}
		}
	}

	return w, nil
}

// Start runs the watch loop in a goroutine. It should be called via `go w.Start()`.
// It returns when Close() is called or the watcher errors out.
//
// Debounce: events within a 200ms window are collapsed into a single DataChanged.
// Safety net: a DataChanged is emitted every 30 seconds regardless of file events.
func (w *Watcher) Start() {
	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}
	pending := false

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			// Suppress events we don't care about (Chmod-only).
			if event.Op == fsnotify.Chmod {
				continue
			}
			// Reset the debounce timer.
			if pending {
				if !debounce.Stop() {
					select {
					case <-debounce.C:
					default:
					}
				}
			}
			debounce.Reset(200 * time.Millisecond)
			pending = true

		case <-debounce.C:
			pending = false
			w.program.Send(DataChanged{})

		case <-ticker.C:
			// safety net: emit DataChanged every 30s in case fsnotify misses events on macOS APFS
			w.program.Send(DataChanged{})

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("cockpit/watch: fsnotify error: %v", err)
		}
	}
}

// Close stops the watcher and releases its resources.
func (w *Watcher) Close() error {
	return w.watcher.Close()
}
