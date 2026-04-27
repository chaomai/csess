package session

import (
	"bufio"
	"encoding/json"
	"io"
	"time"
)

// Meta is a session's summary. Cheap fields are populated by Scanner.Quick
// (readdir + stat only). Expensive fields are populated by ExtractMeta
// (one file open, one pass).
type Meta struct {
	ID         string
	Path       string
	ProjectDir string
	SizeBytes  int64
	ModTime    time.Time

	Enriched    bool
	CWD         string
	GitBranch   string
	Model       string
	Version     string
	StartedAt   time.Time
	UpdatedAt   time.Time
	UserMsgs    int
	AsstMsgs    int
	FirstPrompt string
	LastPrompt  string

	LoadErr error
}

const (
	maxLineBytes     = 16 * 1024 * 1024
	promptTruncateAt = 200
)

// line is a partial view of a session jsonl record.
type line struct {
	Type      string          `json:"type"`
	Cwd       string          `json:"cwd"`
	GitBranch string          `json:"gitBranch"`
	Version   string          `json:"version"`
	Timestamp string          `json:"timestamp"`
	UserType  string          `json:"userType"`
	Message   json.RawMessage `json:"message"`
}

// effectiveType returns the record type. When the top-level "type" field is
// absent (as in assistant records which only carry the role inside message),
// we derive it from message.role.
func (l line) effectiveType() string {
	if l.Type != "" {
		return l.Type
	}
	if len(l.Message) == 0 {
		return ""
	}
	var msg struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(l.Message, &msg); err != nil {
		return ""
	}
	return msg.Role
}

type userStringMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type asstMsg struct {
	Model   string          `json:"model"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ExtractMeta fills the Enriched fields on m by streaming r once. It does
// not close r. Bad lines are tolerated; m.LoadErr is set if any line could
// not be parsed but extraction continues.
func ExtractMeta(r io.Reader, m *Meta) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	var badLines int
	for sc.Scan() {
		var l line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			badLines++
			continue
		}
		applyLine(m, l)
	}
	if err := sc.Err(); err != nil {
		return err
	}
	m.Enriched = true
	if badLines > 0 && m.LoadErr == nil {
		m.LoadErr = &PartialError{BadLines: badLines}
	}
	return nil
}

// PartialError signals that some lines were malformed but extraction
// completed on the well-formed ones.
type PartialError struct{ BadLines int }

func (e *PartialError) Error() string { return "partial parse" }

func applyLine(m *Meta, l line) {
	if l.Cwd != "" && m.CWD == "" {
		m.CWD = l.Cwd
	}
	if l.GitBranch != "" && m.GitBranch == "" {
		m.GitBranch = l.GitBranch
	}
	if l.Version != "" && m.Version == "" {
		m.Version = l.Version
	}

	ts := parseTime(l.Timestamp)

	switch l.effectiveType() {
	case "user":
		if len(l.Message) == 0 {
			return
		}
		var u userStringMsg
		if err := json.Unmarshal(l.Message, &u); err != nil {
			return
		}
		if u.Content == "" {
			// array-shaped content (e.g. tool_result). Count it but
			// don't use for first/last prompt.
			m.UserMsgs++
			return
		}
		if l.UserType != "external" {
			m.UserMsgs++
			return
		}
		m.UserMsgs++
		if m.FirstPrompt == "" {
			m.FirstPrompt = truncate(u.Content, promptTruncateAt)
			m.StartedAt = ts
		}
		m.LastPrompt = truncate(u.Content, promptTruncateAt)
		if ts.After(m.UpdatedAt) {
			m.UpdatedAt = ts
		}
	case "assistant":
		if len(l.Message) == 0 {
			return
		}
		var a asstMsg
		if err := json.Unmarshal(l.Message, &a); err != nil {
			return
		}
		if m.Model == "" && a.Model != "" {
			m.Model = a.Model
		}
		m.AsstMsgs++
		if ts.After(m.UpdatedAt) {
			m.UpdatedAt = ts
		}
	}
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Truncate on rune boundary.
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
