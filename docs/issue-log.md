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
- **Guard:** `TestPathAtWrapped`, `TestPathAtRelative`, `TestPathAtSentImages`.
- **Came back (2026-10-02):** clicking a sent image (`[image] …/before-booking-details.png`) opened its parent folder. Claude prints the file size at the right edge of the wrapped path ("(283.9K" … "B)") and labels it `[image]`; joining the rows glued those into the path, and two files listed back to back joined into one. The guard only tested a plain wrapped path. Fix: `wrapRow` cuts the size and label before joining; the path finder moves past a path that ends above the click; the path under the click is checked before the image-name match; images open in Preview (or the image page if Claude sent them).

### Clicking an image or its folder opened Finder / Preview, not a web page
- **Saw (2026-10-13):** clicking an image path (or its scratchpad folder) in a tile opened Finder; the user wants the HTML page.
- **Cause:** only files in the session's sent list went to the gallery page. Any other image went to `open` (Preview) and folders to Finder.
- **Fix:** `openPath` now opens any image on a one-image page, and a folder that holds images (two levels deep, newest first) on a page of them. Only an empty folder or a PDF still uses `open` (`openFilesPage`, `imagesIn` in `app/gallery.go`).
- **Guard:** `TestImagesIn`.

### PR in the status bar didn't say whether its checks pass
- **Asked (2026-10-13):** the bottom row showed only "PR #593"; the user had to open the browser to see if CI passed or was still running, and wanted it live.
- **Fix:** the PR segment now carries the checks: green "✓ 5/5 checks", red "✗ 1 failed", amber "● 2 of 5 running". Read from `gh pr view --json statusCheckRollup`, re-read every 10s for the active project (`checkLabel`, `prRefreshEvery` in `app/statusbar.go`; `PullRequest.CheckSummary` in `session/git/sync.go`). Clicking it still opens the PR. A ↻ button to its right re-reads the PR and checks at once (`refreshPRNow`); it shows ⟳ while loading.
- **Guard:** `TestCheckLabel`.

### Behind the remote (↓3) but no way to pull from cs
- **Saw (2026-10-13):** glitchgrab showed `↓3 ↑0`; clicking it only offered a push ("nothing to push"), so the user couldn't pull.
- **Fix:** the bar has two buttons: `↓N` pulls (`git pull --ff-only`, after a confirm; shows ⟳ pulling…), `↑N` pushes. A diverged or dirty branch shows git's own refusal (`git.Pull`, `pullProject` in `app/statusbar.go`).
- **Note:** the counts come from the last fetch; a pull fetches first. A terminal "Already up to date" in another folder does not change the project root's count.
- **Guard:** none yet (needs a git remote fixture).

### Which model is each session running? (gai model: labels)
- **Asked (2026-10-07, via glitchgrab#576):** gai now starts issue sessions with `--model sonnet` / `--model 'opus[1m]'` from the issue's `model:` label; the user wants to see that per session.
- **Fix:** every Claude tile's header shows a small model tag before its status: sonnet (green), opus (purple), others grey, with `[1m]` when the process was started with `--model …[1m]`. The model comes from the latest reply in the session's transcript ("model":"claude-sonnet-5-5"); before the first reply, from the process's `--model` argument (`session.AgentModel`; the pid is found in `~/.claude/sessions/*.json`).
- **Guard:** `TestShortModel`.

### Clicking a sent image opened a Finder folder
- **Saw (2026-10-07):** in #2337, clicking the "› [image] /private/tmp/…/d9c6d7c1-…-8c2b2aa13b0" line opened Finder on the project's scratchpad parent (a list of session folders), not the screenshot.
- **Cause:** the tile is narrower than the session's screen, so the line was cut before the file name. The path click takes the longest part that exists (a parent folder), and the match by file name had no file name to match.
- **Fix:** when a click resolves to a folder on a "›" sent-file line, the cut text is matched against the start of the files the session sent. Several with the same start (before and after in one folder) are told apart by the line's place in the list. The image page opens on that file (`cutImageAt`, `app/mouseclick.go`).
- **Guard:** `TestCutImageAt`; activity.log "clicked a cut-off image path".

### Wheel scrolling recalled Claude's prompt history / didn't scroll
- **Saw:** the wheel typed old prompts; `claude attach` tiles didn't scroll; slow wheel notches leaked as arrow keys.
- **Fix:** burst detection (10ms gap, 300ms sticky window); SGR wheel events to alternate-screen panes, tmux copy-mode otherwise (`app/livepane.go`).
- **Guard:** `app/livepane_test.go`; keys.log shows "wheel scroll …".

### Not sure which tile is selected; focus jumps away before ⌃Space W
- **Saw (2026-10-05):** to close the tile in row 1, column 3 the user clicked it three times, because selection kept moving and the selected tile was hard to spot.
- **Cause:** auto-jump to a new question only waited for 3s without a *key*; clicks didn't count. activity.log 08:15:52: click on #2268, auto-jump to #2270 89ms later; 08:13:42 the same with #2265 → #2264. The selected tile differed only by a white frame, and long names cut off the issue number.
- **Fix:** a click counts as input. A tile picked by hand (click or arrow) holds focus for 30s (`pickHold`); a question meanwhile shows "❓ X needs you · press ! to jump there" instead of moving. When it does jump, it says "❓ moved to X". The selected tile has a white "▶" name tag and "◀ SELECTED" on its top border. Cut names keep their "#NNNN". The close question names the position: "Close X (row 1, column 3)?" (`app/grid.go`, `app/mouseclick.go`, `app/terminal.go`, `ui/grid.go`).
- **Guard:** activity.log "question …: no auto-jump, tile picked Ns ago"; `TestCutKeepingIssue` (`ui/grid_test.go`).

### Closing a session took /exit then exit
- **Fix:** ⌃Space W closes the selected session or terminal after a y/n question; background sessions via `claude stop` (`app/terminal.go`).
- **Guard:** live test on a throwaway terminal tile.
- **Came back (2026-10-02): closed session opens again.** ⌃Space W on a background session (whatsapp-callback-setup, d0d20006) logged "session closed" and it reappeared 20s later, twice; e1a61cd2 "closed" 4 times. `/exit` inside such a tile needed a second `exit`. Cause: cs shows background sessions through its own `claude attach` wrapper, and `claude attach` resumes a stopped session; the 10s-cached `claude agents` list still had it, so cs attached again. The guard only tested a terminal tile. Fix: `session.MarkClosed` hides the session and drops it from the cache before `claude stop`; `EnsureLive` never re-attaches a closed one; when cs's own wrapper ends (the user's `/exit`), the session is closed and stopped instead of re-attached. It shows again only when Claude reports it busy (resumed elsewhere). `CloseAttachWrappers` forgets wrappers first, so quitting cs never counts as `/exit`.
- **Guard:** `TestMarkClosedHidesUntilBusy`, `TestCloseAttachWrappersIsNotExit` (`session/closed_test.go`).

### No way to start a Claude session from cs
- **Saw:** an empty project said "run claude in the terminal"; the terminal is for dev servers.
- **Cause:** the new-session key was a plain letter, which goes to the session being typed into, so it was unreachable.
- **Fix:** ⌃Space C starts Claude in the project folder; the new tile gets focus (`newClaudeSession`, `app/terminal.go`). Shown first on the key bar and in the empty-project message.
- **Guard:** live test with `cs -p 'bash --norc'` (no real Claude started): ⌃Space C adds a focused `cc_<project>` tile.

### Ctrl+Space gives no sign of command mode (and sometimes doesn't enter it)
- **Saw:** pressing Ctrl+Space showed nothing that said "now press a command key".
- **Cause:** the only sign was the small key bar. Also, one press can arrive as two key events (keys.log 09:26:42: two `ctrl+@` 164ms apart, both "sent" to the session): the second counted as "pressed twice → send a real Ctrl+Space", which left command mode at once.
- **Fix:** a repeated ⌃Space while in command mode is ignored (only Ctrl+] twice sends itself on); a blue "⌃Space · command mode" box with every command key shows in the middle of the screen until a key or esc (`leaderMenu`, `app/leader.go`).
- **Guard:** live test: two quick ⌃Space presses keep the box up; activity.log "command key: repeated ⌃Space ignored".

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

### Enter does nothing; typed text and `^[` appear under Claude's box
- **Saw (2026-10-02):** in a terminal tile running Claude by hand (`csterm_claude-squad-2`, image1-check-terminal-viewer), `/stage` + Enter did nothing; `/stage` and `^[` were printed below the prompt box.
- **Cause:** the pane's terminal was back in line mode (`stty`: `icanon echo`) while Claude (idle, 0% CPU, no editor child) still ran: something in that session reset the terminal and Claude didn't restore raw mode. Not a cs bug; what reset it is unknown.
- **Way out:** `stty -f <pane tty> raw -echo` gives Claude its keys back (tty from `tmux display -p -t <session> '#{pane_tty}'`), or close the tile (⌃Space W) and `claude --resume`.
- **Done for this case:** `stty -f /dev/ttys012 raw -echo` then Ctrl+L: Claude redrew its screen (the `^[` lines went), so it reads keys again.
- **Auto-fix:** every 5s cs checks each tmux pane whose foreground program is Claude; one still in line mode 4s later gets Node's raw-mode settings back plus Ctrl+L, logs "keyboard restored: …" and shows "fixed the keyboard of …" (`session/keyboard.go`, `keyboardCheck` in `app/terminal.go`).
- **Guard:** `TestRepairClaudeKeyboards` (private tmux server, stand-in `claude`). If it keeps happening, find what runs `stty`/`reset` in that session.

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

## Background runner

### Switching Claude subscription (/login) left every tile stopped
- **Saw (2026-10-05):** after `/login` to another subscription, every tile showed "Remote Control disconnected — signed-in claude.ai account or organization changed" and `/rc failed`. Tiles stopped on the old account's usage limit stayed stopped until its reset time. The user had to type `/remote-control` and `continue` in each tile by hand.
- **Cause:** the runner only resumed a limit after its reset time, and nothing turned Remote Control back on.
- **Fix:** when that notice is the last step on a tile with an empty prompt, the runner types `/remote-control`. If the tile was stopped on a limit, it also types `continue` straight away, because the new account has its own limit. It does this once per tile per account (`accountUuid/organizationUuid` from `~/.claude.json`). It also counts "⏺" as a step, not only "●" (`app/limitresume.go`).
- **Came back (same day): `/stage` couldn't upload screenshots.** The claude.ai connectors (Glitchgrab) stayed signed in to the old account, so #2307 asked how to attach its shots. `/reload-plugins` reconnects them; the runner now types it right after `/remote-control`.
- **Guard:** `TestAccountSwitch`; activity.log "claude.ai account changed, typed /remote-control and /reload-plugins" and "usage limit on the old account, typed continue". First live run: 10 tiles, 3 resumed from a limit.

### Issues that failed testing never came back to the auto pickup
- **Saw (2026-10-06):** #2268 failed testing (label `qa:failed`, reopened) but the issue picker hid it as "already in a PR (open or merged)", so no session picked it up again. 7 issues were stuck like this.
- **Cause:** any merged PR closing an issue marked it taken (#944: avoid redoing merged work), with no exception for a fix that failed testing.
- **Fix:** an issue with `qa:failed` that was labelled *after* its latest merged PR is free again. It shows as "(failed testing, again)" and the auto pickup takes it. If a newer re-fix PR is open or merged after the label, the issue stays taken (`session/issues.go` `qaFailedAfter`, one GitHub events call per qa:failed issue).
- **Guard:** live probe on practise_stack: #2171, #2181, #2203, #2207, #2262, #2268, #2269 all free.
- **Came back (2026-10-09): #2268 failed testing a second time and stayed hidden.** Its re-fix (PR #2354) merged 6 Oct 05:41 and QA reopened it at 07:56. They didn't add `qa:failed` again because it was still on from the first failure, and cs only looked for a fresh label. Now a reopen after the latest merge counts too: the issue is free when it was labelled `qa:failed` or reopened after its latest merged fix (`qaFailedAfter`).

### Issue sessions all look alike in Claude (no /color)
- **Saw (2026-10-06):** the user ran `/color` by hand in #2207 to tell sessions apart and asked for every issue session to get one when it starts.
- **Fix:** the background runner types `/color <colour>` into each issue session once Claude's prompt is up and empty. Claude applies it at once even while busy and doesn't send it as a message (tested on a throwaway Claude). Each session gets a colour no other session of its project has. A colour the user set is kept. Picks are saved in `~/.claude-squad/session-colors.json`, so a restart doesn't colour a session twice. The cs tile uses the same colour (`session.AgentColor` reads `agent-color` from the transcript, only new bytes). A new tile kept its old colour because a transcript not found yet was remembered as missing for good; now it is looked for again after 5s. Files: `app/sessioncolor.go`, `session/agentcolor.go`, `app/closeghost.go`.
- **Came up on the first run:** picks made in one pass all saw the same free colours (Claude saves `/color` a moment later), so #2268 and #2269 got the same colours as #2171 and #2181. Picks now count as taken right away. The user then asked that a colour only ever be set at the very start: a session is coloured once, within 3 minutes of Claude starting (`startedAt` in `~/.claude/sessions/<pid>.json`), and never changed after that. The clash re-colouring was removed.
- **Came back (2026-10-09): no new issue session got a colour.** The start-time check used the session's process id, which tmux tiles don't record (always 0). The start time came back unknown, so every new session was marked "not new, leave it" in `session-colors.json`. Now the process is found by session ID (`session.SessionStartedAt`). An unknown start time is checked again on the next pass, never marked. The 7 sessions started during the bug were left uncoloured, by the user's choice.
- **Came back (2026-10-09): #587's tile wasn't Claude's yellow.** Two causes. The selected tile was drawn all white, hiding its colour. And cs's own shades (a sand yellow `#e5c07b`) weren't Claude's: in Terminal.app Claude uses 256-colour palette entries (read off a test Claude's screen: red 167, blue 110, green 35, yellow 178, purple 140, orange 174, pink 175, cyan 37). cs now draws exactly those, and the selected tile keeps its colour as a double frame, with the white "◀ SELECTED" label.
- **Asked again (2026-10-10): "/color pink" showed at the end of #2366's first reply, so it looked set late.** It wasn't: activity.log "typed /color pink" at 23:07:33, 19s after the session started, and its transcript has the `agent-color` record right after the first prompt. Claude applies a `/color` typed while busy at once but prints its "/color … · Session color set" note only when that turn ends. Claude has no start-up colour option and gai sends the issue prompt as Claude starts, so there is no idle moment before the first turn. Nothing to fix; check the transcript's `agent-color` line position, not where the note is printed.
- **Came back (2026-10-10): "why is it changing colour in between" (#2372, #2389, #2398).** Two things. (1) The tile did change once: until Claude's prompt was up (~80s after the tile appeared, while gai asked its questions) cs drew it in one of its own colours, then switched to Claude's. Now the background runner picks the colour the moment the tile appears (right after it starts the session, and on every 15s pass) and saves it as `tile:<tmux name>` in `session-colors.json`; the window draws an uncoloured tile in that pick, and the later `/color` types the same colour. A closed tile's pick is dropped. (2) The "/color …" note at the end of the first reply is only where Claude prints it (see above).
- **Came back (2026-10-13, #2441): "why is it adding colour in between, I said on start only".** Third time the late "/color purple" row showed after the first reply. Root cause: `gai` ran `claude "<issue prompt>"`, so Claude was busy from its first second and any `/color` came after. Fixed in gai-tools (`gai`, `_gai_issue_claude_session`): in a tmux pane Claude starts with `/color <c>` as its first message (colour = palette[issue % 8], the same order and start as `pickFor`), then gai pastes the issue prompt once "Session color set" is on screen (45s fallback). The note now prints at the top, before any work. `GAI_FIRST_COLOR=no` turns it off. The daemon's later `/color` is skipped (the session already has an `agent-color`). On a clash with another session of the project, cs's tile still shows its own pick until Claude's colour is read, then switches once.
- **Guard:** `TestAgentColorReadsLatest`, `TestColorPickedAtTileStart`; activity.log "colour <c> picked at start", "typed /color <colour>".

### cs switched practice-stack from dev to main while sessions were coding
- **Saw (2026-10-06):** the branch went from dev to main mid-work. The sessions' new commits (23 by 12:30) landed on local main.
- **Cause:** the new-batch cleanup (`freshBase`, added 3 Oct) ran at 11:12. It ran because cs saw no open PR with issues. PR #2355 had been opened a minute earlier, but gai's model refused to write its body ("I apologize, but I cannot assist…"), so it listed no "Closes #…". The guard only checked for a clean working tree, which it was for that instant, while 1 session was still running.
- **Fix:** the switch now waits until no Claude session is open in the project, and refuses while any PR is open from the current branch (`app/issuesdaemon.go`, `app/batchbase.go`).
- **Guard:** activity.log "not starting a new batch yet: N session(s) still open" / "PR #N is still open from dev".
- **Side effect (2026-10-07):** glitchgrab stopped picking up issues. #542 (done, but still open) kept the cleanup waiting, and the new PR #577 (dev → main) listed no "Closes #…", so cs saw no batch PR. Now an open PR from the checked-out branch counts as the batch's PR even with no issues listed (`openPRHere`, `app/issuesdaemon.go`); the batch continues on it with no switch. Live: 22:11 "PR #577 room 15: queued 4 issue(s)".
- **Came back (2026-10-09): glitchgrab #585 not picked.** The repo was on main with no open PR, and #542 had been open for days, so the cleanup waited for it forever. Now, when the repo is already on main, nothing needs switching: the batch starts on main as it is, with no pull while sessions are open ("new batch starts on the current main"). An old local `dev` that is only an older copy of main (its files match one of main's last 50 commits) is deleted first, as the user agreed. A `dev` with work of its own makes the batch wait and say why (`staleDev`). gai's "Branch name [dev] (n to abort):" prompt, never seen before because the repo was always on dev, is now answered with Enter (`isGaiPrompt`).

### 189 zombie processes under cs
- **Saw (2026-10-08):** while checking why the Mac was slow, 189 `<defunct>` processes, all children of the cs process (up for 6 days).
- **Cause:** cs restarts itself in place (syscall.Exec, self-update). Helper commands (tmux, git, gh) still running at that moment lost their waiter, and each became a zombie when it ended. They use no RAM or CPU, only process slots.
- **Fix:** `reapLeftoverChildren` in `main.go` collects finished children at startup, before this run starts any of its own.
- **Guard:** `ps -Ao ppid,stat | awk '$2 ~ /Z/'` under cs's pid should be near 0 after a self-update.

### A closed project tab came straight back (abhyasika_turbo_repo, 2026-10-13)
- **Saw:** Ctrl+Space x, Enter on the confirm: the tab stayed ("abhyasika_turbo_repo · 6 free"). It had appeared on its own: a Claude session started in a GitHub runner checkout (`~/github-runners/runner-1/_work/abhyasika_turbo_repo/…`) opened a temporary tab.
- **Cause:** the close worked (`config.json` lost it), but the tab's docked terminal `csterm_abhyasika_turbo_repo` kept running, and every live session's folder gets a tab, a plain shell included.
- **Fix:** a docked terminal with no Claude in it no longer holds a tab open (`setExternalSessions`), and closing a tab also closes its docked terminal when it sits at the prompt (`termIdle`). Files: `app/app.go`, `app/terminal.go`.
- **Check:** usage.log "ui tabs=" drops by one after the close.

### A full batch PR got one issue too many (PR #2435 at 16 of 15, 2026-10-13)
- **Saw:** the header said 15/15, yet #2415 started. The user: "already 15 issues there, why did it start a new session?"
- **Cause:** room was counted only from the issues already linked on the PR. #2414 started at 14/15; 53s later its gai was still rewriting the PR body, so #2414 wasn't on the PR yet, the room was still 1 and #2415 started too.
- **Fix:** issues whose session is running but which aren't on a PR yet count as taking a place (`joining`, used by the background runner and the window's picker). Test: `TestJoiningTakesPRRoom`.
- **Also found:** the runner's activity.log lines were unreadable since 10 Oct. The window and the runner share the log; when one cut it back (renamed to `.old`), the other kept writing to the renamed file, then to a deleted one. Each process now reopens the log by path when the file there is a different one (`samePath`, checked every 500ms).

### Unticking in the issue picker skipped the issue for good (2026-10-13)
- **Saw:** the user wanted 3 or 4 of the ticked issues; unticking the rest with space marked each "(skipped)", so auto never offered them again and they looked gone. They asked for skip to be its own key, and for a quick way to start exactly 1, 2 or 6.
- **Fix:** space only ticks/unticks (ticking a skipped issue takes the skip back). `x` skips or un-skips; a skipped issue stays in the list, greyed, and can be ticked any time. Digits 1-9 tick the first N startable issues in list order (no session, not skipped) and untick the rest. File: `ui/issuepicker.go`. Test: `TestPickerTickSkipAndCount`.

## Test incidents (rules for live tests)

- A test pressed Enter on the user's real session and moved it. → Check the selected row on screen before any destructive key (CLAUDE.md "Live tests").
- A test `cd`'d into a temp repo; the zsh hook started gai-watch, which committed. → Use `git -C`, never `cd`.
- Tests wrote to the real activity.log. → `inTest` guard in `app/keylog.go`.
