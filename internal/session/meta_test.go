package session

import (
	"os"
	"testing"
	"time"
)

func TestExtractMeta_Happy(t *testing.T) {
	f, err := os.Open("testdata/happy.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	m := Meta{ID: "test-session-01", Path: "testdata/happy.jsonl"}
	if err := ExtractMeta(f, &m); err != nil {
		t.Fatal(err)
	}

	if !m.Enriched {
		t.Error("Enriched should be true")
	}
	if m.CWD != "/Users/test/proj" {
		t.Errorf("CWD = %q", m.CWD)
	}
	if m.GitBranch != "main" {
		t.Errorf("GitBranch = %q", m.GitBranch)
	}
	if m.Model != "claude-opus-4-7" {
		t.Errorf("Model = %q", m.Model)
	}
	if m.Version != "2.1.117" {
		t.Errorf("Version = %q", m.Version)
	}
	if m.UserMsgs != 2 {
		t.Errorf("UserMsgs = %d; want 2", m.UserMsgs)
	}
	if m.AsstMsgs != 2 {
		t.Errorf("AsstMsgs = %d; want 2", m.AsstMsgs)
	}
	if m.FirstPrompt != "hello claude" {
		t.Errorf("FirstPrompt = %q", m.FirstPrompt)
	}
	if m.LastPrompt != "run ls" {
		t.Errorf("LastPrompt = %q", m.LastPrompt)
	}
	want := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	if !m.StartedAt.Equal(want) {
		t.Errorf("StartedAt = %v; want %v", m.StartedAt, want)
	}
}
