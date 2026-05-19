package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"csess/internal/session"
)

func TestBookmarksPane_EmptyViewIsPaddedToWidth(t *testing.T) {
	p := NewBookmarksPane(60, 10)
	v := p.View()
	if !strings.Contains(v, "no bookmarks") {
		t.Errorf("empty view missing hint text: %q", v)
	}
	first := strings.Split(v, "\n")[0]
	if got := ansi.StringWidth(first); got < 60 {
		t.Errorf("empty view first-line width = %d; want >= 60", got)
	}
}

func TestBookmarksPane_SetItemsSortsByStarredAtDesc(t *testing.T) {
	p := NewBookmarksPane(80, 10)
	p.SetItems(
		[]session.Meta{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		map[string]time.Time{
			"a": time.Unix(100, 0),
			"b": time.Unix(300, 0),
			"c": time.Unix(200, 0),
		},
	)
	got := p.Items()
	want := []string{"b", "c", "a"}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("items[%d] = %q; want %q", i, got[i].ID, id)
		}
	}
}

func TestBookmarksPane_RowShowsTimeIDProjectPrompt(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	p.SetItems(
		[]session.Meta{{ID: "abc12345xyz", CWD: "/work/proj-alpha", FirstPrompt: "fix bug", Enriched: true}},
		map[string]time.Time{"abc12345xyz": time.Now()},
	)
	v := p.View()
	for _, want := range []string{"abc12345", "proj-alpha", "fix bug"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q: %q", want, v)
		}
	}
}

func TestBookmarksPane_JMovesCursorDown(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	items := make([]session.Meta, 3)
	sa := map[string]time.Time{}
	for i := range items {
		id := fmt.Sprintf("id%d", i)
		items[i] = session.Meta{ID: id}
		sa[id] = time.Unix(int64(100-i), 0) // id0 newest
	}
	p.SetItems(items, sa)
	if p.Cursor() != 0 {
		t.Fatalf("initial cursor = %d; want 0", p.Cursor())
	}
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if p.Cursor() != 1 {
		t.Errorf("cursor after j = %d; want 1", p.Cursor())
	}
}

func TestBookmarksPane_DesiredHeightCapsAtMax(t *testing.T) {
	p := NewBookmarksPane(80, 100)
	items := make([]session.Meta, 20)
	sa := map[string]time.Time{}
	for i := range items {
		id := fmt.Sprintf("id%d", i)
		items[i] = session.Meta{ID: id}
		sa[id] = time.Unix(int64(i), 0)
	}
	p.SetItems(items, sa)
	if got := p.DesiredHeight(5); got != 5 {
		t.Errorf("DesiredHeight(5) with 20 items = %d; want 5", got)
	}
	if got := p.DesiredHeight(50); got != 20 {
		t.Errorf("DesiredHeight(50) with 20 items = %d; want 20", got)
	}
}

func TestBookmarksPane_AddInsertsAtSortedPosition(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	p.SetItems(
		[]session.Meta{{ID: "old"}, {ID: "mid"}},
		map[string]time.Time{"old": time.Unix(100, 0), "mid": time.Unix(200, 0)},
	)
	p.Add(session.Meta{ID: "new"}, time.Unix(300, 0))
	got := p.Items()
	want := []string{"new", "mid", "old"}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("items[%d] = %q; want %q", i, got[i].ID, id)
		}
	}
}

func TestBookmarksPane_AddPreservesCursorByID(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	p.SetItems(
		[]session.Meta{{ID: "a"}, {ID: "b"}},
		map[string]time.Time{"a": time.Unix(200, 0), "b": time.Unix(100, 0)},
	)
	// Cursor on "b" (index 1).
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if sel, _ := p.Selected(); sel.ID != "b" {
		t.Fatalf("precondition: selected = %q; want b", sel.ID)
	}
	// Add newer bookmark — goes to index 0, "b" shifts to index 2.
	p.Add(session.Meta{ID: "c"}, time.Unix(300, 0))
	if sel, _ := p.Selected(); sel.ID != "b" {
		t.Errorf("after Add: selected = %q; want b", sel.ID)
	}
}

func TestBookmarksPane_RemoveDropsByID(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	p.SetItems(
		[]session.Meta{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		map[string]time.Time{
			"a": time.Unix(300, 0), "b": time.Unix(200, 0), "c": time.Unix(100, 0),
		},
	)
	p.Remove("b")
	got := p.Items()
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Errorf("after Remove(b) = %+v; want [a, c]", idsOf(got))
	}
	if _, ok := p.IDIndex()["b"]; ok {
		t.Error("idIndex still has 'b' after Remove")
	}
}

func TestBookmarksPane_RemoveAdjustsCursor(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	p.SetItems(
		[]session.Meta{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		map[string]time.Time{
			"a": time.Unix(300, 0), "b": time.Unix(200, 0), "c": time.Unix(100, 0),
		},
	)
	// Cursor on "c" (index 2).
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	// Remove "a" — cursor should move to index 1 (still "c").
	p.Remove("a")
	if sel, _ := p.Selected(); sel.ID != "c" {
		t.Errorf("selected after Remove(a) = %q; want c", sel.ID)
	}
}

func TestBookmarksPane_ReplaceItemUpdatesInPlace(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	p.SetItems(
		[]session.Meta{{ID: "a"}},
		map[string]time.Time{"a": time.Unix(100, 0)},
	)
	p.ReplaceItem(session.Meta{ID: "a", FirstPrompt: "updated", Enriched: true})
	got := p.Items()
	if got[0].FirstPrompt != "updated" {
		t.Errorf("FirstPrompt = %q; want updated", got[0].FirstPrompt)
	}
}

func idsOf(metas []session.Meta) []string {
	out := make([]string, len(metas))
	for i, m := range metas {
		out[i] = m.ID
	}
	return out
}

func TestBookmarksPane_MissingFileRendersRemovedMarker(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	p.SetItems(
		[]session.Meta{{ID: "gone", LoadErr: session.ErrMissing}},
		map[string]time.Time{"gone": time.Unix(100, 0)},
	)
	v := p.View()
	if !strings.Contains(v, "(removed)") {
		t.Errorf("missing-file row should show (removed): %q", v)
	}
}

// TestBookmarksPane_SetFocusedTogglesState verifies the focus state
// getter/setter pair. The rendered cursor style in View differs by
// focus, but lipgloss strips ANSI in the test environment so we verify
// the state plumbing here; the App-level test confirms SetFocused is
// called on Ctrl-J/Ctrl-K.
func TestBookmarksPane_SetFocusedTogglesState(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	if p.Focused() {
		t.Error("bookmarks pane should default to unfocused")
	}
	p.SetFocused(true)
	if !p.Focused() {
		t.Error("SetFocused(true) not reflected by Focused()")
	}
	p.SetFocused(false)
	if p.Focused() {
		t.Error("SetFocused(false) not reflected by Focused()")
	}
}

func TestBookmarksPane_CtrlVPagesDown(t *testing.T) {
	p := NewBookmarksPane(80, 10) // pageStep = 8
	items := make([]session.Meta, 20)
	sa := map[string]time.Time{}
	for i := range items {
		id := fmt.Sprintf("id%02d", i)
		items[i] = session.Meta{ID: id}
		sa[id] = time.Unix(int64(1000-i), 0) // id00 newest
	}
	p.SetItems(items, sa)

	p.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if p.Cursor() != 8 {
		t.Errorf("cursor after ctrl+v = %d; want 8", p.Cursor())
	}
	p.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	p.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if p.Cursor() != 19 {
		t.Errorf("cursor at bottom should clamp to len-1; got %d, want 19", p.Cursor())
	}
}

func TestBookmarksPane_AltVPagesUp(t *testing.T) {
	p := NewBookmarksPane(80, 10) // pageStep = 8
	items := make([]session.Meta, 20)
	sa := map[string]time.Time{}
	for i := range items {
		id := fmt.Sprintf("id%02d", i)
		items[i] = session.Meta{ID: id}
		sa[id] = time.Unix(int64(1000-i), 0)
	}
	p.SetItems(items, sa)
	p.Update(tea.KeyMsg{Type: tea.KeyEnd}) // cursor = 19

	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if p.Cursor() != 11 {
		t.Errorf("cursor after alt+v from bottom = %d; want 11", p.Cursor())
	}
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if p.Cursor() != 0 {
		t.Errorf("cursor at top should clamp to 0; got %d", p.Cursor())
	}
}

func TestBookmarksPane_CtrlVOnEmptyPane(t *testing.T) {
	p := NewBookmarksPane(80, 10)
	// No items.
	p.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if p.Cursor() != 0 {
		t.Errorf("ctrl+v on empty pane = %d; want 0", p.Cursor())
	}
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if p.Cursor() != 0 {
		t.Errorf("alt+v on empty pane = %d; want 0", p.Cursor())
	}
}
