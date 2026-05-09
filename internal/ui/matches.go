package ui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"csess/internal/search"
)

// MatchList displays rg search results. It mirrors List in structure: same
// cursor/scroll keys, same firstVisible windowing, same style variables.
type MatchList struct {
	width, height int
	items         []search.Match
	cursor        int
	firstVisible  int
}

func NewMatchList(width, height int) *MatchList {
	return &MatchList{width: width, height: height}
}

func (ml *MatchList) SetItems(items []search.Match) {
	ml.items = items
	sort.SliceStable(ml.items, func(i, j int) bool {
		ti, tj := ml.items[i].SortTime, ml.items[j].SortTime
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return ml.items[i].LineNo < ml.items[j].LineNo
	})
	if ml.cursor >= len(ml.items) {
		ml.cursor = maxInt(0, len(ml.items)-1)
	}
	ml.firstVisible = 0
	ml.ensureVisible()
}

func (ml *MatchList) Items() []search.Match { return ml.items }
func (ml *MatchList) Cursor() int           { return ml.cursor }

func (ml *MatchList) Selected() (search.Match, bool) {
	if ml.cursor < 0 || ml.cursor >= len(ml.items) {
		return search.Match{}, false
	}
	return ml.items[ml.cursor], true
}

func (ml *MatchList) SetSize(w, h int) {
	ml.width, ml.height = w, h
	ml.ensureVisible()
}

// Update handles j/k/↑/↓/ctrl+p/ctrl+n/g/G/home/end navigation.
func (ml *MatchList) Update(msg tea.Msg) (*MatchList, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "j", "down", "ctrl+n":
			if ml.cursor < len(ml.items)-1 {
				ml.cursor++
			}
		case "k", "up", "ctrl+p":
			if ml.cursor > 0 {
				ml.cursor--
			}
		case "g", "home":
			ml.cursor = 0
		case "G", "end":
			ml.cursor = maxInt(0, len(ml.items)-1)
		}
		ml.ensureVisible()
	}
	return ml, nil
}

func (ml *MatchList) View() string {
	if len(ml.items) == 0 {
		// Pad to pane width so JoinHorizontal in the parent doesn't
		// collapse the left column to the length of "no matches".
		return lipgloss.NewStyle().Width(ml.width).Render(listDimStyle.Render("no matches"))
	}
	height := ml.height
	if height <= 0 {
		height = 1
	}
	end := min(ml.firstVisible+height, len(ml.items))
	visible := ml.items[ml.firstVisible:end]

	var b strings.Builder
	rowWidth := ml.width - 2 // account for "▶ " or "  " prefix
	if rowWidth < 10 {
		rowWidth = 10
	}
	for i, m := range visible {
		globalIdx := ml.firstVisible + i
		row := ansi.Truncate(ml.row(m), rowWidth, "…")
		if globalIdx == ml.cursor {
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

// row formats one match as: "3d  02c753f2  …pre<match>post…"
// The match substring is highlighted. The raw jsonl line number is not
// shown because the preview pane renders a pretty-printed view where that
// number is meaningless; Match.LineNo is still used internally by preview
// to locate and mark the corresponding turn.
func (ml *MatchList) row(m search.Match) string {
	tRel := humanDelta(m.SortTime)
	id := m.SessionID
	if len(id) > 8 {
		id = id[:8]
	}
	return fmt.Sprintf("%3s  %-8s  %s", tRel, id, matchSnippet(m))
}

// matchSnippetBeforeRunes is how many runes of the raw jsonl line we keep
// immediately before the match. The remainder of the snippet is the match
// itself (highlighted) plus whatever follows — right-side truncation is
// handled by the caller's width budget.
const matchSnippetBeforeRunes = 20

var matchHighlightStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("3")). // yellow
	Bold(true)

// matchSnippet builds a one-line excerpt of m.Line centered on the match
// position reported by rg. Up to matchSnippetBeforeRunes runes precede
// the match (with an ellipsis if we trimmed); the match itself is wrapped
// in matchHighlightStyle.
func matchSnippet(m search.Match) string {
	line := m.Line
	start, end := m.MatchStart, m.MatchEnd
	if start < 0 || end < start || end > len(line) {
		return flattenPrompt(line)
	}
	pre := line[:start]
	mid := line[start:end]
	post := line[end:]

	prefix := ""
	if runes := []rune(pre); len(runes) > matchSnippetBeforeRunes {
		pre = string(runes[len(runes)-matchSnippetBeforeRunes:])
		prefix = "…"
	}
	return prefix + pre + matchHighlightStyle.Render(mid) + post
}

func (ml *MatchList) ensureVisible() {
	height := ml.height
	if height <= 0 {
		height = 1
	}
	if ml.cursor < ml.firstVisible {
		ml.firstVisible = ml.cursor
	}
	if ml.cursor >= ml.firstVisible+height {
		ml.firstVisible = ml.cursor - height + 1
	}
	if ml.firstVisible < 0 {
		ml.firstVisible = 0
	}
	maxFirst := len(ml.items) - height
	if maxFirst < 0 {
		maxFirst = 0
	}
	if ml.firstVisible > maxFirst {
		ml.firstVisible = maxFirst
	}
}
