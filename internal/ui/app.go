package ui

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"csess/internal/search"
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

// searchTriggerMsg fires after the debounce timer expires.
type searchTriggerMsg struct{ query string }

// searchResultsMsg delivers rg results back to the App.
type searchResultsMsg struct {
	query   string
	matches []search.Match
	err     error
}

// --- Modes ---

type mode int

const (
	modeNormal mode = iota
	modeSearch
	modeConfirm
)

// hexPrefixRe matches queries that look like session-id prefixes.
var hexPrefixRe = regexp.MustCompile(`^[0-9a-f-]{3,}$`)

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

	// RunSearch, when non-nil, replaces the in-memory filter with rg-backed
	// full-text search. Receives the active context so callers can cancel.
	RunSearch func(ctx context.Context, query string) ([]search.Match, error)
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
	allIndex  map[string]int // ID -> allItems[index] for O(1) enrich updates
	banner    string
	bannerExp time.Time

	transcriptSeq  int
	transcriptCtx  context.Context
	transcriptStop context.CancelFunc

	// Full-text search state.
	searchMatches  []search.Match
	matchList      *MatchList
	matchExpanded  bool // true = showing full transcript with marker
	showMatches    bool // true = left pane renders matchList instead of list
	searchCancel   context.CancelFunc
	currentMatch   *search.Match // the match currently previewed (for re-show on Esc from expanded)
}

func NewApp(cfg AppConfig) *App {
	listW, prevW := splitWidth(cfg.Width)
	list := NewList(listW, cfg.Height-2, cfg.AllMode)
	preview := NewPreview(prevW, cfg.Height-2)
	search := NewSearchBar()
	matchList := NewMatchList(listW, cfg.Height-2)
	app := &App{cfg: cfg, list: list, preview: preview, search: search, matchList: matchList}
	app.transcriptCtx, app.transcriptStop = context.WithCancel(context.Background())
	app.searchCancel = func() {} // no-op until first search
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
		a.matchList.SetSize(listW, m.Height-2)
		a.preview.SetSize(prevW, m.Height-2)
		return a, nil

	case ScanMsg:
		a.allItems = m.Metas
		a.allIndex = make(map[string]int, len(m.Metas))
		for i, it := range m.Metas {
			a.allIndex[it.ID] = i
		}
		a.list.SetItems(m.Metas)
		a.updatePreviewFromSelection()
		return a, a.loadTranscriptForSelection()

	case EnrichMsg:
		// O(1) update via index; search operates on allItems so we keep
		// it in sync too.
		if i, ok := a.allIndex[m.Meta.ID]; ok && i < len(a.allItems) {
			a.allItems[i] = m.Meta
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
		if a.showMatches && a.matchExpanded && a.currentMatch != nil {
			// Expanded match view: use SetExpandedMatch to render full
			// transcript with ▶▶▶ marker on the matched turn.
			a.preview.SetExpandedMatch(*a.currentMatch, m.Turns)
		} else {
			a.preview.AddTurns(m.Turns)
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
		delete(a.allIndex, m.ID)
		for i, it := range keep {
			a.allIndex[it.ID] = i
		}
		a.applyFilter()
		a.updatePreviewFromSelection()
		a.banner = "deleted " + m.ID
		a.bannerExp = time.Now().Add(3 * time.Second)
		return a, a.loadTranscriptForSelection()

	case tea.KeyMsg:
		return a.handleKey(m)

	case searchTriggerMsg:
		return a, a.runSearch(m.query)

	case searchResultsMsg:
		// Guard against stale results from a previous query.
		if m.query != a.search.Query() {
			return a, nil
		}
		if m.err != nil {
			a.banner = "search error: " + m.err.Error()
			a.bannerExp = time.Now().Add(5 * time.Second)
			return a, nil
		}
		a.searchMatches = m.matches
		a.matchList.SetItems(m.matches)
		a.showMatches = true
		a.matchExpanded = false
		a.currentMatch = nil
		a.updatePreviewFromMatchSelection()
		return a, nil
	}
	return a, nil
}

func (a *App) handleKey(km tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch a.mode {
	case modeSearch:
		switch km.String() {
		case "esc":
			if a.showMatches && a.matchExpanded {
				// First Esc from expanded: return to context (match) view.
				a.matchExpanded = false
				a.updatePreviewFromMatchSelection()
				return a, nil
			}
			// Esc from context view or plain search: exit search entirely.
			a.mode = modeNormal
			a.search.Blur()
			a.search.Reset()
			a.showMatches = false
			a.matchExpanded = false
			a.searchMatches = nil
			a.currentMatch = nil
			a.searchCancel()
			a.applyFilter()
			return a, nil

		case "up", "down", "ctrl+p", "ctrl+n":
			if a.showMatches && !a.matchExpanded {
				// Navigate match list.
				a.matchList.Update(km)
				a.updatePreviewFromMatchSelection()
				return a, nil
			}
			if a.showMatches && a.matchExpanded {
				// In expanded view, delegate to preview scroll.
				_, cmd := a.preview.Update(km)
				return a, cmd
			}
			// Plain in-memory filter list navigation.
			a.list.Update(km)
			a.updatePreviewFromSelection()
			return a, a.loadTranscriptForSelection()

		case "enter":
			if a.showMatches {
				if a.matchExpanded {
					// Second Enter: resume the session for this match.
					sel, matchMeta := a.matchListSelectedMeta()
					if !sel {
						return a, nil
					}
					if !matchMeta.Enriched || matchMeta.CWD == "" {
						a.banner = "session not ready (enrichment pending)"
						a.bannerExp = time.Now().Add(3 * time.Second)
						return a, nil
					}
					if a.cfg.ResumeSelected != nil {
						return a, a.cfg.ResumeSelected(matchMeta)
					}
					return a, nil
				}
				// First Enter: expand to full transcript with marker.
				match, ok := a.matchList.Selected()
				if !ok {
					return a, nil
				}
				a.currentMatch = &match
				a.matchExpanded = true
				// Load transcript and pass it to SetExpandedMatch.
				parentMeta := a.metaForMatch(match)
				a.preview.SetMeta(parentMeta)
				a.transcriptStop()
				a.transcriptSeq++
				a.transcriptCtx, a.transcriptStop = context.WithCancel(context.Background())
				var loadCmd tea.Cmd
				if a.cfg.LoadTranscript != nil {
					loadCmd = a.cfg.LoadTranscript(a.transcriptSeq, parentMeta, a.transcriptCtx)
				}
				return a, loadCmd
			}
			// Plain in-memory search: resume highlighted session.
			selMeta, ok := a.list.Selected()
			if !ok {
				return a, nil
			}
			if !selMeta.Enriched || selMeta.CWD == "" {
				a.banner = "session not ready (enrichment pending)"
				a.bannerExp = time.Now().Add(3 * time.Second)
				return a, nil
			}
			if a.cfg.ResumeSelected != nil {
				return a, a.cfg.ResumeSelected(selMeta)
			}
			return a, nil
		}

		// Default: pass keystroke to search input, then trigger debounced search.
		_, cmd := a.search.Update(km)
		q := a.search.Query()
		if !a.showMatches {
			// Still using in-memory filter — always apply it immediately.
			a.applyFilter()
		}
		debounceCmd := a.scheduleSearch(q)
		return a, tea.Batch(cmd, debounceCmd)

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
	case "j", "down", "ctrl+n", "k", "up", "ctrl+p", "g", "G", "home", "end":
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
	var left string
	if a.showMatches {
		left = a.matchList.View()
	} else {
		left = a.list.View()
	}
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
		if a.showMatches && a.matchExpanded {
			parts = append(parts, "[Enter] resume  [Esc] back to matches")
		} else if a.showMatches {
			parts = append(parts, fmt.Sprintf("%d matches", len(a.searchMatches)))
			parts = append(parts, "[Enter] expand  [Esc] exit search")
		}
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

// scheduleSearch debounces rg invocation. If RunSearch is nil or the query
// looks like a hex prefix, we skip rg and rely on applyFilter instead.
func (a *App) scheduleSearch(q string) tea.Cmd {
	if q == "" || a.cfg.RunSearch == nil || hexPrefixRe.MatchString(q) {
		// Hex prefix or no provider: just filter in memory.
		a.showMatches = false
		a.matchExpanded = false
		a.currentMatch = nil
		a.searchCancel()
		a.applyFilter()
		return nil
	}
	// Debounce: emit a trigger after 200ms.
	return tea.Tick(200*time.Millisecond, func(_ time.Time) tea.Msg {
		return searchTriggerMsg{query: q}
	})
}

// runSearch cancels any prior search and spawns a new one.
func (a *App) runSearch(query string) tea.Cmd {
	if query != a.search.Query() {
		return nil // stale trigger
	}
	a.searchCancel()
	ctx, cancel := context.WithCancel(context.Background())
	a.searchCancel = cancel
	fn := a.cfg.RunSearch
	return func() tea.Msg {
		matches, err := fn(ctx, query)
		return searchResultsMsg{query: query, matches: matches, err: err}
	}
}

// metaForMatch looks up the session.Meta for a match from allItems.
// Returns an empty Meta with the ID set if not found.
func (a *App) metaForMatch(m search.Match) session.Meta {
	if i, ok := a.allIndex[m.SessionID]; ok && i < len(a.allItems) {
		return a.allItems[i]
	}
	return session.Meta{ID: m.SessionID}
}

// updatePreviewFromMatchSelection shows a context snippet for the selected match.
func (a *App) updatePreviewFromMatchSelection() {
	match, ok := a.matchList.Selected()
	if !ok {
		a.preview.SetMeta(session.Meta{})
		return
	}
	parent := a.metaForMatch(match)
	a.preview.SetMatchContext(parent, match)
}

// matchListSelectedMeta returns (true, Meta) for the session of the selected match.
func (a *App) matchListSelectedMeta() (bool, session.Meta) {
	match, ok := a.matchList.Selected()
	if !ok {
		return false, session.Meta{}
	}
	return true, a.metaForMatch(match)
}
