package session

import (
	"claude-squad/config"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeLogin sets a private config dir, ~/.claude.json and an in-memory keychain.
func fakeLogin(t *testing.T, uuid, token string) (kc *string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_SQUAD_HOME", filepath.Join(dir, "cs"))
	cj := filepath.Join(dir, "claude.json")
	claudeJSON = func() string { return cj }
	k := `{"claudeAiOauth":{"accessToken":"` + token + `"},"mcpOAuth":{"vercel":"keep"}}`
	kc = &k
	keychainRead = func() (string, error) { return *kc, nil }
	keychainWrite = func(v string) error { *kc = v; return nil }
	setLogin(t, cj, uuid)
	t.Cleanup(func() { keychainRead, keychainWrite = realKeychainRead, realKeychainWrite })
	return kc
}

func setLogin(t *testing.T, cj, uuid string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"other": 1, "oauthAccount": map[string]string{"accountUuid": uuid, "emailAddress": uuid + "@x.in"}})
	if err := os.WriteFile(cj, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAccountSwapKeepsConnectorsAndOtherSettings(t *testing.T) {
	kc := fakeLogin(t, "A", "tokA")
	if _, err := SaveAccount("a"); err != nil {
		t.Fatal(err)
	}
	// Sign in as B by hand.
	*kc = `{"claudeAiOauth":{"accessToken":"tokB"},"mcpOAuth":{"vercel":"keep2"}}`
	setLogin(t, claudeJSON(), "B")
	if _, err := SaveAccount("b"); err != nil {
		t.Fatal(err)
	}
	if err := UseAccount("a"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(*kc, "tokA") || !strings.Contains(*kc, "keep2") || strings.Contains(*kc, "tokB") {
		t.Fatalf("keychain after swap: %s", *kc)
	}
	if CurrentAccountUUID() != "A" {
		t.Fatalf("current = %q", CurrentAccountUUID())
	}
	raw, _ := os.ReadFile(claudeJSON())
	if !strings.Contains(string(raw), `"other":1`) {
		t.Fatalf("other settings lost: %s", raw)
	}
	p, _ := accountPath("a")
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("saved account mode %v", st.Mode())
	}
}

func TestNextAccountSkipsCurrentAndLimited(t *testing.T) {
	fakeLogin(t, "A", "tokA")
	SaveAccount("a")
	setLogin(t, claudeJSON(), "B")
	SaveAccount("b")
	setLogin(t, claudeJSON(), "C")
	SaveAccount("c")
	now := time.Now()
	MarkAccountLimited("B", now.Add(time.Hour))
	n, ok := NextAccount(now) // current is C
	if !ok || n.Name != "a" {
		t.Fatalf("next = %v %v", n, ok)
	}
	MarkAccountLimited("A", now.Add(time.Hour))
	if _, ok := NextAccount(now); ok {
		t.Fatal("all limited: want none")
	}
	if _, ok := NextAccount(now.Add(2 * time.Hour)); !ok {
		t.Fatal("after reset: want one")
	}
}

func TestBadAccountName(t *testing.T) {
	fakeLogin(t, "A", "t")
	for _, n := range []string{"", "../x", "a.b"} {
		if _, err := SaveAccount(n); err == nil {
			t.Fatalf("%q accepted", n)
		}
	}
}

// Real `security -i` round trip on a throwaway keychain: quotes and
// backslashes in the JSON must survive, and the value must not be in argv.
func TestKeychainWriteViaStdinRoundTrip(t *testing.T) {
	kcFile := filepath.Join(t.TempDir(), "t.keychain-db")
	if out, err := exec.Command("security", "create-keychain", "-p", "pw", kcFile).CombinedOutput(); err != nil {
		t.Skipf("no keychain here: %s", out)
	}
	defer exec.Command("security", "delete-keychain", kcFile).Run()
	val := `{"claudeAiOauth":{"accessToken":"a\"b\\c","x":"é"},"mcpOAuth":{}}`
	if err := keychainWriteTo(kcFile, val); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("security", "find-generic-password", "-s", keychainService, "-a", keychainAccount(), "-w", kcFile).Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(string(out))
	if got != val {
		// security prints non-ASCII/odd values as hex
		if h, e := hex.DecodeString(got); e != nil || string(h) != val {
			t.Fatalf("round trip: got %q want %q", got, val)
		}
	}
}

func TestSaveRefusesSameLoginUnderTwoNames(t *testing.T) {
	fakeLogin(t, "A", "tokA")
	if _, err := SaveAccount("main"); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveAccount("second"); err == nil {
		t.Fatal("same login saved twice")
	}
	if _, err := SaveAccount("main"); err != nil {
		t.Fatal("re-saving under its own name must work:", err)
	}
}

func TestRemoveAccount(t *testing.T) {
	kc := fakeLogin(t, "A", "tokA")
	SaveAccount("a")
	before := *kc
	if err := RemoveAccount("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAccount("a"); err == nil {
		t.Fatal("still saved")
	}
	if *kc != before || CurrentAccountUUID() != "A" {
		t.Fatal("remove touched the active login")
	}
	if err := RemoveAccount("a"); err == nil {
		t.Fatal("removing twice should say not found")
	}
}

// The real login is ~38 KB: past security -i's line limit. The write must
// still land, whole.
func TestKeychainWriteLongValue(t *testing.T) {
	kcFile := filepath.Join(t.TempDir(), "t.keychain-db")
	if out, err := exec.Command("security", "create-keychain", "-p", "pw", kcFile).CombinedOutput(); err != nil {
		t.Skipf("no keychain here: %s", out)
	}
	defer exec.Command("security", "delete-keychain", kcFile).Run()
	val := `{"claudeAiOauth":{"accessToken":"t"},"mcpOAuth":{"k":"` + strings.Repeat("a", 38000) + `"}}`
	if err := keychainWriteTo(kcFile, val); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("security", "find-generic-password", "-s", keychainService, "-a", keychainAccount(), "-w", kcFile).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != val {
		t.Fatalf("stored %d bytes, want %d", len(got), len(val))
	}
}

func TestKeychainWriteErrorHidesValue(t *testing.T) {
	secret := "SECRET" + strings.Repeat("z", 100)
	err := keychainWriteTo("/nonexistent/x.keychain-db", secret)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("want error without the value, got %v", err)
	}
}

// A stray file in accounts/ (the old keychain backup lived there) must not
// become a blank account that NextAccount picks.
func TestStrayFileIsNotAnAccount(t *testing.T) {
	fakeLogin(t, "A", "tokA")
	SaveAccount("a")
	setLogin(t, claudeJSON(), "B")
	SaveAccount("b")
	dir, _ := accountsDir()
	os.WriteFile(filepath.Join(dir, "keychain-backup.json"), []byte(`{"claudeAiOauth":{}}`), 0o600)
	if all, _ := ListAccounts(); len(all) != 2 {
		t.Fatalf("listed %d accounts, want 2", len(all))
	}
	n, ok := NextAccount(time.Now()) // current is B
	if !ok || n.Name != "a" {
		t.Fatalf("next = %+v %v", n, ok)
	}
	// UseAccount's backup must land outside accounts/.
	if err := UseAccount("a"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.GetConfigDir()
	if _, err := os.Stat(filepath.Join(cfg, "keychain-backup.json")); err != nil {
		t.Fatal("backup not in config dir:", err)
	}
}
