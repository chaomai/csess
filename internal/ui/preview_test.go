package ui

import (
	"strings"
	"testing"
	"time"

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

type testErr string

func (e testErr) Error() string { return string(e) }
