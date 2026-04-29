package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"time"
)

// Turn is a single user or assistant message, rendered to plain text for
// display. Thinking blocks are dropped entirely. Tool_use blocks render
// as one line "→ Name(brief args)".
type Turn struct {
	Role      string
	Timestamp time.Time
	Text      string
	LineNo    int // 1-based line number in the source JSONL file
}

// StreamTurns reads fsys[path] line by line and sends each user/assistant
// turn on ch until EOF or ctx is cancelled. ch is not closed by this
// function; the caller owns it.
func StreamTurns(ctx context.Context, fsys fs.FS, path string, ch chan<- Turn) error {
	f, err := fsys.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	lineNo := 0
	for sc.Scan() {
		lineNo++
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var l line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			continue
		}
		t, ok := renderTurn(l)
		if !ok {
			continue
		}
		t.LineNo = lineNo
		select {
		case ch <- t:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return sc.Err()
}

func renderTurn(l line) (Turn, bool) {
	ts := parseTime(l.Timestamp)
	switch l.effectiveType() {
	case "user":
		text, ok := renderUserContent(l.Message)
		if !ok {
			return Turn{}, false
		}
		return Turn{Role: "user", Timestamp: ts, Text: text}, true
	case "assistant":
		text, ok := renderAssistantContent(l.Message)
		if !ok {
			return Turn{}, false
		}
		return Turn{Role: "assistant", Timestamp: ts, Text: text}, true
	}
	return Turn{}, false
}

func renderUserContent(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	// Try string first.
	var us userStringMsg
	if err := json.Unmarshal(raw, &us); err == nil && us.Content != "" {
		return us.Content, true
	}
	// Try array.
	var arr struct {
		Content []struct {
			Type    string          `json:"type"`
			Text    string          `json:"text"`
			Content json.RawMessage `json:"content"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &arr); err != nil {
		return "", false
	}
	var parts []string
	for _, c := range arr.Content {
		switch c.Type {
		case "text":
			parts = append(parts, c.Text)
		case "tool_result":
			// Keep first line, summarize the rest.
			s := string(c.Content)
			s = strings.Trim(s, `"`)
			firstLine, rest, _ := strings.Cut(s, "\n")
			if rest == "" {
				parts = append(parts, firstLine)
			} else {
				n := strings.Count(rest, "\n") + 1
				parts = append(parts, firstLine+fmt.Sprintf("\n…(%d more lines)", n))
			}
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n"), true
}

func renderAssistantContent(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var a struct {
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", false
	}
	var parts []string
	for _, c := range a.Content {
		switch c.Type {
		case "thinking":
			// Dropped.
		case "text":
			if c.Text != "" {
				parts = append(parts, c.Text)
			}
		case "tool_use":
			parts = append(parts, fmt.Sprintf("→ %s(%s)", c.Name, briefArgs(c.Input)))
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, "\n"), true
}

// briefArgs returns a single-line summary of a tool_use input JSON. For a
// map, shows "key=value" pairs truncated; for anything else, best-effort.
func briefArgs(raw json.RawMessage) string {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	var parts []string
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%s=%s", k, briefValue(v)))
	}
	s := strings.Join(parts, ", ")
	if len(s) > 80 {
		s = s[:77] + "..."
	}
	return s
}

func briefValue(v any) string {
	s := fmt.Sprintf("%v", v)
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 40 {
		s = s[:37] + "..."
	}
	return s
}
