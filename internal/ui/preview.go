package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"csess/internal/session"
)

const viewportTurnCap = 5000

var (
	// ANSI palette colors (0-15) let the terminal theme decide the actual
	// shade — dayfox, nord, solarized, etc. all render correctly.
	previewHeaderKey = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // bright black / dim
	previewErrStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true) // red
	previewRoleUser  = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true) // blue
	previewRoleAsst  = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true) // magenta
	previewSep       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))            // dim
)

type Preview struct {
	width, height int
	meta          session.Meta
	turns         []session.Turn
	expanded      bool
	vp            viewport.Model
}

func NewPreview(width, height int) *Preview {
	vp := viewport.New(width, height)
	return &Preview{width: width, height: height, vp: vp}
}

func (p *Preview) SetSize(w, h int) {
	p.width, p.height = w, h
	p.vp.Width = w
	p.vp.Height = h
	p.reflow()
}

// SetMeta resets the preview to a new session. Turns are cleared.
func (p *Preview) SetMeta(m session.Meta) {
	p.meta = m
	p.turns = nil
	p.expanded = false
	p.reflow()
	p.vp.GotoTop()
}

// UpdateMeta refreshes the header fields for the currently displayed
// session without clearing already-streamed turns. Use when an
// enrichment update arrives for the selected session.
func (p *Preview) UpdateMeta(m session.Meta) {
	p.meta = m
	p.reflow()
}

func (p *Preview) AddTurn(t session.Turn) {
	p.turns = append(p.turns, t)
	p.reflow()
}

func (p *Preview) ToggleExpanded() {
	p.expanded = !p.expanded
	p.reflow()
}

func (p *Preview) Update(msg tea.Msg) (*Preview, tea.Cmd) {
	var cmd tea.Cmd
	p.vp, cmd = p.vp.Update(msg)
	return p, cmd
}

func (p *Preview) View() string { return p.vp.View() }

func (p *Preview) reflow() {
	var b strings.Builder
	b.WriteString(p.header())
	b.WriteString("\n")
	b.WriteString(previewSep.Render(strings.Repeat("─", maxInt(10, p.width-2))))
	b.WriteString("\n")
	b.WriteString(p.body())
	p.vp.SetContent(b.String())
}

func (p *Preview) header() string {
	m := p.meta
	lines := []string{
		fmt.Sprintf("%s %s", previewHeaderKey.Render("session"), m.ID),
		fmt.Sprintf("%s %s", previewHeaderKey.Render("cwd    "), emptyDash(m.CWD)),
		fmt.Sprintf("%s %s @ %s", previewHeaderKey.Render("branch "), emptyDash(m.GitBranch), emptyDash(m.Model)),
		fmt.Sprintf("%s %s  %s", previewHeaderKey.Render("started"), fmtTime(m.StartedAt), previewHeaderKey.Render(fmt.Sprintf("updated %s", fmtTime(m.UpdatedAt)))),
		fmt.Sprintf("%s %d user · %d assistant  (%d bytes, v%s)",
			previewHeaderKey.Render("msgs   "), m.UserMsgs, m.AsstMsgs, m.SizeBytes, emptyDash(m.Version)),
	}
	var pe *session.PartialError
	if errors.As(m.LoadErr, &pe) {
		lines = append(lines, previewErrStyle.Render(fmt.Sprintf("⚠ %d bad lines skipped", pe.BadLines)))
	} else if m.LoadErr != nil {
		lines = append(lines, previewErrStyle.Render("corrupt: "+m.LoadErr.Error()))
	}
	return strings.Join(lines, "\n")
}

func (p *Preview) body() string {
	turns := p.turns
	cut := 0
	if !p.expanded && len(turns) > viewportTurnCap {
		cut = len(turns) - viewportTurnCap
		turns = turns[cut:]
	}
	var b strings.Builder
	// Render newest turn first so the most recent exchange is visible
	// without scrolling. Oldest turns appear at the bottom.
	for i := len(turns) - 1; i >= 0; i-- {
		b.WriteString(p.renderTurn(turns[i]))
		b.WriteString("\n")
	}
	if cut > 0 {
		b.WriteString(previewSep.Render(fmt.Sprintf("… %d earlier turns hidden, press 'a' to show all", cut)))
		b.WriteString("\n")
	}
	return b.String()
}

func (p *Preview) renderTurn(t session.Turn) string {
	var head string
	switch t.Role {
	case "user":
		head = previewRoleUser.Render(fmt.Sprintf("▸ %s  user", fmtTime(t.Timestamp)))
	case "assistant":
		head = previewRoleAsst.Render(fmt.Sprintf("▸ %s  assistant", fmtTime(t.Timestamp)))
	default:
		head = t.Role
	}
	return head + "\n" + unescapeLiterals(t.Text)
}

// unescapeLiterals expands literal "\n", "\t", "\r" escape sequences
// (two characters: backslash + letter) into their actual whitespace. Real
// newlines and tabs already present in the source text are left alone.
// Also unescapes \" and \\ so embedded quotes read naturally.
func unescapeLiterals(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case 't':
				b.WriteByte('\t')
				i++
				continue
			case 'r':
				b.WriteByte('\r')
				i++
				continue
			case '"':
				b.WriteByte('"')
				i++
				continue
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func emptyDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func fmtTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02 15:04:05")
}
