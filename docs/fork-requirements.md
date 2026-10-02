# Fork requirements (WebNaresh/claude-squad)

What this fork must do on top of upstream `smtg-ai/claude-squad`, and why.
Source: the user's own description of their workflow (2026-10-01).

## The problem today

- The user works GitHub issues across several projects (e.g. `practice-stack` ~79 open, `basic` ~75 open).
- Each issue is started by hand: open a VS Code terminal, run `gai issue <n>`, which attaches the issue to the open PR and launches a Claude session seeded with the issue thread.
- VS Code's Claude panel caps them at ~5 parallel agents and two VS Code windows cost ~2.5 GB RAM.
- No single view shows: which agents are running per project, what each one changed, and which one is waiting for an answer.

## Hard constraints

| Constraint | Meaning for the code |
|---|---|
| Lightweight, fast | Terminal UI only (Go + tmux). **No Electron, no web UI, no browser.** A new agent must start in about a second. |
| Free, user's own Claude subscription | Agents run the local `claude` CLI that is already logged in. No API keys, no paid services. |
| Local AI for git/PR text | PR title, body and commit messages come from `gai` (local Ollama, `qwen2.5-coder:1.5b`). Never call Claude for that. |
| Never merge | The tool may create and update PRs. **Only the user merges.** |
| Open source, editable | Stay on this fork; pull upstream fixes from the `upstream` remote. |

## Features

### 1. Project tabs (top row)
- Done: one tab per opened project across the top, like editor tabs (`ui/projecttabs.go`). `←`/`→` or a click switches; the list shows only that project's agents, and `n` starts agents there. Each tab shows its agent count.
- Done: `+ add project` (top right, click or `+`) opens the macOS folder chooser. The user picks only the folders they work on (e.g. practice-stack, basic, glitchgrab), not everything in `~/coding-line`.
- Done: `x` closes a tab, blocked while the project still has agents so none is hidden.
- Done: `cs` starts from any folder. Inside a repo it opens that repo as a tab; elsewhere it reopens the last active tab, and asks for a folder only the first time.
- Still to do: count of agents waiting for input on each tab.

### 1b. Never lose running agents
- The TUI is only a **viewer**. Agents live in their own tmux sessions; quitting, crashing or rebuilding `cs` must never stop them. Verified: `kill -9` of `cs` left the agent running and it reappeared on restart.
- Only `D` (delete agent) kills an agent's session.
- `state.json` and `config.json` are written atomically (`config/atomic.go`), so a crash mid-save can't corrupt them and orphan agents.
- Seamless self-update (user: "nothing should change, nothing lost even while typing"): the old cs saves its last screen and the session being typed into (`~/.claude-squad/last-frame`), flushes logs and execs the new build with `CS_INPLACE_RESTART=1`, leaving the terminal in raw mode and on the alternate screen. The new cs does NOT re-enter the alternate screen (that clears it: black flash in Terminal.app); it moves the cursor home and paints over the old picture, shows the saved screen until loaded, restores typing focus at once, and reads keys typed during the switch (held by the raw tty). On quit it leaves the alternate screen and runs `stty sane` itself. Without the alternate-screen switch the renderer draws relative to the cursor, so a full-height view scrolls the terminal one line and leaves doubled lines: in this mode cs draws one line short of the window and trims the saved screen to match. The saved screen stays up until the first tile capture (cap 20s, no "Loading" after an update); the first status fetch runs immediately. Tested: 120/120 frames unchanged, text typed across the update arrived intact.
- Changes show up in the open `cs` with no restart: it checks its own Go sources every 3s, rebuilds, and re-executes itself (same pid, same lock) when back on the main screen. A broken build is skipped. No server, no extra process.
- The `cs` launcher rebuilds to a temp file and swaps it in; a running `cs` keeps the old binary, and a failed build keeps the last good one.
- Background work (issue polling, feature 5) must run in the daemon, not the TUI, so it keeps going when the TUI is closed.

### 1c. Every Claude session in one view
- Goal: never open VS Code just to watch Claude. `cs` shows all Claude sessions, not only its own agents, and you can type into them from `cs` or from their own terminal.
- Done (live, two-way): `scripts/claude-tmux`, symlinked as `~/.claude-squad/bin/claude` (first on PATH via `~/.zshrc`) runs each interactive `claude` started from a terminal inside tmux as `cc_<folder>[_n]`. `cs` lists them per project under "Other Claude sessions" (`session/external.go`); preview is live, Enter attaches, ctrl-q returns. They are never killed, saved, resized or diffed by `cs`.
- The launcher passes straight through when: already in tmux, not a TTY (VS Code extension, scripts), `-p`/`--version`/`--help`, a subcommand (`update`, `mcp`…), or `CLAUDE_NO_TMUX=1`.
- Tabs: the user's own tabs (added with `+`, or opened by starting `cs` in a repo) are saved; a project with a running Claude session but no saved tab (e.g. a GitHub Actions runner checkout) gets a temporary tab that is never saved and disappears when its sessions end. Same-named projects get the parent folder that tells them apart ("repo · runner-2").
- The typing cursor is drawn into the live pane and grid tiles (tmux's capture leaves it out), placed by counting rows from the bottom so `-J` line joins don't shift it.
- Done: every other Claude session comes from `claude agents --json` (refreshed every 2s, so no restart needed). Background sessions (Claude Code's own service) open with `claude attach <id>`, Ctrl+Z back. Sessions running directly in a terminal or VS Code are view only; their preview is the newest part of the transcript in `~/.claude/projects/*/<sessionId>.jsonl`.
- Command key: **Ctrl+]** then one key (`app/leader.go`): C new Claude session in the project, T terminal, W close the selected session or terminal (asks first; Claude sessions stay resumable), G grid, A add project (folder chooser), X close tab, N issues, S source control, Y copy tile text, ! needs you, ? help, I back to typing, ←/→ project, ↑/↓ session, Q quit; Ctrl+] twice sends Ctrl+] to the session. Works in every terminal with no settings (teammates must not need Terminal.app changes).
- Key bar (`ui/keybar.go`): two lines at the bottom drawn as keycaps: line 1 = what's happening now (typing into X / view only / source control) and how to move; line 2 = the Ctrl+] commands (highlighted after Ctrl+]). Top right: "+ add project (⌃] A)". Ctrl+] ? shows the full help screen.
- Keys (user's choice, 2026-10-01, revised to Option): typing goes to the selected session by default; plain arrows, Shift+arrows, Enter, Esc, ctrl+c go to Claude. Option+←/→ switch project tab (also alt+b/alt+f, Terminal.app's default Option+arrow codes), Option+↑/↓ switch session, Option+S Source Control, Option+I back to the session. ctrl+q stops typing; ctrl+q twice quits. Needs Terminal's "Use Option as Meta key" for Option+S/I. `t` starts a new chat in the folder. ← on Claude's empty prompt is dropped (it would open Claude's agent view).
- With a view-only session selected, plain keys don't fire list shortcuts (users type expecting to chat, and j/k/t/q used to move, start or quit things); they show "view only … press Enter to move it into cs", and the bottom bar says view only. Option navigation, Enter, ! + x and ctrl+q still work.
- Sessions running directly in a terminal (view only): Enter offers to move them into cs: cs ends that process (SIGTERM) and runs `claude --bg --resume <id>`, same conversation, then types into it. Refused when the session has no conversation yet; success is checked in `claude agents`.
- No session list column (user: tiles already name the sessions). Left: Source Control; right: the grid (the only view; the one-big-tile view was removed, user: never used). A Source Control file's diff replaces it while Source Control has focus.
- Project terminal (`app/terminal.go`): a shell in the project folder (tmux `csterm_<folder>`, kept between runs) shown as one more grid tile titled "terminal" (user: it must add a tile, not replace the grid). Ctrl+] t opens or jumps to it, Ctrl+] w closes it (`exit` needs two tries because the user's zshrc leaves gai-watch as a job); a project with no sessions gets one automatically. Claude started in it (found via ppid) keeps the same tile and shows its status. The user's zsh takes a few seconds to start; keys typed before that are lost.
- Auto-focus on questions: when a session starts waiting on the user, cs selects it and gives it the keyboard, switching project tab if needed, but only after 3s without a key press (never mid-sentence); otherwise the message line says "❓ <name> needs you · press ! to jump there". Questions already open when cs starts don't jump.
- Grid (`app/grid.go`, `ui/grid.go`): shows the current project's sessions (user's choice; not all projects), titled "project · session". Tiles are at least 40×12; as many columns/rows as fit, the rest paged. The session list column is hidden in grid view (tiles name the sessions). The focused tile (thick border) is the selected session and gets the typing; Shift+arrows move between tiles (Terminal.app sends Option+Shift+arrows as Shift+arrows). Live screens are fitted to tile size (tracked as each session's current size); a "Servers" list sits above it (`app/servers.go`: the project's listening ports with the tile that started each, found via the process tree; ✕ stop sends Ctrl+C to that tile, or SIGTERM when Claude started it or its shell is gone); the project terminal is docked under Source Control instead (`app/dock.go`: fixed size, Ctrl+] T shows/hides it, a one-line status stays when hidden, Shift+← from the left column reaches it; a terminal running Claude moves to the grid and the dock opens a fresh shell); view-only sessions show their conversation.
- Typed keys go to tmux through an ordered background queue (`keyQueue`), so held-down keys keep up.
- Activity log `~/.claude-squad/activity.log` (0600, cut at 1MB, `app/keylog.go`): start (pid, tabs, view, agents), window sizes, every key with where it went (typing target / source control / list / view-only, view, tab, selection), project/session/typing changes with the reason, auto-jumps, sessions appearing/changing status/going away, agent status changes, live-screen resizes, messages shown, self-update builds, issue-queue steps, quit. No typed text. Tests (`*.test` binaries) never write it.
- Performance (2026-10-01 optimize): live screens re-captured every 50ms while typing (<1s since a key), 100ms up to 3s, 400ms idle; status refresh 500ms typing / 1s idle; tiles not being typed into refresh at most once a second; spinner ticks stopped (old list not shown); transcripts re-parsed only when size/mtime change; scroll position queried only for panes cs scrolled; one `ps` for all parent-pid lookups; typed letters and spaces sent to tmux in batches (13 key events → 4 tmux calls).
- No tmux calls on the UI thread: tile captures run on a background goroutine (`refreshGrid` decides, `captureTile` captures, `applyGridCapture` stores); forwarded keys skip re-rendering; logs are buffered (kept-open files, flushed every 500ms); keys typed into a session are only in keys.log.
- Usage log `~/.claude-squad/usage.log` (`app/usage.go`), every 30s: cs CPU/RSS/heap/goroutines and renders per 30s with average render time; Claude processes (count, RSS, CPU); tmux processes and sessions; Mac load and free memory; last session-refresh and git-status durations; tabs/agents/sessions and current view. First reading (2026-10-01): cs ~12% CPU idle from ~19 renders/s (100ms preview tick), session refresh ~350ms.
- Key log: every forwarded key goes to `~/.claude-squad/keys.log` (mode 0600, cut at 1MB) with the gap since the previous key and its queue delay (`sent+Nms`), for diagnosing input issues such as Claude's hold-Space voice dictation (`voice.mode: hold`, `voice.autoSubmit` submits on release). Typed text is never logged: letters as `text(N)`, special keys by name.
- Keys are forwarded to tmux by name (`tmuxKey`); Option+<special key> maps to tmux's names (Option+Backspace → `M-BSpace`, delete word). Unknown keys are dropped, never sent as text (tmux would type the name literally).
- Mouse wheel (mouse capture off): Terminal.app sends the wheel as ↑/↓ keys. Arrows closer than 10ms are a wheel burst, and a single arrow within 300ms of a burst is too (slow notches). Full-screen panes (`#{alternate_on}`, e.g. `claude attach` background sessions, which have no tmux scroll-back) get SGR mouse-wheel events, which Claude scrolls its own view with; other panes scroll back in tmux copy mode, captured via `#{scroll_position}`. A lone arrow still reaches Claude.
- Mouse capture is off by default so Terminal's text selection and Cmd+C/Cmd+V work; `M` toggles it (config `mouse`).
- Done: type into a session without leaving cs. Enter gives the session pane focus (`app/livepane.go`): every key goes to its tmux pane via `send-keys` (pastes via `paste-buffer -p`), except ctrl+q (back to the list) and shift+↑/↓ (next/previous session). `o` still opens it full screen. Background sessions get a hidden `csbg_<id>` tmux session running `claude attach <id>`, closed when cs quits. Sessions are matched to tabs by their git root's real path (`/tmp` vs `/private/tmp`).
- Conversation previews are rendered as markdown with Glamour (`ui/markdown.go`), cached until the transcript or pane width changes.
- `CLAUDE_SQUAD_HOME` moves cs's data folder (state, config, lock); tests use it so they don't touch the real one.
- Done: one `cs` window at a time (`config/lock.go`, `~/.claude-squad/cs.lock`); two windows used to overwrite each other's `state.json` and resurrect deleted agents. Paused agents whose folder was deleted are forgotten on start.

### 2. Agents work in the normal project folder
- User decision: **same folder, like today** — no separate worktree per agent.
- Upstream always creates a git worktree in `Instance.Start()` (`session/instance.go`); this fork needs a mode that runs the program directly in the project folder.
- Done (first step): `s` starts a plain Claude session in the active tab's folder (no worktree, no branch), listed under "Claude sessions in this folder". The cs source folder (`CS_SOURCE_DIR`, set by the `cs` launcher) is always a tab, so cs is developed from inside cs.
- Known risk, accepted by the user: two agents can edit the same file at once. Diff view must still show what changed (diff against the PR branch / `HEAD`, not a per-agent worktree).

### 3. Issue → agent via `gai issue`
- Start an agent for a GitHub issue by running `gai issue <n|url>` in the project folder, inside the agent's tmux session.
- `gai issue` attaches the issue to the open PR (creating one if none is open), rewrites the PR title/body with local AI, then `exec`s `claude` with the issue thread as the prompt.
- Screenshots attached to issues are passed by `gai` via `--add-dir`; keep that working.
- Open item: `gai issue` asks questions when it has a TTY. Background mode needs it to run unattended (check its flags, e.g. `--yes`, before adding new ones in gai).

### 3b. Issues → sessions (⌥N) — done
- ⌥N opens a picker (`ui/issuepicker.go`, `app/issues.go`): choose the project (←/→), see its open issues oldest first via `gh issue list`, the 5 oldest without a session pre-ticked; space ticks/unticks, Enter starts.
- Each picked issue gets its own tmux session `cc_<folder>_i<number>` running `gai issue <url>`. They start one after another (each gai rewrites the one open PR; together they would race). cs answers gai's [Y/n] questions with Enter (default Yes; user's choice 2026-10-01) and moves to the next issue once Claude is running there (seen in `claude agents`), or after 5 minutes.
- Tested with fake `gh`/`gai` on PATH only; never run against real GitHub in tests (gai creates/updates PRs).

### 4. One PR at a time
- All agents in a project attach to the **single open PR** (this is how `gai issue` already behaves).
- A new PR is only created after the current one is **merged** by the user.
- PR creation/updates always go through `gai` (local AI), never through Claude.

### 5. Background / 24-7 mode
- Runs in the background (extend `daemon/daemon.go`; today it only auto-answers prompts).
- Every **5 minutes** per selected project: fetch open issues with `gh`, and start one agent per **new** issue (every new issue, no label filter).
- Limits:
  - Max **7 agents running at once per project**; extra issues wait in a queue.
  - Max **15 issues** picked up before it stops taking new ones. Open item: confirm whether 15 is per PR (reset after merge) or per day.
- Offline-safe: if there is no internet, wait and retry; resume automatically when the connection returns.
- Remember which issues were already picked up (persist in state) so restarts don't double-start them.

### 6. "Needs you" marker
- Done: a session waiting on the user shows ❓ and "needs you" on its row, and its tab shows ❓N. `!` jumps to the next one, switching tabs. The list is not reordered, so the cursor never jumps while typing.
- Sources: `claude agents` status `waiting` (all kinds; values are busy/shell/idle/waiting); for sessions with a screen, prompt text on it ("Enter to select", "Enter to confirm", "Do you want to proceed?"), which also catches the folder-trust dialog Claude doesn't report; for cs's own agents, upstream's permission-prompt check too.

### 7. Changes view (Source Control)
- Done: three columns: Source Control | sessions | live session pane. The left column (`ui/sourcecontrol.go`, `app/sourcecontrol.go`, git ops in `session/git/status.go`) lists the active project's Staged and Changes like VS Code, refreshed every 2s off the UI thread.
- `g` focuses it: ↑/↓ select (diff shows in the right pane's Diff tab), space stage/unstage, `a` stage all, `u` unstage all, `d` discard (confirm; untracked files are deleted, and the confirm says so), `c` commit (message box, Tab then Enter), esc back.
- It covers the project folder. Agents started with `n` work in worktrees, so their changes stay in their own Diff tab.
- gai-watch: the user's zsh starts gai-watch in every git repo a shell enters, and it commits staged files within seconds. The panel warns when it runs for the repo. Tests must never `cd` into a test repo (use `git -C`), or a gai-watch starts there.

### 8. Screenshots
- Terminals can't render images. Add an `i` key on the selected agent: list image files it produced/referenced and open the chosen one with macOS Quick Look (`qlmanage -p`) or `open`.
- Image paths printed by agents should be cmd+clickable in the terminal.

## Out of scope
- Electron / web dashboard.
- Auto-merging PRs.
- Paid APIs or a second AI subscription.

- Auto issue loop (`app/autoissues.go`): on for every project (a in the issue picker switches it off: a `.auto-off` file next to the project's issue cache), max 6 Claude sessions and 15 issues per open PR. When a slot frees on the tab you're on, the picker opens with that many issues ticked (oldest first, skipped ones never); Enter starts, Esc waits until another session closes. Other tabs show "·N free". Never opens while typing (3s) or over another dialog.
- Progress strip (`app/progress.go`): the row above the tiles shows the current project's open PR filling to 15 issues, open issues left (and the change since the morning), sessions running of 6, sessions finished today (a Claude session closed with ⌃Space W, kept in `progress.json`), the day streak, and Σ totals across projects.
- gai's "Multiple open PRs" list is answered with the PR this project's issues already go to (`pickPR` in `app/issues.go`).
- Background issue runner (`app/issuesdaemon.go`, `cs --issues-daemon`): a LaunchAgent (`dev.claudesquad.issues`, installed with `cs --install-issues-daemon`) keeps it running whenever the user is logged in. It does the auto issue loop with no window and no picker: per project up to 6 Claude sessions and 15 issues per open PR (waits for the PR to be merged, then gai opens the next), never skipped issues, gai's questions answered (Enter; n to the PR rewrite while more wait; the project's PR in gai's PR list). The cs window then only shows `⟳ auto` and leaves starting to it. `/skip` (skill) in an issue session adds the issue to the project's skip list. Log: `grep daemon: ~/.claude-squad/activity.log`.
