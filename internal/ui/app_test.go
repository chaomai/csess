package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"csess/internal/session"
)

// stubLoad returns a no-op tea.Cmd; used so App.Update returns non-nil
// cmds even without the real StreamTurns wiring.
func stubLoad(seq int, m session.Meta, ctx context.Context) tea.Cmd {
	return func() tea.Msg { return nil }
}

func TestApp_InitialStateShowsList(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, AllMode: false, LoadTranscript: stubLoad})
	// Inject scan result.
	model, _ := app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "a", FirstPrompt: "one", UpdatedAt: time.Unix(2, 0), Enriched: true},
		{ID: "b", FirstPrompt: "two", UpdatedAt: time.Unix(1, 0), Enriched: true},
	}})
	v := model.View()
	if !strings.Contains(v, "a") || !strings.Contains(v, "one") {
		t.Errorf("list missing: %s", v)
	}
}

func TestApp_CursorMoveSchedulesTranscriptLoad(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	m, _ := app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "a", Path: "a.jsonl", UpdatedAt: time.Unix(2, 0)},
		{ID: "b", Path: "b.jsonl", UpdatedAt: time.Unix(1, 0)},
	}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if cmd == nil {
		t.Error("j should schedule LoadTranscript cmd")
	}
}

func TestApp_SlashEntersSearchMode(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a", Enriched: true}}})
	m, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	mm := m.(*App)
	if mm.mode != modeSearch {
		t.Errorf("mode = %v; want search", mm.mode)
	}
}

func TestApp_EnrichMsgUpdatesItem(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}, {ID: "b"}}})
	app.Update(EnrichMsg{Meta: session.Meta{ID: "a", FirstPrompt: "filled", Enriched: true}})
	items := app.list.Items()
	var found bool
	for _, m := range items {
		if m.ID == "a" && m.FirstPrompt == "filled" {
			found = true
		}
	}
	if !found {
		t.Error("enrich did not update item")
	}
}
