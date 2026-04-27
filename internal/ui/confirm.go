package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var confirmBoxStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("196")).
	Padding(0, 2)

type Confirm struct {
	prompt    string
	done      bool
	confirmed bool
}

func NewConfirm(prompt string) *Confirm { return &Confirm{prompt: prompt} }

func (c *Confirm) Done() bool      { return c.done }
func (c *Confirm) Confirmed() bool { return c.confirmed }

func (c *Confirm) Update(msg tea.Msg) (*Confirm, tea.Cmd) {
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return c, nil
	}
	switch km.String() {
	case "enter", "y":
		c.done, c.confirmed = true, true
	case "esc", "n":
		c.done, c.confirmed = true, false
	}
	return c, nil
}

func (c *Confirm) View() string {
	return confirmBoxStyle.Render(fmt.Sprintf("%s  [y/Enter] yes  [Esc/n] no", c.prompt))
}
