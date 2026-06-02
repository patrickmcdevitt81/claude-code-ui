// Package tui provides the Bubble Tea model for Cockpit's terminal user interface.
package tui

import (
	"fmt"
	"sort"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"

	"cockpit/internal/agent"
	"cockpit/internal/runner"
	"cockpit/internal/store"
	"cockpit/internal/watch"
)

// viewName identifies which top-level screen is active.
type viewName int

const (
	viewDashboard viewName = iota
	viewAgents
	viewTests    // stub for Task 6
	viewSessions // stub for Task 7
	viewCount    // keep last — used for tab cycling modulus
)

// agentBlurMsg is sent when the user detaches from a focused agent (e.g. the
// PTY copy loop ends because the agent exited or stdin closed).
type agentBlurMsg struct{}

// Model is the top-level Bubble Tea model for the Cockpit TUI.
type Model struct {
	claudePath string
	claudeDir  string
	width      int
	height     int

	// active view
	view viewName

	// agent manager
	manager   *agent.Manager
	focusedID string // empty if no agent is currently focused

	// agents view state
	selectedAgentIdx int
	launchInput      textinput.Model // inline CWD input shown when L is pressed
	launching        bool            // true while the launch input is visible

	// tests view state
	runner          *runner.Runner
	runnerResult    *runner.Result   // nil until first run
	runHistory      []*runner.Result // last 5 results, newest first
	testCWD         string           // directory being tested
	testChangingDir bool             // true while the dir-change input is visible
	testDirInput    textinput.Model  // inline dir input shown when d is pressed
	tickerActive    bool             // true while the 1s poll ticker goroutine is running

	// dashboard data (refreshed on DataChanged)
	projects  []store.ProjectSummary
	sessions  []store.Session      // recent sessions, sorted newest-first, max 10
	processes []store.LiveProcess
	history   []store.HistoryEntry // last 20
	loadErr   error

	// sessions view state
	allSessions        []store.Session  // all sessions, capped at 200, sorted newest-first
	filteredSessions   []store.Session  // subset after search filter
	sessionSearch      string           // current search string
	sessionSearching   bool             // true while user is typing search
	sessionInput       textinput.Model  // textinput for search
	selectedSessionIdx int
	sessionEdits       []store.EditRecord // edits for the selected session (last 5)

	// help overlay
	showHelp bool
}

// New creates a new Cockpit TUI model with the given claude binary path, data
// dir, and agent manager.
func New(claudePath, claudeDir string, mgr *agent.Manager) Model {
	ti := textinput.New()
	ti.Placeholder = claudeDir
	ti.CharLimit = 256
	ti.Width = 40

	dirInput := textinput.New()
	dirInput.Placeholder = claudeDir
	dirInput.CharLimit = 256
	dirInput.Width = 50

	sessionIn := textinput.New()
	sessionIn.Placeholder = "filter sessions..."
	sessionIn.CharLimit = 128
	sessionIn.Width = 30

	m := Model{
		claudePath:   claudePath,
		claudeDir:    claudeDir,
		manager:      mgr,
		launchInput:  ti,
		testCWD:      claudeDir,
		testDirInput: dirInput,
		sessionInput: sessionIn,
	}

	// Create runner with testCWD. The onResult callback is a no-op here
	// because the poll tick will pick up the result on the next second.
	m.runner = runner.New(m.testCWD, func(_ *runner.Result) {
		// Result is stored in the runner; the 1s poll tick will retrieve it.
	})

	m.refreshData()
	return m
}

// refreshData reloads all dashboard data from disk.
// refreshData() is synchronous; on most machines ~/.claude reads take <10ms.
func (m *Model) refreshData() {
	var firstErr error

	projects, err := store.ReadProjects(m.claudeDir)
	if err != nil && firstErr == nil {
		firstErr = err
	}
	m.projects = projects

	sessions, err := store.ListSessions(m.claudeDir)
	if err != nil && firstErr == nil {
		firstErr = err
	}
	// Sort newest-first for both dashboard and sessions view.
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	// allSessions: cap at 200 to bound worst-case parse time on large histories.
	all := sessions
	if len(all) > 200 {
		all = all[:200]
	}
	m.allSessions = all
	m.filteredSessions = filterSessions(m.allSessions, m.sessionSearch)

	// sessions (dashboard): cap at 10 most-recent.
	dash := sessions
	if len(dash) > 10 {
		dash = dash[:10]
	}
	m.sessions = dash

	processes, err := store.ReadLiveProcesses(m.claudeDir)
	if err != nil && firstErr == nil {
		firstErr = err
	}
	m.processes = processes

	history, err := store.ReadHistory(m.claudeDir, 20)
	if err != nil && firstErr == nil {
		firstErr = err
	}
	m.history = history

	m.loadErr = firstErr

	// Reload edits for the currently selected session if any.
	m.reloadSessionEdits()
}

// reloadSessionEdits loads the last 5 edits for the currently selected session.
func (m *Model) reloadSessionEdits() {
	if m.selectedSessionIdx >= len(m.filteredSessions) {
		m.sessionEdits = nil
		return
	}
	s := m.filteredSessions[m.selectedSessionIdx]
	if s.FilePath == "" {
		m.sessionEdits = nil
		return
	}
	edits, err := store.ListEdits(s.FilePath)
	if err != nil {
		m.sessionEdits = nil
		return
	}
	// Keep last 5, newest first.
	if len(edits) > 5 {
		edits = edits[len(edits)-5:]
	}
	// Reverse so newest is first.
	for i, j := 0, len(edits)-1; i < j; i, j = i+1, j-1 {
		edits[i], edits[j] = edits[j], edits[i]
	}
	m.sessionEdits = edits
}

// focusAgent suspends Bubble Tea's raw mode and hands the terminal directly to
// the PTY owned by agent id. When the PTY copy loop finishes (agent exits or
// ctrl+d), an agentBlurMsg is sent so the TUI resumes normally.
func (m *Model) focusAgent(id string) tea.Cmd {
	a, ok := m.manager.Get(id)
	if !ok {
		return nil
	}
	ptmx := a.PTY()
	return tea.Exec(agent.NewPTYExec(ptmx), func(err error) tea.Msg {
		return agentBlurMsg{}
	})
}

// Init implements tea.Model. Loads initial data.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		prevView := m.view

		// Global: ? toggles help overlay (before any other dispatch).
		if msg.String() == "?" && !m.testChangingDir && !m.launching && !m.sessionSearching {
			m.showHelp = !m.showHelp
			return m, nil
		}
		// Global: esc closes help overlay if open.
		if m.showHelp && msg.String() == "esc" {
			m.showHelp = false
			return m, nil
		}
		// Swallow all keys while help is shown (except the ones above).
		if m.showHelp {
			return m, nil
		}

		// If the session search input is active, route keys there first.
		if m.sessionSearching {
			switch msg.String() {
			case "esc":
				m.sessionSearching = false
				m.sessionInput.Blur()
				m.sessionSearch = ""
				m.sessionInput.SetValue("")
				m.filteredSessions = filterSessions(m.allSessions, "")
				m.selectedSessionIdx = 0
				m.reloadSessionEdits() // refresh once on deactivate
			case "enter":
				m.sessionSearching = false
				m.sessionInput.Blur()
				m.reloadSessionEdits() // refresh once on commit
			default:
				var cmd tea.Cmd
				m.sessionInput, cmd = m.sessionInput.Update(msg)
				m.sessionSearch = m.sessionInput.Value()
				m.filteredSessions = filterSessions(m.allSessions, m.sessionSearch)
				m.selectedSessionIdx = 0
				// DO NOT call reloadSessionEdits() here — wait for commit or navigation
				return m, cmd
			}
			return m, nil
		}

		// If the test dir input is active, route keys there first.
		if m.testChangingDir {
			switch msg.String() {
			case "esc":
				m.testChangingDir = false
				m.testDirInput.Blur()
				m.testDirInput.SetValue("")
			case "enter":
				newDir := m.testDirInput.Value()
				if newDir == "" {
					newDir = m.claudeDir
				}
				m.testChangingDir = false
				m.testDirInput.Blur()
				m.testDirInput.SetValue("")
				m.testCWD = newDir
				// Replace runner with one pointed at the new directory.
				if m.runner != nil {
					m.runner.Stop()
				}
				m.runner = runner.New(newDir, func(_ *runner.Result) {})
				m.runnerResult = nil
				m.runHistory = nil
				// Allow the ticker to be restarted for the new runner.
				m.tickerActive = false
			default:
				var cmd tea.Cmd
				m.testDirInput, cmd = m.testDirInput.Update(msg)
				return m, cmd
			}
			return m, nil
		}

		// If the launch CWD input is active, route keys there first.
		if m.launching {
			switch msg.String() {
			case "esc":
				m.launching = false
				m.launchInput.Blur()
				m.launchInput.SetValue("")
			case "enter":
				cwd := m.launchInput.Value()
				if cwd == "" {
					cwd = m.claudeDir
				}
				m.launching = false
				m.launchInput.Blur()
				m.launchInput.SetValue("")
				if m.manager != nil {
					newAgent, err := m.manager.Launch(cwd, []string{})
					if err == nil {
						// Move selection to the new agent.
						agents := sortedAgents(m.manager.List())
						for i, a := range agents {
							if a.ID == newAgent.ID {
								m.selectedAgentIdx = i
								break
							}
						}
						// Focus the new agent immediately.
						select {
						case <-newAgent.Wait():
							// exited immediately; skip focus
						default:
							m.focusedID = newAgent.ID
							return m, m.focusAgent(newAgent.ID)
						}
					}
				}
			default:
				var cmd tea.Cmd
				m.launchInput, cmd = m.launchInput.Update(msg)
				return m, cmd
			}
			return m, nil
		}

		switch m.view {
		case viewAgents:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit

			case "up", "k":
				if m.selectedAgentIdx > 0 {
					m.selectedAgentIdx--
				}

			case "down", "j":
				// +1 for the [new] row; max valid index is len(agents)
				agentMax := 0
				if m.manager != nil {
					agentMax = len(m.manager.List())
				}
				if m.selectedAgentIdx < agentMax {
					m.selectedAgentIdx++
				}

			case "f", "enter":
				if m.manager != nil {
					agents := sortedAgents(m.manager.List())
					if m.selectedAgentIdx < len(agents) {
						id := agents[m.selectedAgentIdx].ID
						m.focusedID = id
						return m, m.focusAgent(id)
					}
				}

			case "K", "ctrl+k":
				// Kill selected agent (no confirmation).
				if m.manager != nil {
					agents := sortedAgents(m.manager.List())
					if m.selectedAgentIdx < len(agents) {
						id := agents[m.selectedAgentIdx].ID
						_ = m.manager.Kill(id)
						// Clamp selection to the new list length.
						remaining := sortedAgents(m.manager.List())
						if m.selectedAgentIdx >= len(remaining) && m.selectedAgentIdx > 0 {
							m.selectedAgentIdx = len(remaining) - 1
						}
					}
				}

			case "L":
				// Show the inline CWD launch prompt.
				m.launching = true
				m.launchInput.SetValue("")
				m.launchInput.Focus()

			case "tab":
				m.view = (m.view + 1) % viewCount

			case "1":
				m.view = viewDashboard
			case "2":
				m.view = viewAgents
			case "3":
				m.view = viewTests
			case "4":
				m.view = viewSessions
			}

		case viewTests:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit

			case "w":
				if m.runner != nil {
					m.runner.SetWatch(!m.runner.IsWatching())
				}

			case "r":
				if m.runner != nil {
					_ = m.runner.Run()
				}

			case "d":
				m.testChangingDir = true
				m.testDirInput.SetValue("")
				m.testDirInput.Focus()

			case "tab":
				m.view = (m.view + 1) % viewCount

			case "1":
				m.view = viewDashboard
			case "2":
				m.view = viewAgents
			case "3":
				m.view = viewTests
			case "4":
				m.view = viewSessions
			}

		case viewSessions:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit

			case "up", "k":
				n := len(m.filteredSessions)
				if n > 0 {
					m.selectedSessionIdx = (m.selectedSessionIdx - 1 + n) % n
					m.reloadSessionEdits()
				}

			case "down", "j":
				n := len(m.filteredSessions)
				if n > 0 {
					m.selectedSessionIdx = (m.selectedSessionIdx + 1) % n
					m.reloadSessionEdits()
				}

			case "/":
				m.sessionSearching = true
				m.sessionInput.SetValue("")
				m.sessionInput.Focus()

			case "enter":
				// Resume the selected session by launching a new PTY agent with --resume.
				if m.manager != nil && m.selectedSessionIdx < len(m.filteredSessions) {
					s := m.filteredSessions[m.selectedSessionIdx]
					newAgent, err := m.manager.Launch(s.ProjectPath, []string{"--resume", s.SessionID})
					if err != nil {
						m.loadErr = fmt.Errorf("resume failed: %w", err)
					} else {
						select {
						case <-newAgent.Wait():
							// exited immediately — the session ID may be invalid
							m.loadErr = fmt.Errorf("session %s could not be resumed (claude exited immediately)", s.SessionID[:8])
						default:
							m.focusedID = newAgent.ID
							m.view = viewAgents
							return m, m.focusAgent(newAgent.ID)
						}
					}
				}

			case "tab":
				m.view = (m.view + 1) % viewCount

			case "1":
				m.view = viewDashboard
			case "2":
				m.view = viewAgents
			case "3":
				m.view = viewTests
			case "4":
				m.view = viewSessions
			}

		default: // viewDashboard (and stubs)
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit

			case "r":
				m.refreshData()

			case "tab":
				// Cycle: dashboard → agents → tests → sessions → dashboard
				m.view = (m.view + 1) % viewCount

			case "1":
				m.view = viewDashboard
			case "2", "a":
				m.view = viewAgents
			case "3":
				m.view = viewTests
			case "4":
				m.view = viewSessions

			case "n":
				// Launch a new claude agent in the claude directory and immediately
				// focus it — full PTY passthrough to the real claude CLI.
				if m.manager != nil {
					newAgent, err := m.manager.Launch(m.claudeDir, []string{})
					if err == nil {
						select {
						case <-newAgent.Wait():
							// Process exited immediately; don't attempt to focus.
						default:
							m.focusedID = newAgent.ID
							// Switch to agents view so the user can see it.
							m.view = viewAgents
							return m, m.focusAgent(newAgent.ID)
						}
					}
				}
			}
		}
		// If we just transitioned to the tests view, start the poll ticker
		// (guard against duplicate tickers on rapid tab switching).
		if m.view == viewTests && prevView != viewTests && !m.tickerActive {
			m.tickerActive = true
			return m, doTestPoll()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case watch.DataChanged:
		m.refreshData()
		if m.manager != nil {
			m.manager.Cleanup()
		}

	case agentBlurMsg:
		// The user detached from the focused agent — resume normal TUI rendering.
		m.focusedID = ""

	case testPollMsg:
		// Check whether the runner has a new result.
		if m.runner != nil {
			latest := m.runner.LastResult()
			if resultChanged(m.runnerResult, latest) && latest != nil {
				// Prepend to history (newest first), cap at 5.
				m.runHistory = append([]*runner.Result{latest}, m.runHistory...)
				if len(m.runHistory) > 5 {
					m.runHistory = m.runHistory[:5]
				}
				m.runnerResult = latest
			}
		}
		// Continue polling if on the tests view or watch is active.
		if m.view == viewTests || (m.runner != nil && m.runner.IsWatching()) {
			// tickerActive stays true
			return m, doTestPoll()
		}
		// Ticker is stopping; clear the flag so a future transition can restart it.
		m.tickerActive = false
		return m, nil
	}

	return m, nil
}

// renderCurrentView returns the base view without any overlay.
func (m Model) renderCurrentView() string {
	switch m.view {
	case viewAgents:
		return renderAgents(m)
	case viewTests:
		return renderTests(m)
	case viewSessions:
		return renderSessions(m)
	default:
		return m.renderDashboard()
	}
}

// View implements tea.Model.
func (m Model) View() string {
	base := m.renderCurrentView()
	if m.showHelp {
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			renderHelp(m.width),
			lipgloss.WithWhitespaceChars(" "),
		)
	}
	return base
}
