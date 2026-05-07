// BookmarksPane is the persisted "bookmarked sessions" row rendered
// above the list/preview split. Mirrors List in shape so the
// key-routing code in App can treat both uniformly.
package ui

import (
	"github.com/charmbracelet/lipgloss"

	"csess/internal/session"
)

// BookmarksPane is a pure model; App drives selection and mutation.
type BookmarksPane struct {
	width, height int
	items         []session.Meta
	starredAt     map[string]int64 // id -> unix nanos, for sort
	idIndex       map[string]int
	cursor        int
	firstVisible  int
}

func NewBookmarksPane(w, h int) *BookmarksPane {
	return &BookmarksPane{
		width:     w,
		height:    h,
		starredAt: map[string]int64{},
		idIndex:   map[string]int{},
	}
}

func (p *BookmarksPane) View() string {
	if len(p.items) == 0 {
		return lipgloss.NewStyle().Width(p.width).Render(listDimStyle.Render("no bookmarks — press b to add"))
	}
	return ""
}
