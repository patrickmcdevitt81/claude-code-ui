package agent

import "sync"

// Manager owns a set of Agent instances and provides thread-safe access.
type Manager struct {
	claudePath string
	claudeDir  string
	agents     map[string]*Agent // keyed by Agent.ID
	mu         sync.RWMutex
}

// NewManager creates a Manager that launches agents using claudePath and polls
// claudeDir/sessions/*.json to discover session IDs.
func NewManager(claudePath, claudeDir string) *Manager {
	return &Manager{
		claudePath: claudePath,
		claudeDir:  claudeDir,
		agents:     make(map[string]*Agent),
	}
}

// Launch starts a new agent in cwd with optional extra args, registers it, and
// returns the new Agent.
func (m *Manager) Launch(cwd string, args []string) (*Agent, error) {
	a, err := Start(m.claudePath, cwd, args, m.claudeDir)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.agents[a.ID] = a
	m.mu.Unlock()

	return a, nil
}

// List returns a snapshot of all tracked agents (both running and recently
// exited).
func (m *Manager) List() []*Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Agent, 0, len(m.agents))
	for _, a := range m.agents {
		result = append(result, a)
	}
	return result
}

// Get returns a single agent by ID.
func (m *Manager) Get(id string) (*Agent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.agents[id]
	return a, ok
}

// Kill terminates the agent with the given ID.
func (m *Manager) Kill(id string) error {
	m.mu.RLock()
	a, ok := m.agents[id]
	m.mu.RUnlock()
	if !ok {
		return nil
	}
	return a.Kill()
}

// Cleanup removes agents whose process has already exited (done channel
// closed). Call opportunistically — not on a timer.
func (m *Manager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, a := range m.agents {
		select {
		case <-a.done:
			delete(m.agents, id)
		default:
		}
	}
}
