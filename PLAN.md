# Cockpit — a performance-optimized terminal cockpit for Claude Code

## Context

The goal is a faster, more productive UI for Claude Code for use on ambitious, complex
projects — **without** sacrificing any of Claude Code's power, changing how usage/billing
works, or increasing token usage or latency.

The key realization from exploring `~/.claude/` is that **everything a dashboard needs
already lives on disk as plain JSON/JSONL** and can be read with **zero API calls**:

- `projects/<encoded-cwd>/<session>.jsonl` — full transcripts, one JSON object per line,
  with per-assistant-message `usage` (input/output/cache tokens) and `model`, plus every
  `Edit`/`Write` as a `tool_use` block and tool errors as `tool_result`/`toolUseResult`.
- `sessions/<pid>.json` — **live process status** (`status: idle|busy`, `cwd`, `updatedAt`).
- `tasks/<session>/<n>.json` — structured task/todo state per session.
- `.claude.json` → `projects[path]` — pre-aggregated `lastCost`, `lastModelUsage`,
  `lastTotal*Tokens`, `lastLinesAdded/Removed` (no parsing needed).
- `history.jsonl` — every prompt/slash command. `file-history/<session>/*@v1` — pre-edit
  file snapshots for diffs. `security/log.txt` — hook/error log.

Two design choices make the constraints provably satisfiable:

1. **The dashboard is 100% local file reads + an `fsnotify` watcher (no polling).** It never
   touches the model, so it adds **zero tokens** and near-zero CPU.
2. **We never reimplement the agent.** We spawn the *real* installed `claude` binary
   (`/opt/homebrew/bin/claude`, v2.1.159) inside a PTY. It IS Claude Code — so features,
   behavior, performance, and token/billing accounting are **byte-for-byte identical** to
   running `claude` directly. Cockpit is a frame *around* it, never in front of it.

**Stack:** Go 1.26 + Bubble Tea (TUI) / Lipgloss (style) / Bubbles (components),
`creack/pty` (spawn real claude), `fsnotify` (live updates). Compiles to a single static
binary `cockpit`, launched from the terminal. (Go chosen over Node/Ink for a lighter runtime
and single-binary distribution; Rust has no toolchain on this machine.)

**Prioritized capabilities** (from user): parallel multi-agent orchestration + a fast
test/iterate loop are the depth investments.

## Architecture

Single Go module `cockpit` with focused packages:

- `internal/store` — read-only readers for `~/.claude` (one file per source):
  - `sessions.go`: incremental/tail JSONL parser (cache by file offset+mtime; tolerate a
    half-written final line) → messages, per-msg token usage, model, edits, tool errors.
  - `live.go`: `sessions/*.json` (live status) + `tasks/*/*.json` (task state).
  - `projects.go`: `.claude.json` projects map (pre-aggregated cost/tokens/lines).
  - `history.go`, `filehistory.go`, `seclog.go`: commands, edit diffs, hook/error log.
- `internal/watch` — `fsnotify` over `projects/`, `sessions/`, `tasks/`; debounced events
  emitted as Bubble Tea messages. Lightweight ticker only as a fallback safety net.
- `internal/agent` — PTY manager. Each agent = one real `claude` process via `creack/pty`
  in a chosen cwd. Manages N concurrent agents. **Focus model = full passthrough**: when an
  agent is focused, Bubble Tea suspends and stdin/stdout pipe straight to its PTY, so
  claude's own TUI renders natively at full fidelity with zero emulation cost; unfocused
  agents are tracked via `sessions/*.json` status + a tail of recent output. (Optional later:
  vt buffer for a live unfocused thumbnail.)
- `internal/runner` — test runner. Auto-detect command (package.json `scripts.test`,
  `go test`, `pytest`, `Makefile`, `cargo test`), run on demand or on file-change; stream
  output to a pane in a goroutine; parse pass/fail summary for inline status.
- `internal/tui` — Bubble Tea model/update/view + Lipgloss theme + keybindings/help.

## Views

- **Dashboard** (default): token sparkline + total cost (from `usage`/`.claude.json`),
  active-agent indicators, recent edits, error/issue count, test status, recent sessions.
- **Agents / Mission Control**: list of running PTY agents with per-agent live status and
  token/cost meter (from that session's JSONL `usage`); launch new agent (pick cwd, optional
  git worktree for isolated parallel work), focus (passthrough), kill, switch. This is the
  parallel-orchestration centerpiece.
- **Tests**: runner output + history; `w` toggles watch-mode (auto-run on file change),
  green/red surfaced next to the agent that made the edit. The fast-iterate centerpiece.
- **Sessions**: browse/search all transcripts across projects; one-key **resume**
  (`claude --resume <id>` spawned as a new PTY agent); edit timeline with diffs; Issues panel
  aggregating tool errors + failed tests + security-log entries.

## Build sequence (incremental, each step runnable)

1. Scaffold Go module + `cmd/cockpit`; Bubble Tea shell; locate/validate `~/.claude` & the
   `claude` binary on startup.
2. `internal/store` readers with unit tests against real fixture files copied from `~/.claude`.
3. Dashboard view wired to `store` + `internal/watch` live updates (read-only, no agent yet).
4. `internal/agent`: spawn one real `claude` in a PTY with focus-passthrough; verify it's the
   genuine CLI.
5. Multi-agent mission control: N concurrent agents, status list, launch/focus/kill/switch.
6. `internal/runner`: test auto-detect + on-demand run + watch-mode auto-run, inline status.
7. Sessions browser + search + resume + edit-diff timeline + Issues panel; keybindings, help
   overlay, theme polish, and a performance pass (confirm idle CPU ≈ 0, large-transcript
   parsing stays incremental).

## Critical files

- `cmd/cockpit/main.go` — entrypoint, flag parsing, startup checks.
- `internal/store/{sessions,live,projects,history,filehistory,seclog}.go`
- `internal/watch/watch.go`
- `internal/agent/manager.go`, `internal/agent/pty.go`
- `internal/runner/runner.go`
- `internal/tui/{model,dashboard,agents,tests,sessions,theme,keys}.go`
- `go.mod` (deps: bubbletea, lipgloss, bubbles, creack/pty, fsnotify)
- `README.md` — install (`go build -o ~/bin/cockpit ./cmd/cockpit`) + keybindings.

## Reused / existing

This is greenfield (home dir, not a repo) — no existing code to reuse. It deliberately
**reuses the installed `claude` binary as-is** rather than reimplementing any agent logic,
and **reuses Claude Code's own on-disk data files** rather than instrumenting or duplicating
them. We will follow Bubble Tea's standard model/update/view conventions.

## Verification (end-to-end)

1. `go build -o ~/bin/cockpit ./cmd/cockpit` succeeds; `cockpit` launches a TUI in the
   terminal.
2. Dashboard populates from real `~/.claude` data (recent sessions, cost, edits, command
   history) on first paint, and **updates live** when an unrelated `claude` session writes —
   confirming the `fsnotify` path with zero polling.
3. Launch an agent from Mission Control → it runs the **real** claude (run a prompt; confirm a
   new `.jsonl` appears and its `usage` tokens match what claude reports — proving identical
   billing and that Cockpit adds none of its own).
4. Run two agents concurrently in different cwds; confirm independent status + per-agent token
   meters; focus/switch/kill all work.
5. Tests view auto-detects the project's test command, runs it, shows pass/fail; watch-mode
   re-runs on a file edit.
6. Sessions view: search transcripts, resume a past session (verify it continues the real
   session), view an edit diff from `file-history`.
7. Performance/cost checks: `unit tests for store/*` pass; idle CPU ≈ 0 (event-driven);
   Cockpit itself makes **no network calls** (only the embedded claude does) — verifiable
   because the binary imports no HTTP client. Token usage is therefore identical to using
   `claude` directly.
