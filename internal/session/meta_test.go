package session

import (
	"errors"
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

func TestExtractMeta_ThinkingDoesNotAffectCounts(t *testing.T) {
	f, _ := os.Open("testdata/thinking-heavy.jsonl")
	defer f.Close()
	m := Meta{}
	if err := ExtractMeta(f, &m); err != nil {
		t.Fatal(err)
	}
	if m.AsstMsgs != 1 {
		t.Errorf("AsstMsgs = %d; want 1 (thinking blocks must not count)", m.AsstMsgs)
	}
	if m.UserMsgs != 1 {
		t.Errorf("UserMsgs = %d; want 1", m.UserMsgs)
	}
}

func TestExtractMeta_SurvivesCorruptLines(t *testing.T) {
	f, _ := os.Open("testdata/corrupt-mid.jsonl")
	defer f.Close()
	m := Meta{}
	if err := ExtractMeta(f, &m); err != nil {
		t.Fatal(err)
	}
	if m.FirstPrompt != "first" {
		t.Errorf("FirstPrompt = %q", m.FirstPrompt)
	}
	if m.LastPrompt != "second" {
		t.Errorf("LastPrompt = %q", m.LastPrompt)
	}
	if m.UserMsgs != 2 || m.AsstMsgs != 1 {
		t.Errorf("counts = user=%d asst=%d; want 2, 1", m.UserMsgs, m.AsstMsgs)
	}
	var pe *PartialError
	if !errorsAs(m.LoadErr, &pe) || pe.BadLines != 2 {
		t.Errorf("LoadErr = %v; want PartialError{BadLines:2}", m.LoadErr)
	}
}

func errorsAs(err error, target any) bool {
	return err != nil && errors.As(err, target)
}

func TestExtractMeta_EmptyFile(t *testing.T) {
	f, _ := os.Open("testdata/empty.jsonl")
	defer f.Close()
	m := Meta{}
	if err := ExtractMeta(f, &m); err != nil {
		t.Fatal(err)
	}
	if !m.Enriched {
		t.Error("Enriched should be true even for empty file")
	}
	if m.UserMsgs != 0 || m.AsstMsgs != 0 {
		t.Errorf("counts should be zero")
	}
}

func TestExtractMeta_MetadataOnly(t *testing.T) {
	f, _ := os.Open("testdata/metadata-only.jsonl")
	defer f.Close()
	m := Meta{}
	if err := ExtractMeta(f, &m); err != nil {
		t.Fatal(err)
	}
	if m.FirstPrompt != "" || m.LastPrompt != "" {
		t.Error("no user prompts; FirstPrompt/LastPrompt should remain empty")
	}
}

func TestExtractMeta_IgnoresToolResultUser(t *testing.T) {
	f, _ := os.Open("testdata/tool-result-user.jsonl")
	defer f.Close()
	m := Meta{}
	if err := ExtractMeta(f, &m); err != nil {
		t.Fatal(err)
	}
	if m.FirstPrompt != "real prompt" {
		t.Errorf("FirstPrompt = %q", m.FirstPrompt)
	}
	if m.LastPrompt != "second real prompt" {
		t.Errorf("LastPrompt = %q (tool_result should be skipped)", m.LastPrompt)
	}
}
