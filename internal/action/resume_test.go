package action

import (
	"strings"
	"testing"
)

func TestBuildResumeCmd_BasicShape(t *testing.T) {
	cmd := BuildResumeCmd("/Users/x/proj", "abc-123")
	if cmd.Path == "" || !strings.HasSuffix(cmd.Path, "bash") {
		t.Errorf("cmd.Path = %q; want *bash", cmd.Path)
	}
	if len(cmd.Args) != 3 || cmd.Args[1] != "-lc" {
		t.Fatalf("Args = %v", cmd.Args)
	}
	script := cmd.Args[2]
	if !strings.Contains(script, `cd "/Users/x/proj"`) {
		t.Errorf("missing cd: %q", script)
	}
	if !strings.Contains(script, `exec claude --resume abc-123`) {
		t.Errorf("missing resume call: %q", script)
	}
}

func TestBuildResumeCmd_EscapesCWD(t *testing.T) {
	cmd := BuildResumeCmd(`/tmp/has space/'quote`, "id")
	script := cmd.Args[2]
	// Quote must be escaped; single-quote becomes '\'' in POSIX quoting style,
	// but since we use %q (double-quote) we expect the backslash-escaped form.
	if !strings.Contains(script, `/tmp/has space/`) {
		t.Errorf("missing cwd: %q", script)
	}
	if strings.Count(script, "&&") != 1 {
		t.Errorf("expected exactly one &&: %q", script)
	}
}
