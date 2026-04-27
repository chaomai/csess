package action

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestClipboardPicker_Darwin(t *testing.T) {
	env := ClipEnv{GOOS: "darwin"}
	picker := pickClipboard(env, nil)
	if picker.name != "pbcopy" {
		t.Errorf("picker = %q; want pbcopy", picker.name)
	}
}

func TestClipboardPicker_TmuxFallback(t *testing.T) {
	env := ClipEnv{GOOS: "linux", TMUX: "/tmp/tmux-sock"}
	picker := pickClipboard(env, nil)
	if picker.name != "tmux load-buffer" {
		t.Errorf("picker = %q", picker.name)
	}
}

func TestClipboardPicker_OSC52Fallback(t *testing.T) {
	env := ClipEnv{GOOS: "linux"}
	var buf bytes.Buffer
	c := NewClipboard(env, &buf)
	if err := c.Copy("hello"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "\x1b]52;c;") || !strings.HasSuffix(out, "\x07") {
		t.Errorf("OSC52 framing missing: %q", out)
	}
	// base64 of "hello" = aGVsbG8=
	if !strings.Contains(out, "aGVsbG8=") {
		t.Errorf("payload missing: %q", out)
	}
}

func TestClipboard_FailingCommandReturnsError(t *testing.T) {
	env := ClipEnv{GOOS: "linux", DISPLAY: ":0"}
	c := NewClipboard(env, nil)
	// Override the runner to simulate failure.
	c.run = func(name string, args []string, stdin string) error {
		return errFake
	}
	err := c.Copy("x")
	if err == nil {
		t.Error("expected error")
	}
}

var errFake = errors.New("fake")
