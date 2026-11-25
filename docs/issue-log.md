# Issue log

Every problem the user reported with cs, how it was fixed, and what guards it.
**Before fixing a reported problem, search this file** (symptom words, file
names). If it is here, it came back: find out why the guard missed it, fix the
root cause, and add a "Came back" line to the entry instead of a new entry.
After every fix, add or update an entry.

Entry format: what the user saw → cause → fix → guard (test or log line) → files.

## Display

### Hindi text in one tile shifts borders and other tiles sideways
- **Saw (2026-11-01):** on rows with Hindi in the left tile, the tile borders moved a few columns and the tiles to the right moved with them, cutting text at the screen edge ("then sa").
- **Cause:** cs and Terminal.app count Devanagari mark widths differently, so the row drew wider than cs measured.
- **Fix:** tile lines with non-ASCII text end with an absolute column move, so padding, border and the next tile start in the right place (`pinLineEnd`, `ui/grid.go`). The few Hindi letters past the tile edge are hidden. The spaced-out letters are how Terminal.app draws Hindi; cs can't change that.
- **Guard:** `TestHindiRowsKeepBordersAligned`, `TestPinLineEndLeavesASCIIAlone` (`ui/grid_tile_test.go`), `TestPlaceOverlayKeepsDialogOnPinnedRow` (`ui/overlay/columnmove_test.go`).

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
- **Came back (2026-11-04): the ⏱ run time vanished while staging.** On a 47-column tile, `⋯ staging`, `opus[1m]` and a two-digit-minute time (`1h 44m`) came to one cell over the limit, and the time is the first thing dropped. Fix: the run time and the model both stay; when tight the stage tag shortens (`✓ DONE`, `⋯ stage`, `📖 guide`), then the model loses `[1m]` (`RenderTile`, `stageShort`, `ui/grid.go`). On a selected 47-column tile the top `◀ SELECTED` took the name and the time, leaving an empty colour box: it now shows only when the name keeps its room, and the bottom border still says SELECTED. Hiding the model instead was wrong: the user reads both. Guard: `TestTileAgeAndModelDuringStage`, every stage, selected and not, widest time and model.
### Issue tile text running off the right edge
- **Saw:** an issue tile (#2866) cut every line at the right border ("Issue comment: I posted an update saying the whole fix is now i"); the rest could not be seen.
- **Cause:** a queued issue session was fitted to its tile (65x30), then `StartIssueSession` killed it and made a new one at 200x50 under the same name. cs fits a name only once (`needsFit`), so it never resized the new one. activity.log: `resize … to 65x30 (tile)` then `half-empty tile … dropped=49` (a 50-row pane).
- **Fix:** the replacement session takes the queued session's size (`tmux display-message -t =name:`; the trailing `:` is needed, `=name` alone prints nothing). Same hole on Recently closed → reopen (new session, same name, 200x50): cs now forgets a name's fit when its session goes away (`logSessionChanges`, `app/app.go`).
- **Guard:** none automatic; `tmux list-panes -a -F '#{session_name} #{window_width}x#{window_height}'` shows any tile still at 200x50.
- **Files:** `session/issues.go` (StartIssueSession), `app/app.go`.

### Full-screen session stuck small, dots around it (2026-10-31)
- **Saw:** glitchgrab #617 opened full screen in a 271x66 terminal but drew at 203x60; the area right of and below it was filled with tmux dots.
- **Cause:** `tmux resize-window` (every tile fit) also sets the window to `window-size manual`, so a client that attaches later no longer resizes it. `Attach` kept the tile's size. activity.log: `resize cc_glitchgrab_i617 to 203x60 (tile)`.
- **Fix:** `ExternalSession.Attach` sets `window-size latest` before attaching and resizes back to the tile's size on detach, so `needsFit`'s record stays true.
- **Guard:** none automatic; `tmux list-clients -F '#{session_name} #{client_width}x#{client_height}'` against `list-panes -a` shows any mismatch. `csterm_*` docks are pinned the same way; their Terminal tab attach path is not changed.
- **Files:** `session/external.go` (Attach).

### Issue tile closing and starting again in a loop
- **Saw (2026-10-29):** glitchgrab #720's tile ran gai to "→ Creating PR…", vanished, came back and did it again, every ~20s.
- **Cause:** gai stopped at `gh pr create` (no open PR right after #713 was merged). Its tmux session closed with the error, so nobody saw it, and the runner treated the issue as free again and started it straight away: 60 starts from 11:40 to 12:01. It then worked on its own (PR #721 at 12:01:32), so gai's exact error wasn't seen.
- **Fix:** issue sessions keep their pane when gai exits (`remain-on-exit`), so the tile shows what gai said and still counts as busy. The runner logs gai's last lines ("its tile keeps what gai said: …") and doesn't start that issue again for 30 min after you close the tile (`gaiFailed`, `app/issuesdaemon.go`; `PaneDead`, `session/issues.go`). The issue picker's queue checks `PaneDead` too (`app/issues.go`). Once Claude works in the tile, remain-on-exit goes off again (`KeepOnExit`), so quitting Claude still closes the session.
- **Guard:** live, a failing command on `tmux -L cstest` with remain-on-exit kept its error on screen, `pane_dead`=1.

### Runner started issues a just-merged PR had closed (2026-11-14)
- **Saw:** practice-stack: right after PR #3244 merged (19:27:36 IST), 8 tiles opened on #3238–#3246, every Claude said "already fixed, released, no code change", and gai put all 8 on a new empty PR #3254.
- **Cause:** a race on GitHub's side. The list read at 19:27:45 no longer had #3244 as an open PR, and GitHub hadn't yet closed its issues (closed 19:27:41–53) or listed them on the merged PR. For those seconds they looked open and free ("PR #0 … 14 issue(s) to start"). At 19:28:03 the runner queued 8 of them from that list and started them one by one, never asking again.
- **Fix:** (1) after a project's own PR goes away, a new batch waits `prSettle` (2 min); the log says "its PR closed … ago; new batch waits". (2) Right before gai starts each queued issue, the runner asks GitHub whether it is still open (`session.IssueClosed`). A closed issue, or no answer, isn't started, and its "queued" tile is closed (`CloseQueuedIssue`). activity.log: "not starting #N …" (`app/issuesdaemon.go`).
- **Not covered:** an issue closed by hand while gai is already running in its tile.

### Dead issue tile blank, only "Pane is dead" (2026-11-04)
- **Saw:** abhyaiska #1103 and #1105 tiles: empty, with just `Pane is dead (status 1, …)` at the bottom. activity.log's "its tile keeps what gai said:" was empty for #1103.
- **Cause:** gai's pane was 50 rows when it exited; tmux wrote "Pane is dead" on row 50. cs then shrank the pane to its 29-row tile and tmux pushed the top 21 rows (gai's error with them) into the scroll-back. The real errors were network blips: #1103 `gh auth status` failed offline, which gai reports as "gh CLI not authenticated" (gh was logged in); #1105 `TLS handshake timeout` on the PR-body GraphQL write.
- **Fix:** `deadPaneContent`/`compactDead` (`app/grid.go`): a tile whose screen is blank above "Pane is dead" is rebuilt from the scroll-back with the gap dropped. The runner's log reads the scroll-back too (`PaneHistory`, `session/issues.go`).
- **Guard:** `TestCompactDead`; live, both real panes showed gai's full output and error.
- **Came back (2026-11-14): practice-stack #2944 blank tile, stuck 2 hours.** gai's login check couldn't reach GitHub at 10:06 (`GitHub unreachable — retry 3/3`, `error: could not reach GitHub (gh is logged in)`); Claude never started, no work lost. Two cs gaps: (1) in the 47-col tile tmux wrapped "Pane is dead (status 1, … 202" / "6)", so the last row didn't start with "Pane is dead" and the scroll-back rebuild was skipped: blank tile. (2) gai's own wording isn't gh's, so `gaiNetworkErr` didn't match and the runner never closed the tile; an open tile counts as busy. Fix: `deadLineAt` (`app/grid.go`) finds the dead line within the last 3 rows, used by `deadPaneContent`, `compactDead` and `gaiLastWords`; `gaiNetworkErr` also matches `could not reach github|github unreachable`. Guard: `TestCompactDead` wrapped case, `TestGaiCanRetryUnreachable`; live, after the self-update the tile showed the error and the runner closed and restarted #2944 at 12:12.

### Issue tile dead: "No commits between main and dev" (2026-10-31)
- **Saw:** glitchgrab #747, #750 and #715 stopped before Claude started, each soon after a PR was merged: `pull request create failed: GraphQL: No commits between main and dev`. Closing the tile and starting again worked, through the "Create an empty commit" question.
- **Cause:** a gai bug, not cs. Standing on `main` with an old `dev`, gai counted `dev`'s squash-merged commits, so it decided no empty commit was needed. It then moved `dev` onto `main` (added in gai-tools #74), leaving nothing to open a PR with. The move itself is why the second try worked.
- **Fix:** `gai pr` works out before counting whether the branch will be moved, and counts HEAD if so. `dev` gets its empty commit on the first try.
- **Guard:** temp repo with a fake `gh`, on `main` with a squash-merged `dev`: failed with the same error before, opens the PR after. activity.log: `gai issue #N stopped before Claude started`.
- **Files:** `gai` (gai-tools repo).

### Session limit stuck at 6 (2026-11-05)
- **Saw:** the strip read 8/6 running. The user wants 8 on busy days and 6 on others, for one project or for all, and had no way to set it.
- **Cause:** `autoMaxSessions` was a constant; the 8 were sessions started by hand. Nothing was broken.
- **Fix:** issue picker: `+`/`-` change the limit (1 to 12), `g` switches between this project and all projects. A project at the all-projects number drops its own and follows it. Saved in `~/.claude-squad/session-limits.json`, read by the window and the background runner. Lowering closes nothing.
- **Guard:** `TestSessionLimitOrder`, `TestSessionLimitStep`, `TestAutoLoopUsesTheLimit`, `TestPickerLimitKeys`; live in a test cs. activity.log: `session limit: <project> now N`. `autoHome` sets `CLAUDE_SQUAD_HOME`: without it a test wrote the user's real limits file.
- **Files:** `app/sessionlimit.go`, `app/issues.go`, `app/autoissues.go`, `app/issuesdaemon.go`, `app/progress.go`, `ui/issuepicker.go`.
- **Came back (same day): set to 8, the 8th slot stayed empty.** Not the limit: PR #3073 was at 15 of 15, and that reason showed once in the message line (activity.log: `PR #3073 has 15 issues (the limit)`). Fix: the strip keeps saying it beside the count, `7/8 running (PR full: merge it to start more)`, only while a slot is free, the PR has no room and auto is on (`prFull`, `app/progress.go`). Guard: `TestStripSaysWhyASlotIsEmpty`.

### Two PRs for one branch after a merge (2026-11-05)
- **Saw:** practise_stack had #3072 and #3073, both `dev → main`, right after #3068 was merged. #3072 had no issues, and #3029's tile sat at gai's "Multiple open PRs — select one".
- **Cause:** the background runner started #3022 while the window's ⌥N queue started #3028. Each runs one gai at a time, but only within itself. Both found no open PR and called `gh pr create` in the same second, and `gai pr` took no lock there, so GitHub's "already exists" refusal never fired. #3029 stalled for a second reason: since gai's list gained its last row, "Create a new PR" (gai-tools #80), the pane's last line no longer looked like a list row, so neither runner reached its PR pick. Any "Multiple open PRs" question sat until the 5-minute timeout.
- **Fix:** `gh pr create` runs under a lock per repo, branch and base (`_gai_pr_lock`). The second run waits, is refused and reuses the first PR. The two starters still run side by side; only the PR step is taken in turn. `gaiListLine` counts the "Create a new PR" row, in both runners.
- **Guard:** two parallel `gai pr` from two clones with a fake `gh` that has a check-then-create gap: 2 PRs in 3 of 3 runs before, 1 PR in every run after, no lock left behind. Screen line: `Another gai is writing the PR for 'dev' — waiting for it…`. `TestGaiListLine`; activity.log: `picked PR #N`.
- **Files:** `gai`, `CLAUDE.md`, `CHANGELOG.md`, `docs/usage.md` (gai-tools repo); `app/issues.go`, `app/issuesdaemon.go`.

### Issue tile dead at "Fetching #N context…"
- **Saw (2026-10-30):** practise_stack #2906's tile ended with `dial tcp 20.207.73.85:443: i/o timeout`, `error: could not fetch issue #2906`, `Pane is dead (status 1)`. PR steps had already worked; Claude never started.
- **Cause:** a network blip on gai's last `gh issue view`. gai tried once and gave up, so the whole run was lost. The tile stayed dead as designed (see "Issue tile closing and starting again in a loop").
- **Fix:** `_gai_gh_read` in gai-tools `gai` tries reads up to 4 times, waiting 5s/10s/15s between, plus gh's own timeouts, on network errors only; "not found" still fails at once. Used for the issue context, labels, open PR list and PR view. A longer outage ends with the runner closing the dead tile and starting the issue again (see the 2026-11-04 entry below).
- **Came back (2026-11-04): #2987 died twice during a GitHub incident (Pull Requests, Issues; from 15:19 IST).** `gh` said `non-200 OK status code: 502 Bad Gateway`. The retry filter only knew `HTTP 50[234]`, so this wording counted as permanent and the first try killed the tile. Fix: the filter also matches `status code: 50[234]` (gai-tools `gai`, `_gai_gh_read`). Checked with a fake `gh` giving the same text: 1 try and failed before, 3 tries and worked after, 4 tries and a clean stop when it stays down. Limit: the retries last 30s, so a longer outage still ends in a dead tile.
- **Came back (2026-11-04): dead tile hid its error and blocked the issue.** `#2987`'s tile stayed dead 1.5 hours after GitHub recovered, because an open tile counts as busy, and after a wheel scroll it said "Nothing on screen yet" (scrolled onto the blank rows of the dead pane). Fix: `captureTile` shows the dead pane's history when scrolled onto blank rows (`deadPaneHistory`, `app/grid.go`); the runner closes an issue tile whose gai died on a GitHub or network error (`closeNetworkDeadTile`, `app/issuesdaemon.go`; its `gaiNetworkErr` list mirrors `_gai_gh_read`), and the 30-min wait still applies. Other errors (login, a PR gai can't create, Ollama down) stay on screen for the user, and so does a tile whose gai already printed `✓ Attached`, because the runner skips issues on a PR and closing would lose it (`gaiCanRetry`). Before closing it asks GitHub (`issueNotOnPR`): `#2987` went onto PR #3060 during `#2974`'s gai run after its tile died, the first build closed the tile anyway, and the issue had no session until started by hand. A tile on a PR, or with GitHub not answering, stays open. In projects with auto off nothing is closed.
- **Came back (2026-11-18): practise_stack #3327 died after `✓ Attached #3327 to PR #3329`.** The PR body write (`gh pr edit`) hit `net/http: TLS handshake timeout`; gai ran under `set -e` and the write had no retry, so Claude never started. The tile was kept by design (attached issues are skipped by the runner), so the issue sat on the PR with no session. Fix: (1) gai-tools `gai`, `_gai_pr_write`: the edit goes through `_gai_gh_read` (4 tries on network errors); if the rewrite still fails it warns and goes on to the Claude session, since the issue is already attached (the attach write still stops the run). (2) cs: a dead tile that shows `✓ Attached` and a network error, whose issue GitHub lists on a PR, is taken off the PR in the background with `gai issue N --remove` (`detachDeadTile`, `removeFromPR`, `app/issuesdaemon.go`, `app/unlink.go`). Once GitHub shows it off, the tile closes and the runner starts the issue again (after the usual 30-min wait). A removal that fails is tried again at the next 5-min check. Guard: `TestDeadAfterAttachComesOffPR`; fake `gh` whose `pr edit` times out: rewrite warns and carries on after 4 tries, attach stops.
- **Guard:** fake `gh` run: flaky recovers on try 3, down fails after 4, not-found fails after 1.
- **Same words, other cause (2026-10-30):** #2914 ended with `Could not resolve to an issue` then `could not fetch issue`: it was deleted on GitHub while gai ran (`gh api repos/<repo>/issues/<n>` gives 410). Not a network problem; `gai issue <n> --remove` clears its dead `Closes` line.
- **Files:** `gai` (gai-tools repo).

### Issue tile stuck on "Nothing on screen yet"
- **Saw:** queued issue tiles (#2853, #2855) never started; blank forever.
- **Cause:** a queued issue was started with `tmux respawn-pane -k`, which failed on macOS (`fork failed: Device not configured`) after killing the queued screen, leaving a pane with no process (`pane_pid -1`). The queued flag was already cleared, so cs never retried. Respawning that pane again segfaults tmux 3.7c (`input_free` in `spawn_pane`) and kills every session: never run `respawn-pane` on a `pane_pid -1` pane; `kill-session` it.
- **Fix:** `StartIssueSession` (`session/issues.go`) kills the queued session and opens a new one with `new-session`, retried 3 times.
- **Guard:** activity.log "could not start #N … respawn pane failed" no longer possible; "could not start issue #N" means new-session failed 3 times.

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

### Clicking a sent image opened a page of random images
- **Saw (2026-10-27):** in practice-stack #2811, clicking a "› [image] /private/tmp/…" line opened "images Claude sent" with 200 images of the whole project, not the clicked one.
- **Cause:** two gaps in path joining. The size tail was glued to the path with no space ("…-8871-70B)"), so it wasn't cut and broke the path there; and the file name had spaces ("Settings sub-menu scroll spy (#2811) … (failed).png"), which the path match stops at. The click resolved to the project's tmp folder, and a folder opens as a page of every image under it.
- **Fix:** `sizeTailRe` also cuts a glued "B)"; after the longest existing folder, `descend` walks on into the entry whose name comes next in the text, comparing without spaces (`app/openpath.go`).
- **Guard:** `TestPathAtGluedSizeAndSpacedName`.

### Wheel scrolling recalled Claude's prompt history / didn't scroll
- **Saw:** the wheel typed old prompts; `claude attach` tiles didn't scroll; slow wheel notches leaked as arrow keys.
- **Fix:** burst detection (10ms gap, 300ms sticky window); SGR wheel events to alternate-screen panes, tmux copy-mode otherwise (`app/livepane.go`).
- **Guard:** `app/livepane_test.go`; keys.log shows "wheel scroll …".
- **Came back (2026-10-13): a scrolled tile went blank at the bottom, more the further back.** Back at the live bottom it was fine. The scrolled capture took exactly the pane's rows with `-J`, which joins history lines wrapped when the window was wider, so 54 rows came back as 35 lines (100 back) or 28 (1000 back). Now it captures from two screens higher and keeps the last pane-height lines (`CapturePaneContent`, `session/tmux/tmux.go`). The cursor block also showed in the middle of old text while scrolled; it is no longer drawn then (`app/grid.go`).
- **Guard:** `TestScrolledCaptureFillsTile` (live, private tmux server; old code: 8 of 20 rows).
- **Came back (2026-11-14): a view-only tile didn't scroll at all.** practice-stack #3008 was moved to its own terminal window (`w`), so cs showed it as "view only": cs draws the conversation itself, there is no tmux pane. The wheel only scrolls tmux panes; with no pane it returned at once (`target == ""`, `app/mouseclick.go`), so the wheel did nothing. Fix: each view-only tile keeps how far it is scrolled back (`viewScroll`, `app/viewonlyscroll.go`); the wheel moves it a line per notch, stops at the top, and the tile's status says `view only · ↑N` while scrolled. While scrolled back, new lines arriving don't move the text being read (`viewPos`). Guard: `TestViewOnlyWindow`, `TestViewOnlyWindowStaysPut`.
- **Came back (2026-11-16): scrolled up to read while Claude was writing, the text slid down to the bottom.** monthly-bill-breakup tile, scrolled back at 08:49:26 while busy. tmux counts copy mode's scroll position from the bottom, so each new line moved the view down one line; a long reply carried it all the way down. Fix: while a tile is scrolled back, each capture compares the pane's history size with the last one and scrolls copy mode up by the lines added (`scrollHold`, `scrolledRange`, `session/tmux/tmux.go`). Not covered: once history hits tmux's `history-limit` (2000) its size stops growing, so the slide comes back there. Guard: `TestScrolledViewHoldsWhileOutputGrows` (own tmux server; fails without the fix: top row "line 152" → "line 184").

### Not sure which tile is selected; focus jumps away before ⌃Space W
- **Saw (2026-10-05):** to close the tile in row 1, column 3 the user clicked it three times, because selection kept moving and the selected tile was hard to spot.
- **Cause:** auto-jump to a new question only waited for 3s without a *key*; clicks didn't count. activity.log 08:15:52: click on #2268, auto-jump to #2270 89ms later; 08:13:42 the same with #2265 → #2264. The selected tile differed only by a white frame, and long names cut off the issue number.
- **Fix:** a click counts as input. A tile picked by hand (click or arrow) holds focus for 30s (`pickHold`); a question meanwhile shows "❓ X needs you · press ! to jump there" instead of moving. When it does jump, it says "❓ moved to X". The selected tile has a white "▶" name tag and "◀ SELECTED" on its top border. Cut names keep their "#NNNN". The close question names the position: "Close X (row 1, column 3)?" (`app/grid.go`, `app/mouseclick.go`, `app/terminal.go`, `ui/grid.go`).
- **Guard:** activity.log "question …: no auto-jump, tile picked Ns ago"; `TestCutKeepingIssue` (`ui/grid_test.go`).
- **Follow-up (2026-10-13):** the label only sat on the top border, but the eye ends at the bottom right, next to Claude's own name tag above the prompt. The selected tile now carries "◀ SELECTED" on the right of its bottom border too, same size tile (`ui/grid.go` `selectedTag`; guard `TestSelectedTileBottomLabel`).

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
- **Came back (2026-10-16): copied text kept Claude's line breaks and indent.** A scratchpad path that wrapped over 3 rows pasted as 3 lines, each starting with 2 spaces. Cause: the copy took screen rows as shown. Fix: a row filled to the pane's right edge carries on into the next row, so the two are joined (no space when a word or path was cut, one space for prose), and the indent all copied lines share is dropped; code keeps its indent relative to the first line (`selectedText`, `joinWrapped`, `app/selection.go`). Guard: `TestSelectedTextJoinsWrappedRows`, `TestSelectedTextKeepsCodeIndent`; live drag on `tmux -L cstest` pasted the path as one line.
- **Came back (2026-10-22): a reply drafted as a quote pasted into WhatsApp with "▎" bars.** Claude wrote the message as markdown `> …`; Claude Code draws a `▎` (U+258E) in front of every row of it. The copy kept every bar (blank rows too), one mid-sentence where a full row was joined ("card ▎ lihila"), and a line break where a word moved down ("mi" / "▎ software": the next row's first word looked like "▎", so the word-wrap check never fired). Fix: `unquote` takes the bar out of each row before joining; its columns still count for the wrap checks (`selRow.bar`). Guard: `TestSelectedTextDropsQuoteBars`.
- **Came back (same day): a command still pasted on 2 lines.** `! printf … >` then the `.env.local` path on the next row. Claude moved the whole path down because it didn't fit after ">", so the first row stopped short of the edge and wasn't joined. Now a row also goes on into the next when the next row's first word couldn't have fit after it; rows starting with ⏺ ⎿ ❯, a bullet or "1." stay separate, and a word longer than a whole row on its own row (a path after "is:") starts a new line (`wraps`, `app/selection.go`). Guard: `TestSelectedTextJoinsWordWrappedCommand`, `TestSelectedTextKeepsListItems`; live drag pasted the command as one line.
- **Came back (2026-10-17): a WhatsApp message pasted with 2 spaces before every line but the first.** The drag started on the "H" of "Hi Chirag,", so that line had no spaces and the shared indent came out 0. Now the indent is measured on each line's whole row on screen, and only spaces left of that column are dropped (`joinWrapped`, `app/selection.go`). Guard: `TestSelectedTextDropsIndentWhenDragStartsAtText`; live drag from "Hi" to "Team PracticeStacks" pasted with no leading spaces.
- **Came back (2026-10-28): "I can select other text but not that command".** A `! /private/tmp/…/check-pgbouncer.sh` line in glitchgrab #709. activity.log: press at 19:15:57.592, release 2.6s later with no drag between, so cs took it as a click and opened the file. The terminal sent no motion events during the hold; Force Click is on (`com.apple.trackpad.forceClick` = 1), and a firm press on a path opens macOS Look Up, which swallows the drag. **Proven** by the user's next screenshot: Look Up's "No Results Found · Search with Google" popup over a yellow-highlighted `pgbouncer.sh`. Three-finger tap is off, so the firm press (Force Click) is the trigger. Real fix is the Mac setting: System Settings › Trackpad › Point & Click › "Look up & data detectors" off (or set to three-finger tap). A live drag of the same text on `tmux -L cstest` copied it as one line, so cs's selection works when the drag arrives. Fix: a press held 700ms+ without moving is no longer a click. Nothing opens, and the footer says "held without moving: nothing selected · a firm press opens macOS Look Up …" and names the setting (`heldTooLong`, `app/selection.go`, `app/mouseclick.go`). activity.log: "press held Ns without a drag". Guard: live, a 1.2s hold showed the hint, and a quick click still opened.
- **Still failing after that (same evening):** Force Click stayed on, so the drag still never arrived. Clicks do reach cs even when Look Up fires. Fix: a click (quick or held) on a `! …` command in a selected tile copies the whole command, joined across its wrapped rows; footer "copied the command · ⌘V to paste" (`commandAt`, `app/commandclick.go`). Guard: `TestCommandAtWrapped` (the #709 rows); live, a quick click and a 1.5s hold both copied it.
- **Came back (2026-10-29, practice-stack #2884): a clicked command pasted as 2 lines.** `… psql "$(…)" -f` then the `.sql` path on the next row: Claude moved the long path down to a row of its own, and the join rule "a word longer than a row on its own row starts a new line" (meant for "The file is:" + path) split the command; zsh ran the path alone ("permission denied"). Fix: a `! …` command is one shell line. A click joins every row down to a blank row, a list item or a row indented less than the `!` (`commandAt`). A drag joins rows onto a command line too (`inCommand`, `app/selection.go`). Guard: `TestCommandAtPathOnItsOwnRow` (click and drag).
- **Added (2026-10-29): output taller than the tile couldn't be copied.** A psql table in the dock: a drag stopped at the panel's top. Now dragging past a tile's (or the dock's) top or bottom edge scrolls it a line every 60ms and the selection keeps growing. Rows that scroll out are kept by laying each new picture over the rows already seen, matched by text (tiles are captured with -J and topped up from history, so a tile row isn't a pane row) (`app/dragscroll.go`). activity.log: "drag past the edge: scrolling <session>". Guard: `TestOverlayAt`, `TestDragScrollCopiesRowsOutOfView`; live, a 2.5s hold above the dock copied lines 14–60 with no gap.
- **Same day: the copied psql table had spaces inside words** ("PROPOSAL_SCH EDULE", "prop osalId"), some wrapped rows not joined back, and separate lines glued together. A shell cuts long lines at the exact column, and the screen rules were made for Claude's word wrapping. Now, when tmux wrapped any of the selected rows, the copy finds them among the pane's rows rebuilt from `capture-pane -J` and copies those lines as printed (`tmuxLines`, `app/tmuxlines.go`). Claude's own rows (tmux wrapped none) still use the screen rules. Guard: `TestLinesForShellWrap`; live, 25 rows of 120 characters in a 33-column dock copied exactly as printed.
- **Guard:** activity.log "copied a N-line selection"; check list in `docs/tiles-mouse-debugging.md`.

### Issue picker: r "doesn't refresh"

- **Seen:** pressing r in the issue picker (⌃Space N) looked like nothing happened.
- **Cause:** it did reload (`issue picker: refresh` in activity.log, `gh` answers in under 1s), but the list came back the same, and the "⟳ refreshing…" footer was on screen for less than a second, so nothing showed it worked.
- **Fix:** after a reload the footer says `✓ refreshed 12:08:14: 4 listed` plus `, N new` when issues were added (`ui/issuepicker.go` `refreshed`); activity.log logs `issue picker: loaded <project>, N issues`. Test: `TestPickerRefreshShowsResult`.
- **Came back (same day, 12:13):** footer stuck on "⟳ refreshing…" for 25s. A load took 8–25s: `gh pr list --state merged --limit 300` alone is ~7s on GitHub, then one `gh api` per issue with a merged PR ran one after another, and each extra r started another load in parallel (8 at once). Fixed: merged PRs kept per repo (`mergedCache`, session/issues.go), later loads read only the newest 30 and re-read all 300 every 30 min; merged and open lists load together; the merged-PR checks run 8 at a time; r during a load waits for it (new load only after 30s); footer counts seconds ("refreshing from GitHub… 5s"). practice-stack: 9.3s first load, then 1.8s and 2.2s, same 26 issues.

### Click on another tile did nothing; laptop at load average 225
- **Saw (2026-10-17):** clicking the left tile (#2415) left "◀ SELECTED" on the right one (#2594). The Mac crawled.
- **Cause:** not proven for the click. activity.log had no trace of it: no tile change, nothing. What was proven: cs started a server listing (`ps`, `lsof`) every 3s without waiting for the last one, and under heavy memory pressure (ffmpeg export, CI job, dev server) each `ps` took minutes. 35 were running from cs (42 more from a test cs), load average 225, UI stalls. Clicks were never logged, so a lost click couldn't be told from one that hit the already-selected tile.
- **Fix:** only one server listing runs at a time (`serversLoading`, `app/app.go`); every `ps`/`lsof` call is killed after 5s (`session.ProcOutput`); each process is listed once, not once per port. Every click on the grid now logs where it landed: `click X,Y: tile N of M, <session> (already selected=…)` or `click X,Y: no tile there (…)` (`app/mouseclick.go`). Load fell from 225 to 23 once the pile cleared.
- **Came back (same evening, after the fix): clicks still did nothing.** activity.log: the last mouse event of any kind (click, wheel, drag) was at 17:17:40; none after that, through 5 restarts, each of which turns mouse mode back on. The terminal had stopped sending mouse events (Terminal › View › Allow Mouse Reporting, ⌘R, toggles it). cs now logs `mouse: first event from the terminal this run` once per run. Images didn't open either (same cause: the click never reached cs; the grey bar on the line was Terminal's own selection). When no mouse event has come 2 minutes after cs starts, the key bar now says `⌘R clicks do nothing? turn on Terminal's mouse reporting`; the first click puts `drag select + copy` back (`mouseSilent`, `app/leader.go`).
- **Next morning, still no clicks:** 12 hours on the new build, not one `mouse:` line while keys kept working. Writing the mouse-on sequence straight to cs's tty (`printf '\033[?1002h\033[?1006h' > /dev/ttys011`) and clicking still logged nothing, so cs's own mouse mode was not the problem: Terminal itself wasn't reporting. Fix is in Terminal (View › Allow Mouse Reporting, or a new window).
- **Wheel showed old text:** with reporting off, the wheel scrolled Terminal's own history, so text from before cs (`wrote logs to …`, `^[OB`) appeared above the tiles. cs now clears Terminal's scrollback (`\x1b[3J`) at start (`app/app.go` Run), so nothing old shows.
- **Proof it was the window:** a fresh Terminal window running a 10-line read of the mouse bytes got `\E[<0;1…` on the first click, while the cs window (ttys011) never sent any, even after the mouse-on sequence was written to it. The window stays stuck: start cs in a new window.
- **The moment it stopped (17:17:40):** the last mouse event was a wheel scroll on a glitchgrab tile while the laptop was at load 225; the same second logged `UI FLOOD: key 261` (wheel bytes read as keys, sent to that tile as arrow keys). Not one mouse byte from that window after. Moved to a new window 08:59 next day: `click 94,30: tile 1 of 1` right away. What switched the old window off is not provable from cs (⌘R, or Terminal itself under the load).
- **Servers panel under load:** a killed `lsof` (5s limit) returned "nothing listens", so the Servers panel emptied. `ProcOutput` now says when it timed out; `ListServers` returns that error and the panel keeps its last list (`servers: list failed, keeping the last one` in activity.log).
- **Next time:** grep activity.log for `mouse: first event` and `click`. No `mouse:` line since the last `start` means the terminal sends nothing; a `click` line names the tile hit.

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

### Add project refused a new empty folder (2026-11-15)
- **Saw:** + add project → picked `coding-line/global_career_guidance` → Choose; nothing was added. activity.log: "… is not a git repository" (19:06:49, 19:07:09).
- **Cause:** the folder was new and empty, with no `git init`; cs only added git repositories.
- **Fix:** a picked folder that isn't a repository gets `git init -b main` and is added (`initProjectFolder`, `app/folderdialog.go`). It still refuses, and says why: the home folder and its Desktop, Documents, Downloads, Library, Movies, Music, Pictures, Public; and a folder that holds other repositories (e.g. `coding-line` itself). The new repository has no commits yet.
- **Guard:** `TestInitProjectFolder`.

### Every tile gone after a laptop restart (2026-11-17)
- **Saw:** after a reboot, cs opened with every project empty; practice-stack had 6 tiles (5 issues + a general one), 11 across 4 projects. No ↺ reopen for them either.
- **Cause:** tiles live in the tmux server and a reboot ends it. cs keeps no list of outside tiles (`state.json` instances `[]`), and ↺ reopen only lists ⌃Space W closes. Conversations were all safe on disk.
- **Fix:** cs saves the open tmux Claude tiles with the tmux server's `pid start_time` in `~/.claude-squad/live-tiles.json` on each refresh. When the server is a new one, every saved tile not open now (and not closed by the user after the save) is resumed with `claude --resume`, issue tiles under their `cc_<folder>_i<N>` name, and the message line says "Reopened N tile(s) closed by a restart" (`noteLiveTiles`, `app/rebootrestore.go`; activity.log `lost tiles:`). Only tiles in this tmux server count, so a test cs never picks up the user's real `claude agents` sessions. Not covered: a turn that was mid-reply restarts idle; type `continue` in it.
- **Guard:** `TestCurrentTilesKeepsKnownConversation`, `TestLostTilesSkipsOpenAndUserClosed`; live on `tmux -L cstest` with a stand-in claude (2 tiles reopened with the right `--resume` ids).

### Clicking a sent image did nothing after a restart (2026-11-17)
- **Saw:** #3306 tile, clicks on its `[image] /private/tmp/claude-501/…/scratchpad/*.png` lines opened nothing, no message.
- **Cause:** macOS empties `/private/tmp` on every restart, and Claude's scratchpad (where screenshots are saved) is there. The files are gone; `SentFiles` drops missing files, so the click matched nothing and stayed silent.
- **Fix:** a click on a sent-file block (`›` / `[image]` rows) whose file is gone says "<name> is gone: it was in /tmp, which a restart empties. Ask Claude in this tile to take it again" (`goneImageAt`, `app/mouseclick.go`).
- **Came back (same hour): Finder opened an empty scratchpad.** The resumed Claude made its scratchpad folder again (empty), so the click resolved to that folder and opened it. Now a sent-image block that resolves only to a folder, with no sent file left in it, gets the same "is gone" message (`clickAt`, `app/mouseclick.go`). The images still show on claude.ai (Remote Control) because SendUserFile uploaded them; cs only has the local files.
- **Guard:** `TestGoneImageClickSaysWhy`.

## Speed

### Lag while typing
- **Cause:** tmux captures ran on the UI thread; logs written per key; spinner ticks.
- **Fix:** captures in a background goroutine, buffered logs, cached grid render, no spinner ticks.
- **Guard:** `~/.claude-squad/usage.log` (30s CPU/render counters).

### cs at ~11% CPU while nobody types
- **Saw (2026-11-11):** `cs-fork` steady at 12–15% CPU in `top`; usage.log showed ~340 renders/30s (11/s, ~5ms each) in the idle list view, ~20% while typing.
- **Cause:** the preview tick fired every 400ms forever once idle, and each tick fans out into ~4 messages (preview, grid capture, dock capture, metadata), each a full-screen View; most of that time is lipgloss/ansi width measuring.
- **Fix:** after 10s without a key, scroll or drag the tick waits 1s (`previewIdleAfter`, `previewInterval`, `app/app.go`). `markActive` (every `lastKey` site: keys, wheel, dock wheel, drag scroll) wakes a sleeping tick at once (`previewWait`), so the first key after a pause still echoes without the 1s wait. Typing speeds (50ms/100ms) are unchanged.
- **Result:** idle previewTickMsg 75 → 30 per 30s, renders ~340 → ~210 per 30s. What's left idle: the 1s metadata refresh (`claude agents`, session status; kept for quick status changes) and the 2s source-control tick.
- **Guard:** `TestPreviewIntervalSlowsWhenIdle`, `TestMarkActiveWakesIdleTick` (`app/previewidle_test.go`). usage.log now ends each line with `msgs <type>:<count> …`, the busiest Update() message types in those 30s (`countMsg`, `app/usage.go`); read it first when renders look high.

### Closing a terminal left its tile up for a second
- **Cause:** after the kill, the session list was reloaded (`claude agents`) before the tile went.
- **Fix:** the tile is dropped at once and kept out for 5s; a capture started before the close is discarded (`app/terminal.go`, `app/app.go`, `app/grid.go`).

## Background runner

### Auto pickup started the upstream project's issues in a fork
- **Saw (2026-11-21):** with auto pickup on for claude-squad, cs started #56 "Enable multiple git repos" and #156 "update git stats outside main goroutine": smtg-ai's issues, not the user's.
- **Cause:** gh calls ran without a repo. With `origin` and `upstream` remotes gh picks `upstream` first, and the fork has issues turned off, so the list was upstream's.
- **Fix:** every issue/PR gh call (and `gh pr view <n>` for the batch PR) runs through `git.GhCmd`, which sets `GH_REPO` to origin's owner/repo. "has disabled issues" now means no issues, never a fallback.
- **Guard:** `TestOriginRepoRe` (`session/git/gh_test.go`) for the remote parsing. Live check: `GH_REPO=WebNaresh/claude-squad gh issue list` says "disabled issues"; the claude-squad tab shows no issues.
- **Files:** `session/git/gh.go`, `session/git/sync.go`, `session/issues.go`, `app/issuecomments.go`, `app/batchbase.go`.

### Switching Claude subscription (/login) left every tile stopped
- **Saw (2026-10-05):** after `/login` to another subscription, every tile showed "Remote Control disconnected — signed-in claude.ai account or organization changed" and `/rc failed`. Tiles stopped on the old account's usage limit stayed stopped until its reset time. The user had to type `/remote-control` and `continue` in each tile by hand.
- **Cause:** the runner only resumed a limit after its reset time, and nothing turned Remote Control back on.
- **Fix:** when that notice is the last step on a tile with an empty prompt, the runner types `/remote-control`. If the tile was stopped on a limit, it also types `continue` straight away, because the new account has its own limit. It does this once per tile per account (`accountUuid/organizationUuid` from `~/.claude.json`). It also counts "⏺" as a step, not only "●" (`app/limitresume.go`).
- **Came back (same day): `/stage` couldn't upload screenshots.** The claude.ai connectors (Glitchgrab) stayed signed in to the old account, so #2307 asked how to attach its shots. `/reload-plugins` reconnects them; the runner now types it right after `/remote-control`.
- **Came back (2026-10-13): "it continues, but a little late".** Limit "resets 6pm": continue typed at 18:00:42. A 30s safety margin plus the 15s scan made it up to 45s late. Now 5s after the reset: the runner looks again right at the reset time instead of the next scan. A continue sent before the server's reset (Claude still showing the same limit) is sent once more 90s after the reset; before, it was never retried (`limitMargin`, `limitRetryAfter`, `app/limitresume.go`; `app/issuesdaemon.go`). Guard: `TestResumeAfterLimitTiming` (live, private tmux server).
- **Came back (2026-10-16): #2568 never continued.** Limit "resets 5:10pm": three tiles got continue at 17:10:05, #2568 didn't. At 16:57 "co" had been typed into its prompt and never sent; the runner skips any tile with text in its prompt so it never overwrites a draft. Fix: text that is the start of "continue" ("c", "co", "cont"…) is finished and sent; any other text is still left alone and logged once as "not continued: unsent text in its prompt" (`continueRest`, `promptText`, `app/limitresume.go`, `app/livepane.go`). Guard: `TestContinueRest`; live, #2568 got continue at 17:19:33 after the update.
- **Came back (2026-10-28): nothing continued after "resets 5:30pm".** Two causes: Claude now says "You've hit your **5-hour** limit", and the regex only knew session/usage/weekly; and in a 65-column tile the notice wraps, so "5:30pm" sat on the next line and the notice line alone never matched. Fix: any "hit your <word> limit", and the notice is read joined across its wrapped lines down to "/upgrade" (`limitNotice`, `app/limitresume.go`). Guard: `TestLimitResetWrapped`; live, 11 tiles got continue at 17:33:49 right after the update.
- **Came back (2026-11-04): `/login` updated only the wide tiles.** 4 tiles at 99 columns got `/remote-control`, 8 tiles at 47 columns didn't. Claude wraps the notice itself, so `-J` can't join it: "signed-in" sat on the next line and the one-line match missed. Same class as the wrapped limit notice of 2026-10-28. Fix: `isAccountSwitch` reads the notice across its wrapped lines. Found while fixing it: `❯ /login` counted as a sent message, so the login tile looked unstopped (slash commands no longer count, `limitIsLast`); every cs restart retyped `/remote-control`, which prints no step and the done-list is in memory (`❯ /remote-control` below the notice now means handled, `switchedAccount`); the notice quoted in tool output triggered the tile that read it (steps must start at column 0); a second `/login` pushed the notice off a 29-row tile (`⎿ Login successful` also counts). Not covered: a tile mid-turn whose notice scrolled off before it went idle (#2981) never sees it. Guard: `TestAccountSwitch`.
- **Came back (2026-11-04, same hour): Remote Control back, but no tile continued.** Claude's new wrap-up limit prints no "hit your limit" line: the turn ends normally and only the footer says "⚠ Usage limit reached · limit resets 3:20pm". Fix: the footer below the input box counts, joined across wrapped lines. Account changed in that tile's scroll-back → continue now; else continue at the reset. Skipped: tiles marked "✓ done", tiles mid-turn. Once per tile, read from the scroll-back ("❯ continue" below the notice), so a restart can't repeat it (`wrapUpLimit`, `continuedAfterSwitch`, `resumeWrapUp`, `app/limitresume.go`). Not covered: the tile /login ran in loses its footer, so it needs its own continue; a tile that never continued after an old switch gets one early continue on its next limit (refused, then the reset path carries on); limits shown with a date ("resets Nov 8") have no reset time to read. Guard: `TestWrapUpLimit`, `TestContinuedAfterSwitch`, `TestWrapUpDue`.
- **Came back (2026-11-05): after /login some tiles got no /reload-plugins.** Three misses. #3043 was on a question at the switch, and once it went on working the notice was no longer the last step, so it was never handled. glitchgrab #757 got a second notice after a handled one, but the runner kept one entry per account, so it skipped it, and the wrap-up rule typed a bare `continue`. The claude-squad tile was logged as typed at 12:47:22, but nothing landed. Fix: a tile is switched while its scroll-back has a notice (or `Login successful`) with no `❯ /reload-plugins` below it, whatever came after. It is handled once Claude stops and the prompt is empty, never into a question. Nothing else is typed until then, so `continue` always comes after `/reload-plugins`. Text that didn't land is typed again after 90s, 3 tries at most, then logged as "still not on; type it in that tile" (`remoteControlOff`, `rcDue`, `app/limitresume.go`). Guard: `TestAccountSwitch`, `TestReloadBeforeContinue`, `TestRCRetry`.
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
- **Came back (2026-10-13, #2441): "why is it adding colour in between, I said on start only".** Third time the late "/color purple" row showed after the first reply. Root cause: `gai` ran `claude "<issue prompt>"`, so Claude was busy from its first second and any `/color` came after. Fixed in gai-tools (`gai`, `_gai_issue_claude_session`): in a tmux pane Claude starts with `/color <c>` as its first message (colour = palette[issue % 8], the same order and start as `pickFor`), then gai pastes the issue prompt once "Session color set" is on screen (45s fallback). The note now prints at the top, before any work. `GAI_FIRST_COLOR=no` turns it off. **Not live at first:** `~/.local/bin/gai` is a copy made by gai-tools' `install.sh`, not a link, so #2444 (14:30) still used the old launcher and showed the late "/color pink"; after editing `gai`, copy it to `~/.local/bin/gai` (done 14:46). Sessions started before that keep the old behaviour. The daemon's later `/color` is skipped (the session already has an `agent-color`). On a clash with another session of the project, cs's tile still shows its own pick until Claude's colour is read, then switches once.
- **Came back (2026-10-19): "see yellow get repeated" (#2603 and #2611 both yellow).** gai's first `/color` was palette[issue % 8], so issues 8 apart always clash (2603 % 8 = 2611 % 8 = 3). cs had picked purple for #2611 (`tile:cc_practice-stack_i2611`), but Claude's `agent-color` yellow wins on the tile. Fixed in gai-tools (`gai`): it now reads cs's pick `tile:<tmux session>` from `session-colors.json`; with no pick yet, it takes the first colour from issue % 8 that no other `tile:` of the project holds. Copied to `~/.local/bin/gai`. #2611 keeps yellow (a colour is only set at the start).
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

### macOS kept asking App Management for cs-fork (2026-11-01)
- **Saw:** "cs-fork was prevented from modifying apps on your Mac", again and again; the toggle in Privacy & Security never stuck.
- **Cause:** each rebuild had a new ad-hoc signature (`Identifier=a.out`), and macOS keeps privacy choices per signature, so every build was a new app. The banner comes from something cs started (macOS blames the parent; likely glitchrecord's Electron, not confirmed).
- **Fix:** builds are signed with a local certificate (`scripts/cs-sign`, called by `~/.local/bin/cs` and `app/selfupdate.go`). Key and its keychain live in `~/.claude-squad/signing` (10 years; no login keychain, so no prompts). No keychain = old ad-hoc behaviour.
- **Guard:** `codesign -dr - ~/.local/bin/cs-fork` must show `identifier "cs-fork" and certificate leaf = H"21ac6c71…"`, the same after a rebuild.

### Rarely used projects filled the tab row (2026-11-01)
- **Saw:** all 9 tabs always shown, including monthly-bill-breakup (used monthly) and others with nothing going on. Closing a tab was no answer: it leaves `open_projects`, so its issues stop being read and it can't come back on a new issue.
- **Fix:** quiet projects (no open non-skipped issue, no session, no Claude in 7 days) sit behind `⋯ N more`; click it or ⌃Space R. They stay in `open_projects`. A project with auto off still has its issues read, for this. Files: `app/tabhide.go`, `ui/projecttabs.go`.
- **Guard:** `TestProjectTabs_QuietTabsLeaveTheRow`, `TestReadQuietTabs`, `TestTabPickerFirstRowMatchesRender`.

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

### PR #2435 showed 67 issues, GitHub showed 81 (2026-10-14)
- **Saw:** the strip said `PR #2435 67/15 issues · 33 open`; GitHub's PR list said 81 linked issues. 67 + 33 = exactly 100.
- **Cause:** cs read `gh issue list --limit 100`, newest first. practice-stack had 117 open issues, so the 17 oldest (#2412 and older) were never seen: 14 of them were on the PR (81 - 67), 3 were free (33 open should have been 36). The count is built from that list, not from the PR.
- **Fix:** `issueListLimit` is 1000. The merged-PR read (used to tell an issue a merged PR already fixed) goes back 300 PRs instead of 100, so an old issue newly visible doesn't show as free. File: `session/issues.go`.
- **Guard:** with the project at more than 100 open issues, strip count for the PR = `closingIssuesReferences` total on GitHub (`gh pr view <n> --json closingIssuesReferences`).

### Bottom bar lost the branch's PR during a GitHub blip (2026-11-18)
- **Saw:** practice-stack on `dev`, PR #3337 open, but the bottom bar showed only `⎇ dev ↓0 ↑0`, no `PR #3337`.
- **Cause:** `gh pr view` couldn't reach GitHub (`error connecting to api.github.com`, TLS timeouts on and off that morning). `CurrentPR` returned "no PR" for any gh error, and `applyPR` replaced the known PR with it, so every failed 10s refresh hid the chip until a lookup got through. The strip's PR and the tab's checks chip had the same gap.
- **Fix:** `CurrentPR`/`PRByNumber` also say when gh failed to ask (`failed`; anything but gh's "no pull requests found"), and `applyPR`, `applyStripPR`, `applyTabChecks` keep what they showed on a failed lookup (`session/git/sync.go`, `app/statusbar.go`). A real "no PR" or a branch switch still clears it. Not covered: a cs that starts while GitHub is down has nothing to keep, so the chip appears with the first lookup that works.
- **Guard:** `TestPRKeptWhenGitHubFails`, `TestNoPR`.

### PR checks showed only at the bottom, not on the top strip (2026-10-14)
- **Saw:** the bottom bar had `PR #65 ● 2 of 2 running`; the top strip (`PR #2435 … 81/15 issues`) said nothing about the PR's checks, and the two can be different PRs (checked-out branch vs the batch PR issues gather on).
- **Fix:** the strip reads the checks of its own PR (`PRByNumber`, every 10s with the Source Control refresh) and draws them after the issues count: `● 2 of 2 running` / `✗ 1 failed` / `✓ 5/5 checks`. Merged PRs and PRs with no checks show nothing. Files: `app/progress.go`, `app/statusbar.go`, `session/git/sync.go`. Test: `TestProgressStripShowsChecks`.
- **Same day, clarified:** the user meant the project tabs row (`practice-stack (1) │ abhyaiska │ claude-squad (1) …`), checks only: each tab gets a chip `✓ 2/2` / `● 2 running` / `✗ 1 failed` for its PR (batch PR when issues gathered on one, else the checked-out branch's PR; merged → none). Read per project every 30s (`refreshTabChecks`, from the auto tick). The strip keeps only the checks beside its batch PR. Files: `ui/projecttabs.go`, `app/statusbar.go`. Test: `TestTabChecksChip`.

### Unticking in the issue picker skipped the issue for good (2026-10-13)
- **Saw:** the user wanted 3 or 4 of the ticked issues; unticking the rest with space marked each "(skipped)", so auto never offered them again and they looked gone. They asked for skip to be its own key, and for a quick way to start exactly 1, 2 or 6.
- **Fix:** space only ticks/unticks (ticking a skipped issue takes the skip back). `x` skips or un-skips; a skipped issue stays in the list, greyed, and can be ticked any time. Digits 1-9 tick the first N startable issues in list order (no session, not skipped) and untick the rest. File: `ui/issuepicker.go`. Test: `TestPickerTickSkipAndCount`.

### Skipped issues filled the top of the issue picker (2026-11-25)
- **Saw:** practice-stack's picker opened on 18 skipped or waiting issues (the oldest ones are the ones that get skipped); the ticked, startable ones were at the bottom.
- **Fix:** startable issues come first, then a line `── On hold: 2 with a session · 9 waiting · 9 skipped ──` and those issues, each group by age in the chosen order. `tab` jumps to the on-hold ones and back. After `x` the row stays under the cursor (x again takes it back) and moves when the cursor leaves it or the list reloads. Files: `ui/issuepicker.go` (`arrange`, `moveTo`, `holdLine`).
- **Guard:** `TestPickerOnHoldBelow`, `TestPickerSkipMovesBelowOnLeave`, `TestPickerOnHoldLastRowVisible`.
- **Found on the way:** the list saved on disk lost the video and research marks, so right after a cs restart the picker ticked those issues, and the ticks stayed when the fresh list came. Now saved (`cachedIssue`, `app/issues.go`). Guard: `TestIssueCacheKeepsOnHold`.

### Waiting issue tiles vanished for a minute (2026-11-25)
- **Saw:** 6 issues picked at once in practice-stack; 17s later 5 tiles disappeared. They came back one at a time as each issue started.
- **Cause:** a code edit rebuilt cs, the background runner restarted on the new build, and its start-up cleanup (`CloseStaleQueuedIssues`) closed every "queued" tile, also the window's, whose queue was still intact. No work lost: a queued tile has no Claude yet.
- **Fix:** a queued tile carries the pid of the cs that queued it (`@cs_queued`); the cleanup closes only tiles whose cs no longer runs (`queueOwnerRuns`, `session/issues.go`).
- **Guard:** `TestCloseStaleQueuedKeepsLiveOwners`.

### Issue picker didn't say who asked for an issue (2026-11-25)
- **Saw:** customers' issues were mixed with the user's own and automatic ones; no name, no filter.
- **Fix:** every row shows the person, read from the body's "Reported by" line (GitHub's author is Glitchgrab's app). Issues from anyone but you (git `user.name`) come first, then your own and nameless ones; the background runner starts them in the same order (`ByPriority`). `f` lists the people: enter shows only theirs, space ticks several, `p` pins someone above everyone (saved per project in `.prefs.json`; the filter isn't saved). Files: `session/reporter.go`, `ui/issuepeople.go`, `ui/issuepicker.go`, `app/issuesdaemon.go`.
- **Guard:** `session/reporter_test.go`, `ui/issuepeople_test.go`.

### GlitchRecord runs opened their own tile (2026-10-30)
- **Saw:** a `practise_stack` tab with a view-only tile (`practise-stack-26`) showing GlitchRecord's script-review prompt, opened by itself and replaced by a new one each round.
- **Cause:** GlitchRecord starts `claude -p` per round. `claude agents --json` lists those as `kind: interactive`, so cs treated each as a person's session in another terminal.
- **Fix:** skip any non-background agent whose `~/.claude/sessions/<pid>.json` has an `sdk*` entrypoint (`isHeadless`), decided when the list is fetched: the file is deleted when the run ends while `claude agents` stays cached 10s, and a check at draw time flashed a ghost tile for the last seconds of each run (seen live on the first try). Covers every `claude -p`/Agent SDK caller, not only GlitchRecord; VS Code sessions (`claude-vscode`) stay. The check runs before the parent-pane match, so a `claude -p` started from a tile's shell can't rename that tile either.
- **Guard:** `TestIsHeadless`, `TestHeadlessStaysHiddenAfterFileGone`; activity.log shows no `session appeared: pid-N` for a `claude -p` run.
- **Files:** `session/external.go`, `session/closed_test.go`.

## Test incidents (rules for live tests)

- A test pressed Enter on the user's real session and moved it. → Check the selected row on screen before any destructive key (CLAUDE.md "Live tests").
- A test `cd`'d into a temp repo; the zsh hook started gai-watch, which committed. → Use `git -C`, never `cd`.
- Tests wrote to the real activity.log. → `inTest` guard in `app/keylog.go`.

### Remove an issue from its PR and skip it took two manual steps (2026-10-14)
- **Saw:** the user asked the Claude in an issue's tile to edit the PR body, then pressed `x` for the same issue in the picker (practice-stack #2510 in PR #2519).
- **Fix:** ⌃Space then `U` on an issue's tile asks, marks the issue skipped, and runs `gai issue <n> --remove` in a short-lived tmux session that cs answers (Enter on the rewrite question, cursor onto the issue's PR in the PR list). File: `app/unlink.go`. `/skip` (the skill) also reverts the issue's commits.
- **Guard:** `TestUnlinkResultNotOnPR`; `TestUnlinkSkipsAndAnswersGai` skips unless tmux resolves to its own temp socket.
- **Lesson (same day):** that test first called `tmux kill-server` with only `TMUX_TMPDIR` set. `$TMUX` wins over it, so it killed the user's real tmux server and every session. Tests must clear `TMUX`, check `#{socket_path}`, and never use kill-server.

### Issue picker could not be refreshed by hand (2026-10-14)
- **Saw:** the list sometimes took long to load (117 open issues, plus the PR reads) and showed stale or missing issues; the only way to reload was to close and reopen the picker.
- **Fix:** `r` in the issue picker reloads the list now. Ticks and cursor stay, the footer starts with "⟳ refreshing…", and pressing `r` again just loads again, so a hung load never locks it out. `r` sits right after `x skip` in the key row, because the row is cut at the right on a narrow window. Files: `ui/issuepicker.go`, `app/issues.go`.
- **Guard:** `TestPickerRefreshKeepsListAndCursor`.

### Strip said "no open PR yet" with a PR open (2026-10-15)
- **Saw:** seva_stack PR #555 (`gai pr` from `dev`) was open, but the strip read "no open PR yet · 15 open".
- **Cause:** the strip finds the open PR only through the issues it closes (`closingIssuesReferences`). #555's body had no `Closes #N`, so no issue mapped to it.
- **Fix:** with no issue-mapped PR, the strip shows the checked-out branch's open PR ("PR #N open"). File: `app/progress.go`. It does not count issues toward the 15-per-PR room; that still needs a `Closes #N` in the PR body (gai side).

### Could not open a second terminal while a dev server ran in the dock (2026-10-16)
- **Saw:** the dock terminal ran `turbo dev` (glitchgrab); ⌃Space T only shows/hides that same dock, so there was no shell left for git or a second command.
- **Fix:** ⌃Space E opens a fresh shell in the project folder (`csterm_<folder>-2`, -3, …) as a grid tile and types into it; the dock keeps running. On the key bar and in help. Files: `session/external.go` (`NewProjectShell`), `app/terminal.go` (`newTerminal`), `app/leader.go`, `app/help.go`.
- **Guard:** live test on a `tmux -L cstest` socket: `sleep 999` in the dock, ⌃Space E, typed into the new tile.

### One conversation open in 2 tiles; closing one closed both (#2616, 2026-10-21)
- **Saw:** two tiles of mca-connection-alert-drops-#2616 with "same conversation open in 2 tiles · close one". The user: "if I close one which I am not working in, it closes both".
- **Cause:** (1) one click on "↺ reopen" in Recently closed came as two mouse events; activity.log 13:07:14.707 and .722 "reopening cc_practice-stack (06862012…)" twice, so two `claude --resume` of the same session (`cc_practice-stack`, `cc_practice-stack_2`). (2) Closing a tile hid every tile with its session ID for 20s (`closedKeys` "sid:"), so the other tile vanished too.
- **Fix:** a session reopened in the last 10s isn't reopened again (`reopenHold`, `app/recentclosed.go`). A closed tile is hidden by session ID only when no other tile has that conversation; then only its exiting Claude (a view-only `pid-N` row) is hidden by it (`hideClosed`, `ghostKey`, `app/terminal.go`). Closing a reopened session lifts the 10s hold.
- **Guard:** `TestReopenOnceAndCloseOne`; activity.log shows one "reopening …" per click.

### Closed tile came back as "view only" and W refused to close it (#3253, 2026-11-14)
- **Saw:** admin-users-export-feedback-#3253 closed with W at 20:13:09; at 20:13:29 it was back as `pid-11997 … (own terminal · view only)`, and W said "runs in its own terminal window; close it there". There was no such window.
- **Cause:** `KillSession` killed the tmux session and sent SIGTERM, then SIGKILL, to the pane's Claude (#2239). This Claude was stuck in the kernel (`ps` state `U`, then `E` after SIGKILL), so it lingered for hours with its `~/.claude/sessions/<pid>.json`, and `claude agents` kept listing it. `hideClosed` hides a closed tile's exiting Claude for only 20s, so it came back as a view-only tile.
- **Fix:** `ListExternalSessions` (`session/external.go`) hides a Claude whose parent is cs's tmux server when no pane holds it: that is a closed tile's leftover, with no window and nothing that can type into it. It doesn't need any memory of the close, so it also works after cs restarts. It is hidden, not killed: a tile started between the pane list and the check looks the same for one refresh.
- **Guard:** none automatic (needs a process stuck in the kernel). Check: `ps -o ppid= -p <pid>` equals `tmux display -p '#{pid}'` and `tmux list-panes -a -F '#{pane_pid}'` doesn't list it → no tile.

### Follow-up issues started before the PR they wait on was merged (glitchgrab #627/#628, 2026-10-21)
- **Saw:** "[Follow-up] … after #625 deploys" and "… after #620 deploys" were run by the issue runner right away, landed in the same PR #626 as #625/#620 and closed with it, before anything was deployed.
- **Cause:** cs treated every open issue without a PR as free; nothing read the "after #N" in a follow-up's title.
- **Fix:** `ListOpenIssues` sets `Issue.WaitsFor` when the title is `[Follow-up] … after #N` and #N is still an open issue or open PR (`followUpOf`, `session/issues.go`). The background runner and the auto offer don't count or start it; the picker shows it dimmed as "(waits for #N to merge)" and doesn't tick it (still tickable by hand). Once #N closes (its PR merged to main), it joins the next batch.
- **Guard:** `TestFollowUpOf`, `TestPickerFollowUpNotTicked`.
- **Same gap, dated (2026-10-22):** a session ended with "keep this session open until 23 Oct 1:47 PM" for the last 6 Google requests, and practice-stack #2664 "[Follow-up] 2026-11-04: …" would have been started on the day it was opened. A dated follow-up now waits too: `Issue.NotBefore` comes from a body line `Not before: 2026-10-23 13:47 IST` (date, optional time, optional zone or IST), else a `YYYY-MM-DD` in a `[Follow-up]` title (00:00 local) (`dueDate`). The picker says "(waits until 23 Oct 13:47)". When the time comes, the runner starts a fresh session on it, which reads the steps from the issue. Guard: `TestDueDate`, `TestPickerDatedFollowUpNotTicked`.
- **Same gap, other repo (2026-10-26):** abhyasika #1028 "[Follow-up] … after glitchgrab#670 deploys" started at once and landed in #1026's PR #1027 ("Closes #1028"). The regex wanted `#` right after "after", so `glitchgrab#670` read as no follow-up. `followUpOf` now takes `repo#N` and `owner/repo#N`; a bare repo is looked up under this repo's GitHub owner (`otherRepoOpen`, `gh api repos/…/issues/N`), and counts as waiting when it can't be checked. Picker says "(waits for glitchgrab#670 to merge)". Guard: `TestFollowUpOf`.

### Bar kept saying ↑161 after the push (practice-stack, 2026-10-23)
- **Saw:** the status bar showed `⎇ dev ↓0 ↑161`; `git status` said up to date with origin/dev. Pushing from the bar three times "pushed 161 commit(s)" each time and the number never moved.
- **Cause:** at 09:42 the last session of the temporary github-runners tab closed, so `refreshTabs` moved to practice-stack with `_ = m.switchProject(...)`. That marked practice-stack's git read as loading and threw away the command doing the read. From then on every refresh of that tab (2s tick, tab switch, after push) skipped as "already loading". 161 was how far ahead dev was at 09:42.
- **Fix:** `refreshTabs` clears that mark after its switch. A loading mark older than 30s (`scLoadingStale`) counts as lost and is retried, so a dropped command can't freeze a tab again.
- **Guard:** `TestTemporaryTabEndDoesNotFreezeGitStatus`, `TestLostGitReadIsRetried` (`app/scloading_test.go`). Files: `app/app.go`, `app/sourcecontrol.go`.

### Issues piled onto a teammate's stale PR (glitchgrab #766)
- **Cause:** `prRoom` picked the PR with the most issues.
- **Fix:** `session.ChoosePR` picks only the user's PR (lone, or the one from the checked-out branch); else no keys, the tile asks, and a macOS alert shows.

### Strip lost "PR #N 8/15 issues" on a full project (practice-stack, 2026-11-04)
- **Saw:** practice-stack's strip started at "▶ 6/6 running", with no PR, no bar and no open count; abhyaiska showed "PR #1110 3/15 issues". PR #3060 had 8 issues.
- **Cause:** `autoCheck` skipped a project with no free session before loading its list. The window's `issueCache` starts empty on each restart, and cs restarts on every build, so a project at its limit (6/6, 8/6) never filled it. The daemon's file on disk was correct; the strip doesn't read it. abhyaiska had free sessions, so it loaded.
- **Fix:** the list loads first, the no-free-session check comes after (`app/autoissues.go`). Guard: `TestAutoFullProjectStillLoadsItsList`.
- **Next time:** strip starting at "▶" means `s.known` is false: the window has no list for that project. Its left edge is the tile's, so nothing is hidden.

### Servers panel listed Claude tiles as servers (2026-11-19)
- **Saw:** Servers showed `:49767 gst-gst-notices-download-#3308 · claude --res…` and four more random high ports, each with a `stop`. Looked like GitHub Actions runners.
- **Cause:** the claude CLI itself listens on a 127.0.0.1 random port (lsof names it `2.1.328`, its versioned binary). `ListServers` only skipped Claude's MCP children, not Claude. Its `stop` would have sent Ctrl+C to the tile and interrupted Claude.
- **Fix:** a listener whose own process is `claude` is dropped (`isClaudeCLI`, `session/servers.go`). Guard: `TestIsClaudeCLI`.

### Dev server "address already in use :::4444", Servers panel empty (practice-stack, 2026-11-19)
- **Saw:** terminal failed with `EADDRINUSE :::4444`, but Servers showed nothing, so no way to see who held it.
- **Cause:** 4444 was GlitchRecord's own copy of practice-stack (`~/GlitchRecord Projects/practise_stack`, started by the GlitchRecord Electron app). Servers lists only the active project's tiles and folder, so another folder's server was hidden.
- **Fix:** each listing also reads the dock terminal's last 200 lines for "address already in use …N"; a server on N shows in Servers whatever its folder, labelled `blocks this port · <folder>`, with its stop (`dockBlockedPorts`, `blockedPorts`, `app/servers.go`). Guard: `TestBlockedPorts`.

### No visible way to stop the auto pickup (practice-stack, 2026-11-20)
- **Saw:** the user wanted to finish 2 open issue sessions and do a git cleanup, but closing a session freed a slot and the runner started the next issue. They asked for a stop button ("not pause").
- **Cause:** auto off already existed, but only as `a` inside the issue picker; the strip just said `⟳ auto`, with nothing to click.
- **Fix:** the strip ends with a red `■ stop` button (green `▶ start` when stopped, strip says "auto stopped: no new issues start"). Click toggles the same `.auto-off` file the picker and the background runner read; open sessions keep running (`clickAutoButton`, `app/autoissues.go`; `autoBtnSpan`, `app/progress.go`). Same day: also ⌃Space G in command mode, labelled "stop auto" / "start auto" (`commandKeysNow`, `app/leader.go`). Default stays on; only the user stops it. Guards: `TestStripStopButton`, `TestLeaderGStopsAuto`.

### Switch Claude account by itself when one hits its limit (2026-11-23)
- **Asked:** one account used up (96% of the 5-hour limit) meant `/login` to the next by hand, then Remote Control again in every tile.
- **Built:** `cs account add <name>` saves the current login (keychain `claudeAiOauth` + `oauthAccount` of `~/.claude.json`, `~/.claude-squad/accounts/<name>.json`, 0600; connector tokens `mcpOAuth` are left alone). `cs account use <name>` / `list`. When a tile sits on a "hit your limit" line, the runner marks that account limited until the reset, switches once per limit to a saved account with room, and types `continue` in the stopped tiles (`switchedOnLimit`, `app/limitresume.go`; `session/accounts.go`). A keychain backup goes to `accounts/keychain-backup.json` before every swap.
- **Proven live (2026-11-23):** `cs account use main` while tiles were open: every running Claude saw "Remote Control disconnected, signed-in account changed" within seconds, no restart; the runner typed `/remote-control` and `/reload-plugins` in all 8 idle tiles. Cost: a resumed session shows "Claude can't use thinking from another organization" (its earlier thinking can't be reused, so replies may be slower and use more tokens). Not avoidable.
- **Bug found by that test:** the first real swap failed ("security: unknown command") and left Claude "Not logged in". `security -i` reads one line of about 4000 characters and runs the rest as a command; the real login is 38 KB (86 connector entries). Fix: values over 3500 characters go as the `-w` argument instead; short ones stay on stdin. Nothing was half-written (the write failed whole), `/login` fixed the session. Errors never echo the value (`keychainWriteTo`, `session/accounts.go`). Guard: `TestKeychainWriteLongValue`, `TestKeychainWriteErrorHidesValue`.
- **Came back (same day): auto-switch failed, then both accounts showed "limited until 18:00".** Three causes. (1) The keychain backup sat in `accounts/` and was read as a blank saved account that `NextAccount` picked first; backups now live in `~/.claude-squad/`, and a file without name and accountUuid is ignored. (2) A limit notice left on a tile after a switch or `/login` was credited to the current account; `limitOwner` now remembers who was signed in when a notice first appeared, and an old account's notice neither marks the current one nor switches again (`noteLimitSeen`). (3) The live test `TestResumeAfterLimitTiming` (fake reset 18:00:05) wrote that mark onto the real saved accounts and could have swapped the real login; it now has its own HOME and a stubbed swap. Guard: `TestStrayFileIsNotAnAccount`, `TestStaleNoticeMarksItsOwnAccount`.
- **Kept:** `~/.claude-squad/keychain-backup-full-86.json` is a pre-swap copy of the full keychain item (0600).
- **Chrome extension (one profile, automatic when the browser's claude.ai matches):** Claude in Chrome keeps one account per profile. After every switch (`cs account use`, the limit switch, or `cs account chrome` by hand) cs opens the extension settings page, clicks **Log out**, then the extension's own **Log in** button (its page turns into a "Log in" page after the logout; seen 2026-11-23); that opens claude.ai's "Claude for Chrome would like to connect" page, and cs clicks **Authorize** only on that page (redirect_uri is the extension) and only when it says "Logged in as <the switched-to account>". If claude.ai in Chrome is another account (e.g. still the one that ran out of quota), cs clicks that page's **Switch account** to open claude.ai's sign-in, never Authorizes, and notifies; after you sign in as the right account run `cs account chrome` to finish: signing in to claude.ai needs your own login, which cs never types. Needs macOS Automation for cs-fork (System Settings > Privacy & Security > Automation) and Chrome's View > Developer > Allow JavaScript from Apple Events (both set 2026-11-23); without them cs only opens the page and says which is missing. Log: `daemon: chrome extension: ...` (`session/chrome.go`). Tested against fakes of the page; the first live run is the real check. Two Chrome profiles would avoid the claude.ai sign-in step; not built. Guard: `TestChromeLogoutThenAuthorize`, `TestChromeWrongBrowserAccountIsNotAuthorized`, `TestChromeIgnoresOtherOAuthPages`, `TestChromeFallsBackWhenNotAuthorised`.
- **Guard:** `TestAccountSwap*`, `TestNextAccountSkipsCurrentAndLimited` (`session/accounts_test.go`), `TestSwitchedOnLimitOncePerLimit`.

### Tile closed by itself while the user was typing in it (practice-stack #3490, 2026-11-23) — cause not found
- **Saw:** 12:29:36, the `cc_practice-stack_i3490` tile vanished from cs and a ghost `pid-5474 … (own terminal · view only)` took its place (no such process, no such tmux session afterwards). The user had just typed two slash commands into it (keys.log 12:29:33 and 12:29:35). The Claude session still finished its turn at 12:29:40 (transcript `21d29ecb-…jsonl`, "Nothing new to stage"), so the work was not lost.
- **Known:** cs restarted itself in place at 12:29:33 (self-update, `source changed` at 12:29:31) three seconds before. 4 self-updates in 5 minutes that hour, from source edits in this repo. No `kill-session` call, hook or daemon "tile closed" line explains it; closing paths found: `app/terminal.go`, `app/unlink.go`, `session/issues.go`, `app/issuesdaemon.go` (dead panes only).
- **Not known:** whether the restart, a tmux listing that failed under load (`UI FLOOD` lines at that time), or the session's own exit ended the tmux session. Next time: copy `~/.claude-squad/activity.log` lines around the minute and run `tmux ls` before touching anything.
- **Get it back:** `cd <project> && claude --resume <session id>`.

### Claude in Chrome "Browser extension is not connected" after switching account by hand (#3314 tile, 2026-11-23)
- **Saw:** after `/login` to the other account, `/remote-control` and `/reload-plugins` (typed by cs), a tile's `tabs_context_mcp` failed with "Browser extension is not connected".
- **Cause:** the extension stays signed in to the old account and Claude in Chrome pairs per account; cs ran its Chrome step only when cs itself switched the account (`cs account use`, limit switch). A manual `/login` was never seen by that step. Also seen: the same tile got `/remote-control` + `/reload-plugins` six times in 10 minutes (each account change, activity.log), and 4 `claude --chrome-native-host` processes, two from Nov 17 and Nov 20 (stale; not shown to matter).
- **Fix:** the Chrome step now runs on any account change, once per account (`chromeFollowsAccount`, `session.ChromeNeedsReset`; the last account it ran for is in `~/.claude-squad/chrome-account`). Guard: `TestChromeFollowsAnyAccountChangeOnce`. Unproven live.
- **Came back (same day): extension signed in, the tile still saw no browser (`list_connected_browsers` returned `[]`; this session saw 5, the new one connected).** The tiles reloaded at the account change, minutes before the extension finished signing in. Fix: after a successful Chrome sign-in cs types `/reload-plugins` again into idle tiles with an empty prompt (`ReloadTilesAfterChrome`, `app/limitresume.go`; `cs account chrome` does it too; `cs account reload` does only this). Guard: `TestReloadTilesOnlyAfterSignIn`; the typing itself unproven live.
