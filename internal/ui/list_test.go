package ui

import (
	"strings"
	"testing"
	"time"

	"csess/internal/session"
)

func TestList_RendersIDsAndFirstPrompts(t *testing.T) {
	m := NewList(80, 20, false)
	m.SetItems([]session.Meta{
		{ID: "sess-a", FirstPrompt: "first one", ModTime: time.Unix(2, 0), UpdatedAt: time.Unix(20, 0), Enriched: true},
		{ID: "sess-b", FirstPrompt: "second one", ModTime: time.Unix(1, 0), UpdatedAt: time.Unix(10, 0), Enriched: true},
	})
	v := m.View()
	if !strings.Contains(v, "sess-a") {
		t.Errorf("missing sess-a: %s", v)
	}
	if !strings.Contains(v, "first one") {
		t.Errorf("missing first prompt: %s", v)
	}
}

func TestList_SortByUpdatedAtDesc(t *testing.T) {
	m := NewList(80, 20, false)
	m.SetItems([]session.Meta{
		{ID: "older", UpdatedAt: time.Unix(10, 0), Enriched: true},
		{ID: "newer", UpdatedAt: time.Unix(100, 0), Enriched: true},
	})
	items := m.Items()
	if items[0].ID != "newer" {
		t.Errorf("head = %q; want newer", items[0].ID)
	}
}

func TestList_AllModeShowsProjectColumn(t *testing.T) {
	m := NewList(80, 20, true)
	m.SetItems([]session.Meta{
		{ID: "x", CWD: "/Users/me/proj-one", Enriched: true},
	})
	if !strings.Contains(m.View(), "proj-one") {
		t.Errorf("missing project column: %s", m.View())
	}
}
