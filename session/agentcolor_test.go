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
	if AgentColor("") != "" {
		t.Fatal("no session, but a colour")
	}
}
