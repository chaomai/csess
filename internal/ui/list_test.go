package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

func TestList_WindowsToHeight(t *testing.T) {
	items := make([]session.Meta, 100)
	for i := range items {
		items[i] = session.Meta{ID: fmt.Sprintf("s%03d", i), FirstPrompt: "p", Enriched: true}
	}
	m := NewList(80, 5, false) // height=5
	m.SetItems(items)
	v := m.View()
	lines := strings.Count(v, "\n") + 1
	if lines > 5 {
		t.Errorf("View has %d lines; want <= 5", lines)
	}
	// Cursor at top; s000..s004 visible, s005 not
	if !strings.Contains(v, "s000") {
		t.Error("top item missing")
	}
	if strings.Contains(v, "s099") {
		t.Error("bottom item should not be visible with cursor at top")
	}
}

func TestList_CursorScrollsWindow(t *testing.T) {
	items := make([]session.Meta, 20)
	for i := range items {
		items[i] = session.Meta{ID: fmt.Sprintf("s%02d", i), Enriched: true}
	}
	m := NewList(80, 3, false)
	m.SetItems(items)
	// Move cursor down 10 times
	for i := 0; i < 10; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	v := m.View()
	if !strings.Contains(v, "s10") {
		t.Errorf("cursor pos not visible after 10 j presses: %q", v)
	}
	if strings.Contains(v, "s00") {
		t.Errorf("first item should have scrolled off: %q", v)
	}
}

func TestList_CtrlVPagesDown(t *testing.T) {
	items := make([]session.Meta, 20)
	for i := range items {
		items[i] = session.Meta{ID: fmt.Sprintf("s%02d", i), UpdatedAt: time.Unix(int64(100-i), 0), Enriched: true}
	}
	m := NewList(80, 10, false) // visible height = 10 → pageStep = 8
	m.SetItems(items)

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if m.Cursor() != 8 {
		t.Errorf("cursor after ctrl+v = %d; want 8", m.Cursor())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if m.Cursor() != 16 {
		t.Errorf("cursor after second ctrl+v = %d; want 16", m.Cursor())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if m.Cursor() != 19 {
		t.Errorf("cursor at bottom should clamp to len-1; got %d, want 19", m.Cursor())
	}
}

func TestList_AltVPagesUp(t *testing.T) {
	items := make([]session.Meta, 20)
	for i := range items {
		items[i] = session.Meta{ID: fmt.Sprintf("s%02d", i), UpdatedAt: time.Unix(int64(100-i), 0), Enriched: true}
	}
	m := NewList(80, 10, false) // pageStep = 8
	m.SetItems(items)
	m.Update(tea.KeyMsg{Type: tea.KeyEnd}) // cursor = 19

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if m.Cursor() != 11 {
		t.Errorf("cursor after alt+v from bottom = %d; want 11", m.Cursor())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if m.Cursor() != 3 {
		t.Errorf("cursor after second alt+v = %d; want 3", m.Cursor())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if m.Cursor() != 0 {
		t.Errorf("cursor at top should clamp to 0; got %d", m.Cursor())
	}
}

func TestList_PageStepClampsToOneOnTinyPane(t *testing.T) {
	items := make([]session.Meta, 5)
	for i := range items {
		items[i] = session.Meta{ID: fmt.Sprintf("s%d", i), UpdatedAt: time.Unix(int64(100-i), 0), Enriched: true}
	}
	m := NewList(80, 1, false) // pageStep = max(1, 1-2) = 1
	m.SetItems(items)

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if m.Cursor() != 1 {
		t.Errorf("ctrl+v on h=1 should advance by 1; got %d", m.Cursor())
	}
}
