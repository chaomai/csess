package action

import (
	"encoding/base64"
	"fmt"
	"io"
	"os/exec"
)

// ClipEnv is the subset of the environment used to select a clipboard
// backend. Provide an empty value for tests.
type ClipEnv struct {
	GOOS    string
	TMUX    string
	WAYLAND string
	DISPLAY string
}

type picker struct {
	name string
	cmd  string
	args []string
}

func pickClipboard(env ClipEnv, _ any) picker {
	switch {
	case env.GOOS == "darwin":
		return picker{name: "pbcopy", cmd: "pbcopy"}
	case env.TMUX != "":
		return picker{name: "tmux load-buffer", cmd: "tmux", args: []string{"load-buffer", "-"}}
	case env.WAYLAND != "":
		return picker{name: "wl-copy", cmd: "wl-copy"}
	case env.DISPLAY != "":
		return picker{name: "xclip", cmd: "xclip", args: []string{"-selection", "clipboard"}}
	default:
		return picker{name: "osc52"}
	}
}

// Clipboard copies strings to the host clipboard. Construct with
// NewClipboard. osc52Out is used only for the OSC52 fallback; pass the
// real terminal stderr (os.Stderr) in production.
type Clipboard struct {
	env      ClipEnv
	osc52Out io.Writer
	run      func(name string, args []string, stdin string) error
}

func NewClipboard(env ClipEnv, osc52Out io.Writer) *Clipboard {
	return &Clipboard{
		env:      env,
		osc52Out: osc52Out,
		run:      runCommand,
	}
}

// Copy writes s to the clipboard using the best available backend.
func (c *Clipboard) Copy(s string) error {
	p := pickClipboard(c.env, nil)
	if p.name == "osc52" {
		if c.osc52Out == nil {
			return fmt.Errorf("osc52: no writer")
		}
		enc := base64.StdEncoding.EncodeToString([]byte(s))
		_, err := fmt.Fprintf(c.osc52Out, "\x1b]52;c;%s\x07", enc)
		return err
	}
	return c.run(p.cmd, p.args, s)
}

func runCommand(name string, args []string, stdin string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = newStringReader(stdin)
	return cmd.Run()
}

func newStringReader(s string) io.Reader {
	return &stringReader{s: s}
}

type stringReader struct {
	s string
	i int
}

func (r *stringReader) Read(p []byte) (int, error) {
	if r.i >= len(r.s) {
		return 0, io.EOF
	}
	n := copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}
