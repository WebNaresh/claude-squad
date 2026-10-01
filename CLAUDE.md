# claude-squad (WebNaresh fork)

Personal fork of `smtg-ai/claude-squad`: a Go + tmux terminal UI that runs many Claude Code agents in one window. Being extended for a GitHub-issue → agent → single-PR workflow driven by `gai`.

- **What to build and why**: `docs/fork-requirements.md`. Read it before any feature work.
- **Hard limits**: terminal only (no Electron/web), uses the local logged-in `claude` CLI, PR/commit text via `gai` (local Ollama), never merge PRs.

## Code map

- `app/app.go` — TUI layout, key handling. `ui/` — list, preview, diff, tabbed window, overlays.
- `session/instance.go` — agent lifecycle (`Start`, statuses `Running`/`Ready`/`Paused`). `session/git/` — worktrees + diff. `session/tmux/` — tmux sessions.
- `daemon/daemon.go` — background loop (auto-yes today; issue polling goes here).
- `config/config.go`, `config/state.go` — saved settings and instance state.
- `ui/projecttabs.go` — project tab row. `session/external.go` — Claude sessions started outside cs (launcher: `scripts/claude-tmux`, symlinked as `~/.claude-squad/bin/claude`).

## Build / test

- Run: `cs` is `~/.local/bin/cs`, a launcher that rebuilds this repo into `~/.local/bin/cs-fork` on every start, then runs it. No manual install step. A failed build falls back to the last good binary; the error is in `~/.local/bin/cs-fork.build.log`. `claude-squad` is a symlink to the same launcher; the brew version is uninstalled. A running cs also updates itself: every 3s it checks the Go sources, rebuilds when they changed, and restarts in place from the main screen (`app/selfupdate.go`). No server or watcher process.
- Test: `go test ./...`
- Debugging what the user saw: read `~/.claude-squad/activity.log` (what cs did and why) and `~/.claude-squad/keys.log` (key timing). Both are privacy-safe (no typed text).
- Live tests: run a temp binary with `CLAUDE_SQUAD_HOME=<temp>` on a separate tmux socket (`tmux -L cstest`), and set up repos with `git -C` (never `cd`: the user's zsh starts gai-watch, which commits staged files). `claude agents` is NOT isolated: the test cs lists the user's real sessions. Before any destructive key (Enter on a view-only session = move, `y` on a confirm, `D`), check from the screen that the selected row is the test's own session. A test once moved the user's real session this way.

## Git

- `origin` = WebNaresh/claude-squad (push here), `upstream` = smtg-ai/claude-squad (pull fixes only, never push).
