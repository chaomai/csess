package ui

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"csess/internal/session"
)

// FilterMetas returns a stable-sorted subset of metas matching q.
// Matching is case-insensitive substring on FirstPrompt, CWD basename,
// GitBranch, and ID prefix. Ranking: ID-prefix > FirstPrompt/CWD > Branch.
func FilterMetas(metas []session.Meta, q string) []session.Meta {
	if q == "" {
		out := make([]session.Meta, len(metas))
		copy(out, metas)
		return out
	}
	q = strings.ToLower(q)

	type scored struct {
		m     session.Meta
		score int
		pos   int
	}
	var hits []scored
	for i, m := range metas {
		s := scoreMatch(m, q)
		if s > 0 {
			hits = append(hits, scored{m, s, i})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].pos < hits[j].pos
	})
	out := make([]session.Meta, len(hits))
	for i, h := range hits {
		out[i] = h.m
	}
	return out
}

func scoreMatch(m session.Meta, q string) int {
	score := 0
	if strings.HasPrefix(strings.ToLower(m.ID), q) {
		score += 3
	}
	if strings.Contains(strings.ToLower(m.FirstPrompt), q) {
		score += 2
	}
	if m.CWD != "" && strings.Contains(strings.ToLower(filepath.Base(m.CWD)), q) {
		score += 2
	}
	if strings.Contains(strings.ToLower(m.GitBranch), q) {
		score += 1
	}
	return score
}

// SearchBar is the textinput overlay shown when the user presses /.
type SearchBar struct {
	input textinput.Model
}

func NewSearchBar() *SearchBar {
	ti := textinput.New()
	ti.Placeholder = "filter sessions..."
	ti.Prompt = "/ "
	ti.CharLimit = 128
	return &SearchBar{input: ti}
}

func (s *SearchBar) Focus() tea.Cmd { return s.input.Focus() }
func (s *SearchBar) Blur()          { s.input.Blur() }
func (s *SearchBar) Query() string  { return s.input.Value() }
func (s *SearchBar) Reset()         { s.input.Reset() }

func (s *SearchBar) Update(msg tea.Msg) (*SearchBar, tea.Cmd) {
	var cmd tea.Cmd
	s.input, cmd = s.input.Update(msg)
	return s, cmd
}

func (s *SearchBar) View() string { return s.input.View() }
