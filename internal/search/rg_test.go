package search

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestParse_FixtureFile(t *testing.T) {
	f, err := os.Open("testdata/rg-output.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	matches, err := parse(f, 1000)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("got %d matches; want 2", len(matches))
	}

	// First match: in file 02c753f2-...
	m0 := matches[0]
	if !strings.HasPrefix(m0.SessionID, "02c753f2") {
		t.Errorf("match[0].SessionID = %q; want prefix 02c753f2", m0.SessionID)
	}
	if m0.LineNo != 2 {
		t.Errorf("match[0].LineNo = %d; want 2", m0.LineNo)
	}
	if !strings.Contains(m0.Line, "hello world") {
		t.Errorf("match[0].Line = %q; want to contain 'hello world'", m0.Line)
	}
	if len(m0.Before) != 1 {
		t.Errorf("match[0].Before len = %d; want 1", len(m0.Before))
	} else if !strings.Contains(m0.Before[0], "context line before") {
		t.Errorf("match[0].Before[0] = %q", m0.Before[0])
	}
	if len(m0.After) != 1 {
		t.Errorf("match[0].After len = %d; want 1", len(m0.After))
	} else if !strings.Contains(m0.After[0], "context line after") {
		t.Errorf("match[0].After[0] = %q", m0.After[0])
	}

	// Second match: in file 039f60b8-...
	m1 := matches[1]
	if !strings.HasPrefix(m1.SessionID, "039f60b8") {
		t.Errorf("match[1].SessionID = %q; want prefix 039f60b8", m1.SessionID)
	}
	if m1.LineNo != 12 {
		t.Errorf("match[1].LineNo = %d; want 12", m1.LineNo)
	}
	if !strings.Contains(m1.Line, "hello there") {
		t.Errorf("match[1].Line = %q; want to contain 'hello there'", m1.Line)
	}
	if len(m1.Before) != 2 {
		t.Errorf("match[1].Before len = %d; want 2", len(m1.Before))
	}
	// After is nil because there are no context events after this match before end.
	if len(m1.After) != 0 {
		t.Errorf("match[1].After = %v; want empty", m1.After)
	}
}

func TestParse_MaxMatchesCap(t *testing.T) {
	f, err := os.Open("testdata/rg-output.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	matches, err := parse(f, 1) // cap at 1
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(matches) != 1 {
		t.Errorf("got %d matches; want 1 (capped)", len(matches))
	}
}

func TestSessionIDFromPath(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/home/u/.claude/projects/-Users-me/abc123.jsonl", "abc123"},
		{"/home/u/.claude/projects/-Users-me/02c753f2-1234-5678-abcd.jsonl", "02c753f2-1234-5678-abcd"},
		{"just-a-file.jsonl", "just-a-file"},
	}
	for _, c := range cases {
		got := sessionIDFromPath(c.path)
		if got != c.want {
			t.Errorf("sessionIDFromPath(%q) = %q; want %q", c.path, got, c.want)
		}
	}
}

// TestRun_ExcludeGlobs verifies that entries in Options.ExcludeGlobs are
// passed through to rg as additional -g patterns. Requires rg on PATH.
func TestRun_ExcludeGlobs(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not in PATH")
	}

	root := t.TempDir()
	keep := filepath.Join(root, "-Users-me-proj")
	hide := filepath.Join(root, "-Users-me")
	for _, d := range []string{keep, hide} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(d, "sess.jsonl")
		if err := os.WriteFile(path, []byte(`{"type":"user","message":{"content":"needle"}}`+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx := context.Background()

	// Baseline: no excludes, both dirs contribute a match.
	got, err := Run(ctx, Options{ProjectsDir: root, Query: "needle"})
	if err != nil {
		t.Fatalf("baseline Run: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("baseline match count = %d; want 2", len(got))
	}

	// With exclude glob for the hide dir, only the keep dir remains.
	got, err = Run(ctx, Options{
		ProjectsDir:  root,
		Query:        "needle",
		ExcludeGlobs: []string{"!**/-Users-me/**"},
	})
	if err != nil {
		t.Fatalf("excluded Run: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("excluded match count = %d; want 1", len(got))
	}
	if !strings.Contains(got[0].FilePath, "-Users-me-proj") {
		t.Errorf("excluded match FilePath = %q; want match from -Users-me-proj", got[0].FilePath)
	}
}

// TestRun_FiltersNonConversationTypes verifies that match rows are kept
// only for JSONL lines whose type is user or assistant. Claude Code writes
// several side-channel record types (last-prompt, permission-mode, …) that
// echo prompt text and would otherwise produce duplicate hits. Requires rg
// on PATH.
func TestRun_FiltersNonConversationTypes(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not in PATH")
	}

	root := t.TempDir()
	dir := filepath.Join(root, "-Users-me-proj")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Mimic a real claude session file: the word "needle" appears once in
	// a real user turn and then several more times in side-channel records
	// that should be filtered out.
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"needle first"}}`,
		`{"type":"last-prompt","lastPrompt":"needle second"}`,
		`{"type":"permission-mode","mode":"acceptEdits","note":"needle third"}`,
		`{"type":"ai-title","title":"needle fourth"}`,
		`{"type":"attachment","body":"needle fifth"}`,
		`{"type":"assistant","message":{"role":"assistant","content":"needle sixth"}}`,
		`{"type":"system","content":"needle seventh"}`,
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "sess.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := Run(context.Background(), Options{ProjectsDir: root, Query: "needle"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("match count = %d; want 2 (only user+assistant)", len(got))
	}

	lineNos := []int{got[0].LineNo, got[1].LineNo}
	sort.Ints(lineNos)
	if lineNos[0] != 1 || lineNos[1] != 6 {
		t.Errorf("kept lines = %v; want [1 6]", lineNos)
	}
}

func TestIsConversationTurn(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{`{"type":"user","message":{"role":"user"}}`, true},
		{`{"type":"assistant","message":{"role":"assistant"}}`, true},
		{`{"type":"last-prompt","lastPrompt":"x"}`, false},
		{`{"type":"permission-mode","mode":"default"}`, false},
		{`{"type":"ai-title"}`, false},
		{`{"type":"attachment"}`, false},
		{`{"type":"file-history-snapshot"}`, false},
		{`{"type":"system"}`, false},
		{`{"no":"type field"}`, false},
		{`not json`, false},
		{``, false},
	}
	for _, c := range cases {
		if got := isConversationTurn(c.line); got != c.want {
			t.Errorf("isConversationTurn(%q) = %v; want %v", c.line, got, c.want)
		}
	}
}
