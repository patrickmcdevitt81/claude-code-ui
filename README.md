# Cockpit

A performance-optimized terminal cockpit for Claude Code — zero extra tokens,
full CLI power, mission-control productivity.

## Why

Running complex projects with Claude Code means juggling multiple agents,
sessions, and test runs. Cockpit gives you a single terminal UI that surfaces
all of this without adding any overhead: it reads Claude Code's local data files
directly and embeds the real `claude` binary — so token usage, billing, and
behavior are byte-for-byte identical to running `claude` directly.

## Install

```bash
cd ~/cockpit
go build -o ~/bin/cockpit ./cmd/cockpit
```

Make sure `~/bin` is in your PATH, then run:

```bash
cockpit
```

## Views

| Key | View | Description |
|-----|------|-------------|
| `1` | Dashboard | Token/cost summary, live agents, recent edits, issues |
| `2` | Agents | Launch, focus, kill parallel claude agents |
| `3` | Tests | Auto-detect and run tests, watch mode |
| `4` | Sessions | Browse all past sessions, resume any session |

## Key Bindings

| Key | Action |
|-----|--------|
| `q` / `ctrl+c` | Quit |
| `tab` | Next view |
| `?` | Toggle keybindings help |
| `n` | Launch new agent (Dashboard) |
| `r` | Refresh / run tests |
| `f` / `enter` | Focus agent (full PTY passthrough) |
| `K` | Kill selected agent |
| `L` | Launch agent in custom directory |
| `w` | Toggle test watch mode |
| `/` | Search sessions |

## Guarantees

- **Zero extra tokens**: Cockpit reads local files only. It never calls the Claude API.
- **Full CLI power**: The embedded agent IS the real `claude` binary — all features,
  all plugins, all hooks, identical billing.
- **Near-zero overhead**: Dashboard updates are event-driven via `fsnotify`. Idle
  CPU usage is effectively 0%.

## Architecture

```
cockpit/
  cmd/cockpit/        # Thin entrypoint: flag parsing, startup validation
  internal/store/     # Read-only readers for ~/.claude data (JSONL, JSON)
  internal/watch/     # fsnotify watcher → Bubble Tea DataChanged messages
  internal/agent/     # PTY manager: spawn/focus/kill real claude processes
  internal/runner/    # Test runner: auto-detect, run, watch mode
  internal/tui/       # Bubble Tea model, views, theme
  internal/build/     # Version constant
```

## Requirements

- Go 1.26+
- Claude Code CLI installed (`brew install claude-code` or equivalent)
