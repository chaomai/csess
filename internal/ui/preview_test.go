package ui

import (
	"strings"
	"testing"
	"time"

	"csess/internal/search"
	"csess/internal/session"
)

func TestPreview_HeaderWithoutTurns(t *testing.T) {
	p := NewPreview(80, 20)
	p.SetMeta(session.Meta{
		ID: "abc", CWD: "/x", Model: "claude-opus-4-7", Version: "2.1.117",
		StartedAt: time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC),
		UserMsgs:  3, AsstMsgs: 3, Enriched: true,
	})
	v := p.View()
	if !strings.Contains(v, "abc") {
		t.Errorf("missing id")
	}
	if !strings.Contains(v, "/x") {
		t.Errorf("missing cwd")
	}
	if !strings.Contains(v, "claude-opus-4-7") {
		t.Errorf("missing model")
	}
}

func TestPreview_AppendsTurns(t *testing.T) {
	p := NewPreview(80, 20)
	p.SetMeta(session.Meta{ID: "x", Enriched: true})
	p.AddTurn(session.Turn{Role: "user", Text: "hello"})
	p.AddTurn(session.Turn{Role: "assistant", Text: "hi"})
	v := p.View()
	if !strings.Contains(v, "hello") || !strings.Contains(v, "hi") {
		t.Errorf("turns missing: %s", v)
	}
}

func TestPreview_CorruptMarker(t *testing.T) {
	p := NewPreview(80, 20)
	p.SetMeta(session.Meta{ID: "x", LoadErr: testErr("bad json"), Enriched: true})
	if !strings.Contains(p.View(), "corrupt") {
		t.Error("missing corrupt marker")
	}
}

func TestPreview_SetMatchContext(t *testing.T) {
	p := NewPreview(80, 20)
	meta := session.Meta{
		ID: "02c753f2-aaa", CWD: "/Users/chaomai/Downloads", Enriched: true,
	}
	m := search.Match{
		SessionID: "02c753f2-aaa",
		FilePath:  "/p/a.jsonl",
		LineNo:    42,
		Line:      `{"cwd":"/Users/chaomai/Downloads/test"}`,
		Before:    []string{"before line content"},
		After:     []string{"after line content"},
	}
	p.SetMatchContext(meta, m)
	v := p.View()

	if !strings.Contains(v, "02c753f2-aaa") {
		t.Errorf("missing session id in header: %s", v)
	}
	if !strings.Contains(v, "match at line 42") {
		t.Errorf("missing match line header: %s", v)
	}
	if !strings.Contains(v, "/Users/chaomai/Downloads/test") {
		t.Errorf("missing match line content: %s", v)
	}
	if !strings.Contains(v, "before line content") {
		t.Errorf("missing before context: %s", v)
	}
	if !strings.Contains(v, "after line content") {
		t.Errorf("missing after context: %s", v)
	}
}

func TestPreview_SetExpandedMatch(t *testing.T) {
	p := NewPreview(80, 20)
	p.SetMeta(session.Meta{ID: "sess-x", Enriched: true})

	turns := []session.Turn{
		{Role: "user", Text: "hello", LineNo: 3},
		{Role: "assistant", Text: "hi there", LineNo: 5},
		{Role: "user", Text: "run ls", LineNo: 6},
	}
	match := search.Match{
		SessionID: "sess-x",
		LineNo:    5, // should mark the assistant turn
	}
	p.SetExpandedMatch(match, turns)
	v := p.View()

	// All turns should appear in the content.
	if !strings.Contains(v, "hello") {
		t.Errorf("missing 'hello' turn: %s", v)
	}
	if !strings.Contains(v, "hi there") {
		t.Errorf("missing 'hi there' turn: %s", v)
	}
	// The target turn should have the ▶▶▶ marker.
	if !strings.Contains(v, "▶▶▶") {
		t.Errorf("missing ▶▶▶ marker for target turn: %s", v)
	}
}

type testErr string

func (e testErr) Error() string { return string(e) }
