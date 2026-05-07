package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestBookmarksPane_EmptyViewIsPaddedToWidth(t *testing.T) {
	p := NewBookmarksPane(60, 10)
	v := p.View()
	if !strings.Contains(v, "no bookmarks") {
		t.Errorf("empty view missing hint text: %q", v)
	}
	first := strings.Split(v, "\n")[0]
	if got := ansi.StringWidth(first); got < 60 {
		t.Errorf("empty view first-line width = %d; want >= 60", got)
	}
}
