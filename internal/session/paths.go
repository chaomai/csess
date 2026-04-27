// Package session reads and parses Claude Code session JSONL files.
package session

import "strings"

// EncodeCWD returns the project-dir name that Claude Code uses to group
// sessions for the given working directory. The encoding replaces every
// "/" with "-" (an absolute path therefore begins with "-"). The reverse
// is ambiguous when the original path contains "-"; callers should read
// the real CWD from inside the session file instead.
func EncodeCWD(cwd string) string {
	return strings.ReplaceAll(cwd, "/", "-")
}
