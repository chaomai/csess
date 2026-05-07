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
