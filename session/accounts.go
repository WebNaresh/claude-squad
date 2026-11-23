package session

import (
	"claude-squad/config"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Several Claude subscriptions, one saved login each. Claude Code keeps its
// login in two places: the keychain item "Claude Code-credentials" (its
// claudeAiOauth part is the account; mcpOAuth holds connector tokens that must
// stay) and the oauthAccount block of ~/.claude.json. SaveAccount stores both
// for the current login; UseAccount puts a saved pair back, so the next
// Claude request runs on that account without a browser login.

const keychainService = "Claude Code-credentials"

// Saved login, one file per name in <config>/accounts, mode 0600.
type SavedAccount struct {
	Name          string          `json:"name"`
	Email         string          `json:"email"`
	AccountUUID   string          `json:"accountUuid"`
	ClaudeAiOauth json.RawMessage `json:"claudeAiOauth"`
	OAuthAccount  json.RawMessage `json:"oauthAccount"`
	// LimitedUntil: set when this account hit its usage limit, so the
	// switcher doesn't hand work back to it before the reset.
	LimitedUntil time.Time `json:"limitedUntil,omitempty"`
}

// Swapped in tests.
var (
	keychainRead  = realKeychainRead
	keychainWrite = realKeychainWrite
	claudeJSON    = func() string {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".claude.json")
	}
)

func keychainAccount() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USER")
}

func realKeychainRead() (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", keychainService, "-a", keychainAccount(), "-w").Output()
	if err != nil {
		return "", fmt.Errorf("read keychain: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// The secret goes to `security -i` on stdin, never argv (visible in ps).
func realKeychainWrite(value string) error {
	return keychainWriteTo("", value)
}

// secQuote quotes for security -i's parser: double quotes, \ and " escaped.
func secQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// stdinLineLimit: `security -i` reads one line of about 4000 characters and
// runs the rest as the next command (measured: a 4000-char value fails, 3000
// works, no line continuation). Real logins are far longer (86 connector
// entries = 38 KB), which is how the first swap on the real keychain failed.
const stdinLineLimit = 3500

// keychainFile "" = the default keychain (tests pass a throwaway one).
// Short values go through stdin so they never appear in ps. Longer ones have
// no stdin route, so they go as the -w argument for the instant security
// runs (same as Claude Code itself does for large logins); readable only by
// processes on this Mac, and ps shows them for milliseconds.
func keychainWriteTo(keychainFile, value string) error {
	if strings.ContainsAny(value, "\n\r") {
		return fmt.Errorf("write keychain: value has a newline")
	}
	var cmd *exec.Cmd
	line := "add-generic-password -U -s " + secQuote(keychainService) + " -a " + secQuote(keychainAccount()) + " -w " + secQuote(value)
	if keychainFile != "" {
		line += " " + secQuote(keychainFile)
	}
	if len(line) <= stdinLineLimit {
		cmd = exec.Command("security", "-i")
		cmd.Stdin = strings.NewReader(line + "\n")
	} else {
		args := []string{"add-generic-password", "-U", "-s", keychainService, "-a", keychainAccount(), "-w", value}
		if keychainFile != "" {
			args = append(args, keychainFile)
		}
		cmd = exec.Command("security", args...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil || strings.Contains(string(out), "security:") {
		// The message may echo the command; never include the value.
		msg := strings.ReplaceAll(strings.TrimSpace(string(out)), value, "<value>")
		if len(msg) > 200 {
			msg = msg[:200] + "…"
		}
		return fmt.Errorf("write keychain: %v: %s", err, msg)
	}
	return nil
}

func accountsDir() (string, error) {
	dir, err := config.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "accounts"), nil
}

func accountPath(name string) (string, error) {
	if name == "" || strings.ContainsAny(name, `/\.`) {
		return "", fmt.Errorf("bad account name %q (letters, digits, - and _ only)", name)
	}
	dir, err := accountsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".json"), nil
}

func readClaudeJSON() (map[string]json.RawMessage, error) {
	b, err := os.ReadFile(claudeJSON())
	if err != nil {
		return nil, err
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// SaveAccount stores the current login under name.
func SaveAccount(name string) (*SavedAccount, error) {
	path, err := accountPath(name)
	if err != nil {
		return nil, err
	}
	raw, err := keychainRead()
	if err != nil {
		return nil, err
	}
	var kc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &kc); err != nil || kc["claudeAiOauth"] == nil {
		return nil, fmt.Errorf("keychain has no claudeAiOauth login; run /login first")
	}
	cj, err := readClaudeJSON()
	if err != nil || cj["oauthAccount"] == nil {
		return nil, fmt.Errorf("~/.claude.json has no oauthAccount; run /login first")
	}
	var oa struct {
		Email string `json:"emailAddress"`
		UUID  string `json:"accountUuid"`
	}
	_ = json.Unmarshal(cj["oauthAccount"], &oa)
	if all, _ := ListAccounts(); all != nil {
		for _, a := range all {
			if a.AccountUUID == oa.UUID && a.Name != name {
				return nil, fmt.Errorf("this login (%s) is already saved as %q; /login to the other account first", oa.Email, a.Name)
			}
		}
	}
	acc := &SavedAccount{Name: name, Email: oa.Email, AccountUUID: oa.UUID, ClaudeAiOauth: kc["claudeAiOauth"], OAuthAccount: cj["oauthAccount"]}
	if old, err := LoadAccount(name); err == nil {
		acc.LimitedUntil = old.LimitedUntil
	}
	return acc, writeAccount(path, acc)
}

func writeAccount(path string, acc *SavedAccount) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(acc, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func LoadAccount(name string) (*SavedAccount, error) {
	path, err := accountPath(name)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var acc SavedAccount
	if err := json.Unmarshal(b, &acc); err != nil {
		return nil, err
	}
	return &acc, nil
}

// RemoveAccount deletes a saved login. The active login in the keychain is
// untouched: this only forgets the saved copy.
func RemoveAccount(name string) error {
	path, err := accountPath(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no saved account %q", name)
		}
		return err
	}
	return nil
}

// ListAccounts returns the saved accounts, by name.
func ListAccounts() ([]*SavedAccount, error) {
	dir, err := accountsDir()
	if err != nil {
		return nil, err
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	sort.Strings(files)
	var out []*SavedAccount
	for _, f := range files {
		if acc, err := LoadAccount(strings.TrimSuffix(filepath.Base(f), ".json")); err == nil && acc.Name != "" && acc.AccountUUID != "" {
			out = append(out, acc)
		}
	}
	return out, nil
}

// CurrentAccountUUID is the account Claude is signed in to now.
func CurrentAccountUUID() string {
	cj, err := readClaudeJSON()
	if err != nil {
		return ""
	}
	var oa struct {
		UUID string `json:"accountUuid"`
	}
	_ = json.Unmarshal(cj["oauthAccount"], &oa)
	return oa.UUID
}

// CurrentAccountEmail is the address Claude is signed in as now.
func CurrentAccountEmail() string {
	cj, err := readClaudeJSON()
	if err != nil {
		return ""
	}
	var oa struct {
		Email string `json:"emailAddress"`
	}
	_ = json.Unmarshal(cj["oauthAccount"], &oa)
	return oa.Email
}

// UseAccount makes name the signed-in login. The old keychain item is kept as
// <config>/accounts/keychain-backup.json (0600) first. A login that was
// refreshed since it was saved is saved again before leaving it, so no
// account is left with a dead refresh token.
func UseAccount(name string) error {
	acc, err := LoadAccount(name)
	if err != nil {
		return fmt.Errorf("no saved account %q: %w", name, err)
	}
	raw, err := keychainRead()
	if err != nil {
		return err
	}
	// Not in accounts/: everything there is read as a saved account (the blank
	// "account" it made was picked by the runner on 2026-11-23).
	cfg, err := config.GetConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cfg, "keychain-backup.json"), []byte(raw), 0o600); err != nil {
		return err
	}
	var kc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &kc); err != nil {
		return fmt.Errorf("keychain item is not JSON: %w", err)
	}
	// Keep the account being left up to date (Claude refreshes tokens).
	if cur := CurrentAccountUUID(); cur != "" && kc["claudeAiOauth"] != nil {
		if all, _ := ListAccounts(); all != nil {
			for _, a := range all {
				if a.AccountUUID == cur && a.Name != name {
					a.ClaudeAiOauth = kc["claudeAiOauth"]
					if p, err := accountPath(a.Name); err == nil {
						_ = writeAccount(p, a)
					}
				}
			}
		}
	}
	kc["claudeAiOauth"] = acc.ClaudeAiOauth
	nb, err := json.Marshal(kc)
	if err != nil {
		return err
	}
	if err := keychainWrite(string(nb)); err != nil {
		return err
	}
	cj, err := readClaudeJSON()
	if err != nil {
		return err
	}
	cj["oauthAccount"] = acc.OAuthAccount
	out, err := json.Marshal(cj)
	if err != nil {
		return err
	}
	path := claudeJSON()
	tmp := path + ".cs-tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// MarkAccountLimited records that the account Claude is on hit its limit
// until reset, so NextAccount skips it.
func MarkAccountLimited(uuid string, until time.Time) {
	all, _ := ListAccounts()
	for _, a := range all {
		if a.AccountUUID == uuid {
			a.LimitedUntil = until
			if p, err := accountPath(a.Name); err == nil {
				_ = writeAccount(p, a)
			}
		}
	}
}

// NextAccount picks the saved account to switch to when the current one hit
// its limit: not the current one and not limited at now; the one whose limit
// ended longest ago (or never was) first.
func NextAccount(now time.Time) (*SavedAccount, bool) {
	cur := CurrentAccountUUID()
	all, _ := ListAccounts()
	var best *SavedAccount
	for _, a := range all {
		if a.AccountUUID == cur || a.LimitedUntil.After(now) {
			continue
		}
		if best == nil || a.LimitedUntil.Before(best.LimitedUntil) {
			best = a
		}
	}
	return best, best != nil
}
