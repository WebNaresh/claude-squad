package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAgentColorReadsLatest(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	os.WriteFile(p, []byte(`{"type":"user"}`+"\n"+`{"type":"agent-color","agentColor":"red","sessionId":"s"}`+"\n"), 0o644)
	transcriptMu.Lock()
	transcriptPaths["test-sid"] = p
	transcriptMu.Unlock()
	if c := AgentColor("test-sid"); c != "red" {
		t.Fatalf("got %q, want red", c)
	}
	// Only the new bytes are read; the later /color wins.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"agent-color","agentColor":"cyan","sessionId":"s"}` + "\n")
	f.Close()
	agentColorMu.Lock()
	e := agentColorCache["test-sid"]
	e.checked = time.Time{}
	agentColorCache["test-sid"] = e
	agentColorMu.Unlock()
	if c := AgentColor("test-sid"); c != "cyan" {
		t.Fatalf("got %q after a second /color, want cyan", c)
	}
	f, _ = os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"type":"assistant","message":{"model":"claude-sonnet-5-5"}}` + "\n")
	f.WriteString(`{"type":"assistant","isSidechain":true,"message":{"model":"claude-haiku-4-5"}}` + "\n")
	f.Close()
	agentColorMu.Lock()
	e = agentColorCache["test-sid"]
	e.checked = time.Time{}
	agentColorCache["test-sid"] = e
	agentColorMu.Unlock()
	if m := AgentModel("test-sid", -1); m != "sonnet" {
		t.Fatalf("model = %q, want sonnet (a subagent's haiku reply must not count)", m)
	}
	if AgentColor("") != "" {
		t.Fatal("no session, but a colour")
	}
}

func TestShortModel(t *testing.T) {
	for in, want := range map[string]string{
		"claude-sonnet-5-5": "sonnet", "claude-opus-5-5": "opus", "opus[1m]": "opus[1m]",
		"sonnet": "sonnet", "claude-haiku-4-5-20251001": "haiku", "<synthetic>": "", "": "",
	} {
		if got := shortModel(in); got != want {
			t.Errorf("shortModel(%q) = %q, want %q", in, got, want)
		}
	}
	if m := modelArgRe.FindStringSubmatch(`claude --model 'opus[1m]' Work on #1`); m == nil || m[1] != "opus[1m]" {
		t.Errorf("--model 'opus[1m]' not read: %v", m)
	}
}
