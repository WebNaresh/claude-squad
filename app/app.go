package app

import (
	"claude-squad/config"
	"claude-squad/keys"
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/git"
	"claude-squad/ui"
	"claude-squad/ui/overlay"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

const GlobalInstanceLimit = 10

// DECAWM: the terminal's automatic wrap at the right margin.
const (
	autoWrapOff = "\x1b[?7l"
	autoWrapOn  = "\x1b[?7h"
)

// Run is the main entrypoint into the application.
// It returns ErrRestart when cs quit to restart itself on a newer build.
func Run(ctx context.Context, program string, autoYes bool) error {
	h := newHome(ctx, program, autoYes)
	// After an in-place self-update the terminal is still in raw mode, with
	// keys typed during the switch waiting. The alternate screen is entered
	// as usual (full-screen drawing never leaves stray lines), and the first
	// frame is the previous process's saved screen, so the switch is one frame.
	restarted := os.Getenv(restartedEnv) == "1"
	// cs never restarts in place while an issue queue runs, so any
	// "queued" issue session now is left from a quit mid-queue.
	session.CloseStaleQueuedIssues()
	os.Unsetenv(restartedEnv)
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	// Mouse capture is on by default (click tiles, tabs and images, wheel
	// scrolls); Ctrl+] m turns it off for the terminal's own text selection.
	if !h.appConfig.MouseOff {
		opts = append(opts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(h, opts...)
	startWatchdog()
	// Auto-wrap off while cs draws. Some characters (⚠️ and other emoji)
	// are drawn wider by the terminal than cs measures them; with wrapping on,
	// such a row spills onto the next one, every row below shifts down, and
	// the renderer (which only redraws changed rows) leaves old text behind.
	// With it off, the row's last column is clipped instead.
	fmt.Print(autoWrapOff)
	_, err := p.Run()
	fmt.Print(autoWrapOn)
	if restarted {
		// The raw mode inherited from the previous process was recorded as
		// "normal" and restored on exit; put the terminal back to normal mode.
		fmt.Print("\x1b[?1049l\x1b[?25h")
		if tty, terr := os.Open("/dev/tty"); terr == nil {
			cmd := exec.Command("stty", "sane")
			cmd.Stdin = tty
			_ = cmd.Run()
			_ = tty.Close()
		}
	}
	if err == nil && h.restart {
		return ErrRestart{}
	}
	return err
}

type state int

const (
	stateDefault state = iota
	// stateNew is the state when the user is creating a new instance.
	stateNew
	// statePrompt is the state when the user is entering a prompt.
	statePrompt
	// stateHelp is the state when a help screen is displayed.
	stateHelp
	// stateConfirm is the state when a confirmation modal is displayed.
	stateConfirm
	// stateCommit is the state when the commit message box is displayed.
	stateCommit
	// stateIssuePicker is the state when the ⌥N issue picker is displayed.
	stateIssuePicker
)

type home struct {
	ctx context.Context

	// -- Storage and Configuration --

	program string
	autoYes bool

	// storage is the interface for saving/loading data to/from the app's state
	storage *session.Storage
	// appConfig stores persistent application configuration
	appConfig *config.Config
	// appState stores persistent application state like seen help screens
	appState config.AppState

	// -- State --

	// state is the current discrete state of the application
	state state
	// newInstanceFinalizer is called when the state is stateNew and then you press enter.
	// It registers the new instance in the list after the instance has been started.
	newInstanceFinalizer func()

	// promptAfterName tracks if we should enter prompt mode after naming
	promptAfterName bool

	// keySent is used to manage underlining menu items
	keySent bool

	// instanceStarting is true while a background instance start is in progress.
	// Prevents double-submission and guards against interacting with a not-yet-started instance.
	instanceStarting bool
	// startingInstance holds a reference to the instance being started in the background.
	startingInstance *session.Instance

	// -- UI Components --

	// list displays the list of instances
	list *ui.List
	// menu displays the bottom menu
	menu *ui.Menu
	// tabbedWindow displays the tabbed window with preview and diff panes
	tabbedWindow *ui.TabbedWindow
	// errBox displays error messages
	errBox *ui.ErrBox
	// global spinner instance. we plumb this down to where it's needed
	spinner spinner.Model
	// textInputOverlay handles text input with state
	textInputOverlay *overlay.TextInputOverlay
	// textOverlay displays text information
	textOverlay *overlay.TextOverlay
	// confirmationOverlay displays confirmation modals
	confirmationOverlay *overlay.ConfirmationOverlay
	// projectTabs is the row of open projects across the top
	projectTabs *ui.ProjectTabs
	// contentHeight is the height given to the list and preview panes
	contentHeight int
	// screenWidth/screenHeight are the whole terminal, for overlays.
	screenWidth, screenHeight int
	// repoRoots caches the git root of each external session's folder;
	// sessionRoots are the projects that currently have a running session
	repoRoots    map[string]string
	sessionRoots []string
	// updater rebuilds cs when its source changes; nil when not run via the launcher
	updater *updater
	// updateReady is set when a new build is waiting for the main screen;
	// restart makes Run report ErrRestart after quitting.
	updateReady bool
	restart     bool
	// sessionFocus is the tmux session that receives typed keys while the
	// session pane has focus; "" when the list has focus.
	sessionFocus string
	// sessionFocusRow is the list row that focus belongs to
	sessionFocusRow string
	// leader is set after Ctrl+] until the next key (the command)
	leader bool
	// ready is set once sessions and the first tile screens have loaded;
	// until then a loading screen shows instead of a half-drawn layout
	ready          bool
	sessionsLoaded bool
	startedAt      time.Time
	// savedFrame is the last screen of the process before a self-update
	savedFrame     string
	savedW, savedH int
	restoredRow    string
	winW, winH     int
	// autoFocused is set once typing has been given to the first session
	autoFocused bool
	// gridTiles is the last captured content of the grid's tiles (all of the
	// project's sessions); gridMarkdown renders view-only ones.
	gridTiles []ui.GridTile
	gridFocus int
	tileCache map[string]cachedTile
	// gridCapturing is set while a background tile capture runs
	gridCapturing bool
	// gridRenderKey/gridRendered cache the drawn grid
	gridRenderKey string
	gridRendered  string
	// gridKeys names the sessions gridTiles shows, in order; a different
	// list (another tab) is redrawn from tileCache at once
	gridKeys string
	// issuePicker is the ⌥N picker; issueQueue/issueJob start the picked
	// issues' sessions one after another
	issuePicker *ui.IssuePicker
	issueCache  map[string][]session.Issue // last issue list per project, shown at once next time
	issueQueue  []queuedIssue
	issueJob    *issueJob
	// needsSeen holds the sessions known to wait on the user (to spot new
	// questions); questionJumped the ones focus already moved to once;
	// lastKey is the time of the last key press
	sel            tileSelection        // mouse text selection in a tile (selection.go)
	justClosed     map[string]time.Time // tiles closed a moment ago (closeSession)
	needsSeen      map[string]bool
	questionJumped map[string]bool
	lastKey        time.Time
	gridMarkdown   map[string]*ui.MarkdownCache
	// paneWidth is the width of the session pane / grid area
	paneWidth int
	// leftWidth is the width of the Source Control + dock column. dockNames
	// is each project's docked terminal; dockHidden folds it to one status
	// line; dockShots keeps each dock's last capture, so a tab switch shows
	// its terminal at once (dock.go).
	leftWidth  int
	dockNames  map[string]string
	dockHidden bool
	dockShots  map[string]dockCapturedMsg
	// servers are the listening ports, with the tile that started each (servers.go)
	servers        []session.Server
	serversFocused bool
	// Auto issue loop (autoissues.go): autoDeclined is how many sessions ran
	// when its offer was declined, autoFetched when the issue list was last
	// loaded for it, autoSaid the last message shown, autoPrompt the project
	// of the picker it opened.
	autoDeclined map[string]int
	autoFetched  map[string]time.Time
	autoSaid     map[string]string
	autoPrompt   string
	// progress counts finished sessions per day for the strip above the tiles
	progress *progressData
	// recent is the last closed Claude sessions, newest first (recentclosed.go)
	recent []closedEntry
	// ghost is a just-closed tile's place, shown a few seconds (closeghost.go)
	ghost *closeGhost
	// accentOf is each session's colour index (closeghost.go)
	accentOf      map[string]int
	serverCursor  int
	dockCapturing bool
	// lastSessions/lastAgents are what the last refresh saw (for the activity log)
	lastSessions map[string]string
	lastAgents   map[string]string
	// skipRender reuses lastView for the next View() call (set when a key was
	// only forwarded to a session); lastView is the last rendered screen
	skipRender bool
	lastView   string
	// confirmResult is what a confirmed action returned, delivered after the
	// dialog closes
	confirmResult tea.Msg
	// fitted remembers which live screens were already sized to the pane
	fitted map[string]string
	// sourceControl is the left column. scLoading marks projects whose git
	// status is being read; scCache keeps each project's last status so
	// switching tabs shows it at once instead of an empty list.
	sourceControl *ui.SourceControl
	scLoading     map[string]bool
	scCache       map[string]scStatusMsg
	bar           statusBar // branch, push and PR at the right of the bottom row
	// commitOverlay is the commit message box shown in stateCommit
	commitOverlay *overlay.TextInputOverlay
}

func newHome(ctx context.Context, program string, autoYes bool) *home {
	// Load application config
	appConfig := config.LoadConfig()

	// Load application state
	appState := config.LoadState()

	// Initialize storage
	storage, err := session.NewStorage(appState)
	if err != nil {
		fmt.Printf("Failed to initialize storage: %v\n", err)
		os.Exit(1)
	}

	h := &home{
		ctx:          ctx,
		spinner:      spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		menu:         ui.NewMenu(),
		tabbedWindow: ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane()),
		errBox:       ui.NewErrBox(),
		storage:      storage,
		appConfig:    appConfig,
		program:      program,
		autoYes:      autoYes,
		state:        stateDefault,
		appState:     appState,
	}
	h.list = ui.NewList(&h.spinner, autoYes)
	h.updater = newUpdater()
	h.fitted = map[string]string{}
	h.dockNames = map[string]string{}
	h.dockShots = map[string]dockCapturedMsg{}
	h.progress = loadProgress()
	h.recent = loadRecent()
	h.sourceControl = ui.NewSourceControl()
	h.scLoading = map[string]bool{}
	h.scCache = map[string]scStatusMsg{}
	h.gridMarkdown = map[string]*ui.MarkdownCache{}
	h.startedAt = time.Now()
	var focus string
	h.savedFrame, h.savedW, h.savedH, focus, h.restoredRow = loadFrame()
	if focus != "" {
		// Keep typing into the same session straight away: keys typed during
		// the restart were held by the terminal and go where they belong.
		h.sessionFocus, h.sessionFocusRow, h.autoFocused = focus, h.restoredRow, true
	}
	h.projectTabs = ui.NewProjectTabs()

	// Load saved instances
	instances, err := storage.LoadInstances()
	if err != nil {
		fmt.Printf("Failed to load instances: %v\n", err)
		os.Exit(1)
	}

	// Add loaded instances to the list
	for _, instance := range instances {
		// Call the finalizer immediately.
		h.list.AddInstance(instance)()
		if autoYes {
			instance.AutoYes = true
		}
	}

	// A paused agent whose folder was deleted can never run again; forget it so
	// it doesn't hold a dead tab open.
	var dropped bool
	for _, instance := range append([]*session.Instance(nil), h.list.GetInstances()...) {
		if _, err := os.Stat(instance.Path); err != nil && instance.Paused() {
			log.InfoLog.Printf("forgetting agent %q: folder %s no longer exists", instance.Title, instance.Path)
			h.list.Remove(instance)
			dropped = true
		}
	}
	if dropped {
		instances = h.list.GetInstances()
		if err := storage.SaveInstances(instances); err != nil {
			log.WarningLog.Printf("failed to save agents: %v", err)
		}
	}

	// Every project with saved agents gets a tab, so no agent is ever hidden.
	for _, instance := range instances {
		if root, err := git.RepoRoot(instance.Path); err == nil && !containsString(appConfig.OpenProjects, root) {
			appConfig.OpenProjects = append(appConfig.OpenProjects, root)
			if err := config.SaveConfig(appConfig); err != nil {
				log.WarningLog.Printf("failed to save open projects: %v", err)
			}
		}
	}
	// The cs source folder is always a tab, so cs itself can be worked on from cs.
	if src := os.Getenv("CS_SOURCE_DIR"); src != "" && !containsString(appConfig.OpenProjects, src) {
		if _, err := os.Stat(src); err == nil {
			appConfig.OpenProjects = append(appConfig.OpenProjects, src)
			if err := config.SaveConfig(appConfig); err != nil {
				log.WarningLog.Printf("failed to save open projects: %v", err)
			}
		}
	}

	// Drop tabs whose folder was deleted, unless agents still point at them.
	var open []string
	for _, p := range appConfig.OpenProjects {
		if _, err := os.Stat(p); err == nil || h.list.CountInProject(p) > 0 {
			open = append(open, p)
		}
	}
	if len(open) != len(appConfig.OpenProjects) {
		appConfig.OpenProjects = open
		if !containsString(open, appConfig.ActiveProject) {
			appConfig.ActiveProject = ""
			if len(open) > 0 {
				appConfig.ActiveProject = open[0]
			}
		}
		if err := config.SaveConfig(appConfig); err != nil {
			log.WarningLog.Printf("failed to save open projects: %v", err)
		}
	}
	h.projectTabs.SetProjects(appConfig.OpenProjects, appConfig.ActiveProject)
	h.list.SetProject(h.projectTabs.Active())

	logEvent("start pid=%d program=%q tabs=%v active=%s mouse=%v agents=%d",
		os.Getpid(), program, ui.TabNames(h.projectTabs.Projects()), filepath.Base(h.projectTabs.Active()),
		!h.appConfig.MouseOff, len(instances))
	return h
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// updateHandleWindowSizeEvent sets the sizes of the components.
// The components will try to render inside their bounds.
func (m *home) updateHandleWindowSizeEvent(msg tea.WindowSizeMsg) {
	m.winW, m.winH = msg.Width, msg.Height // the real window size
	// The left column holds Source Control and, under it, the project's
	// docked terminal; the session tiles get the rest.
	scWidth := leftColumnWidth(msg.Width)
	m.leftWidth = scWidth
	// No session list column: the grid tiles (or the single session's title)
	// name the sessions, and Ctrl+] / Shift+arrows move between them.
	listWidth := 0
	tabsWidth := msg.Width - scWidth - listWidth
	m.paneWidth = tabsWidth

	m.projectTabs.SetWidth(msg.Width)

	// Every row but the tab row, the gap under it, the key bar and the error line.
	contentHeight := msg.Height - 4
	menuHeight := 1
	m.contentHeight = contentHeight
	m.screenWidth, m.screenHeight = msg.Width, msg.Height
	if m.issuePicker != nil {
		m.issuePicker.SetSize(msg.Width, msg.Height)
	}
	m.errBox.SetSize(msg.Width, 1) // error box takes 1 row

	m.tabbedWindow.SetSize(tabsWidth, contentHeight)
	m.list.SetSize(listWidth, contentHeight)
	m.layoutLeft()
	if m.commitOverlay != nil {
		m.commitOverlay.SetSize(int(float32(msg.Width)*0.5), int(float32(msg.Height)*0.3))
	}

	if m.textInputOverlay != nil {
		m.textInputOverlay.SetSize(int(float32(msg.Width)*0.6), int(float32(msg.Height)*0.4))
	}
	if m.textOverlay != nil {
		m.textOverlay.SetWidth(int(float32(msg.Width) * 0.6))
	}

	previewWidth, previewHeight := m.tabbedWindow.GetPreviewSize()
	if err := m.list.SetSessionPreviewSize(previewWidth, previewHeight); err != nil {
		log.ErrorLog.Print(err)
	}
	m.menu.SetSize(msg.Width, menuHeight)
}

func (m *home) Init() tea.Cmd {
	// Upon starting, we want to start the spinner. Whenever we get a spinner.TickMsg, we
	// update the spinner, which sends a new spinner.TickMsg. I think this lasts forever lol.
	return tea.Batch(
		func() tea.Msg {
			time.Sleep(100 * time.Millisecond)
			return previewTickMsg{}
		},
		tickUpdateMetadataCmd(m.snapshotActiveInstances(), m.list.GetSelectedInstance(), 0),
		m.updateCheck(),
		m.refreshSourceControl(),
		scTick(),
		keyboardCheck(),
		usageTick(),
		refreshServers(),
		serversTick(serversEvery),
		autoTick(),
	)
}

// previewInterval is how often live screens are re-captured: fast while the
// user is typing (so echo is instant), slower when idle to save CPU.
func (m *home) previewInterval() time.Duration {
	switch since := time.Since(m.lastKey); {
	case since < time.Second:
		return 50 * time.Millisecond // typing: show each key quickly
	case since < 3*time.Second:
		return 100 * time.Millisecond
	}
	return 400 * time.Millisecond
}

// refreshInterval is how often session status is refreshed (claude agents,
// tmux panes): every 500ms while typing, every second when idle.
func (m *home) refreshInterval() time.Duration {
	if time.Since(m.lastKey) < 3*time.Second {
		return 500 * time.Millisecond
	}
	return time.Second
}

func (m *home) updateCheck() tea.Cmd {
	if m.updater == nil {
		return nil
	}
	return m.updater.checkCmd()
}

// toggleMouse turns mouse capture on or off and remembers it.
func (m *home) toggleMouse() tea.Cmd {
	m.appConfig.MouseOff = !m.appConfig.MouseOff
	if err := config.SaveConfig(m.appConfig); err != nil {
		log.WarningLog.Printf("failed to save mouse setting: %v", err)
	}
	if !m.appConfig.MouseOff {
		return tea.Batch(tea.EnableMouseCellMotion, m.handleError(fmt.Errorf("mouse on: click a tile or an image, wheel scrolls (Ctrl+Space m: off, to select text)")))
	}
	return tea.Batch(tea.DisableMouse, m.handleError(fmt.Errorf("mouse off: drag selects text, Cmd+C copies (Ctrl+Space m: on)")))
}

// placeCentered draws an overlay in the middle of the screen. PlaceOverlay's
// own centering measures the background, whose session panes can hold link
// escape codes that it counts as width, pushing the overlay right.
func (m *home) placeCentered(fg, bg string) string {
	x := (m.screenWidth - lipgloss.Width(fg)) / 2
	y := (m.screenHeight - lipgloss.Height(fg)) / 2
	return overlay.PlaceOverlay(max(0, x), max(0, y), fg, bg, true, false)
}

// restartIfReady restarts on the new build, but only from the main screen so
// nothing being typed (a name, a prompt, a confirmation) is lost. It also
// waits for the issue queue, which lives only in memory.
func (m *home) restartIfReady() tea.Cmd {
	if !m.updateReady || m.state != stateDefault || m.instanceStarting || m.issueJob != nil || len(m.issueQueue) > 0 {
		return nil
	}
	logEvent("restart: new build ready, restarting in place")
	if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
		return m.handleError(err)
	}
	if RestartNow != nil {
		// Keep the current picture on screen through the restart.
		saveFrame(m.lastView, m.winW, m.winH, m.sessionFocus, m.sessionFocusRow)
		flushLogs()
		err := RestartNow() // only returns on failure
		logEvent("restart in place failed (%v), restarting the slow way", err)
	}
	m.restart = true
	return tea.Quit
}

func (m *home) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	defer watchUpdate(msg)() // lag watchdog (watchdog.go)
	switch msg := msg.(type) {
	case hideErrMsg:
		m.errBox.Clear()
	case dockCapturedMsg:
		m.applyDockCapture(msg)
		return m, nil
	case gridCapturedMsg:
		m.applyGridCapture(msg)
		if m.sessionsLoaded && !m.ready {
			m.ready = true
			logEvent("ready after %s (saved screen shown meanwhile: %v)", time.Since(m.startedAt).Round(time.Millisecond), m.savedFrame != "")
		}
		return m, nil
	case previewTickMsg:
		cmd := m.instanceChanged()
		interval := m.previewInterval()
		return m, tea.Batch(
			cmd,
			m.refreshGrid(),
			m.refreshDock(),
			func() tea.Msg {
				time.Sleep(interval)
				return previewTickMsg{}
			},
		)
	case issuesLoadedMsg:
		if msg.err == nil {
			if m.issueCache == nil {
				m.issueCache = map[string][]session.Issue{}
			}
			m.issueCache[msg.project] = msg.issues
			go writeIssueCache(msg.project, msg.issues)
		}
		if m.issuePicker != nil && m.issuePicker.Project == msg.project {
			m.issuePicker.SetIssues(msg.issues, m.busyIssues(msg.project), msg.err)
		}
		return m, nil
	case usageTickMsg:
		writeUsage(msg.line + fmt.Sprintf(" | ui tabs=%d agents=%d sessions=%d view=%s", len(m.projectTabs.Projects()),
			len(m.list.GetInstances()), len(m.list.ExternalSessions()), strings.Trim(m.stateName(), "[]")))
		return m, usageTick()
	case issueTickMsg:
		return m, m.stepIssues()
	case autoTickMsg:
		return m, m.stepAuto()
	case serversTickMsg:
		return m, tea.Batch(refreshServers(), serversTick(serversEvery))
	case serversMsg:
		return m, m.applyServers(msg)
	case keyboardCheckMsg:
		for _, name := range msg.repaired {
			logEvent("keyboard restored: %s had fallen back to line mode under Claude", name)
		}
		if len(msg.repaired) > 0 {
			return m, tea.Batch(keyboardCheck(), m.handleError(fmt.Errorf("fixed the keyboard of %s: Enter works again", strings.Join(msg.repaired, ", "))))
		}
		return m, keyboardCheck()
	case scTickMsg:
		return m, tea.Batch(m.refreshSourceControl(), scTick())
	case scStatusMsg:
		delete(m.scLoading, msg.root)
		m.scCache[msg.root] = msg
		if msg.root == m.projectTabs.Active() {
			m.sourceControl.SetStatus(msg.root, msg.branch, msg.files, msg.autoCommit, msg.err)
		}
		if m.bar.sync == nil {
			m.bar.sync = map[string]git.SyncState{}
		}
		m.bar.sync[msg.root] = msg.sync
		return m, m.refreshPR(msg.root, msg.branch)
	case prMsg:
		m.applyPR(msg)
		return m, nil
	case pushStartMsg:
		return m, m.startPush(msg.root)
	case pushDoneMsg:
		return m, m.pushDone(msg)
	case scActionDoneMsg:
		return m, tea.Sequence(m.refreshSourceControl(), func() tea.Msg { return scRefreshDiffMsg{} })
	case scRefreshDiffMsg:
		if m.sourceControl.Focused() {
			return m, m.showSelectedFileDiff()
		}
		return m, nil
	case updateCheckMsg:
		return m, m.updateCheck()
	case updateReadyMsg:
		m.updateReady = true
		return m, m.restartIfReady()
	case keyupMsg:
		m.menu.ClearKeydown()
		return m, nil
	case instanceStartDoneMsg:
		m.instanceStarting = false
		inst := msg.instance
		m.startingInstance = nil

		if msg.err != nil {
			// Start failed — remove the instance from the list and show the error.
			m.list.Kill()
			return m, tea.Batch(tea.WindowSize(), m.instanceChanged(), m.handleError(msg.err))
		}

		// Save after successful start.
		if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
			return m, m.handleError(err)
		}

		if m.promptAfterName {
			m.state = statePrompt
			m.menu.SetState(ui.StatePrompt)
			m.textInputOverlay = overlay.NewTextInputOverlay("Enter prompt", "")
			m.promptAfterName = false
		} else {
			m.showHelpScreen(helpStart(inst), nil)
		}

		return m, tea.Batch(tea.WindowSize(), m.instanceChanged())
	case metadataUpdateDoneMsg:
		prompted := map[*session.Instance]bool{}
		for _, r := range msg.results {
			prompted[r.instance] = r.hasPrompt
		}
		for _, inst := range m.list.GetInstances() {
			inst.NeedsYou = prompted[inst] || msg.agentStatus[inst.TmuxName()] == "waiting"
		}
		for _, r := range msg.results {
			// Skip instances that were paused while metadata was being computed
			if r.instance.Status == session.Paused {
				continue
			}
			if r.updated {
				r.instance.SetStatus(session.Running)
			} else if r.hasPrompt {
				r.instance.TapEnter()
			} else {
				r.instance.SetStatus(session.Ready)
			}
			if r.diffStats != nil && r.diffStats.Error != nil {
				if !strings.Contains(r.diffStats.Error.Error(), "base commit SHA not set") {
					log.WarningLog.Printf("could not update diff stats: %v", r.diffStats.Error)
				}
				r.instance.SetDiffStats(nil)
			} else {
				r.instance.SetDiffStats(r.diffStats)
			}
		}
		m.setExternalSessions(msg.external)
		if !m.sessionsLoaded && m.restoredRow != "" {
			// Just updated: select the session that was being typed into.
			m.selectRowKey(m.restoredRow)
		}
		m.sessionsLoaded = true
		if len(m.gridRows()) == 0 {
			m.ready = true // nothing to capture
		}
		focus := m.focusNewQuestions()
		if !m.autoFocused && (m.list.GetSelectedInstance() != nil || m.list.GetSelectedExternal() != nil || len(m.gridRows()) == 0) {
			// Start with the keyboard on the selected session.
			m.autoFocused = true
			focus = m.autoFocus()
		}
		return m, tea.Batch(
			focus,
			tickUpdateMetadataCmd(m.snapshotActiveInstances(), m.list.GetSelectedInstance(), m.refreshInterval()),
			m.restartIfReady(),
		)
	case sessionMovedMsg:
		m.setExternalSessions(msg.sessions)
		m.list.SelectExternal(msg.id)
		return m, m.autoFocus()
	case sessionStartedMsg:
		m.setExternalSessions(msg.sessions)
		m.list.SelectExternal(msg.name)
		return m, tea.Batch(m.instanceChanged(), m.autoFocus())
	case projectChosenMsg:
		if msg.err != nil {
			return m, m.handleError(msg.err)
		}
		if msg.path == "" {
			return m, nil
		}
		if err := m.appConfig.OpenProject(msg.path); err != nil {
			log.WarningLog.Printf("failed to save open project: %v", err)
		}
		m.refreshTabs(msg.path)
		return m, m.switchProject(msg.path)
	case tea.MouseMsg:
		// Clicks on the project tab row: switch tab or add a project.
		// Clicks on the status bar (the last row).
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.Y == m.screenHeight-1 && m.state == stateDefault {
			if cmd, ok := m.clickBottomRow(msg.X); ok {
				return m, cmd
			}
		}
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && msg.Y == 0 && m.state == stateDefault {
			switch hit := m.projectTabs.HitTest(msg.X); {
			case hit == ui.HitAddProject:
				return m, chooseProjectCmd
			case hit >= 0:
				return m, m.switchProject(m.projectTabs.Select(hit))
			}
			return m, nil
		}
		if cmd, ok := m.handleRecentMouse(msg); ok {
			return m, cmd
		}
		if cmd, ok := m.handleServersMouse(msg); ok {
			return m, cmd
		}
		if cmd, ok := m.handleDockMouse(msg); ok {
			return m, cmd
		}
		if cmd, ok := m.handleGridMouse(msg); ok {
			return m, cmd
		}
		// Handle mouse wheel events for scrolling the diff/preview pane
		if msg.Action == tea.MouseActionPress {
			if msg.Button == tea.MouseButtonWheelDown || msg.Button == tea.MouseButtonWheelUp {
				selected := m.list.GetSelectedInstance()
				if selected == nil || selected.Status == session.Paused {
					return m, nil
				}

				switch msg.Button {
				case tea.MouseButtonWheelUp:
					m.tabbedWindow.ScrollUp()
				case tea.MouseButtonWheelDown:
					m.tabbedWindow.ScrollDown()
				}
			}
		}
		return m, nil
	case branchSearchDebounceMsg:
		// Debounce timer fired — check if this is still the current filter version
		if m.textInputOverlay == nil {
			return m, nil
		}
		if msg.version != m.textInputOverlay.BranchFilterVersion() {
			return m, nil // stale, a newer debounce is pending
		}
		return m, m.runBranchSearch(msg.filter, msg.version)
	case branchSearchResultMsg:
		if m.textInputOverlay != nil {
			m.textInputOverlay.SetBranchResults(msg.branches, msg.version)
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		logEvent("window %dx%d", msg.Width, msg.Height)
		m.updateHandleWindowSizeEvent(msg)
		return m, nil
	case error:
		// Handle errors from confirmation actions
		return m, m.handleError(msg)
	case externalClosedMsg:
		if m.justClosed == nil {
			m.justClosed = map[string]time.Time{}
		}
		if msg.finished && msg.project != "" && m.progress != nil {
			m.progress.addDone(msg.project)
		}
		if msg.project != "" {
			entry := closedEntry{Project: msg.project, Name: msg.name, Title: msg.title,
				SessionID: msg.sessionID, Issue: session.IssueNumberOf(msg.name), At: time.Now()}
			m.noteClosed(entry)
			m.noteGhost(msg.name, msg.title) // before the grid drops it
		}
		for _, k := range closedKeys(msg.name, msg.sessionID, msg.pid) {
			m.justClosed[k] = time.Now()
		}
		m.setExternalSessions(m.list.ExternalSessions())
		delete(m.tileCache, "session:"+msg.name)
		// Rebuild the grid now, even if a capture is running (its stale
		// result is dropped in applyGridCapture).
		m.gridCapturing = false
		return m, tea.Batch(m.autoFocus(), m.refreshGrid())
	case instanceChangedMsg:
		// Handle instance changed after confirmation action
		return m, m.instanceChanged()
	case instanceStartedMsg:
		// Select the instance that just started (or failed)
		m.list.SelectInstance(msg.instance)

		if msg.err != nil {
			m.list.Kill()
			return m, tea.Batch(m.handleError(msg.err), m.instanceChanged())
		}

		// Save after successful start
		if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
			return m, m.handleError(err)
		}
		if m.autoYes {
			msg.instance.AutoYes = true
		}

		if msg.promptAfterName {
			m.state = statePrompt
			m.menu.SetState(ui.StatePrompt)
			m.textInputOverlay = m.newPromptOverlay()
		} else {
			// If instance has a prompt (set from Shift+N flow), send it now
			if msg.instance.Prompt != "" {
				if err := msg.instance.SendPrompt(msg.instance.Prompt); err != nil {
					log.ErrorLog.Printf("failed to send prompt: %v", err)
				}
				msg.instance.Prompt = ""
			}
			m.menu.SetState(ui.StateDefault)
			m.showHelpScreen(helpStart(msg.instance), nil)
		}

		return m, tea.Batch(tea.WindowSize(), m.instanceChanged())
	case spinner.TickMsg:
		// The spinner belonged to the old session list, which is no longer
		// shown; letting it tick would redraw the screen ~8 times a second.
		return m, nil
	}
	return m, nil
}

func (m *home) handleQuit() (tea.Model, tea.Cmd) {
	logEvent("quit")
	if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
		return m, m.handleError(err)
	}
	closeLiveWrappers()
	return m, tea.Quit
}

func (m *home) handleMenuHighlighting(msg tea.KeyMsg) (cmd tea.Cmd, returnEarly bool) {
	// Handle menu highlighting when you press a button. We intercept it here and immediately return to
	// update the ui while re-sending the keypress. Then, on the next call to this, we actually handle the keypress.
	if m.keySent {
		m.keySent = false
		return nil, false
	}
	if m.state == statePrompt || m.state == stateHelp || m.state == stateConfirm {
		return nil, false
	}
	// If it's in the global keymap, we should try to highlight it.
	name, ok := keys.GlobalKeyStringsMap[msg.String()]
	if !ok {
		return nil, false
	}

	if m.list.GetSelectedInstance() != nil && m.list.GetSelectedInstance().Paused() && name == keys.KeyEnter {
		return nil, false
	}
	if name == keys.KeyShiftDown || name == keys.KeyShiftUp {
		return nil, false
	}

	// Skip the menu highlighting if the key is not in the map or we are using the shift up and down keys.
	// TODO: cleanup: when you press enter on stateNew, we use keys.KeySubmitName. We should unify the keymap.
	if name == keys.KeyEnter && m.state == stateNew {
		name = keys.KeySubmitName
	}
	m.keySent = true
	return tea.Batch(
		func() tea.Msg { return msg },
		m.keydownCallback(name)), true
}

func (m *home) handleKeyPress(msg tea.KeyMsg) (mod tea.Model, cmd tea.Cmd) {
	if !m.appConfig.MouseOff && isMouseFragment(msg) {
		// Pieces of a mouse event split across reads: never type them.
		logEvent("dropped a split mouse event (%d chars)", len(msg.Runes))
		return m, nil
	}
	m.lastKey = time.Now()
	m.sel.shown = false // a key press clears the selection highlight
	// Keys typed into a session are in keys.log already; describing the full
	// state for each of them (hundreds a second when scrolling) costs CPU.
	if m.sessionFocus == "" || m.state != stateDefault {
		logEvent("key %-14s %s", describeKey(msg), m.stateName())
	}

	// Ctrl+] command key works from anywhere on the main screen.
	if m.state == stateDefault {
		if m.leader {
			if msg.String() == leaderSpace {
				// Terminal.app can deliver one Ctrl+Space as two key events;
				// a repeat keeps command mode on instead of cancelling it.
				logEvent("command key: repeated ⌃Space ignored")
				return m, nil
			}
			return m, m.handleLeader(msg)
		}
		if isLeaderKey(msg.String()) {
			m.leader = true
			return m, nil
		}
	}

	// Typing into a session comes first, before any cs shortcut (q, ctrl+c…).
	if m.sessionFocus != "" && m.state == stateDefault {
		return m.handleSessionKey(msg)
	}
	if m.state == stateCommit {
		return m, m.handleCommitKey(msg)
	}
	if m.state == stateIssuePicker {
		return m, m.handleIssuePickerKey(msg)
	}
	if m.serversFocused && m.state == stateDefault {
		return m, m.handleServersKey(msg)
	}
	if m.sourceControl.Focused() && m.state == stateDefault {
		if cmd, ok := m.handleSourceControlKey(msg); ok {
			return m, cmd
		}
	}
	// A view-only session is selected: plain keys would otherwise fire list
	// shortcuts (j/k, arrows, t, q…) while the user thinks they're typing.
	// (logged as "view-only:" in the key line's state)
	if e := m.list.GetSelectedExternal(); e != nil && e.Kind == session.KindTerminal &&
		m.state == stateDefault && !m.sourceControl.Focused() {
		if cmd, ok := m.navKey(msg); ok {
			return m, cmd
		}
		switch msg.String() {
		case "enter", "ctrl+q", "!", "+", "x":
			// handled below: move into cs, quit, jump, add/close tab
		default:
			return m, m.handleError(fmt.Errorf("view only: this session runs in its own window. Press Enter to move it into cs so you can type here"))
		}
	}

	cmd, returnEarly := m.handleMenuHighlighting(msg)
	if returnEarly {
		return m, cmd
	}

	if m.state == stateHelp {
		return m.handleHelpState(msg)
	}

	if m.state == stateNew {
		// Handle quit commands first. Don't handle q because the user might want to type that.
		if msg.String() == "ctrl+c" {
			m.state = stateDefault
			m.promptAfterName = false
			m.list.Kill()
			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					return nil
				},
			)
		}

		// The instance being named is the one just added and selected.
		instance := m.list.GetSelectedInstance()
		switch msg.Type {
		// Start the instance (enable previews etc) and go back to the main menu state.
		case tea.KeyEnter:
			if len(instance.Title) == 0 {
				return m, m.handleError(fmt.Errorf("title cannot be empty"))
			}

			// If promptAfterName, show prompt+branch overlay before starting
			if m.promptAfterName {
				m.promptAfterName = false
				m.state = statePrompt
				m.menu.SetState(ui.StatePrompt)
				m.textInputOverlay = m.newPromptOverlay()
				// Trigger initial branch search (no debounce, version 0)
				initialSearch := m.runBranchSearch("", m.textInputOverlay.BranchFilterVersion())
				return m, tea.Batch(tea.WindowSize(), initialSearch)
			}

			// Set Loading status and finalize into the list immediately
			instance.SetStatus(session.Loading)
			m.newInstanceFinalizer()
			m.promptAfterName = false
			m.state = stateDefault
			m.menu.SetState(ui.StateDefault)

			// Return a tea.Cmd that runs instance.Start in the background
			startCmd := func() tea.Msg {
				err := instance.Start(true)
				return instanceStartedMsg{
					instance:        instance,
					err:             err,
					promptAfterName: false,
				}
			}

			return m, tea.Batch(tea.WindowSize(), m.instanceChanged(), startCmd)
		case tea.KeyRunes:
			if runewidth.StringWidth(instance.Title) >= 32 {
				return m, m.handleError(fmt.Errorf("title cannot be longer than 32 characters"))
			}
			if err := instance.SetTitle(instance.Title + string(msg.Runes)); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeyBackspace:
			runes := []rune(instance.Title)
			if len(runes) == 0 {
				return m, nil
			}
			if err := instance.SetTitle(string(runes[:len(runes)-1])); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeySpace:
			if err := instance.SetTitle(instance.Title + " "); err != nil {
				return m, m.handleError(err)
			}
		case tea.KeyEsc:
			m.list.Kill()
			m.state = stateDefault
			m.instanceChanged()

			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					return nil
				},
			)
		default:
		}
		return m, nil
	} else if m.state == statePrompt {
		// Handle cancel via ctrl+c before delegating to the overlay
		if msg.String() == "ctrl+c" {
			return m, m.cancelPromptOverlay()
		}

		// Use the new TextInputOverlay component to handle all key events
		shouldClose, branchFilterChanged := m.textInputOverlay.HandleKeyPress(msg)

		// Check if the form was submitted or canceled
		if shouldClose {
			selected := m.list.GetSelectedInstance()
			if selected == nil {
				return m, nil
			}

			if m.textInputOverlay.IsCanceled() {
				return m, m.cancelPromptOverlay()
			}

			if m.textInputOverlay.IsSubmitted() {
				prompt := m.textInputOverlay.GetValue()
				selectedBranch := m.textInputOverlay.GetSelectedBranch()
				selectedProgram := m.textInputOverlay.GetSelectedProgram()

				if !selected.Started() {
					// Shift+N flow: instance not started yet — set branch, start, then send prompt
					if selectedBranch != "" {
						selected.SetSelectedBranch(selectedBranch)
					}
					if selectedProgram != "" {
						selected.Program = selectedProgram
					}
					selected.Prompt = prompt

					// Finalize into list and start
					selected.SetStatus(session.Loading)
					m.newInstanceFinalizer()
					m.textInputOverlay = nil
					m.state = stateDefault
					m.menu.SetState(ui.StateDefault)

					startCmd := func() tea.Msg {
						err := selected.Start(true)
						return instanceStartedMsg{
							instance:        selected,
							err:             err,
							promptAfterName: false,
							selectedBranch:  selectedBranch,
						}
					}

					return m, tea.Batch(tea.WindowSize(), m.instanceChanged(), startCmd)
				}

				// Regular flow: instance already running, just send prompt
				if err := selected.SendPrompt(prompt); err != nil {
					return m, m.handleError(err)
				}
			}

			// Close the overlay and reset state
			m.textInputOverlay = nil
			m.state = stateDefault
			return m, tea.Sequence(
				tea.WindowSize(),
				func() tea.Msg {
					m.menu.SetState(ui.StateDefault)
					m.showHelpScreen(helpStart(selected), nil)
					return nil
				},
			)
		}

		// Schedule a debounced branch search if the filter changed
		if branchFilterChanged {
			filter := m.textInputOverlay.BranchFilter()
			version := m.textInputOverlay.BranchFilterVersion()
			return m, m.scheduleBranchSearch(filter, version)
		}

		return m, nil
	}

	// Handle confirmation state
	if m.state == stateConfirm {
		shouldClose := m.confirmationOverlay.HandleKeyPress(msg)
		if shouldClose {
			m.state = stateDefault
			m.confirmationOverlay = nil
			if result := m.confirmResult; result != nil {
				m.confirmResult = nil
				return m, func() tea.Msg { return result }
			}
			return m, nil
		}
		return m, nil
	}

	// Exit scrolling mode when ESC is pressed and preview pane is in scrolling mode
	// Check if Escape key was pressed and we're not in the diff tab (meaning we're in preview tab)
	// Always check for escape key first to ensure it doesn't get intercepted elsewhere
	if msg.Type == tea.KeyEsc {
		// If in preview tab and in scroll mode, exit scroll mode
		if m.tabbedWindow.IsInPreviewTab() && m.tabbedWindow.IsPreviewInScrollMode() {
			// Use the selected instance from the list
			selected := m.list.GetSelectedInstance()
			err := m.tabbedWindow.ResetPreviewToNormalMode(selected)
			if err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
		// If in terminal tab and in scroll mode, exit scroll mode
		if m.tabbedWindow.IsInTerminalTab() && m.tabbedWindow.IsTerminalInScrollMode() {
			m.tabbedWindow.ResetTerminalToNormalMode()
			return m, m.instanceChanged()
		}
	}

	// Handle quit commands first. ctrl+q quits too, so pressing it twice from
	// a session (stop typing, then quit) always gets out of cs.
	if msg.String() == "ctrl+c" || msg.String() == "q" || msg.String() == "ctrl+q" {
		return m.handleQuit()
	}

	if cmd, ok := m.navKey(msg); ok {
		return m, cmd
	}
	switch msg.String() {
	case "left":
		return m, m.moveProject(true)
	case "right":
		return m, m.moveProject(false)
	case "i", "I":
		return m, m.autoFocus()
	case "+", "=":
		return m, chooseProjectCmd
	case "x":
		return m, m.closeActiveProject()
	case "!":
		return m, m.jumpToNeedsYou()
	case "M":
		return m, m.toggleMouse()
	case "s", "S", "ctrl+s":
		return m, m.openSourceControl()
	}

	name, ok := keys.GlobalKeyStringsMap[msg.String()]
	if !ok {
		return m, nil
	}

	if (name == keys.KeyNew || name == keys.KeyPrompt || name == keys.KeySession) && !m.activeProjectExists() {
		return m, m.handleError(fmt.Errorf("this project's folder no longer exists; press x to close the tab"))
	}

	switch name {
	case keys.KeySession:
		return m, m.newClaudeSession()
	case keys.KeyHelp:
		return m.showHelpScreen(helpTypeGeneral{}, nil)
	case keys.KeyPrompt:
		if m.list.NumInstances() >= GlobalInstanceLimit {
			return m, m.handleError(
				fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
		}

		// Start a background fetch so branches are up to date by the time the picker opens
		fetchCmd := func() tea.Msg {
			currentDir, _ := os.Getwd()
			git.FetchBranches(currentDir)
			return nil
		}

		instance, err := session.NewInstance(session.InstanceOptions{
			Title:   "",
			Path:    ".",
			Program: m.program,
		})
		if err != nil {
			return m, m.handleError(err)
		}

		m.newInstanceFinalizer = m.list.AddInstance(instance)
		m.list.SetSelectedInstance(m.list.NumInstances() - 1)
		m.state = stateNew
		m.menu.SetState(ui.StateNewInstance)
		m.promptAfterName = true

		return m, fetchCmd
	case keys.KeyNew:
		if m.list.NumInstances() >= GlobalInstanceLimit {
			return m, m.handleError(
				fmt.Errorf("you can't create more than %d instances", GlobalInstanceLimit))
		}
		instance, err := session.NewInstance(session.InstanceOptions{
			Title:   "",
			Path:    ".",
			Program: m.program,
		})
		if err != nil {
			return m, m.handleError(err)
		}

		m.newInstanceFinalizer = m.list.AddInstance(instance)
		m.list.SetSelectedInstance(m.list.NumInstances() - 1)
		m.state = stateNew
		m.menu.SetState(ui.StateNewInstance)

		return m, nil
	case keys.KeyUp:
		m.list.Up()
		return m, m.instanceChanged()
	case keys.KeyDown:
		m.list.Down()
		return m, m.instanceChanged()
	case keys.KeyShiftUp:
		m.tabbedWindow.ScrollUp()
		return m, m.instanceChanged()
	case keys.KeyShiftDown:
		m.tabbedWindow.ScrollDown()
		return m, m.instanceChanged()
	case keys.KeyKill:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}

		// Create the kill action as a tea.Cmd
		killAction := func() tea.Msg {
			// Get worktree and check if branch is checked out
			worktree, err := selected.GetGitWorktree()
			if err != nil {
				return err
			}

			checkedOut, err := worktree.IsBranchCheckedOut()
			if err != nil {
				return err
			}

			if checkedOut {
				return fmt.Errorf("instance %s is currently checked out", selected.Title)
			}

			// Clean up terminal session for this instance
			m.tabbedWindow.CleanupTerminalForInstance(selected.Title)

			// Delete from storage first
			if err := m.storage.DeleteInstance(selected.Title); err != nil {
				return err
			}

			// Then kill the instance
			m.list.Kill()
			return instanceChangedMsg{}
		}

		// Show confirmation modal
		message := fmt.Sprintf("[!] Kill session '%s'?", selected.Title)
		return m, m.confirmAction(message, killAction)
	case keys.KeySubmit:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}

		// Create the push action as a tea.Cmd
		pushAction := func() tea.Msg {
			// Default commit message with timestamp
			commitMsg := fmt.Sprintf("[claudesquad] update from '%s' on %s", selected.Title, time.Now().Format(time.RFC822))
			worktree, err := selected.GetGitWorktree()
			if err != nil {
				return err
			}
			if err = worktree.PushChanges(commitMsg, true); err != nil {
				return err
			}
			return nil
		}

		// Show confirmation modal
		message := fmt.Sprintf("[!] Push changes from session '%s'?", selected.Title)
		return m, m.confirmAction(message, pushAction)
	case keys.KeyCheckout:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}

		// Show help screen before pausing
		m.showHelpScreen(helpTypeInstanceCheckout{}, func() {
			if err := selected.Pause(); err != nil {
				m.handleError(err)
			}
			m.tabbedWindow.CleanupTerminalForInstance(selected.Title)
			m.instanceChanged()
		})
		return m, nil
	case keys.KeyMoveUp:
		if m.list.MoveUp() {
			if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
		return m, nil
	case keys.KeyMoveDown:
		if m.list.MoveDown() {
			if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
				return m, m.handleError(err)
			}
			return m, m.instanceChanged()
		}
		return m, nil
	case keys.KeyResume:
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Status == session.Loading {
			return m, nil
		}
		if err := selected.Resume(); err != nil {
			return m, m.handleError(err)
		}
		return m, tea.WindowSize()
	case keys.KeyEnter:
		if e := m.list.GetSelectedExternal(); e != nil && e.Kind == session.KindTerminal {
			e := e
			return m, m.confirmAction(
				fmt.Sprintf("Move '%s' into cs? It closes in its other window (that window keeps a shell prompt) and continues here with the same conversation.", e.Title()),
				func() tea.Msg {
					id, err := e.MoveIntoBackground()
					if err != nil {
						return err
					}
					list, _, _ := session.ListExternalSessions()
					return sessionMovedMsg{id: id, sessions: list}
				})
		}
		if msg.String() == "enter" {
			return m, m.focusSession()
		}
		// o: open the session full screen instead.
		if external := m.list.GetSelectedExternal(); external != nil && external.Kind == session.KindBackground {
			// Claude's own attach; Ctrl+Z returns here and the session keeps running.
			return m, tea.ExecProcess(external.AttachCommand(), func(err error) tea.Msg {
				if err != nil {
					return fmt.Errorf("could not open session: %w", err)
				}
				return instanceChangedMsg{}
			})
		}
		if external := m.list.GetSelectedExternal(); external != nil {
			m.showHelpScreen(helpTypeInstanceAttach{}, func() {
				ch, err := m.list.Attach()
				if err != nil {
					m.handleError(err)
					return
				}
				<-ch
				m.state = stateDefault
				m.instanceChanged()
			})
			return m, nil
		}
		if m.list.NumInstances() == 0 {
			return m, nil
		}
		selected := m.list.GetSelectedInstance()
		if selected == nil || selected.Paused() || selected.Status == session.Loading || !selected.TmuxAlive() {
			return m, nil
		}
		// Terminal tab: attach to terminal session
		if m.tabbedWindow.IsInTerminalTab() {
			m.showHelpScreen(helpTypeInstanceAttach{}, func() {
				ch, err := m.tabbedWindow.AttachTerminal()
				if err != nil {
					m.handleError(err)
					return
				}
				<-ch
				m.state = stateDefault
			})
			return m, nil
		}
		// Show help screen before attaching
		m.showHelpScreen(helpTypeInstanceAttach{}, func() {
			ch, err := m.list.Attach()
			if err != nil {
				m.handleError(err)
				return
			}
			<-ch
			m.state = stateDefault
			m.instanceChanged()
		})
		return m, nil
	default:
		return m, nil
	}
}

// setExternalSessions shows the Claude sessions started outside cs, giving each
// project that has one a tab so nothing running is hidden. Sessions outside
// any git repository are listed under no tab.
func (m *home) setExternalSessions(sessions []*session.ExternalSession) {
	if len(m.justClosed) > 0 {
		kept := sessions[:0:0]
		for _, e := range sessions {
			hidden := false
			for _, k := range closedKeys(e.Name, e.SessionID, e.Pid) {
				if at, ok := m.justClosed[k]; ok && time.Since(at) < justClosedFor {
					hidden = true
				}
			}
			if !hidden {
				kept = append(kept, e)
			}
		}
		sessions = kept
	}
	if m.repoRoots == nil {
		m.repoRoots = map[string]string{}
	}
	var roots []string
	for _, e := range sessions {
		root, ok := m.repoRoots[e.Path]
		if !ok {
			root, _ = git.RepoRoot(e.Path)
			m.repoRoots[e.Path] = root
		}
		if root != "" {
			// Match tabs by the repo's real path (/tmp vs /private/tmp, symlinks).
			e.Path = root
			if !containsString(roots, root) {
				roots = append(roots, root)
			}
		}
	}
	m.logSessionChanges(sessions)
	m.list.SetExternal(sessions)
	if !equalStrings(roots, m.sessionRoots) {
		m.sessionRoots = roots
		m.refreshTabs(m.projectTabs.Active())
	}
}

// logSessionChanges logs sessions that appeared or went away since the last
// refresh.
func (m *home) logSessionChanges(sessions []*session.ExternalSession) {
	now := map[string]string{}
	for _, e := range sessions {
		now[e.Name] = e.Title() + " (" + e.Describe() + ")"
	}
	for name, d := range now {
		prev, ok := m.lastSessions[name]
		switch {
		case !ok && m.lastSessions != nil:
			logEvent("session appeared: %s %s", name, d)
		case ok && prev != d:
			logEvent("session changed: %s %s -> %s", name, prev, d)
		}
	}
	// cs's own agents: log status changes too.
	agents := map[string]string{}
	for _, inst := range m.list.GetInstances() {
		d := inst.Title + " status=" + strconv.Itoa(int(inst.Status))
		if inst.NeedsYou {
			d += " needs-you"
		}
		agents[inst.TmuxName()] = d
		if prev, ok := m.lastAgents[inst.TmuxName()]; ok && prev != d {
			logEvent("agent changed: %s %s -> %s", inst.TmuxName(), prev, d)
		}
	}
	m.lastAgents = agents
	for name, d := range m.lastSessions {
		if _, ok := now[name]; !ok {
			logEvent("session gone: %s %s", name, d)
		}
	}
	m.lastSessions = now
}

// refreshTabs rebuilds the tab row: the tabs the user opened (saved), then a
// temporary tab for every other project with a running Claude session (e.g.
// a CI runner's checkout), which disappears when its sessions end.
func (m *home) refreshTabs(active string) {
	tabs := append([]string(nil), m.appConfig.OpenProjects...)
	for _, r := range m.sessionRoots {
		if !containsString(tabs, r) {
			tabs = append(tabs, r)
		}
	}
	m.projectTabs.SetProjects(tabs, active)
	if active != "" && m.projectTabs.Active() != active {
		// The active tab was a temporary one whose sessions ended.
		_ = m.switchProject(m.projectTabs.Active())
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sessionMovedMsg reports a terminal session moved into the background, so
// it can be selected and typed into.
type sessionMovedMsg struct {
	id       string
	sessions []*session.ExternalSession
}

// sessionStartedMsg reports a Claude session started with s, so it can be
// selected straight away.
type sessionStartedMsg struct {
	name     string
	sessions []*session.ExternalSession
}

// projectChosenMsg carries the result of the add-project folder chooser.
type projectChosenMsg struct {
	path string
	err  error
}

// chooseProjectCmd opens the macOS folder chooser off the UI thread.
func chooseProjectCmd() tea.Msg {
	path, err := ChooseProjectFolder()
	return projectChosenMsg{path: path, err: err}
}

// switchProject makes project the active tab: new agents start there and the
// list shows only its agents. Agents in other projects keep running.
func (m *home) switchProject(project string) tea.Cmd {
	logEvent("project -> %s", filepath.Base(project))
	if project == "" {
		return nil
	}
	m.list.SetProject(project)
	m.layoutLeft() // each project has its own servers list
	if cached, ok := m.scCache[project]; ok {
		m.sourceControl.SetStatus(cached.root, cached.branch, cached.files, cached.autoCommit, cached.err)
	} else {
		m.sourceControl.SetLoading(project)
	}
	refresh := m.refreshSourceControl()
	if err := os.Chdir(project); err != nil {
		// Still show the tab and its agents so it can be closed with x, but
		// don't make it the saved active project.
		m.instanceChanged()
		return m.handleError(fmt.Errorf("%s folder no longer exists; press x to close this tab", filepath.Base(project)))
	}
	m.appConfig.ActiveProject = project
	if err := config.SaveConfig(m.appConfig); err != nil {
		log.WarningLog.Printf("failed to save active project: %v", err)
	}
	// Redraw the grid for the new tab now (from cache), not on the next tick.
	return tea.Batch(m.instanceChanged(), refresh, m.refreshGrid(), m.refreshDock())
}

// jumpToNeedsYou selects the next session waiting on the user, switching
// tab if it is in another project.
func (m *home) jumpToNeedsYou() tea.Cmd {
	project, inst, ext := m.list.NextNeedsYou(m.projectTabs.Projects(), m.projectTabs.Active())
	if project == "" {
		return m.handleError(fmt.Errorf("no session is waiting on you"))
	}
	var cmd tea.Cmd
	if project != m.projectTabs.Active() {
		cmd = m.switchProject(m.projectTabs.Select(indexOf(m.projectTabs.Projects(), project)))
	}
	if inst != nil {
		m.list.SelectInstance(inst)
	} else {
		m.list.SelectExternal(ext.Name)
	}
	return tea.Batch(cmd, m.instanceChanged())
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// activeProjectExists reports whether the active tab's folder is still on disk.
func (m *home) activeProjectExists() bool {
	_, err := os.Stat(m.projectTabs.Active())
	return err == nil
}

// closeActiveProject removes the active tab. A project with agents can't be
// closed, so no agent ends up running without a tab to reach it from.
func (m *home) closeActiveProject() tea.Cmd {
	project := m.projectTabs.Active()
	if project == "" {
		return nil
	}
	name := filepath.Base(project)
	n := m.list.CountInProject(project)
	if _, err := os.Stat(project); err != nil {
		// The folder is gone, so its agents can't run or be deleted normally.
		// Closing the tab forgets them.
		return m.confirmAction(fmt.Sprintf("%s folder no longer exists. Close the tab and forget its %d agent(s)?", name, n), func() tea.Msg {
			for _, inst := range append([]*session.Instance(nil), m.list.GetInstances()...) {
				if !ui.InProject(inst, project) {
					continue
				}
				if err := inst.Kill(); err != nil {
					log.WarningLog.Printf("cleanup of %s: %v", inst.Title, err)
				}
				m.list.Remove(inst)
			}
			if err := m.storage.SaveInstances(m.list.GetInstances()); err != nil {
				return err
			}
			if err := m.appConfig.CloseProject(project); err != nil {
				log.WarningLog.Printf("failed to save open projects: %v", err)
			}
			m.refreshTabs(m.appConfig.ActiveProject)
			if cmd := m.switchProject(m.projectTabs.Active()); cmd != nil {
				return cmd()
			}
			return instanceChangedMsg{}
		})
	}
	if n > 0 {
		return m.handleError(fmt.Errorf("%s still has %d agent(s); delete them (D) before closing the tab", name, n))
	}
	if e := m.externalCount(project); e > 0 {
		return m.handleError(fmt.Errorf("%s has %d Claude session(s) running; the tab closes once they end", name, e))
	}
	if len(m.projectTabs.Projects()) == 1 {
		return m.handleError(fmt.Errorf("can't close the only project tab; add another with + first"))
	}
	closeAction := func() tea.Msg {
		if err := m.appConfig.CloseProject(project); err != nil {
			log.WarningLog.Printf("failed to save open projects: %v", err)
		}
		m.refreshTabs(m.appConfig.ActiveProject)
		if cmd := m.switchProject(m.projectTabs.Active()); cmd != nil {
			return cmd()
		}
		return nil
	}
	return m.confirmAction(fmt.Sprintf("Close the %s tab? (agents elsewhere keep running)", filepath.Base(project)), closeAction)
}

// instanceChanged updates the preview pane, menu, and diff pane based on the selected instance. It returns an error
// Cmd if there was any error.
func (m *home) instanceChanged() tea.Cmd {
	// selected may be nil
	selected := m.list.GetSelectedInstance()

	// Typing must never reach a session other than the one on screen. A new
	// selection showing the same screen (e.g. Claude started in the project
	// terminal) keeps it.
	if m.sessionFocus != "" && m.sessionsLoaded && m.selectedRowKey() != m.sessionFocusRow {
		if m.selectedLiveName() == m.sessionFocus {
			m.sessionFocusRow = m.selectedRowKey()
		} else {
			logEvent("typing off: selection moved from %s to %s", m.sessionFocusRow, m.selectedRowKey())
			m.sessionFocus = ""
		}
	}

	// Session screens are shown by the grid tiles, captured off the UI
	// thread (refreshGrid). The old preview pane is only used for Source
	// Control diffs, so nothing is captured here: blocking tmux calls on
	// this path made every key and switch lag.
	if external := m.list.GetSelectedExternal(); external != nil {
		m.tabbedWindow.SetInstance(nil)
		m.menu.SetInstance(nil)
		return nil
	}
	m.tabbedWindow.SetInstance(selected)
	m.menu.SetInstance(selected)
	return nil
}

type keyupMsg struct{}

// keydownCallback clears the menu option highlighting after 500ms.
func (m *home) keydownCallback(name keys.KeyName) tea.Cmd {
	m.menu.Keydown(name)
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}

		return keyupMsg{}
	}
}

// hideErrMsg implements tea.Msg and clears the error text from the screen.
type hideErrMsg struct{}

// previewTickMsg implements tea.Msg and triggers a preview update
type previewTickMsg struct{}

type instanceChangedMsg struct{}

type instanceStartedMsg struct {
	instance        *session.Instance
	err             error
	promptAfterName bool
	selectedBranch  string
}

// branchSearchDebounceMsg fires after the debounce interval to trigger a search.
type branchSearchDebounceMsg struct {
	filter  string
	version uint64
}

// branchSearchResultMsg carries search results back to Update.
type branchSearchResultMsg struct {
	branches []string
	version  uint64
}

const branchSearchDebounce = 150 * time.Millisecond

// scheduleBranchSearch returns a debounced tea.Cmd: sleeps, then triggers a search message.
func (m *home) scheduleBranchSearch(filter string, version uint64) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(branchSearchDebounce)
		return branchSearchDebounceMsg{filter: filter, version: version}
	}
}

// runBranchSearch returns a tea.Cmd that performs the git search in the background.
func (m *home) runBranchSearch(filter string, version uint64) tea.Cmd {
	return func() tea.Msg {
		currentDir, _ := os.Getwd()
		branches, err := git.SearchBranches(currentDir, filter)
		if err != nil {
			log.WarningLog.Printf("branch search failed: %v", err)
			return nil
		}
		return branchSearchResultMsg{branches: branches, version: version}
	}
}

// instanceMetaResult holds the results of a single instance's metadata update,
// computed in a background goroutine.
type instanceMetaResult struct {
	instance  *session.Instance
	updated   bool
	hasPrompt bool
	diffStats *git.DiffStats
}

// metadataUpdateDoneMsg is sent when the background metadata update completes.
type metadataUpdateDoneMsg struct {
	results []instanceMetaResult
	// external is the current list of Claude sessions started outside cs.
	external []*session.ExternalSession
	// agentStatus is Claude's status for cs's own agents, by tmux name.
	agentStatus map[string]string
}

// instanceStartDoneMsg is sent when the background instance start completes.
type instanceStartDoneMsg struct {
	instance *session.Instance
	err      error
}

// runInstanceStartCmd returns a Cmd that performs the expensive instance.Start(true)
// in a background goroutine so the main event loop stays responsive.
func runInstanceStartCmd(instance *session.Instance) tea.Cmd {
	return func() tea.Msg {
		err := instance.Start(true)
		return instanceStartDoneMsg{instance: instance, err: err}
	}
}

// snapshotActiveInstances returns the currently active (started, not paused)
// instances. Called on the main thread so the filtering doesn't race with
// state mutations.
func (m *home) snapshotActiveInstances() []*session.Instance {
	var out []*session.Instance
	for _, inst := range m.list.GetInstances() {
		if inst.Started() && !inst.Paused() {
			out = append(out, inst)
		}
	}
	return out
}

// tickUpdateMetadataCmd returns a self-chaining Cmd that sleeps 500ms, then performs
// expensive metadata I/O (tmux capture, git diff) in parallel background goroutines.
// Because it only re-schedules after completing, overlapping ticks are impossible.
// The active instances slice should be snapshotted on the main thread via
// snapshotActiveInstances() before being passed here.
//
// Only the selected instance gets a full diff (with Content); the rest get a
// lightweight numstat-only summary. This keeps per-instance memory bounded
// since the diff pane only ever renders the selected one.
func tickUpdateMetadataCmd(active []*session.Instance, selected *session.Instance, interval time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(interval)

		listStart := time.Now()
		external, agentStatus, err := session.ListExternalSessions()
		perf.sessionsNanos.Store(int64(time.Since(listStart)))
		if err != nil {
			log.WarningLog.Printf("could not list external sessions: %v", err)
		}

		if len(active) == 0 {
			return metadataUpdateDoneMsg{external: external, agentStatus: agentStatus}
		}

		results := make([]instanceMetaResult, len(active))
		var wg sync.WaitGroup
		for idx, inst := range active {
			wg.Add(1)
			go func(i int, instance *session.Instance) {
				defer wg.Done()
				r := &results[i]
				r.instance = instance
				r.updated, r.hasPrompt = instance.HasUpdated()
				if instance == selected {
					r.diffStats = instance.ComputeDiff()
				} else {
					r.diffStats = instance.ComputeDiffNumstat()
				}
			}(idx, inst)
		}
		wg.Wait()

		return metadataUpdateDoneMsg{results: results, external: external, agentStatus: agentStatus}
	}
}

// handleError handles all errors which get bubbled up to the app. sets the error message. We return a callback tea.Cmd that returns a hideErrMsg message
// which clears the error message after 3 seconds.
func (m *home) handleError(err error) tea.Cmd {
	logEvent("message shown: %v", err)
	log.ErrorLog.Printf("%v", err)
	m.errBox.SetError(err)
	return func() tea.Msg {
		select {
		case <-m.ctx.Done():
		case <-time.After(3 * time.Second):
		}

		return hideErrMsg{}
	}
}

func (m *home) newPromptOverlay() *overlay.TextInputOverlay {
	return overlay.NewTextInputOverlayWithBranchPicker("Enter prompt", "", m.appConfig.GetProfiles())
}

// cancelPromptOverlay cancels the prompt overlay, cleaning up unstarted instances.
func (m *home) cancelPromptOverlay() tea.Cmd {
	selected := m.list.GetSelectedInstance()
	if selected != nil && !selected.Started() {
		m.list.Kill()
	}
	m.textInputOverlay = nil
	m.state = stateDefault
	return tea.Sequence(
		tea.WindowSize(),
		func() tea.Msg {
			m.menu.SetState(ui.StateDefault)
			return nil
		},
	)
}

// confirmAction shows a confirmation modal and stores the action to execute on confirm
func (m *home) confirmAction(message string, action tea.Cmd) tea.Cmd {
	m.state = stateConfirm

	// Create and show the confirmation overlay using ConfirmationOverlay
	m.confirmationOverlay = overlay.NewConfirmationOverlay(message)
	// Set a fixed width for consistent appearance
	m.confirmationOverlay.SetWidth(50)

	// Set callbacks for confirmation and cancellation
	m.confirmationOverlay.OnConfirm = func() {
		m.state = stateDefault
		// Run the action here (it may change the list, which must stay on the
		// UI thread) and keep its result, an error or a follow-up message, so
		// it isn't lost.
		if action != nil {
			m.confirmResult = action()
		}
	}

	m.confirmationOverlay.OnCancel = func() {
		m.state = stateDefault
	}

	return nil
}

func (m *home) View() string {
	if m.skipRender && m.lastView != "" {
		m.skipRender = false
		return m.lastView
	}
	m.skipRender = false
	started := time.Now()
	done := watchBegin("View")
	defer func() {
		done()
		d := time.Since(started)
		perf.renders.Add(1)
		perf.renderNanos.Add(int64(d))
		if d > slowFrame {
			logEvent("slow frame: View took %s", d.Round(time.Millisecond))
		}
	}()
	m.lastView = onBlack(m.view(), m.winW, m.winH)
	return m.highlightSelection(m.lastView)
}

func (m *home) view() string {
	if !m.ready && m.savedFrame != "" && (m.winW == 0 || m.winW == m.savedW && m.winH == m.savedH) {
		// Just updated: keep showing what the previous process showed until
		// everything is loaded, however long a busy Mac takes (capped).
		if time.Since(m.startedAt) < 20*time.Second {
			return m.savedFrame
		}
		m.ready = true
	}
	if !m.ready {
		if time.Since(m.startedAt) > 4*time.Second {
			m.ready = true // don't wait forever on a slow refresh
		} else {
			return lipgloss.Place(max(m.winW, 40), max(m.winH-1, 10), lipgloss.Center, lipgloss.Center,
				lipgloss.NewStyle().Bold(true).Render("Loading your sessions…"))
		}
	}
	for _, p := range m.projectTabs.Projects() {
		m.projectTabs.SetCount(p, m.list.CountInProject(p)+m.externalCount(p))
		m.projectTabs.SetNeeds(p, m.list.CountNeedsInProject(p))
	}

	scWithPadding := lipgloss.NewStyle().PaddingTop(1).Render(
		lipgloss.JoinVertical(lipgloss.Left, nonEmpty(m.sourceControl.String(), m.renderServers(), m.renderRecent(), m.renderDock())...))
	previewWithPadding := lipgloss.NewStyle().PaddingTop(1).Render(m.tabbedWindow.String())
	if !m.sourceControl.Focused() {
		previewWithPadding = m.renderGridCached()
	}
	listAndPreview := lipgloss.JoinHorizontal(lipgloss.Top, scWithPadding, previewWithPadding)
	// Never let the panes grow past their share of the screen: extra lines
	// would push the project tab row off the top.
	if m.contentHeight > 0 {
		listAndPreview = lipgloss.NewStyle().MaxHeight(m.contentHeight + 1).Render(listAndPreview)
	}

	mainView := lipgloss.JoinVertical(
		lipgloss.Left,
		m.projectTabs.String(),
		listAndPreview,
		" "+m.keyBar(m.screenWidth-2),
		m.statusRow(m.screenWidth),
	)

	if m.leader && m.state == stateDefault {
		return m.placeCentered(m.leaderMenu(), mainView)
	}
	if m.state == statePrompt {
		if m.textInputOverlay == nil {
			log.ErrorLog.Printf("text input overlay is nil")
		}
		return m.placeCentered(m.textInputOverlay.Render(), mainView)
	} else if m.state == stateHelp {
		if m.textOverlay == nil {
			log.ErrorLog.Printf("text overlay is nil")
		}
		return m.placeCentered(m.textOverlay.Render(), mainView)
	} else if m.state == stateIssuePicker && m.issuePicker != nil {
		return m.placeCentered(m.issuePicker.Render(), mainView)
	} else if m.state == stateCommit && m.commitOverlay != nil {
		return m.placeCentered(m.commitOverlay.Render(), mainView)
	} else if m.state == stateConfirm {
		if m.confirmationOverlay == nil {
			log.ErrorLog.Printf("confirmation overlay is nil")
		}
		return m.placeCentered(m.confirmationOverlay.Render(), mainView)
	}

	return mainView
}
