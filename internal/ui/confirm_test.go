package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConfirm_EnterReturnsConfirmed(t *testing.T) {
	c := NewConfirm("delete session abc?")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !c.Done() || !c.Confirmed() {
		t.Errorf("Done=%v Confirmed=%v", c.Done(), c.Confirmed())
	}
}

func TestConfirm_YReturnsConfirmed(t *testing.T) {
	c := NewConfirm("x")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if !c.Confirmed() {
		t.Error("y should confirm")
	}
}

func TestConfirm_EscCancels(t *testing.T) {
	c := NewConfirm("x")
	c, _ = c.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !c.Done() || c.Confirmed() {
		t.Error("esc should cancel")
	}
}

func TestConfirm_ViewIncludesMessage(t *testing.T) {
	c := NewConfirm("delete abc?")
	if !strings.Contains(c.View(), "delete abc?") {
		t.Error("view missing prompt")
	}
}
