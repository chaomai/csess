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
	previewHeaderKey = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	previewErrStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	previewRoleUser  = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	previewRoleAsst  = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true)
	previewSep       = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
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
	if cut > 0 {
		b.WriteString(previewSep.Render(fmt.Sprintf("… %d earlier turns hidden, press 'a' to show all\n", cut)))
	}
	for _, t := range turns {
		b.WriteString(p.renderTurn(t))
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
	return head + "\n" + t.Text
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
