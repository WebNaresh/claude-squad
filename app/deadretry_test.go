package app

import (
	"claude-squad/session"
	"claude-squad/session/tmux"
	"claude-squad/ui"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestGaiNetworkErr(t *testing.T) {
	for _, s := range []string{
		`non-200 OK status code: 502 Bad Gateway body: "<html>"`,
		"HTTP 503: Service Unavailable",
		"error connecting to api.github.com",
		"Get https://api.github.com: dial tcp: i/o timeout",
	} {
		if !gaiNetworkErr.MatchString(s) {
			t.Errorf("not matched: %q", s)
		}
	}
	for _, s := range []string{
		"GraphQL: Could not resolve to an Issue with the number of 12",
		"HTTP 404: Not Found",
		"error: gh CLI not authenticated",
		"gh: Validation Failed (HTTP 422)",
	} {
		if gaiNetworkErr.MatchString(s) {
			t.Errorf("matched: %q", s)
		}
	}
}

func TestGaiCanRetry(t *testing.T) {
	const bad = "non-200 OK status code: 502 Bad Gateway\n\nPane is dead (status 1)\n"
	if !gaiCanRetry("→ Fetching open PRs\n" + bad) {
		t.Error("502 before attaching should retry")
	}
	if gaiCanRetry("✓ Attached #5 to PR #9 — closes: #5\n" + bad) {
		t.Error("attached issue: the runner would never start it again")
	}
	if gaiCanRetry("Post http://localhost:11434/api/generate: dial tcp: connection refused\n") {
		t.Error("Ollama down is the user's to fix")
	}
	if gaiCanRetry("error: gh CLI not authenticated\n") {
		t.Error("login error is the user's to fix")
	}
}

// deadTmux starts a pane that prints what, exits and stays dead, on a tmux
// server of the test's own (TMUX_TMPDIR), never the user's.
func deadTmux(t *testing.T, name, what string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "csd")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("TMUX", "")
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-server").Run()
		_ = os.RemoveAll(dir)
	})
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, out)
		}
	}
	run("new-session", "-d", "-s", name, "-x", "49", "-y", "60", "sleep 0.3; printf '%s\\n' '"+what+"'; exit 1")
	run("set-option", "-t", name, "remain-on-exit", "on")
	for i := 0; i < 100 && !session.PaneDead(name); i++ {
		_ = exec.Command("sleep", "0.05").Run()
	}
	if !session.PaneDead(name) {
		t.Fatal("pane did not die")
	}
	run("resize-window", "-t", name, "-x", "49", "-y", "25")
}

// Wheel-scrolled onto a dead pane's blank rows, the tile still shows gai's error.
func TestDeadTileScrolledShowsError(t *testing.T) {
	const name = "cc_deadtest_i1"
	deadTmux(t, name, "boom 502")
	if out, err := exec.Command("tmux", "copy-mode", "-t", name).CombinedOutput(); err != nil {
		t.Fatalf("copy-mode: %v %s", err, out)
	}
	if out, err := exec.Command("tmux", "send-keys", "-t", name, "-X", "scroll-up").CombinedOutput(); err != nil {
		t.Fatalf("scroll: %v %s", err, out)
	}
	tmux.SetScrolled(name, true)
	defer tmux.SetScrolled(name, false)
	e := &session.ExternalSession{Name: name, Kind: session.KindTmux}
	got := captureTile(gridJob{row: ui.Row{External: e}}, 49, 25)
	if !strings.Contains(got.Content, "boom 502") {
		t.Fatalf("error not shown: %q", got.Content)
	}
}

func TestCloseNetworkDeadTile(t *testing.T) {
	const name = "cc_deadtest_i2"
	deadTmux(t, name, "non-200 OK status code: 502 Bad Gateway")
	e := &session.ExternalSession{Name: name, Kind: session.KindTmux}
	if !closeNetworkDeadTile(e) {
		t.Fatal("not closed")
	}
	if exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil {
		t.Fatal("session still there")
	}
}

// A live pane scrolled to text that merely says "Pane is dead" stays as it is.
func TestLiveScrolledTileNotTakenForDead(t *testing.T) {
	const name = "cc_deadtest_i4"
	deadTmux(t, name, "x")
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", "livepane", "-x", "49", "-y", "25",
		"printf 'old line\\n\\nPane is dead\\n'; exec sleep 30").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	tmux.SetScrolled("livepane", true)
	defer tmux.SetScrolled("livepane", false)
	if session.PaneDead("livepane") {
		t.Fatal("pane should be alive")
	}
	e := &session.ExternalSession{Name: "livepane", Kind: session.KindTmux}
	got := captureTile(gridJob{row: ui.Row{External: e}}, 49, 25)
	if strings.Contains(got.Content, "boom") || !strings.Contains(got.Content, "Pane is dead") {
		t.Fatalf("content changed: %q", got.Content)
	}
}

func TestDeadTileOtherErrorStays(t *testing.T) {
	const name = "cc_deadtest_i3"
	deadTmux(t, name, "error: gh CLI not authenticated")
	e := &session.ExternalSession{Name: name, Kind: session.KindTmux}
	if closeNetworkDeadTile(e) {
		t.Fatal("closed a tile the user must read")
	}
	if exec.Command("tmux", "has-session", "-t", "="+name).Run() != nil {
		t.Fatal("session gone")
	}
}
