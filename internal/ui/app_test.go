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

// TestApp_SearchEnterResumesDirectly verifies that Enter on a match row
// resumes the parent session immediately (one-stage, no expand).
func TestApp_SearchEnterResumesDirectly(t *testing.T) {
	var resumed session.Meta
	resumeFn := func(m session.Meta) tea.Cmd {
		resumed = m
		return func() tea.Msg { return tea.Quit() }
	}

	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad, ResumeSelected: resumeFn})
	app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "abc123", Path: "/fake/abc123.jsonl", Enriched: true, CWD: "/work/proj"},
	}})

	m, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	mm := m.(*App)

	matches := makeTestSearchMatches()[:1]
	m, _ = m.Update(searchResultsMsg{query: mm.search.Query(), matches: matches})
	mm = m.(*App)
	if !mm.showMatches {
		t.Fatal("expected showMatches after results")
	}

	// One Enter resumes directly.
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter should return a ResumeSelected cmd")
	}
	cmd()
	if resumed.ID != "abc123" {
		t.Errorf("resumed.ID = %q; want abc123", resumed.ID)
	}
}

// TestApp_EnrichDoneMsgResortsByUpdatedAt reproduces the bug where the
// list was stuck in ModTime order until a search+exit cycle triggered an
// implicit resort via applyFilter. After EnrichDoneMsg, the list must be
// sorted by UpdatedAt with the selected session (by ID) preserved.
func TestApp_EnrichDoneMsgResortsByUpdatedAt(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	// Scan: ordered by ModTime desc (matches what scanner.Quick does).
	// UpdatedAt disagrees — "b" has the newest UpdatedAt despite oldest ModTime.
	app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "a", ModTime: time.Unix(300, 0)},
		{ID: "b", ModTime: time.Unix(200, 0)},
		{ID: "c", ModTime: time.Unix(100, 0)},
	}})

	// Sanity: before enrichment, order follows ModTime (a, b, c).
	if got := app.list.Items()[0].ID; got != "a" {
		t.Fatalf("pre-enrich head = %q; want a (ModTime order)", got)
	}

	// Enrich: UpdatedAt order will be b > c > a.
	app.Update(EnrichMsg{Meta: session.Meta{ID: "a", ModTime: time.Unix(300, 0), UpdatedAt: time.Unix(10, 0), Enriched: true}})
	app.Update(EnrichMsg{Meta: session.Meta{ID: "b", ModTime: time.Unix(200, 0), UpdatedAt: time.Unix(900, 0), Enriched: true}})
	app.Update(EnrichMsg{Meta: session.Meta{ID: "c", ModTime: time.Unix(100, 0), UpdatedAt: time.Unix(500, 0), Enriched: true}})

	// Still in ModTime order — ReplaceItem deliberately does not re-sort.
	if got := app.list.Items()[0].ID; got != "a" {
		t.Fatalf("post-enrich head before Done = %q; want a (still ModTime order)", got)
	}

	// EnrichDoneMsg triggers the resort.
	app.Update(EnrichDoneMsg{})

	items := app.list.Items()
	want := []string{"b", "c", "a"}
	for i, w := range want {
		if items[i].ID != w {
			t.Errorf("items[%d] = %q; want %q (UpdatedAt order)", i, items[i].ID, w)
		}
	}
}

// TestApp_EnrichDoneMsgPreservesCursorByID verifies the cursor follows the
// selected session across the post-enrichment resort.
func TestApp_EnrichDoneMsgPreservesCursorByID(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "a", ModTime: time.Unix(300, 0)},
		{ID: "b", ModTime: time.Unix(200, 0)},
		{ID: "c", ModTime: time.Unix(100, 0)},
	}})

	// Move cursor to "b" (index 1 pre-resort).
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if sel, _ := app.list.Selected(); sel.ID != "b" {
		t.Fatalf("selected = %q; want b", sel.ID)
	}

	// Enrich so post-sort order becomes c, b, a (UpdatedAt desc).
	app.Update(EnrichMsg{Meta: session.Meta{ID: "a", ModTime: time.Unix(300, 0), UpdatedAt: time.Unix(10, 0), Enriched: true}})
	app.Update(EnrichMsg{Meta: session.Meta{ID: "b", ModTime: time.Unix(200, 0), UpdatedAt: time.Unix(500, 0), Enriched: true}})
	app.Update(EnrichMsg{Meta: session.Meta{ID: "c", ModTime: time.Unix(100, 0), UpdatedAt: time.Unix(900, 0), Enriched: true}})
	app.Update(EnrichDoneMsg{})

	// Cursor must still select "b", now at index 1 (c, b, a).
	sel, ok := app.list.Selected()
	if !ok || sel.ID != "b" {
		t.Errorf("post-resort selected = %q (ok=%v); want b", sel.ID, ok)
	}
}

// TestApp_EnrichDoneMsgPreservesPreviewTurns guards against a regression
// where the resort handler called updatePreviewFromSelection, whose
// SetMeta clears the loaded transcript. Since Resort() preserves cursor
// by ID, the preview must be left alone.
func TestApp_EnrichDoneMsgPreservesPreviewTurns(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "a", ModTime: time.Unix(300, 0)},
		{ID: "b", ModTime: time.Unix(200, 0)},
	}})

	// Simulate a transcript turn landing for the selected session.
	app.preview.AddTurn(session.Turn{Role: "user", Text: "hello-transcript"})
	if !strings.Contains(app.preview.View(), "hello-transcript") {
		t.Fatalf("precondition: turn should be in preview view")
	}

	// Enrich + Done re-sorts the list. Selected session ("a") moves
	// position but preview should retain the turn.
	app.Update(EnrichMsg{Meta: session.Meta{ID: "a", UpdatedAt: time.Unix(10, 0), Enriched: true}})
	app.Update(EnrichMsg{Meta: session.Meta{ID: "b", UpdatedAt: time.Unix(900, 0), Enriched: true}})
	app.Update(EnrichDoneMsg{})

	if !strings.Contains(app.preview.View(), "hello-transcript") {
		t.Errorf("preview lost transcript turn after EnrichDoneMsg resort")
	}
}

// TestApp_MatchSelectionRendersFullTranscript verifies that selecting a
// match in search mode loads the parent session's transcript and the
// matched line is highlighted with ▶▶▶. Visual parity with non-search
// session preview.
func TestApp_MatchSelectionRendersFullTranscript(t *testing.T) {
	// LoadTranscript that returns a canned transcript for any session.
	loadFn := func(seq int, m session.Meta, ctx context.Context) tea.Cmd {
		return func() tea.Msg {
			return BatchTurnsMsg{Seq: seq, Turns: []session.Turn{
				{Role: "user", Text: "hello", LineNo: 3},
				{Role: "assistant", Text: "matched content", LineNo: 5},
				{Role: "user", Text: "followup", LineNo: 6},
			}}
		}
	}

	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: loadFn})
	app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "abc123", Path: "/fake/abc123.jsonl", Enriched: true, CWD: "/work"},
	}})

	// Enter search, inject a match whose LineNo targets the assistant turn.
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	match := search.Match{
		SessionID: "abc123",
		FilePath:  "/fake/abc123.jsonl",
		LineNo:    5,
		Line:      `{"type":"assistant","content":"matched content"}`,
	}
	_, cmd := app.Update(searchResultsMsg{query: app.search.Query(), matches: []search.Match{match}})
	if cmd == nil {
		t.Fatal("searchResultsMsg should return a LoadTranscript cmd for match preview")
	}

	// Execute the cmd to get BatchTurnsMsg, then deliver it.
	app.Update(cmd())

	v := app.preview.View()
	if !strings.Contains(v, "hello") {
		t.Errorf("preview should render full transcript (missing 'hello'): %s", v)
	}
	if !strings.Contains(v, "matched content") {
		t.Errorf("preview should render matched turn body: %s", v)
	}
	if !strings.Contains(v, "followup") {
		t.Errorf("preview should render surrounding turns: %s", v)
	}
	if !strings.Contains(v, "▶▶▶") {
		t.Errorf("matched turn should be marked with ▶▶▶: %s", v)
	}
}

func TestApp_SearchEscExitsSearch(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "abc123", Enriched: true, CWD: "/w"},
	}})
	m, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	mm := m.(*App)
	matches := makeTestSearchMatches()[:1]
	m, _ = m.Update(searchResultsMsg{query: mm.search.Query(), matches: matches})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mm = m.(*App)
	if mm.mode != modeNormal {
		t.Errorf("mode = %v; want modeNormal after Esc", mm.mode)
	}
	if mm.showMatches {
		t.Error("showMatches should be false after exiting search")
	}
}

func TestApp_CtrlKSwitchesFocusToBookmarks(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	// Manually seed a bookmark so the pane is non-empty.
	app.bookmarks.SetItems(
		[]session.Meta{{ID: "bm1"}},
		map[string]time.Time{"bm1": time.Unix(1, 0)},
	)

	app.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if app.focus != focusBookmarks {
		t.Errorf("focus after Ctrl-K = %d; want focusBookmarks", app.focus)
	}
	// j should now drive the bookmarks pane, not the list.
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if app.list.Cursor() != 0 {
		t.Errorf("list cursor moved despite focusBookmarks: %d", app.list.Cursor())
	}
}

func TestApp_CtrlKEmptyBookmarksIsNoop(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	// bookmarks pane is empty by default.
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if app.focus != focusList {
		t.Errorf("focus after Ctrl-K with empty bookmarks = %d; want focusList", app.focus)
	}
}

func TestApp_CtrlJReturnsFocusToList(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	app.bookmarks.SetItems(
		[]session.Meta{{ID: "bm1"}},
		map[string]time.Time{"bm1": time.Unix(1, 0)},
	)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if app.focus != focusList {
		t.Errorf("focus after Ctrl-J = %d; want focusList", app.focus)
	}
}

func TestApp_JInFocusBookmarksMovesBookmarkCursor(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	app.bookmarks.SetItems(
		[]session.Meta{{ID: "bm1"}, {ID: "bm2"}},
		map[string]time.Time{"bm1": time.Unix(200, 0), "bm2": time.Unix(100, 0)},
	)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if app.bookmarks.Cursor() != 1 {
		t.Errorf("bookmarks cursor after j = %d; want 1", app.bookmarks.Cursor())
	}
	if app.list.Cursor() != 0 {
		t.Errorf("list cursor changed despite focusBookmarks: %d", app.list.Cursor())
	}
}

func TestApp_EnterInFocusBookmarksResumesBookmarked(t *testing.T) {
	var resumed session.Meta
	app := NewApp(AppConfig{
		Width: 120, Height: 40, LoadTranscript: stubLoad,
		ResumeSelected: func(m session.Meta) tea.Cmd {
			resumed = m
			return func() tea.Msg { return nil }
		},
	})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	app.bookmarks.SetItems(
		[]session.Meta{{ID: "bm1", Enriched: true, CWD: "/work"}},
		map[string]time.Time{"bm1": time.Unix(1, 0)},
	)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter in focusBookmarks should return a resume cmd")
	}
	cmd()
	if resumed.ID != "bm1" {
		t.Errorf("resumed.ID = %q; want bm1", resumed.ID)
	}
}
