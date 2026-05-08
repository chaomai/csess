// BookmarksPane is the persisted "bookmarked sessions" row rendered
// above the list/preview split. Mirrors List in shape so the
// key-routing code in App can treat both uniformly.
package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"csess/internal/session"
)

// BookmarksPane is a pure model; App drives selection and mutation.
type BookmarksPane struct {
	width, height int
	items         []session.Meta
	starredAt     map[string]time.Time
	idIndex       map[string]int
	cursor        int
	firstVisible  int
}

func NewBookmarksPane(w, h int) *BookmarksPane {
	return &BookmarksPane{
		width:     w,
		height:    h,
		starredAt: map[string]time.Time{},
		idIndex:   map[string]int{},
	}
}

// SetItems replaces the pane's contents. items and starredAt must
// cover the same set of ids; items need not be pre-sorted (this
// function sorts them by starredAt desc).
func (p *BookmarksPane) SetItems(items []session.Meta, starredAt map[string]time.Time) {
	p.items = append(p.items[:0], items...)
	p.starredAt = map[string]time.Time{}
	for k, v := range starredAt {
		p.starredAt[k] = v
	}
	p.sortItems()
	p.rebuildIndex()
	if p.cursor >= len(p.items) {
		p.cursor = maxInt(0, len(p.items)-1)
	}
	p.firstVisible = 0
	p.ensureVisible()
}

func (p *BookmarksPane) Items() []session.Meta { return p.items }
func (p *BookmarksPane) Cursor() int           { return p.cursor }

func (p *BookmarksPane) Selected() (session.Meta, bool) {
	if p.cursor < 0 || p.cursor >= len(p.items) {
		return session.Meta{}, false
	}
	return p.items[p.cursor], true
}

func (p *BookmarksPane) SetSize(w, h int) {
	p.width, p.height = w, h
	p.ensureVisible()
}

// DesiredHeight is what the pane would like to be rendered at; App
// passes the cap it's willing to allow.
func (p *BookmarksPane) DesiredHeight(maxRows int) int {
	if len(p.items) == 0 {
		return 1
	}
	if len(p.items) < maxRows {
		return len(p.items)
	}
	return maxRows
}

func (p *BookmarksPane) Update(msg tea.Msg) (*BookmarksPane, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "j", "down", "ctrl+n":
			if p.cursor < len(p.items)-1 {
				p.cursor++
			}
		case "k", "up", "ctrl+p":
			if p.cursor > 0 {
				p.cursor--
			}
		case "g", "home":
			p.cursor = 0
		case "G", "end":
			p.cursor = maxInt(0, len(p.items)-1)
		}
		p.ensureVisible()
	}
	return p, nil
}

func (p *BookmarksPane) View() string {
	if len(p.items) == 0 {
		return lipgloss.NewStyle().Width(p.width).Render(
			listDimStyle.Render("no bookmarks — press b to add"),
		)
	}
	height := p.height
	if height <= 0 {
		height = 1
	}
	end := min(p.firstVisible+height, len(p.items))
	visible := p.items[p.firstVisible:end]

	var b strings.Builder
	rowWidth := p.width - 2 // account for "▶ " or "  " prefix
	if rowWidth < 10 {
		rowWidth = 10
	}
	for i, m := range visible {
		globalIdx := p.firstVisible + i
		row := ansi.Truncate(p.row(m), rowWidth, "…")
		if globalIdx == p.cursor {
			b.WriteString(listCursorStyle.Render("▶ " + row))
		} else {
			b.WriteString("  " + row)
		}
		if i < len(visible)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (p *BookmarksPane) row(m session.Meta) string {
	tRel := humanDelta(p.starredAt[m.ID])
	id := m.ID
	if len(id) > 8 {
		id = id[:8]
	}
	proj := "-"
	if m.CWD != "" {
		proj = filepath.Base(m.CWD)
	}
	if len(proj) > 14 {
		proj = proj[:13] + "…"
	}
	prompt := flattenPrompt(m.FirstPrompt)
	if prompt == "" {
		prompt = listDimStyle.Render("(not loaded)")
	}
	return fmt.Sprintf("%3s  %-8s  %-14s  %s", tRel, id, proj, prompt)
}

func (p *BookmarksPane) sortItems() {
	sort.SliceStable(p.items, func(i, j int) bool {
		return p.starredAt[p.items[i].ID].After(p.starredAt[p.items[j].ID])
	})
}

func (p *BookmarksPane) rebuildIndex() {
	p.idIndex = make(map[string]int, len(p.items))
	for i, m := range p.items {
		p.idIndex[m.ID] = i
	}
}

// Add inserts or replaces a bookmark entry. Cursor tracks the selected
// session by ID across the resulting re-sort.
func (p *BookmarksPane) Add(m session.Meta, starredAt time.Time) {
	selID := ""
	if sel, ok := p.Selected(); ok {
		selID = sel.ID
	}
	if i, exists := p.idIndex[m.ID]; exists {
		p.items[i] = m
	} else {
		p.items = append(p.items, m)
	}
	p.starredAt[m.ID] = starredAt
	p.sortItems()
	p.rebuildIndex()
	if i, ok := p.idIndex[selID]; ok {
		p.cursor = i
	}
	p.ensureVisible()
}

// Remove drops the entry by ID. Cursor stays on the same session (if
// still present) or clamps into the remaining range.
func (p *BookmarksPane) Remove(id string) {
	i, ok := p.idIndex[id]
	if !ok {
		return
	}
	selID := ""
	if sel, ok := p.Selected(); ok && sel.ID != id {
		selID = sel.ID
	}
	p.items = append(p.items[:i], p.items[i+1:]...)
	delete(p.starredAt, id)
	p.rebuildIndex()
	if selID != "" {
		if j, ok := p.idIndex[selID]; ok {
			p.cursor = j
		}
	} else if p.cursor >= len(p.items) {
		p.cursor = maxInt(0, len(p.items)-1)
	}
	p.ensureVisible()
}

// ReplaceItem updates the Meta for an existing bookmarked ID in place
// without changing its position. Used by BookmarkEnrichMsg.
func (p *BookmarksPane) ReplaceItem(m session.Meta) {
	if i, ok := p.idIndex[m.ID]; ok && i < len(p.items) {
		p.items[i] = m
	}
}

// IDIndex exposes the id→index map for tests. Callers must not mutate.
func (p *BookmarksPane) IDIndex() map[string]int { return p.idIndex }

func (p *BookmarksPane) ensureVisible() {
	height := p.height
	if height <= 0 {
		height = 1
	}
	if p.cursor < p.firstVisible {
		p.firstVisible = p.cursor
	}
	if p.cursor >= p.firstVisible+height {
		p.firstVisible = p.cursor - height + 1
	}
	if p.firstVisible < 0 {
		p.firstVisible = 0
	}
	maxFirst := len(p.items) - height
	if maxFirst < 0 {
		maxFirst = 0
	}
	if p.firstVisible > maxFirst {
		p.firstVisible = maxFirst
	}
}
