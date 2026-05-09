// Package search provides full-text content search over Claude session files
// using ripgrep as the backend.
package search

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Match is a single line match inside a session file.
type Match struct {
	SessionID  string    // file basename without .jsonl
	FilePath   string    // absolute path
	LineNo     int       // 1-based
	Line       string    // the matching JSONL line text (may be large)
	MatchStart int       // byte offset of the first submatch in Line (rune-aligned)
	MatchEnd   int       // byte offset of first submatch end in Line (rune-aligned)
	Before     []string  // up to context-size lines before
	After      []string  // up to context-size lines after
	SortTime   time.Time // populated by caller from session Meta.UpdatedAt
}

// Options controls a Run call.
type Options struct {
	ProjectsDir  string   // e.g. ~/.claude/projects
	Query        string   // literal (fixed-string) query
	Context      int      // lines of context before/after (default 3)
	MaxMatches   int      // hard cap on returned matches (default 1000)
	RgPath       string   // optional override; empty = look up in PATH
	ExcludeGlobs []string // additional rg -g patterns, e.g. "!-Users-me/**"
}

// Run executes ripgrep and returns up to opts.MaxMatches matches.
// If rg is not in PATH it returns an error wrapping exec.ErrNotFound.
// If rg exits with code 1 (no matches), Run returns an empty slice and nil.
func Run(ctx context.Context, opts Options) ([]Match, error) {
	if opts.Context < 0 {
		opts.Context = 0
	}
	if opts.MaxMatches <= 0 {
		opts.MaxMatches = 1000
	}

	rgBin := opts.RgPath
	if rgBin == "" {
		var err error
		rgBin, err = exec.LookPath("rg")
		if err != nil {
			return nil, fmt.Errorf("rg not found in PATH: %w", err)
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	args := []string{
		"--json",
		"-F",
		"-i",
		// Match only session jsonl files. Claude stores auxiliary content
		// under each project dir (tool-results/*.txt, subagents/*.jsonl,
		// memory/...) — none of those correspond to entries in the top-
		// level session scan, so matches from them would surface as
		// orphan rows in the match list.
		"-g", "*.jsonl",
		"-g", "!**/subagents/**",
	}
	for _, g := range opts.ExcludeGlobs {
		args = append(args, "-g", g)
	}
	args = append(args,
		fmt.Sprintf("-C%d", opts.Context),
		"--",
		opts.Query,
		opts.ProjectsDir,
	)
	cmd := exec.CommandContext(ctx, rgBin, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("rg pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("rg start: %w", err)
	}

	matches, parseErr := parse(stdout, opts.MaxMatches)
	if len(matches) >= opts.MaxMatches {
		cancel() // stop rg early
	}

	waitErr := cmd.Wait()

	if parseErr != nil {
		return nil, fmt.Errorf("rg parse: %w", parseErr)
	}

	// rg exit code 1 means "no matches found" — that's not an error.
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) && exitErr.ExitCode() == 1 {
			return filterConversationMatches(matches), nil
		}
		// Context cancellation (we triggered it after MaxMatches) is expected.
		if errors.Is(waitErr, context.Canceled) || ctx.Err() != nil {
			return filterConversationMatches(matches), nil
		}
		return filterConversationMatches(matches), fmt.Errorf("rg: %w", waitErr)
	}

	return filterConversationMatches(matches), nil
}

// filterConversationMatches drops matches that aren't in an actual
// conversation turn. Claude Code writes several side-channel record types
// into the same .jsonl file (last-prompt, permission-mode, ai-title,
// attachment, file-history-snapshot, system); those records often echo the
// user's prompt text and would otherwise surface as duplicate hits for the
// same logical message. We keep only rows whose JSON "type" is user or
// assistant. Rows we can't parse as JSON are dropped — every well-formed
// session line is a JSON object, so a parse failure means the line isn't
// conversation content anyway.
func filterConversationMatches(in []Match) []Match {
	out := in[:0]
	for _, m := range in {
		if isConversationTurn(m.Line) {
			out = append(out, m)
		}
	}
	return out
}

func isConversationTurn(line string) bool {
	var hdr struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(line), &hdr); err != nil {
		return false
	}
	return hdr.Type == "user" || hdr.Type == "assistant"
}

// ------- ndjson parser -------

// rgEvent is the top-level envelope for each ndjson line rg emits.
type rgEvent struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type rgBeginData struct {
	Path rgText `json:"path"`
}

type rgMatchData struct {
	Path       rgText    `json:"path"`
	Lines      rgText    `json:"lines"`
	LineNumber int       `json:"line_number"`
	SubMatches []rgSubmatch `json:"submatches"`
}

type rgContextData struct {
	Path       rgText `json:"path"`
	Lines      rgText `json:"lines"`
	LineNumber int    `json:"line_number"`
}

type rgText struct {
	Text string `json:"text"`
}

type rgSubmatch struct {
	Match rgText `json:"match"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// fileState accumulates context lines while scanning a single file.
type fileState struct {
	path       string
	contextBuf []rgContextData // context lines seen since last match (or begin)
	pendingCtx []rgContextData // context lines emitted before the match
}

// parse reads rg --json ndjson from r and returns at most maxMatches matches.
// It does NOT invoke rg itself; callers can test it with a fixture reader.
func parse(r io.Reader, maxMatches int) ([]Match, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var (
		out      []Match
		state    *fileState
		// contextWindow holds lines seen since the previous match event within
		// the current file, so we can attach them as "after" to the previous
		// match and "before" to the next.
		//
		// We collect context lines as they arrive; when a match arrives we
		// split the accumulated window: lines before this match are "before",
		// lines after the previous match are "after" (retroactively patched).
		afterBuf []string // context lines emitted after the last match (retroactive after)
		lastMatchIdx int  // index into out of the last match for this file (-1 if none)
	)

	resetFile := func(path string) {
		state = &fileState{path: path}
		afterBuf = nil
		lastMatchIdx = -1
	}

	for sc.Scan() {
		if len(out) >= maxMatches {
			break
		}

		var ev rgEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			continue // tolerate malformed lines
		}

		switch ev.Type {
		case "begin":
			var d rgBeginData
			if err := json.Unmarshal(ev.Data, &d); err != nil {
				continue
			}
			resetFile(d.Path.Text)

		case "context":
			if state == nil {
				continue
			}
			var d rgContextData
			if err := json.Unmarshal(ev.Data, &d); err != nil {
				continue
			}
			line := strings.TrimRight(d.Lines.Text, "\n")
			// If there was a recent match, these context lines are "after" it.
			if lastMatchIdx >= 0 {
				afterBuf = append(afterBuf, line)
			} else {
				// "before" context for the next match.
				state.contextBuf = append(state.contextBuf, d)
			}

		case "match":
			if state == nil {
				continue
			}
			var d rgMatchData
			if err := json.Unmarshal(ev.Data, &d); err != nil {
				continue
			}

			// Retroactively patch the after-lines onto the previous match.
			if lastMatchIdx >= 0 {
				out[lastMatchIdx].After = copyStrings(afterBuf)
				// Those context lines might also be "before" for the current match.
				// rg reuses them in the window; they appear in state.contextBuf next.
			}
			afterBuf = nil

			// Build before-context from state.contextBuf.
			var before []string
			for _, c := range state.contextBuf {
				before = append(before, strings.TrimRight(c.Lines.Text, "\n"))
			}
			state.contextBuf = nil

			m := Match{
				SessionID: sessionIDFromPath(d.Path.Text),
				FilePath:  d.Path.Text,
				LineNo:    d.LineNumber,
				Line:      strings.TrimRight(d.Lines.Text, "\n"),
				Before:    before,
			}
			if len(d.SubMatches) > 0 {
				m.MatchStart = d.SubMatches[0].Start
				m.MatchEnd = d.SubMatches[0].End
			}
			out = append(out, m)
			lastMatchIdx = len(out) - 1

		case "end":
			if state == nil {
				continue
			}
			// Patch any remaining after-lines onto the last match.
			if lastMatchIdx >= 0 {
				out[lastMatchIdx].After = copyStrings(afterBuf)
			}
			afterBuf = nil
			lastMatchIdx = -1
			state = nil

		case "summary":
			// Nothing to do.
		}
	}

	return out, sc.Err()
}

func sessionIDFromPath(p string) string {
	base := filepath.Base(p)
	return strings.TrimSuffix(base, ".jsonl")
}

func copyStrings(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	cp := make([]string, len(s))
	copy(cp, s)
	return cp
}
