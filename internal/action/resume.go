// Package action wraps side-effectful operations: resume (exec claude),
// clipboard writes, and soft-delete of session files.
package action

import (
	"fmt"
	"os/exec"
)

// BuildResumeCmd returns an unstarted *exec.Cmd that, when run, changes
// into cwd and execs `claude --resume sessionID`. The outer shell's cwd
// is not modified because the cd happens in the child bash process.
//
// Callers pass the command to bubbletea's tea.ExecProcess, which hands
// the terminal to claude and restores the TUI on exit.
func BuildResumeCmd(cwd, sessionID string) *exec.Cmd {
	script := fmt.Sprintf("cd %q && exec claude --resume %s", cwd, sessionID)
	return exec.Command("bash", "-lc", script)
}
