// Package ui contains the bubbletea models that make up the csess TUI.
package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"csess/internal/session"
)

var (
	listCursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	listDimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

// List is the left-pane session list. It is a pure model; selection
// movement is driven by the parent via Update.
type List struct {
	width, height int
	allMode       bool
	items         []session.Meta
	cursor        int
	firstVisible  int
}

func NewList(width, height int, allMode bool) *List {
	return &List{width: width, height: height, allMode: allMode}
}

func (l *List) SetItems(items []session.Meta) {
	l.items = items
	l.resort()
	if l.cursor >= len(l.items) {
		l.cursor = maxInt(0, len(l.items)-1)
	}
	l.firstVisible = 0
	l.ensureVisible()
}

func (l *List) ReplaceItem(m session.Meta) {
	for i, it := range l.items {
		if it.ID == m.ID {
			l.items[i] = m
			l.resort()
			return
		}
	}
}

func (l *List) Items() []session.Meta { return l.items }
func (l *List) Cursor() int           { return l.cursor }
func (l *List) Selected() (session.Meta, bool) {
	if l.cursor < 0 || l.cursor >= len(l.items) {
		return session.Meta{}, false
	}
	return l.items[l.cursor], true
}

func (l *List) resort() {
	sort.SliceStable(l.items, func(i, j int) bool {
		ti, tj := sortTime(l.items[i]), sortTime(l.items[j])
		return ti.After(tj)
	})
}

func sortTime(m session.Meta) time.Time {
	if !m.UpdatedAt.IsZero() {
		return m.UpdatedAt
	}
	return m.ModTime
}

func (l *List) SetSize(w, h int) {
	l.width, l.height = w, h
	l.ensureVisible()
}

// Update handles j/k movement only; parent forwards only relevant keys.
func (l *List) Update(msg tea.Msg) (*List, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "j", "down":
			if l.cursor < len(l.items)-1 {
				l.cursor++
			}
		case "k", "up":
			if l.cursor > 0 {
				l.cursor--
			}
		case "g", "home":
			l.cursor = 0
		case "G", "end":
			l.cursor = len(l.items) - 1
		}
		l.ensureVisible()
	}
	return l, nil
}

func (l *List) View() string {
	if len(l.items) == 0 {
		return listDimStyle.Render("no sessions")
	}
	height := l.height
	if height <= 0 {
		height = 1
	}
	end := min(l.firstVisible+height, len(l.items))
	visible := l.items[l.firstVisible:end]

	var b strings.Builder
	for i, it := range visible {
		globalIdx := l.firstVisible + i
		row := l.row(it)
		if globalIdx == l.cursor {
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

func (l *List) row(m session.Meta) string {
	tRel := humanDelta(sortTime(m))
	id := m.ID
	if len(id) > 8 {
		id = id[:8]
	}
	prompt := m.FirstPrompt
	if prompt == "" {
		prompt = listDimStyle.Render("(not loaded)")
	}
	if l.allMode {
		proj := "-"
		if m.CWD != "" {
			proj = filepath.Base(m.CWD)
		}
		return fmt.Sprintf("%s  %s  %-12s  %s", tRel, id, proj, prompt)
	}
	return fmt.Sprintf("%s  %s  %s", tRel, id, prompt)
}

func humanDelta(t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ensureVisible adjusts firstVisible so cursor stays within the visible window.
func (l *List) ensureVisible() {
	height := l.height
	if height <= 0 {
		height = 1
	}
	if l.cursor < l.firstVisible {
		l.firstVisible = l.cursor
	}
	if l.cursor >= l.firstVisible+height {
		l.firstVisible = l.cursor - height + 1
	}
	// Clamp firstVisible to valid range.
	if l.firstVisible < 0 {
		l.firstVisible = 0
	}
	maxFirst := len(l.items) - height
	if maxFirst < 0 {
		maxFirst = 0
	}
	if l.firstVisible > maxFirst {
		l.firstVisible = maxFirst
	}
}
