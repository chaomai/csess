package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"csess/internal/search"
	"csess/internal/session"
)

const viewportTurnCap = 5000

var (
	// ANSI palette colors (0-15) let the terminal theme decide the actual
	// shade — dayfox, nord, solarized, etc. all render correctly.
	previewHeaderKey  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))                 // bright black / dim
	previewErrStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)      // red
	previewRoleUser   = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)      // blue
	previewRoleAsst   = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)      // magenta
	previewSep        = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))                 // dim
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

// AddTurns appends many turns and reflows once. Use for batched delivery
// (e.g. BatchTurnsMsg) to avoid O(n^2) reflow cost on large sessions.
func (p *Preview) AddTurns(ts []session.Turn) {
	if len(ts) == 0 {
		return
	}
	p.turns = append(p.turns, ts...)
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
		b.WriteString(previewSep.Render(fmt.Sprintf("… %d earlier turns hidden, press 'a' to show all", cut)))
		b.WriteString("\n")
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
	body := unescapeLiterals(t.Text)
	if p.width > 0 {
		// Wrap each line to pane width so long paragraphs (and CJK) don't
		// bleed into the list pane on the left.
		body = ansi.Wordwrap(body, p.width, " ,.-、。，")
	}
	return head + "\n" + body
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

// SetExpandedMatch switches the preview to full-transcript mode for the
// session of match, with a ▶▶▶ marker on the turn whose LineNo matches
// match.LineNo. The viewport scrolls so that turn sits in the upper third
// of the pane, with a few earlier turns visible above for context.
func (p *Preview) SetExpandedMatch(match search.Match, turns []session.Turn) {
	p.turns = turns
	p.expanded = true

	// Build the content. Body renders oldest-first.
	var b strings.Builder
	b.WriteString(p.header())
	b.WriteString("\n")
	b.WriteString(previewSep.Render(strings.Repeat("─", maxInt(10, p.width-2))))
	b.WriteString("\n")
	b.WriteString(p.bodyExpanded(match.LineNo))

	p.vp.SetContent(b.String())

	// Compute scroll offset: header lines + separator + lines rendered for
	// turns that appear before the target turn.
	headerLines := strings.Count(p.header(), "\n") + 2 // +1 for the header itself, +1 for sep
	offset := headerLines

	// Turns render oldest-first: turns[0], turns[1], ..., turns[n-1].
	// We accumulate line counts until we hit the target turn.
	found := false
	for _, t := range turns {
		if t.LineNo == match.LineNo {
			found = true
			break
		}
		offset += p.renderedTurnHeight(t)
	}
	if found {
		// Pull the scroll target back by a third of the viewport so the
		// ▶▶▶ line sits in the upper-middle, with context above.
		scroll := offset - p.vp.Height/3
		if scroll < 0 {
			scroll = 0
		}
		p.vp.SetYOffset(scroll)
	} else {
		p.vp.GotoTop()
	}
}

// bodyExpanded renders turns with a ▶▶▶ prefix on the turn matching targetLineNo.
func (p *Preview) bodyExpanded(targetLineNo int) string {
	turns := p.turns
	cut := 0
	if !p.expanded && len(turns) > viewportTurnCap {
		cut = len(turns) - viewportTurnCap
		turns = turns[cut:]
	}
	var b strings.Builder
	if cut > 0 {
		b.WriteString(previewSep.Render(fmt.Sprintf("… %d earlier turns hidden, press 'a' to show all", cut)))
		b.WriteString("\n")
	}
	for _, t := range turns {
		if t.LineNo == targetLineNo {
			b.WriteString(p.renderTurnMarked(t))
		} else {
			b.WriteString(p.renderTurn(t))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderTurnMarked renders a turn with a ▶▶▶ prefix on the head line.
func (p *Preview) renderTurnMarked(t session.Turn) string {
	var head string
	switch t.Role {
	case "user":
		head = previewRoleUser.Render(fmt.Sprintf("▶▶▶ %s  user", fmtTime(t.Timestamp)))
	case "assistant":
		head = previewRoleAsst.Render(fmt.Sprintf("▶▶▶ %s  assistant", fmtTime(t.Timestamp)))
	default:
		head = "▶▶▶ " + t.Role
	}
	body := unescapeLiterals(t.Text)
	if p.width > 0 {
		body = ansi.Wordwrap(body, p.width, " ,.-、。，")
	}
	return head + "\n" + body
}

// renderedTurnHeight returns the number of split-by-"\n" rows a turn block
// occupies in the viewport's indexed content. A block is rendered as
// `head + "\n" + body` followed by a loop-inserted "\n"; the row count for
// the next block's head equals the \n characters this block contributed,
// which is `Count(body, "\n") + 2` (1 after head, 1 from the separator).
func (p *Preview) renderedTurnHeight(t session.Turn) int {
	body := unescapeLiterals(t.Text)
	if p.width > 0 {
		body = ansi.Wordwrap(body, p.width, " ,.-、。，")
	}
	return strings.Count(body, "\n") + 2
}
