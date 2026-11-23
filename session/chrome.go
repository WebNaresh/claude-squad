package session

import (
	"claude-squad/config"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// After an account switch the Claude in Chrome extension is still signed in to
// the old account ("Claude in Chrome uses a different account"). It keeps one
// account per Chrome profile. cs does what it safely can in that one profile:
//
//  1. open the extension settings page and click Log out;
//  2. the extension then sends the tab to claude.ai's "Claude for Chrome would
//     like to connect" page. If that page says "Logged in as <the account we
//     switched to>", click Authorize. If claude.ai in the browser is another
//     account, stop and say so: signing in to claude.ai takes the person's own
//     login, which cs never types.
//
// Clicking needs macOS Automation for cs-fork and Chrome's View > Developer >
// Allow JavaScript from Apple Events; without them cs only opens the page.

const (
	claudeExtensionID      = "fcoeoabgfenejglbffodgkkbkcdhcgfn"
	claudeExtensionOptions = "chrome-extension://" + claudeExtensionID + "/options.html"
)

// isExtOAuth reports whether tabURL is claude.ai's OAuth page for this
// extension: https, host exactly claude.ai, path exactly /oauth/authorize and
// a single redirect_uri that is the extension. Parsed, not substring-matched:
// "https://evil.example/?x=claude.ai/oauth/authorize&redirect_uri=chrome-extension…"
// must not pass, since cs clicks Authorize on a page that passes.
func isExtOAuth(tabURL string) bool {
	u, err := url.Parse(tabURL)
	if err != nil || u.Scheme != "https" || strings.ToLower(strings.TrimSuffix(u.Hostname(), ".")) != "claude.ai" ||
		u.Port() != "" || u.User != nil || u.Path != "/oauth/authorize" {
		return false
	}
	ru := u.Query()["redirect_uri"]
	return len(ru) == 1 && strings.HasPrefix(ru[0], "chrome-extension://"+claudeExtensionID+"/")
}

// Swapped in tests, so a test never opens the user's Chrome.
var (
	chromeRunning = func() bool { return exec.Command("pgrep", "-x", "Google Chrome").Run() == nil }
	chromeOpen    = func(url string) error { return exec.Command("open", "-a", "Google Chrome", url).Run() }
	chromeScript  = func(script string) (string, error) {
		out, err := exec.Command("osascript", "-e", script).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	notify = func(msg string) {
		_ = exec.Command("osascript", "-e", fmt.Sprintf("display notification %q with title %q", msg, "claude-squad")).Run()
	}
	chromeWait = func() { time.Sleep(2 * time.Second) }
)

const onActiveTab = `tell application "Google Chrome" to execute (active tab of front window) javascript `

// stateJS reads the active tab: its URL and visible text, split by a tab-like mark.
const stateJS = `/*STATE*/location.href+"␟"+(document.body?document.body.innerText:"").replace(/\s+/g," ").slice(0,1500)`

// clickJS(label) clicks the visible button or link whose text is exactly label.
func clickJS(label string) string {
	return fmt.Sprintf(`/*CLICK*/var b=[].slice.call(document.querySelectorAll("button,a,[role=button],div")).filter(function(e){return (e.textContent||"").replace(/\s+/g," ").trim().toLowerCase()===%q&&e.children.length<3}).pop();if(b){b.click();"clicked"}else{"nobutton"}`, strings.ToLower(label))
}

func chromeTab() (url, text string, err error) {
	out, err := chromeScript(onActiveTab + fmt.Sprintf("%q", stateJS))
	if err != nil {
		return "", out, err
	}
	url, text, _ = strings.Cut(out, "␟")
	return url, text, nil
}

func chromeClick(label string) (string, error) {
	return chromeScript(onActiveTab + fmt.Sprintf("%q", clickJS(label)))
}

// ResetChromeExtension logs the extension out and back in as email when it can,
// and returns a one-line result for the log. Chrome not running: nothing to do.
// chromeMu: one run at a time, so two switches in a row never click over each other.
var chromeMu sync.Mutex

func ResetChromeExtension(email string) string {
	chromeMu.Lock()
	defer chromeMu.Unlock()
	if !chromeRunning() {
		return "chrome not running, nothing to do"
	}
	if err := chromeOpen(claudeExtensionOptions); err != nil {
		return "could not open the extension settings page: " + err.Error()
	}
	loggedOut, loggedIn := false, false
	for try := 0; try < 8; try++ {
		chromeWait()
		url, text, err := chromeTab()
		if err != nil {
			return manual(email, whyNotClicked(text, err))
		}
		switch {
		case isExtOAuth(url):
			if !loggedInAs(text, email) {
				// Open claude.ai's account switcher for the person; signing in
				// takes their own login, which cs never types. Never Authorize.
				clicked, _ := chromeClick("Switch account")
				notify("claude.ai in Chrome is signed in as another account. Sign in there as " + email + ", then run: cs account chrome")
				if clicked == "clicked" {
					return "claude.ai in Chrome is another account: opened its account switcher; sign in as " + email + ", then run cs account chrome"
				}
				return "claude.ai in Chrome is another account: sign in there as " + email + ", then run cs account chrome"
			}
			out, err := chromeClick("Authorize")
			if err != nil || out != "clicked" {
				return manual(email, whyNotClicked(out, err))
			}
			notify("Chrome extension signed in as " + email + ".")
			return "extension logged out and signed in as " + email
		case strings.HasPrefix(url, claudeExtensionOptions):
			lt := strings.ToLower(text)
			switch {
			case !loggedOut && strings.Contains(lt, "log out"):
				out, err := chromeClick("Log out")
				if err != nil {
					return manual(email, whyNotClicked(out, err))
				}
				loggedOut = out == "clicked"
			case !loggedIn && strings.Contains(lt, "log in") && !strings.Contains(lt, "log out"):
				// Logged out (by us or earlier): its own Log in button opens
				// the claude.ai connect page, handled above on the next read.
				out, err := chromeClick("Log in")
				if err != nil {
					return manual(email, whyNotClicked(out, err))
				}
				loggedIn = out == "clicked"
			}
		}
	}
	return manual(email, "the sign-in page did not appear")
}

func manual(email, why string) string {
	notify("Chrome extension: log out and sign in as " + email + " on the page that opened.")
	return "opened the extension settings page; click Log out, then sign in as " + email + " (not done for you: " + why + ")"
}

// ChromePageText returns the visible text of the active tab, for working out
// what to click next (cs account chrome prints it).
func ChromePageText() string {
	url, text, err := chromeTab()
	if err != nil {
		return "(cannot read the page: " + whyNotClicked(text, err) + ")"
	}
	if len(text) > 300 {
		text = text[:300]
	}
	return url + " :: " + text
}

// whyNotClicked names the one thing to fix, from osascript's own message.
func whyNotClicked(out string, err error) string {
	msg := out
	if err != nil {
		msg = err.Error() + " " + out
	}
	switch {
	case strings.Contains(msg, "-1743"):
		return "macOS has not allowed this program to control Chrome: System Settings > Privacy & Security > Automation"
	case strings.Contains(msg, "turned off") || strings.Contains(msg, "Apple Events"):
		return "Chrome has scripting off: View > Developer > Allow JavaScript from Apple Events"
	case err == nil && out == "nobutton":
		return "no matching button found on the page"
	}
	if len(msg) > 120 {
		msg = msg[:120]
	}
	return strings.TrimSpace(msg)
}

// loggedInAs reports whether the page says "Logged in as <email>" with the
// whole address: "a@x.in" must not match "a@x.in.evil.com".
func loggedInAs(text, email string) bool {
	re := regexp.MustCompile(`(?i)logged in as ` + regexp.QuoteMeta(email) + `(\s|$)`)
	return email != "" && re.MatchString(text)
}

func chromeMarkerPath() string {
	dir, err := config.GetConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "chrome-account")
}

// MarkChromeAccount records that the extension step ran for the account
// signed in now (or uuid), so a later account-changed notice doesn't repeat it.
func MarkChromeAccount(uuid string) {
	if p := chromeMarkerPath(); p != "" && uuid != "" {
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		_ = os.WriteFile(p, []byte(uuid+"\n"), 0o600)
	}
}

// ChromeNeedsReset says whether the extension step has not run yet for the
// account Claude is signed in as, and if so which address to sign in as. It
// marks the account as done: the caller runs the step right after. This is
// how a manual /login (cs never saw it) still gets the extension moved.
func ChromeNeedsReset() (email string, ok bool) {
	uuid, email := CurrentAccountUUID(), CurrentAccountEmail()
	p := chromeMarkerPath()
	if uuid == "" || email == "" || p == "" {
		return "", false
	}
	if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) == uuid {
		return "", false
	}
	MarkChromeAccount(uuid)
	return email, true
}
