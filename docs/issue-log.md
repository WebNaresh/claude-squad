# Issue log

Every problem the user reported with cs, how it was fixed, and what guards it.
**Before fixing a reported problem, search this file** (symptom words, file
names). If it is here, it came back: find out why the guard missed it, fix the
root cause, and add a "Came back" line to the entry instead of a new entry.
After every fix, add or update an entry.

Entry format: what the user saw → cause → fix → guard (test or log line) → files.

## Display

### Doubled lines after cs updated itself
- **Saw:** after a self-update, "Source Control" twice, tile bottoms repeated, the tab row cutting into the tiles, a key-bar line missing.
- **Cause:** the new process drew without the alternate screen ("inline") over the old picture; every update shifted the picture and left stray lines.
- **Fix:** always enter the alternate screen; the first frame drawn is the previous process's saved screen (`app/frame.go`), so the switch is one frame.
- **Guard:** the live tmux test of 3 updates in a row (count "Source Control" = 1, 50/50 rows). No unit test.
- **Files:** `app/app.go` (Run), `app/frame.go`, `restart_unix.go`.

### "Loading your sessions…" / black flash on update
- **Saw:** a loading screen or a black flash every time cs updated.
- **Cause:** readiness took more than 4s on a busy Mac; re-entering the alternate screen cleared it.
- **Fix:** the saved frame stays until the first grid capture is ready (20s cap).
- **Guard:** activity.log line "ready after … (saved screen shown meanwhile: true)".
- **Files:** `app/app.go` (view), `app/frame.go`.

### Tile corners out of line
- **Saw:** each tile's top-right corner pushed one column right of the tile below.
- **Cause:** the title-in-border line was one column wider than the tile.
- **Guard:** `TestRenderTileWidth`, `TestBoxWidths` (`ui/grid_tile_test.go`).

### Key hints wrapping onto a second line
- **Saw:** the issue picker's hints wrapped, "esc cancel" missing.
- **Cause:** `KeyRow` appended an overflow hint without checking the width.
- **Guard:** `TestKeyRowFits`, `TestKeyBarKeepsHelpKey`.

### Navy background, harsh dark-green diff blocks
- **Saw:** the terminal's navy background showed through; Claude's diffs in tiles were saturated dark green/red. Wanted the VS Code terminal look.
- **Fix:** cs paints its own background (#1f1f1f, VS Code Dark Modern) and maps 256-colour diff backgrounds 22/28/52/88 to Claude's softer true colours (`app/background.go`); accent #0078d4.
- **Guard:** `TestOnBlack`, `TestSofterDiffColours`.

### /stage status not visible on the tile, and cleared too soon
- **Saw:** the `#N ✓ done · close session` badge was cut off in the tile footer, and it reset on the next message.
- **Fix:** the tile border shows the /stage step (`session/stagephase.go`). The status-line hook also writes `<session>.stagelast`, never cleared, only replaced by the next /stage.
- **Guard:** `TestStagePhaseKeepsLast`; hook lifecycle check (fake session through /stage → add → stop → prompt).
- **Files:** `session/stagephase.go`, `~/.claude/hooks/session-state.sh` (shared with the team).

### Half-empty tiles (text at the top, blank below)
- **Causes:** inline Claude after its window grew; captured right after a resize; caught mid-redraw; a background session whose own size is smaller (not fixable from cs).
- **Fix:** `fillFromHistory`, 150ms wait after a resize, re-capture when much emptier (`app/grid.go`). Details in `docs/tiles-mouse-debugging.md`.
- **Guard:** activity.log "half-empty tile NAME: text in R of H rows".

### All sessions in one row of tall narrow strips
- **Saw:** 7 sessions on a wide screen (small font) as 7 strips ~45 columns wide and the full height; "why isn't it a grid".
- **Cause:** the layout took as many columns as fit at 40 wide, so rows stayed at 1 until columns ran out.
- **Fix:** `balancedLayout` (`ui/grid.go`) tries every columns × rows that shows all tiles and picks the tile shape closest to 2:1 columns:rows (about square on screen), with a cost for each empty slot. 7 → 4+3, 4 → 2×2, 12 → 4×3. Paging only when nothing fits.
- **Guard:** `TestGridLayoutBalanced` (`ui/grid_layout_test.go`).

## Input and mouse

### Clicks hit the wrong tile
- **Saw:** (found in review) clicks after Source Control got a border.
- **Cause:** the grid was located by the first "╭" corner on screen, which became Source Control's.
- **Fix:** the grid position is computed from the layout (`app/mouseclick.go`).
- **Guard:** `TestHitGridSkipsSourceControl`.

### File paths in tiles can't be opened
- **Saw:** a scratchpad path wrapped over 3 lines; no way to open or copy it.
- **Fix:** clicking a path opens it (VS Code, else the default editor; folders in Finder). Wrapped rows are joined (`app/openpath.go`).
- **Guard:** `TestPathAtWrapped`, `TestPathAtRelative`.

### Wheel scrolling recalled Claude's prompt history / didn't scroll
- **Saw:** the wheel typed old prompts; `claude attach` tiles didn't scroll; slow wheel notches leaked as arrow keys.
- **Fix:** burst detection (10ms gap, 300ms sticky window); SGR wheel events to alternate-screen panes, tmux copy-mode otherwise (`app/livepane.go`).
- **Guard:** `app/livepane_test.go`; keys.log shows "wheel scroll …".

### Closing a session took /exit then exit
- **Fix:** ⌃Space W closes the selected session or terminal after a y/n question; background sessions via `claude stop` (`app/terminal.go`).
- **Guard:** live test on a throwaway terminal tile.

### No way to start a Claude session from cs
- **Saw:** an empty project said "run claude in the terminal"; the terminal is for dev servers.
- **Cause:** the new-session key was a plain letter, which goes to the session being typed into, so it was unreachable.
- **Fix:** ⌃Space C starts Claude in the project folder; the new tile gets focus (`newClaudeSession`, `app/terminal.go`). Shown first on the key bar and in the empty-project message.
- **Guard:** live test with `cs -p 'bash --norc'` (no real Claude started): ⌃Space C adds a focused `cc_<project>` tile.

### Can't select text (mouse capture on)
- **Saw:** drag did nothing after cs turned mouse capture on for image clicks; no hint how to select.
- **Cause:** while cs captures the mouse, Terminal.app's own selection is off.
- **Fix:** cs selects itself: drag inside a tile highlights that tile's text only and copies on release (`app/selection.go`). Key bar shows `drag select + copy`; help lists it; ⌃Space m turns capture off.
- **Guard:** activity.log "copied a N-line selection"; check list in `docs/tiles-mouse-debugging.md`.

### Typing does nothing after clicking a tile
- **Saw:** clicked a tile, typed `/stage`, nothing appeared; activity.log "key send failed … exit status 1".
- **Cause 1:** the pane was still in tmux copy mode (scrolled by the wheel) after cs restarted for a build; the "scrolled" memory was lost, so keys went to copy mode ("t"/"g" fail there, reproduced on `tmux -L cstest`).
- **Cause 2:** the click landed on an `[image]` line, opened the gallery in Chrome, and Chrome took the keyboard.
- **Fix:** before typing, cs asks tmux `#{pane_in_mode}` (at most every 2s per pane) and leaves copy mode (`app/livepane.go`). A click opens an image/path only in a tile that already had focus (`app/mouseclick.go`).
- **Guard:** activity.log "typing into X: it was still scrolled back".

### Mouse reports typed into sessions (`^[[<65;345;81M`)
- **Cause:** a fast wheel burst split mid-report; bubbletea read ESC [ as alt+[ and the rest as text.
- **Fix:** `isMouseFragment` drops them (`app/selection.go`).
- **Guard:** activity.log "dropped a split mouse event".

### Session "frozen": typing, clicks, wheel do nothing
- **Cause:** Ctrl+G in Claude opened the prompt in VS Code (`code -w`); Claude waits for the tab to close. Leftover lines stay until Claude redraws (Ctrl+L).
- **Fix:** the tile title says "? waiting for your editor · close its tab" (`app/grid.go`).

### Shift+↑/↓ don't move between tile rows
- **Saw:** with tiles in two rows, Shift+↑/↓ did nothing in cs (and went to Claude as plain arrows).
- **Cause:** Terminal.app sends Shift+↑/↓ as plain ↑/↓ unless its profile maps them; keys.log never shows shift+up/down.
- **Fix:** Shift+←/→ run through tiles in reading order, wrapping rows; ← goes to the dock only from the first tile (`app/grid.go` moveTile). Terminal mapping steps in `docs/tiles-mouse-debugging.md`.

## Speed

### Lag while typing
- **Cause:** tmux captures ran on the UI thread; logs written per key; spinner ticks.
- **Fix:** captures in a background goroutine, buffered logs, cached grid render, no spinner ticks.
- **Guard:** `~/.claude-squad/usage.log` (30s CPU/render counters).

### Closing a terminal left its tile up for a second
- **Cause:** after the kill, the session list was reloaded (`claude agents`) before the tile went.
- **Fix:** the tile is dropped at once and kept out for 5s; a capture started before the close is discarded (`app/terminal.go`, `app/app.go`, `app/grid.go`).

## Test incidents (rules for live tests)

- A test pressed Enter on the user's real session and moved it. → Check the selected row on screen before any destructive key (CLAUDE.md "Live tests").
- A test `cd`'d into a temp repo; the zsh hook started gai-watch, which committed. → Use `git -C`, never `cd`.
- Tests wrote to the real activity.log. → `inTest` guard in `app/keylog.go`.
