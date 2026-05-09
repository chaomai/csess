package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"csess/internal/search"
)

func makeTestMatches() []search.Match {
	t0 := time.Unix(200, 0)
	t1 := time.Unix(100, 0)
	return []search.Match{
		{SessionID: "02c753f2-aaa", FilePath: "/p/a.jsonl", LineNo: 42, Line: `{"cwd":"/Users/chaomai/Downloads/..."}`, SortTime: t0},
		{SessionID: "039f60b8-bbb", FilePath: "/p/b.jsonl", LineNo: 12, Line: "text content snippet here", SortTime: t1},
		{SessionID: "039f60b8-bbb", FilePath: "/p/b.jsonl", LineNo: 87, Line: "another match in same session", SortTime: t1},
	}
}

func TestMatchList_RendersRows(t *testing.T) {
	ml := NewMatchList(80, 20)
	ml.SetItems(makeTestMatches())
	v := ml.View()

	// Session ID prefixes
	if !strings.Contains(v, "02c753f2") {
		t.Errorf("missing 02c753f2: %s", v)
	}
	if !strings.Contains(v, "039f60b8") {
		t.Errorf("missing 039f60b8: %s", v)
	}
	// Line numbers
	if !strings.Contains(v, "L42") {
		t.Errorf("missing L42: %s", v)
	}
	if !strings.Contains(v, "L12") {
		t.Errorf("missing L12: %s", v)
	}
	// Snippet content
	if !strings.Contains(v, "/Users/chaomai/Downloads") {
		t.Errorf("missing snippet: %s", v)
	}
	// Cursor marker on first item
	if !strings.Contains(v, "▶") {
		t.Errorf("missing cursor marker: %s", v)
	}
}

func TestMatchList_CursorScrolls(t *testing.T) {
	items := make([]search.Match, 20)
	for i := range items {
		items[i] = search.Match{
			SessionID: "id",
			LineNo:    i + 1,
			Line:      "line content",
			SortTime:  time.Unix(int64(i), 0),
		}
	}
	ml := NewMatchList(80, 3)
	ml.SetItems(items)

	// Move cursor down 10 times
	for i := 0; i < 10; i++ {
		ml.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	}
	v := ml.View()
	// The 10th item (L10) should be visible; L1 should not.
	if !strings.Contains(v, "L10") {
		t.Errorf("L10 not visible after 10 j presses: %q", v)
	}
	if strings.Contains(v, "L1 ") || strings.HasPrefix(v, "L1") {
		t.Errorf("L1 should have scrolled off: %q", v)
	}
}

func TestMatchList_EmptyShowsMessage(t *testing.T) {
	ml := NewMatchList(80, 10)
	ml.SetItems(nil)
	if !strings.Contains(ml.View(), "no matches") {
		t.Error("empty list should show 'no matches'")
	}
}

// Newest session's matches must rank first, regardless of input order.
func TestMatchList_SortsByTimeDescending(t *testing.T) {
	old := time.Unix(100, 0)
	newer := time.Unix(200, 0)
	// Inserted oldest-first to prove the sort actually reorders.
	items := []search.Match{
		{SessionID: "old-sess", LineNo: 5, SortTime: old},
		{SessionID: "new-sess", LineNo: 9, SortTime: newer},
		{SessionID: "old-sess", LineNo: 12, SortTime: old},
	}
	ml := NewMatchList(80, 20)
	ml.SetItems(items)

	got := ml.Items()
	if got[0].SessionID != "new-sess" {
		t.Errorf("top row SessionID = %q; want new-sess", got[0].SessionID)
	}
	if got[1].SessionID != "old-sess" || got[2].SessionID != "old-sess" {
		t.Errorf("old-sess rows should follow new-sess; got %q, %q",
			got[1].SessionID, got[2].SessionID)
	}
}

// Matches in the same session (equal SortTime) come out LineNo-ascending.
func TestMatchList_GroupsSameSessionByLineNo(t *testing.T) {
	t0 := time.Unix(100, 0)
	// Inserted descending to prove LineNo ascending is enforced.
	items := []search.Match{
		{SessionID: "s", LineNo: 87, SortTime: t0},
		{SessionID: "s", LineNo: 42, SortTime: t0},
		{SessionID: "s", LineNo: 12, SortTime: t0},
	}
	ml := NewMatchList(80, 20)
	ml.SetItems(items)

	got := ml.Items()
	for i, want := range []int{12, 42, 87} {
		if got[i].LineNo != want {
			t.Errorf("row %d LineNo = %d; want %d", i, got[i].LineNo, want)
		}
	}
}
