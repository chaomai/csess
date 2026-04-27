package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"csess/internal/session"
)

// --- Messages ---

type ScanMsg struct {
	Metas []session.Meta
}

type EnrichMsg struct {
	Meta session.Meta
}

type TurnMsg struct {
	Seq  int
	Turn session.Turn
}

type TurnErrMsg struct {
	Seq int
	Err error
}

type BannerMsg struct {
	Text    string
	IsError bool
	Until   time.Time
}

type ResumeDoneMsg struct {
	Err error
}

type DeleteDoneMsg struct {
	ID  string
	Err error
}

// BatchTurnsMsg carries a full transcript load result back to App.Update.
type BatchTurnsMsg struct {
	Seq   int
	Turns []session.Turn
}

// --- Modes ---

type mode int

const (
	modeNormal mode = iota
	modeSearch
	modeConfirm
)

// --- Config ---

type AppConfig struct {
	Width, Height int
	AllMode       bool
	Scope         string

	// Optional command providers — left nil in tests.
	LoadTranscript func(seq int, m session.Meta, ctx context.Context) tea.Cmd
	ResumeSelected func(m session.Meta) tea.Cmd
	CopySelected   func(m session.Meta) tea.Cmd
	TrashSelected  func(m session.Meta) tea.Cmd
}

// --- App model ---

type App struct {
	cfg       AppConfig
	mode      mode
	list      *List
	preview   *Preview
	search    *SearchBar
	confirm   *Confirm
	allItems  []session.Meta
	banner    string
	bannerExp time.Time

	transcriptSeq  int
	transcriptCtx  context.Context
	transcriptStop context.CancelFunc
}

func NewApp(cfg AppConfig) *App {
	listW, prevW := splitWidth(cfg.Width)
	list := NewList(listW, cfg.Height-2, cfg.AllMode)
	preview := NewPreview(prevW, cfg.Height-2)
	search := NewSearchBar()
	app := &App{cfg: cfg, list: list, preview: preview, search: search}
	app.transcriptCtx, app.transcriptStop = context.WithCancel(context.Background())
	return app
}

func splitWidth(total int) (list, preview int) {
	list = total / 3
	if list < 32 {
		list = 32
	}
	preview = total - list - 1
	if preview < 32 {
		preview = 32
	}
	return
}

// Init is a no-op; the caller dispatches the initial ScanMsg cmd.
func (a *App) Init() tea.Cmd { return nil }

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.cfg.Width, a.cfg.Height = m.Width, m.Height
		listW, prevW := splitWidth(m.Width)
		a.list.SetSize(listW, m.Height-2)
		a.preview.SetSize(prevW, m.Height-2)
		return a, nil

	case ScanMsg:
		a.allItems = m.Metas
		a.list.SetItems(m.Metas)
		a.updatePreviewFromSelection()
		return a, a.loadTranscriptForSelection()

	case EnrichMsg:
		// Update allItems too so search still works.
		for i, it := range a.allItems {
			if it.ID == m.Meta.ID {
				a.allItems[i] = m.Meta
			}
		}
		a.list.ReplaceItem(m.Meta)
		if sel, ok := a.list.Selected(); ok && sel.ID == m.Meta.ID {
			a.preview.UpdateMeta(m.Meta)
		}
		return a, nil

	case TurnMsg:
		if m.Seq != a.transcriptSeq {
			return a, nil
		}
		a.preview.AddTurn(m.Turn)
		return a, nil

	case BatchTurnsMsg:
		if m.Seq != a.transcriptSeq {
			return a, nil
		}
		for _, t := range m.Turns {
			a.preview.AddTurn(t)
		}
		return a, nil

	case BannerMsg:
		a.banner = m.Text
		if !m.Until.IsZero() {
			a.bannerExp = m.Until
		} else {
			a.bannerExp = time.Now().Add(3 * time.Second)
		}
		return a, nil

	case ResumeDoneMsg:
		if m.Err != nil {
			a.banner = "resume failed: " + m.Err.Error()
			a.bannerExp = time.Now().Add(5 * time.Second)
		}
		return a, nil

	case DeleteDoneMsg:
		if m.Err != nil {
			a.banner = "delete failed: " + m.Err.Error()
			a.bannerExp = time.Now().Add(5 * time.Second)
			return a, nil
		}
		// Remove from list + allItems
		var keep []session.Meta
		for _, it := range a.allItems {
			if it.ID != m.ID {
				keep = append(keep, it)
			}
		}
		a.allItems = keep
		a.applyFilter()
		a.updatePreviewFromSelection()
		a.banner = "deleted " + m.ID
		a.bannerExp = time.Now().Add(3 * time.Second)
		return a, a.loadTranscriptForSelection()

	case tea.KeyMsg:
		return a.handleKey(m)
	}
	return a, nil
}

func (a *App) handleKey(km tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch a.mode {
	case modeSearch:
		switch km.String() {
		case "esc":
			a.mode = modeNormal
			a.search.Blur()
			a.search.Reset()
			a.applyFilter()
			return a, nil
		case "enter":
			a.mode = modeNormal
			a.search.Blur()
			return a, nil
		}
		_, cmd := a.search.Update(km)
		a.applyFilter()
		return a, cmd

	case modeConfirm:
		_, _ = a.confirm.Update(km)
		if !a.confirm.Done() {
			return a, nil
		}
		confirmed := a.confirm.Confirmed()
		a.confirm = nil
		a.mode = modeNormal
		if !confirmed {
			return a, nil
		}
		sel, ok := a.list.Selected()
		if !ok {
			return a, nil
		}
		if a.cfg.TrashSelected != nil {
			return a, a.cfg.TrashSelected(sel)
		}
		return a, nil
	}

	// modeNormal
	switch km.String() {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "/":
		a.mode = modeSearch
		return a, a.search.Focus()
	case "d":
		sel, ok := a.list.Selected()
		if !ok {
			return a, nil
		}
		a.confirm = NewConfirm(fmt.Sprintf("delete session %s?", sel.ID))
		a.mode = modeConfirm
		return a, nil
	case "y":
		sel, ok := a.list.Selected()
		if !ok {
			return a, nil
		}
		if a.cfg.CopySelected != nil {
			return a, a.cfg.CopySelected(sel)
		}
		return a, nil
	case "enter":
		sel, ok := a.list.Selected()
		if !ok {
			return a, nil
		}
		if !sel.Enriched || sel.CWD == "" {
			a.banner = "session not ready (enrichment pending)"
			a.bannerExp = time.Now().Add(3 * time.Second)
			return a, nil
		}
		if a.cfg.ResumeSelected != nil {
			return a, a.cfg.ResumeSelected(sel)
		}
		return a, nil
	case "a":
		a.preview.ToggleExpanded()
		return a, nil
	case "j", "down", "k", "up", "g", "G", "home", "end":
		a.list.Update(km)
		a.updatePreviewFromSelection()
		return a, a.loadTranscriptForSelection()
	case "pgup", "pgdown":
		_, cmd := a.preview.Update(km)
		return a, cmd
	}
	return a, nil
}

func (a *App) applyFilter() {
	q := a.search.Query()
	a.list.SetItems(FilterMetas(a.allItems, q))
	a.updatePreviewFromSelection()
}

func (a *App) updatePreviewFromSelection() {
	sel, ok := a.list.Selected()
	if !ok {
		a.preview.SetMeta(session.Meta{})
		return
	}
	a.preview.SetMeta(sel)
}

func (a *App) loadTranscriptForSelection() tea.Cmd {
	sel, ok := a.list.Selected()
	if !ok || a.cfg.LoadTranscript == nil {
		return nil
	}
	a.transcriptStop()
	a.transcriptSeq++
	a.transcriptCtx, a.transcriptStop = context.WithCancel(context.Background())
	return a.cfg.LoadTranscript(a.transcriptSeq, sel, a.transcriptCtx)
}

// --- View ---

func (a *App) View() string {
	left := a.list.View()
	right := a.preview.View()
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, lipgloss.NewStyle().Padding(0, 1).Render("│"), right)

	status := a.statusLine()
	return body + "\n" + status
}

func (a *App) statusLine() string {
	var parts []string
	switch a.mode {
	case modeSearch:
		parts = append(parts, a.search.View())
	case modeConfirm:
		if a.confirm != nil {
			parts = append(parts, a.confirm.View())
		}
	default:
		n := len(a.list.Items())
		parts = append(parts, fmt.Sprintf("%d sessions", n))
		parts = append(parts, "[/] filter  [Enter] resume  [y] copy id  [d] delete  [q] quit")
	}
	if a.banner != "" && time.Now().Before(a.bannerExp) {
		parts = append(parts, "· "+a.banner)
	}
	return strings.Join(parts, "   ")
}
