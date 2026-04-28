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

// stubLoad returns a no-op tea.Cmd; used so App.Update returns non-nil
// cmds even without the real StreamTurns wiring.
func stubLoad(seq int, m session.Meta, ctx context.Context) tea.Cmd {
	return func() tea.Msg { return nil }
}

func TestApp_InitialStateShowsList(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, AllMode: false, LoadTranscript: stubLoad})
	// Inject scan result.
	model, _ := app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "a", FirstPrompt: "one", UpdatedAt: time.Unix(2, 0), Enriched: true},
		{ID: "b", FirstPrompt: "two", UpdatedAt: time.Unix(1, 0), Enriched: true},
	}})
	v := model.View()
	if !strings.Contains(v, "a") || !strings.Contains(v, "one") {
		t.Errorf("list missing: %s", v)
	}
}

func TestApp_CursorMoveSchedulesTranscriptLoad(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	m, _ := app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "a", Path: "a.jsonl", UpdatedAt: time.Unix(2, 0)},
		{ID: "b", Path: "b.jsonl", UpdatedAt: time.Unix(1, 0)},
	}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if cmd == nil {
		t.Error("j should schedule LoadTranscript cmd")
	}
}

func TestApp_SlashEntersSearchMode(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a", Enriched: true}}})
	m, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	mm := m.(*App)
	if mm.mode != modeSearch {
		t.Errorf("mode = %v; want search", mm.mode)
	}
}

func TestApp_EnrichMsgUpdatesItem(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}, {ID: "b"}}})
	app.Update(EnrichMsg{Meta: session.Meta{ID: "a", FirstPrompt: "filled", Enriched: true}})
	items := app.list.Items()
	var found bool
	for _, m := range items {
		if m.ID == "a" && m.FirstPrompt == "filled" {
			found = true
		}
	}
	if !found {
		t.Error("enrich did not update item")
	}
}

// makeTestSearchMatches returns a small set of matches across two sessions.
func makeTestSearchMatches() []search.Match {
	return []search.Match{
		{
			SessionID: "abc123",
			FilePath:  "/fake/abc123.jsonl",
			LineNo:    5,
			Line:      `{"type":"user","content":"hello world"}`,
			SortTime:  time.Unix(100, 0),
		},
		{
			SessionID: "def456",
			FilePath:  "/fake/def456.jsonl",
			LineNo:    12,
			Line:      `{"type":"assistant","content":"hi there"}`,
			SortTime:  time.Unix(200, 0),
		},
	}
}

// TestApp_SearchResultsMsgSwitchesToMatchMode verifies that receiving
// searchResultsMsg with a matching query shows the match list.
func TestApp_SearchResultsMsgSwitchesToMatchMode(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "abc123", Path: "/fake/abc123.jsonl", Enriched: true, CWD: "/work"},
		{ID: "def456", Path: "/fake/def456.jsonl", Enriched: true, CWD: "/work"},
	}})

	// Enter search mode and type a query.
	m, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	mm := m.(*App)
	// Fake that the search input now holds "h" (it does, handled by textinput).
	// Inject results as if rg returned them.
	matches := makeTestSearchMatches()
	m, _ = m.Update(searchResultsMsg{query: mm.search.Query(), matches: matches})
	mm = m.(*App)

	if !mm.showMatches {
		t.Error("showMatches should be true after searchResultsMsg")
	}
	if len(mm.matchList.Items()) != 2 {
		t.Errorf("matchList has %d items; want 2", len(mm.matchList.Items()))
	}
	v := m.View()
	if !strings.Contains(v, "abc123") {
		t.Errorf("match abc123 missing from view: %s", v)
	}
}

// TestApp_SearchEnterExpandsThenResumes verifies the two-stage Enter UX.
func TestApp_SearchEnterExpandsThenResumes(t *testing.T) {
	var resumed session.Meta
	resumeFn := func(m session.Meta) tea.Cmd {
		resumed = m
		return func() tea.Msg { return tea.Quit() }
	}
	loadFn := func(seq int, m session.Meta, ctx context.Context) tea.Cmd {
		// Return turns including one at lineNo 5 to match the first match.
		return func() tea.Msg {
			return BatchTurnsMsg{Seq: seq, Turns: []session.Turn{
				{Role: "user", Text: "hello world", LineNo: 5},
				{Role: "assistant", Text: "hi", LineNo: 7},
			}}
		}
	}

	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: loadFn, ResumeSelected: resumeFn})
	app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "abc123", Path: "/fake/abc123.jsonl", Enriched: true, CWD: "/work/proj"},
	}})

	// Enter search mode.
	m, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	mm := m.(*App)

	// Inject search results.
	matches := makeTestSearchMatches()[:1] // just abc123
	m, _ = m.Update(searchResultsMsg{query: mm.search.Query(), matches: matches})
	mm = m.(*App)
	if !mm.showMatches {
		t.Fatal("expected showMatches after results")
	}

	// First Enter: should expand to full transcript.
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm = m.(*App)
	if !mm.matchExpanded {
		t.Error("matchExpanded should be true after first Enter")
	}
	if cmd == nil {
		t.Error("first Enter should return a LoadTranscript cmd")
	}

	// Execute the transcript load cmd.
	msg := cmd()
	m, _ = m.Update(msg)
	mm = m.(*App)

	// Second Enter: should trigger ResumeSelected.
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error("second Enter should return a ResumeSelected cmd")
	}
	// Execute the resume cmd.
	cmd()
	if resumed.ID != "abc123" {
		t.Errorf("resumed.ID = %q; want abc123", resumed.ID)
	}
}

// TestApp_SearchEscFromExpandedBacksToContextView verifies that Esc from
// matchExpanded returns to context (match) view without exiting search.
func TestApp_SearchEscFromExpandedBacksToContextView(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "abc123", Enriched: true, CWD: "/w"},
	}})
	m, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	mm := m.(*App)
	matches := makeTestSearchMatches()[:1]
	m, _ = m.Update(searchResultsMsg{query: mm.search.Query(), matches: matches})

	// First Enter to expand.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm = m.(*App)
	if !mm.matchExpanded {
		t.Fatal("expected matchExpanded")
	}

	// Esc should return to context view (not exit search).
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mm = m.(*App)
	if mm.matchExpanded {
		t.Error("matchExpanded should be false after Esc")
	}
	if mm.mode != modeSearch {
		t.Error("mode should still be modeSearch after Esc from expanded")
	}
	if !mm.showMatches {
		t.Error("showMatches should still be true after Esc from expanded")
	}

	// Second Esc should exit search entirely.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mm = m.(*App)
	if mm.mode != modeNormal {
		t.Errorf("mode = %v; want modeNormal after second Esc", mm.mode)
	}
	if mm.showMatches {
		t.Error("showMatches should be false after exiting search")
	}
}
