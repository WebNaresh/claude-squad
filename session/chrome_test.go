package session

import (
	"errors"
	"strings"
	"testing"
)

const extPage = claudeExtensionOptions
const oauthPage = "https://claude.ai/oauth/authorize?client_id=x&redirect_uri=chrome-extension%3A%2F%2F" + claudeExtensionID + "%2Foauth_callback.html"

// fakeChrome plays Chrome: pages is the sequence of (url, text) the active tab
// shows on each read; clicks records what was clicked and answers click(label).
type fakeChrome struct {
	pages   [][2]string
	read    int
	clicks  []string
	click   func(label string) (string, error)
	opened  []string
	notes   []string
	running bool
}

func (f *fakeChrome) install() {
	chromeRunning = func() bool { return f.running }
	chromeOpen = func(u string) error { f.opened = append(f.opened, u); return nil }
	chromeWait = func() {}
	notify = func(m string) { f.notes = append(f.notes, m) }
	chromeScript = func(script string) (string, error) {
		if strings.Contains(script, "/*STATE*/") {
			p := f.pages[len(f.pages)-1]
			if f.read < len(f.pages) {
				p = f.pages[f.read]
			}
			f.read++
			return p[0] + "␟" + p[1], nil
		}
		label := "?"
		for _, c := range []string{"log out", "log in", "authorize", "switch account"} {
			if strings.Contains(strings.ToLower(script), `===\"`+c) {
				label = c
			}
		}
		f.clicks = append(f.clicks, label)
		return f.click(label)
	}
}

func okClick(string) (string, error) { return "clicked", nil }

func TestChromeNotRunningDoesNothing(t *testing.T) {
	f := &fakeChrome{running: false}
	f.install()
	if r := ResetChromeExtension("a@x.in"); !strings.Contains(r, "not running") || len(f.opened) != 0 || len(f.notes) != 0 {
		t.Fatalf("result %q opened %v notes %v", r, f.opened, f.notes)
	}
}

// Options page -> Log out -> its Log in page -> Log in -> extension OAuth page
// logged in as the right account -> Authorize.
func TestChromeLogoutThenLoginThenAuthorize(t *testing.T) {
	f := &fakeChrome{running: true, click: okClick, pages: [][2]string{
		{extPage, "Claude in Chrome settings Log out"},
		{extPage, "Log in Claude in Chrome is available to all paid plan subscribers Log in"},
		{oauthPage, "Claude for Chrome would like to connect Authorize Decline Logged in as A@x.in Switch account"},
	}}
	f.install()
	r := ResetChromeExtension("a@x.in")
	if !strings.Contains(r, "signed in as a@x.in") || strings.Join(f.clicks, ",") != "log out,log in,authorize" {
		t.Fatalf("result %q clicks %v", r, f.clicks)
	}
}

// Already logged out when cs starts: straight to Log in, no Log out click.
func TestChromeAlreadyLoggedOutClicksLogIn(t *testing.T) {
	f := &fakeChrome{running: true, click: okClick, pages: [][2]string{
		{extPage, "Log in Claude in Chrome is available to all paid plan subscribers Log in"},
		{oauthPage, "would like to connect Authorize Decline Logged in as a@x.in Switch account"},
	}}
	f.install()
	ResetChromeExtension("a@x.in")
	if strings.Join(f.clicks, ",") != "log in,authorize" {
		t.Fatalf("clicks %v", f.clicks)
	}
}

// claude.ai in the browser is another account: open its account switcher,
// never Authorize, and say what to do.
func TestChromeWrongBrowserAccountIsNotAuthorized(t *testing.T) {
	f := &fakeChrome{running: true, click: okClick, pages: [][2]string{
		{oauthPage, "would like to connect Authorize Decline Logged in as other@x.in Switch account"},
	}}
	f.install()
	r := ResetChromeExtension("a@x.in")
	if strings.Join(f.clicks, ",") != "switch account" || !strings.Contains(r, "another account") || !strings.Contains(f.notes[0], "a@x.in") {
		t.Fatalf("result %q clicks %v notes %v", r, f.clicks, f.notes)
	}
}

// Authorize is clicked only on the extension's own OAuth page, never on some
// other claude.ai authorize page.
func TestChromeIgnoresOtherOAuthPages(t *testing.T) {
	f := &fakeChrome{running: true, click: okClick, pages: [][2]string{
		{"https://claude.ai/oauth/authorize?client_id=x&redirect_uri=https%3A%2F%2Fevil.example", "Authorize Logged in as a@x.in"},
	}}
	f.install()
	ResetChromeExtension("a@x.in")
	if len(f.clicks) != 0 {
		t.Fatalf("clicked %v on a foreign page", f.clicks)
	}
}

// No macOS permission: the page is still opened and the person is told what to do.
func TestChromeFallsBackWhenNotAuthorised(t *testing.T) {
	f := &fakeChrome{running: true, click: okClick, pages: nil}
	f.install()
	chromeScript = func(string) (string, error) { return "", errors.New("Not authorised (-1743)") }
	r := ResetChromeExtension("a@x.in")
	if !strings.Contains(r, "Log out") || !strings.Contains(r, "Automation") || len(f.opened) != 1 || len(f.notes) != 1 {
		t.Fatalf("result %q opened %v notes %v", r, f.opened, f.notes)
	}
}

func TestChromeWhyNotClicked(t *testing.T) {
	for _, c := range []struct {
		out  string
		err  error
		want string
	}{
		{"", errors.New("x (-1743)"), "Automation"},
		{"Executing JavaScript through AppleScript is turned off.", errors.New("exit 1"), "Allow JavaScript"},
		{"nobutton", nil, "no matching button"},
	} {
		if got := whyNotClicked(c.out, c.err); !strings.Contains(got, c.want) {
			t.Fatalf("%q/%v -> %q, want %q", c.out, c.err, got, c.want)
		}
	}
}

// The page check is a parse, not a substring match.
func TestIsExtOAuthIsStrict(t *testing.T) {
	cb := "chrome-extension%3A%2F%2F" + claudeExtensionID + "%2Foauth_callback.html"
	for url, want := range map[string]bool{
		oauthPage: true,
		"https://claude.ai/oauth/authorize?redirect_uri=" + cb:                              true,
		"https://evil.example/?x=claude.ai/oauth/authorize&redirect_uri=" + cb:              false,
		"https://claude.ai.evil.example/oauth/authorize?redirect_uri=" + cb:                 false,
		"https://evil.example/claude.ai/oauth/authorize?redirect_uri=" + cb:                 false,
		"http://claude.ai/oauth/authorize?redirect_uri=" + cb:                               false,
		"https://claude.ai@evil.example/oauth/authorize?redirect_uri=" + cb:                 false,
		"https://claude.ai/oauth/authorize/x?redirect_uri=" + cb:                            false,
		"https://claude.ai/oauth/authorize?redirect_uri=https%3A%2F%2Fevil.example":         false,
		"https://claude.ai/oauth/authorize?redirect_uri=" + cb + "&redirect_uri=" + cb:      false,
		"https://claude.ai/oauth/authorize?redirect_uri=chrome-extension%3A%2F%2Fother%2Fx": false,
		"": false,
	} {
		if got := isExtOAuth(url); got != want {
			t.Errorf("isExtOAuth(%q) = %v, want %v", url, got, want)
		}
	}
}

func TestLoggedInAsNeedsTheWholeAddress(t *testing.T) {
	for text, want := range map[string]bool{
		"Authorize Logged in as a@x.in Switch account": true,
		"Logged in as A@X.IN":                          true,
		"Logged in as a@x.in.evil.com Switch":          false,
		"Logged in as aa@x.in Switch":                  false,
		"Logged in as b@x.in a@x.in":                   false,
	} {
		if got := loggedInAs(text, "a@x.in"); got != want {
			t.Errorf("loggedInAs(%q) = %v, want %v", text, got, want)
		}
	}
	if loggedInAs("Logged in as ", "") {
		t.Error("empty email matched")
	}
}
