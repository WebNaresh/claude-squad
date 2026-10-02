package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// The LaunchAgent that keeps `cs --issues-daemon` running while the user is
// logged in. It starts the cs launcher (which rebuilds the fork first), with
// the PATH of the shell that installed it, since launchd's own PATH has no
// tmux, gh, gai or claude.

const issuesAgentLabel = "dev.claudesquad.issues"

// InstallIssuesDaemon writes the LaunchAgent and (re)loads it.
func InstallIssuesDaemon() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	launcher := filepath.Join(home, ".local", "bin", "cs")
	if _, err := os.Stat(launcher); err != nil {
		if launcher, err = os.Executable(); err != nil {
			return err
		}
	}
	logDir := filepath.Join(home, ".claude-squad")
	plist := filepath.Join(home, "Library", "LaunchAgents", issuesAgentLabel+".plist")
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array><string>%s</string><string>--issues-daemon</string></array>
	<key>EnvironmentVariables</key>
	<dict><key>PATH</key><string>%s</string><key>HOME</key><string>%s</string></dict>
	<key>WorkingDirectory</key><string>%s</string>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>ThrottleInterval</key><integer>30</integer>
	<key>StandardOutPath</key><string>%s</string>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, issuesAgentLabel, launcher, xmlEscape(os.Getenv("PATH")), home, home,
		filepath.Join(logDir, "issues-daemon.out.log"), filepath.Join(logDir, "issues-daemon.out.log"))
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(plist, []byte(body), 0o644); err != nil {
		return err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	_ = exec.Command("launchctl", "bootout", domain+"/"+issuesAgentLabel).Run() // reload if present
	if out, err := exec.Command("launchctl", "bootstrap", domain, plist).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl bootstrap: %s", strings.TrimSpace(string(out)))
	}
	fmt.Printf("Background issue runner installed: %s\nIt runs now and at every login. Log: grep daemon: %s\n", plist, filepath.Join(logDir, "activity.log"))
	return nil
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
