# Tiles, mouse and selection: how they work, how they broke, how to debug

Read this before changing anything in `app/grid.go`, `app/mouseclick.go`,
`app/selection.go`, `app/livepane.go` or mouse handling in `app/app.go`.
Each section is a bug the user actually hit.

## Mouse capture and text selection

- Mouse capture is **on** by default (`config.MouseOff` turns it off, `⌃Space m`).
  While it's on, Terminal.app's own drag-select does nothing. That broke
  selection once (user: "we add one feature and create a new bug").
- So cs does selection itself (`app/selection.go`): a drag inside a tile
  highlights text in that tile only and copies it on release. A press with no
  drag is a click: focus, open an image (`gallery.go`) or a path (`openpath.go`).
- The key bar shows `drag  select + copy` while typing, and help (`⌃Space ?`) lists it.
- **Rule:** any new mouse feature must keep drag-select, click and wheel working.
  Check all three (list below) before saying it's done.

## Half-empty tiles

A tile can show text in the top part and nothing below. Three causes so far:

1. **Inline Claude after its window grew.** Claude not in full-screen mode
   draws from the top, so the extra rows stay empty. `fillFromHistory` drops
   the empty rows and fills the top from the pane's history (inline panes only,
   `alternate_on=0`). The cursor row shifts by the dropped rows (`cursorInto`).
2. **Captured right after a resize.** Every Terminal window size change
   resizes all sessions. `captureTile` waits 150ms after `FitTo` before it
   captures.
3. **Caught mid-redraw.** Claude erases its lower lines, then redraws them. A
   capture much emptier than the previous one at the same size is taken again
   40ms later.

4. **A background session drawn smaller than its window** (`csbg_*`, full
   screen): the background Claude keeps the height it has, and `claude attach`
   shows it at that size; a fresh attach doesn't change it. Not fixable from
   cs. The log repeats "half-empty tile csbg_…" with fit=false.

A resize to the size a window already has sends no SIGWINCH (checked on a test
tmux server), so repeated "resize X to WxH" log lines are harmless.

## Ghost text: old lines left on screen, rows repeated or shifted

- Cause: a row the terminal draws wider than cs measured it (`⚠️` is 2 columns
  to lipgloss/ansi but 1 to go-runewidth, and Terminal.app may differ from
  both). With auto-wrap on, that row spills into the next one, every row below
  shifts down, and bubbletea (which only redraws changed rows) leaves old text
  behind, e.g. a closed issue picker mixed into tiles.
- Fix: `app.Run` turns auto-wrap off (`\x1b[?7l`) while cs runs and back on
  at exit, so an over-wide row is clipped at the edge instead. Don't remove it.
  If a killed cs leaves the shell not wrapping, `printf '\e[?7h'` restores it.

## "Nothing happens when I type" in a session

- **Claude is waiting for an editor.** Ctrl+G in Claude Code opens the prompt
  in `code -w` (VS Code). Claude reads nothing until that tab closes, and the
  screen shows "Save and close editor to continue…". Find it with
  `ps -axo pid,ppid,command | grep "code -w"` under the pane's pid. Closing
  the tab (or ending the `code -w` process) brings the session back. The tile
  title says "? waiting for your editor · close its tab" meanwhile. Leftover
  lines from that wait stay on screen until Claude redraws (Ctrl+L in it).
- **Mouse reports typed as text** (`^[[<65;345;81M` in a session): a fast wheel
  burst split mid-report. bubbletea reads its start as alt+[ and the rest as
  text. `isMouseFragment` (`app/selection.go`) drops them, and
  activity.log logs "dropped a split mouse event".

## Shift+↑/↓ between tile rows

Terminal.app sends Shift+↑/↓ as plain ↑/↓ by default (Shift+←/→ do arrive),
so cs can't see them and Claude gets an arrow key instead (it recalls old
prompts). Shift+←/→ wrap between rows, so every tile is reachable without
them. To make Shift+↑/↓ work, the user adds two keys in Terminal → Settings →
Profiles → (their profile) → Keyboard → "+":

| Key | Modifier | Action | Text |
|---|---|---|---|
| ↑ Cursor Up | Shift | Send Text | `\033[1;2A` |
| ↓ Cursor Down | Shift | Send Text | `\033[1;2B` |

## Debugging what the user saw

1. **Is the user on your build?** Self-update waits while `~/.claude-squad/hold-update`
   exists (another session pauses updates) and while the issue queue is running.
   Compare the last `start pid=` line in `~/.claude-squad/activity.log` with the
   time of your edit before asking the user to test. A visible label you
   changed (key hint text) is a quick check.
2. **Half-empty tile:** `grep "half-empty tile" ~/.claude-squad/activity.log`
   gives the session, the rows that held text, and whether a resize (`fit`) came just before.
   Then `tmux display -p -t <session> '#{window_width}x#{window_height} alt=#{alternate_on}'`
   and `tmux capture-pane -p -t <session> | awk 'NF{l=NR} END{print l, NR}'`.
3. **Keys and clicks:** `~/.claude-squad/keys.log` shows key timing, and activity.log
   shows `command key:`, `wheel scroll`, `copied`, `opened`.
4. **Reproduce on a separate tmux server** (`tmux -L cstest`). Put a `tmux`
   wrapper script first on PATH so the package's plain `tmux` calls reach it.
   Never test against the user's real sessions.

## Several sessions edit cs at once

Other Claude sessions often change the same files (`whose <file>`). Keep edits
small, re-read before editing, and put new features in new files.

## Before saying a cs change is done

- [ ] Drag inside a tile selects only that tile's text, and ⌘V pastes it
- [ ] A click on a tile focuses it; a click on an `[image]` line opens the gallery
- [ ] The wheel scrolls the tile under the pointer
- [ ] After a Terminal window resize, every tile is full height
- [ ] Overlays (issue picker) fit the screen with the header visible
- [ ] The user's cs is on the new build (see "Is the user on your build?")
