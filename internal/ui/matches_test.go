package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"csess/internal/search"
)

func makeTestMatches() []search.Match {
	t0 := time.Unix(200, 0)
	t1 := time.Unix(100, 0)
	line := `{"cwd":"/Users/chaomai/Downloads/..."}`
	// Match "Downloads" inside line to exercise the centered snippet.
	start := strings.Index(line, "Downloads")
	end := start + len("Downloads")
	return []search.Match{
		{SessionID: "02c753f2-aaa", FilePath: "/p/a.jsonl", LineNo: 42, Line: line, MatchStart: start, MatchEnd: end, SortTime: t0},
		{SessionID: "039f60b8-bbb", FilePath: "/p/b.jsonl", LineNo: 12, Line: "text content snippet here", MatchStart: 5, MatchEnd: 12, SortTime: t1},
		{SessionID: "039f60b8-bbb", FilePath: "/p/b.jsonl", LineNo: 87, Line: "another match in same session", MatchStart: 8, MatchEnd: 13, SortTime: t1},
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
	// Raw line numbers must NOT appear — the preview is pretty-printed and
	// the jsonl line number would be misleading.
	if strings.Contains(v, "L42") || strings.Contains(v, "L12") {
		t.Errorf("row should not show raw jsonl line number: %s", v)
	}
	// Snippet content (the matched text is part of each line)
	if !strings.Contains(v, "Downloads") {
		t.Errorf("missing snippet content: %s", v)
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
			Line:      fmt.Sprintf("line content %d", i+1),
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
	// Items are sorted SortTime desc: item 20 first, item 1 last. After 10
	// j presses from top, the cursor is on item 11 (from top), i.e. the
	// row whose Line ends with "10".
	if !strings.Contains(v, "content 10") {
		t.Errorf("10th-from-top row not visible after 10 j presses: %q", v)
	}
	if strings.Contains(v, "content 20") {
		t.Errorf("top row should have scrolled off: %q", v)
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

func TestMatchSnippet(t *testing.T) {
	// Short pre-context: no ellipsis, all of pre is shown.
	short := search.Match{Line: "hello needle world", MatchStart: 6, MatchEnd: 12}
	got := matchSnippet(short)
	if strings.HasPrefix(got, "…") {
		t.Errorf("short pre should not get an ellipsis prefix: %q", got)
	}
	if !strings.Contains(got, "hello ") || !strings.Contains(got, " world") {
		t.Errorf("surrounding context missing: %q", got)
	}

	// Long pre-context: only the last matchSnippetBeforeRunes runes of pre
	// are kept, prefixed with an ellipsis.
	longPre := strings.Repeat("a", 100) + "XneedleY"
	long := search.Match{
		Line:       longPre,
		MatchStart: 100 + 1,       // "needle" starts after 100*'a' + "X"
		MatchEnd:   100 + 1 + 6,
	}
	got = matchSnippet(long)
	if !strings.HasPrefix(got, "…") {
		t.Errorf("long pre should start with ellipsis: %q", got)
	}
	// The portion of pre visible before the highlight should be exactly
	// matchSnippetBeforeRunes 'a's plus the immediate "X" (last 21 runes
	// would be 20 'a's + 'X'; we keep the last 20 runes, which is 19 'a's
	// + 'X'). Be tolerant: just check we don't still have 100 'a's.
	if strings.Count(got, "a") >= 100 {
		t.Errorf("pre should have been trimmed: %q", got)
	}

	// Invalid offsets fall back to the raw line (flattened).
	bad := search.Match{Line: "plain text", MatchStart: -1, MatchEnd: 5}
	if got := matchSnippet(bad); !strings.Contains(got, "plain text") {
		t.Errorf("invalid offsets should fall back to raw line: %q", got)
	}
}
