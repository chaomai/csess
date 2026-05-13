package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"csess/internal/search"
	"csess/internal/session"
)

// TestApp_CtrlNSwitchesPreviewSession reproduces the user-reported flow:
//
//	1. press `/`
//	2. type the query
//	3. ctrl+n to a different match row
//
// After step 3 the preview's session metadata must reflect the newly
// selected match's session, not whichever session was previewed before.
func TestApp_CtrlNSwitchesPreviewSession(t *testing.T) {
	// Record which sessions LoadTranscript is invoked for.
	var loadedSessions []string
	loader := func(seq int, m session.Meta, ctx context.Context) tea.Cmd {
		loadedSessions = append(loadedSessions, m.ID)
		return func() tea.Msg { return nil }
	}

	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: loader})
	app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "21a37335", Path: "/fake/21a37335.jsonl", Enriched: true, CWD: "/skills/database-ops", UpdatedAt: time.Unix(300, 0)},
		{ID: "75888ddf", Path: "/fake/75888ddf.jsonl", Enriched: true, CWD: "/work", UpdatedAt: time.Unix(200, 0)},
		{ID: "36605df1", Path: "/fake/36605df1.jsonl", Enriched: true, CWD: "/Downloads", UpdatedAt: time.Unix(100, 0)},
	}})

	// Normal-mode: focus lands on the newest session (21a37335).
	m, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	mm := m.(*App)
	matches := []search.Match{
		{SessionID: "75888ddf", FilePath: "/fake/75888ddf.jsonl", LineNo: 10, Line: `{"type":"user","content":"x 根据 y"}`, SortTime: time.Unix(200, 0)},
		{SessionID: "36605df1", FilePath: "/fake/36605df1.jsonl", LineNo: 187, Line: `{"type":"user","content":"y 根据 z"}`, SortTime: time.Unix(100, 0)},
	}
	m, _ = m.Update(searchResultsMsg{query: mm.search.Query(), matches: matches})
	mm = m.(*App)

	// After search results, cursor should be at match[0] (75888ddf).
	if sel, ok := mm.matchList.Selected(); !ok || sel.SessionID != "75888ddf" {
		t.Fatalf("after search, selected match = %+v; want 75888ddf", sel)
	}
	if mm.preview.meta.ID != "75888ddf" {
		t.Errorf("after search, preview.meta.ID = %q; want 75888ddf", mm.preview.meta.ID)
	}

	// Simulate ctrl+n.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	mm = m.(*App)

	if sel, ok := mm.matchList.Selected(); !ok || sel.SessionID != "36605df1" {
		t.Fatalf("after ctrl+n, selected match = %+v; want 36605df1", sel)
	}
	if mm.preview.meta.ID != "36605df1" {
		t.Errorf("after ctrl+n, preview.meta.ID = %q; want 36605df1", mm.preview.meta.ID)
	}
	if got := strings.Join(loadedSessions, ","); !strings.Contains(got, "36605df1") {
		t.Errorf("LoadTranscript not called for 36605df1; loaded: %s", got)
	}
}
