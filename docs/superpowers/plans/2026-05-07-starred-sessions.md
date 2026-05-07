# Bookmarked Sessions Pane Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a third navigable pane above csess's existing list/preview split showing user-bookmarked sessions, persisted across runs, cross-project.

**Architecture:** A new `session.BookmarkStore` (disk I/O, atomic writes) plus a new `ui.BookmarksPane` model mirroring the shape of `List` / `MatchList`. `App` gains a `focus` enum routing keystrokes between the list and bookmarks panes; `Ctrl-J`/`Ctrl-K` switch focus, `b` toggles, save is a tea.Cmd injected via `AppConfig`.

**Tech Stack:** Go 1.22, bubbletea, lipgloss, stdlib `encoding/json` / `os`. Same conventions as the rest of csess.

**Reference spec:** `docs/specs/2026-05-07-starred-sessions-design.md`

---

## File Structure

```
internal/session/
  bookmarks.go          BookmarkStore + Bookmark struct + ErrMissing sentinel
  bookmarks_test.go     Load/Save/atomic/round-trip tests

internal/ui/
  bookmarks.go          BookmarksPane model
  bookmarks_test.go     View + cursor + Add/Remove/ReplaceItem tests
  app.go                +focus enum, +bookmarks field, +key routing, vertical layout
  app_test.go           +focus, +toggle, +off-scope enrich tests

cmd/csess/main.go       +BookmarkStore wiring, +--bookmarks-file flag,
                        +off-scope enrichment bridge, +save cmd factory
```

Two new files (~250 LOC total) + surgical edits to `app.go` and `main.go`. No changes to `scanner.go`, `preview.go`, `matches.go`, `search.go`, `list.go`, `confirm.go`.

---

## Task 1: `session.BookmarkStore` + persistence tests

**Files:**
- Create: `internal/session/bookmarks.go`
- Create: `internal/session/bookmarks_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/session/bookmarks_test.go`:

```go
package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBookmarkStore_LoadMissingReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	store := NewBookmarkStore(path)
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load missing = %v; want nil err", err)
	}
	if len(got) != 0 {
		t.Errorf("Load missing = %d entries; want 0", len(got))
	}
}

func TestBookmarkStore_LoadMalformedReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	store := NewBookmarkStore(path)
	if _, err := store.Load(); err == nil {
		t.Error("Load malformed = nil err; want error")
	}
}

func TestBookmarkStore_LoadWrongVersionReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.json")
	body, _ := json.Marshal(map[string]any{
		"version":   2,
		"bookmarks": []Bookmark{},
	})
	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatal(err)
	}
	store := NewBookmarkStore(path)
	if _, err := store.Load(); err == nil {
		t.Error("Load wrong version = nil err; want error")
	}
}

func TestBookmarkStore_SaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bm.json")
	store := NewBookmarkStore(path)
	want := []Bookmark{
		{ID: "abc", StarredAt: time.Unix(200, 0).UTC()},
		{ID: "def", StarredAt: time.Unix(100, 0).UTC()},
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("loaded %d; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID || !got[i].StarredAt.Equal(want[i].StarredAt) {
			t.Errorf("entry[%d] = %+v; want %+v", i, got[i], want[i])
		}
	}
}

func TestBookmarkStore_SaveCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "subdir", "bm.json")
	store := NewBookmarkStore(path)
	if err := store.Save([]Bookmark{{ID: "x", StarredAt: time.Unix(1, 0)}}); err != nil {
		t.Fatalf("Save with missing parent: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestBookmarkStore_LoadIgnoresLeftoverTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bm.json")
	store := NewBookmarkStore(path)
	if err := store.Save([]Bookmark{{ID: "real", StarredAt: time.Unix(1, 0)}}); err != nil {
		t.Fatal(err)
	}
	// Simulate interrupted prior save: leftover .tmp file with garbage.
	if err := os.WriteFile(path+".tmp", []byte("garbage"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || got[0].ID != "real" {
		t.Errorf("Load after .tmp leftover = %+v; want single 'real' entry", got)
	}
}

func TestErrMissingIsExported(t *testing.T) {
	var err error = ErrMissing
	if !errors.Is(err, ErrMissing) {
		t.Error("ErrMissing should satisfy errors.Is against itself")
	}
	if err.Error() == "" {
		t.Error("ErrMissing.Error() should be non-empty")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/session/ -run BookmarkStore -v`
Expected: FAIL — `undefined: NewBookmarkStore`, `undefined: Bookmark`, `undefined: ErrMissing`.

- [ ] **Step 3: Implement `internal/session/bookmarks.go`**

Create `internal/session/bookmarks.go`:

```go
package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrMissing signals that a bookmarked session's jsonl file could not
// be found on disk. Off-scope enrichment uses this to mark stub rows.
var ErrMissing = errors.New("session file missing")

// Bookmark is a single persisted entry in the bookmarks file.
type Bookmark struct {
	ID        string    `json:"id"`
	StarredAt time.Time `json:"starredAt"`
}

// bookmarkFile is the on-disk shape.
type bookmarkFile struct {
	Version   int        `json:"version"`
	Bookmarks []Bookmark `json:"bookmarks"`
}

const bookmarkFileVersion = 1

// BookmarkStore persists bookmark ids + timestamps to a single JSON file.
type BookmarkStore struct {
	path string
}

func NewBookmarkStore(path string) *BookmarkStore {
	return &BookmarkStore{path: path}
}

// Load returns the stored bookmarks. A missing file is not an error —
// it returns an empty slice. Malformed JSON or an unknown version
// returns an error; the caller decides whether to proceed.
func (s *BookmarkStore) Load() ([]Bookmark, error) {
	body, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read bookmarks: %w", err)
	}
	var f bookmarkFile
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("parse bookmarks: %w", err)
	}
	if f.Version != bookmarkFileVersion {
		return nil, fmt.Errorf("bookmarks: unknown version %d", f.Version)
	}
	return f.Bookmarks, nil
}

// Save writes bookmarks atomically: tmp file in the same directory,
// fsync, rename. Creates the parent directory if absent.
func (s *BookmarkStore) Save(bookmarks []Bookmark) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("mkdir bookmarks dir: %w", err)
	}
	body, err := json.MarshalIndent(bookmarkFile{
		Version:   bookmarkFileVersion,
		Bookmarks: bookmarks,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode bookmarks: %w", err)
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open bookmarks tmp: %w", err)
	}
	if _, err := f.Write(body); err != nil {
		f.Close()
		return fmt.Errorf("write bookmarks tmp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("fsync bookmarks tmp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close bookmarks tmp: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename bookmarks: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/session/ -run 'BookmarkStore|ErrMissing' -v`
Expected: PASS for all 7 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/session/bookmarks.go internal/session/bookmarks_test.go
git commit -m "session: add BookmarkStore with atomic persistence"
```

---

## Task 2: `ui.BookmarksPane` empty state

**Files:**
- Create: `internal/ui/bookmarks.go`
- Create: `internal/ui/bookmarks_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/ui/bookmarks_test.go`:

```go
package ui

import (
	"strings"
	"testing"
)

func TestBookmarksPane_EmptyViewIsPaddedToWidth(t *testing.T) {
	p := NewBookmarksPane(60, 10)
	v := p.View()
	if !strings.Contains(v, "no bookmarks") {
		t.Errorf("empty view missing hint text: %q", v)
	}
	// lipgloss.Width() reports the visible width of the rendered string.
	if got := lipglossWidth(v); got < 60 {
		t.Errorf("empty view width = %d; want >= 60", got)
	}
}
```

Add a small helper at the bottom of the new test file:

```go
// lipglossWidth is a thin wrapper so the test doesn't need to import
// lipgloss directly in every file.
func lipglossWidth(s string) int { return lipglossWidthFn(s) }
```

And at the top of the test file, below `package ui`:

```go
import _ "github.com/charmbracelet/lipgloss"
```

Wait — simpler: just use `github.com/charmbracelet/x/ansi.StringWidth` which is already in the project deps. Replace the helper with:

```go
import "github.com/charmbracelet/x/ansi"

// ... inside test:
// if got := ansi.StringWidth(strings.Split(v, "\n")[0]); got < 60 { ... }
```

Final test file:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run BookmarksPane -v`
Expected: FAIL — `undefined: NewBookmarksPane`.

- [ ] **Step 3: Implement minimal `internal/ui/bookmarks.go`**

Create `internal/ui/bookmarks.go`:

```go
// Package ui — BookmarksPane is the persisted "bookmarked sessions"
// row rendered above the list/preview split. Mirrors List in shape so
// the key-routing code in App can treat both uniformly.
package ui

import (
	"github.com/charmbracelet/lipgloss"

	"csess/internal/session"
)

// BookmarksPane is a pure model; App drives selection and mutation.
type BookmarksPane struct {
	width, height int
	items         []session.Meta
	starredAt     map[string]int64 // id -> unix nanos, for sort
	idIndex       map[string]int
	cursor        int
	firstVisible  int
}

func NewBookmarksPane(w, h int) *BookmarksPane {
	return &BookmarksPane{
		width:     w,
		height:    h,
		starredAt: map[string]int64{},
		idIndex:   map[string]int{},
	}
}

func (p *BookmarksPane) View() string {
	if len(p.items) == 0 {
		return lipgloss.NewStyle().Width(p.width).Render(listDimStyle.Render("no bookmarks — press b to add"))
	}
	return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run BookmarksPane -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/bookmarks.go internal/ui/bookmarks_test.go
git commit -m "ui: scaffold BookmarksPane with empty state"
```

---

## Task 3: `BookmarksPane` SetItems + View + cursor

**Files:**
- Modify: `internal/ui/bookmarks.go`
- Modify: `internal/ui/bookmarks_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/bookmarks_test.go`:

```go
import tea "github.com/charmbracelet/bubbletea"
import "csess/internal/session"
import "time"
import "fmt"

func TestBookmarksPane_SetItemsSortsByStarredAtDesc(t *testing.T) {
	p := NewBookmarksPane(80, 10)
	p.SetItems(
		[]session.Meta{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		map[string]time.Time{
			"a": time.Unix(100, 0),
			"b": time.Unix(300, 0),
			"c": time.Unix(200, 0),
		},
	)
	got := p.Items()
	want := []string{"b", "c", "a"}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("items[%d] = %q; want %q", i, got[i].ID, id)
		}
	}
}

func TestBookmarksPane_RowShowsTimeIDProjectPrompt(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	p.SetItems(
		[]session.Meta{{ID: "abc12345xyz", CWD: "/work/proj-alpha", FirstPrompt: "fix bug", Enriched: true}},
		map[string]time.Time{"abc12345xyz": time.Now()},
	)
	v := p.View()
	for _, want := range []string{"abc12345", "proj-alpha", "fix bug"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q: %q", want, v)
		}
	}
}

func TestBookmarksPane_JMovesCursorDown(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	items := make([]session.Meta, 3)
	sa := map[string]time.Time{}
	for i := range items {
		id := fmt.Sprintf("id%d", i)
		items[i] = session.Meta{ID: id}
		sa[id] = time.Unix(int64(100-i), 0) // id0 newest
	}
	p.SetItems(items, sa)
	if p.Cursor() != 0 {
		t.Fatalf("initial cursor = %d; want 0", p.Cursor())
	}
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if p.Cursor() != 1 {
		t.Errorf("cursor after j = %d; want 1", p.Cursor())
	}
}

func TestBookmarksPane_DesiredHeightCapsAtMax(t *testing.T) {
	p := NewBookmarksPane(80, 100)
	items := make([]session.Meta, 20)
	sa := map[string]time.Time{}
	for i := range items {
		id := fmt.Sprintf("id%d", i)
		items[i] = session.Meta{ID: id}
		sa[id] = time.Unix(int64(i), 0)
	}
	p.SetItems(items, sa)
	if got := p.DesiredHeight(5); got != 5 {
		t.Errorf("DesiredHeight(5) with 20 items = %d; want 5", got)
	}
	if got := p.DesiredHeight(50); got != 20 {
		t.Errorf("DesiredHeight(50) with 20 items = %d; want 20", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run BookmarksPane -v`
Expected: FAIL — `SetItems undefined`, `Items undefined`, etc.

- [ ] **Step 3: Flesh out `internal/ui/bookmarks.go`**

Replace the contents of `internal/ui/bookmarks.go`:

```go
// Package ui — BookmarksPane is the persisted "bookmarked sessions"
// row rendered above the list/preview split. Mirrors List in shape so
// the key-routing code in App can treat both uniformly.
package ui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"csess/internal/session"
)

// BookmarksPane is a pure model; App drives selection and mutation.
type BookmarksPane struct {
	width, height int
	items         []session.Meta
	starredAt     map[string]time.Time
	idIndex       map[string]int
	cursor        int
	firstVisible  int
}

func NewBookmarksPane(w, h int) *BookmarksPane {
	return &BookmarksPane{
		width:     w,
		height:    h,
		starredAt: map[string]time.Time{},
		idIndex:   map[string]int{},
	}
}

// SetItems replaces the pane's contents. items and starredAt must
// cover the same set of ids; items need not be pre-sorted (this
// function sorts them by starredAt desc).
func (p *BookmarksPane) SetItems(items []session.Meta, starredAt map[string]time.Time) {
	p.items = append(p.items[:0], items...)
	p.starredAt = map[string]time.Time{}
	for k, v := range starredAt {
		p.starredAt[k] = v
	}
	p.sortItems()
	p.rebuildIndex()
	if p.cursor >= len(p.items) {
		if len(p.items) == 0 {
			p.cursor = 0
		} else {
			p.cursor = len(p.items) - 1
		}
	}
	p.firstVisible = 0
	p.ensureVisible()
}

func (p *BookmarksPane) Items() []session.Meta { return p.items }
func (p *BookmarksPane) Cursor() int           { return p.cursor }

func (p *BookmarksPane) Selected() (session.Meta, bool) {
	if p.cursor < 0 || p.cursor >= len(p.items) {
		return session.Meta{}, false
	}
	return p.items[p.cursor], true
}

func (p *BookmarksPane) SetSize(w, h int) {
	p.width, p.height = w, h
	p.ensureVisible()
}

// DesiredHeight is what the pane would like to be rendered at; App
// passes the cap it's willing to allow.
func (p *BookmarksPane) DesiredHeight(maxRows int) int {
	if len(p.items) == 0 {
		return 1
	}
	if len(p.items) < maxRows {
		return len(p.items)
	}
	return maxRows
}

func (p *BookmarksPane) Update(msg tea.Msg) (*BookmarksPane, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "j", "down", "ctrl+n":
			if p.cursor < len(p.items)-1 {
				p.cursor++
			}
		case "k", "up", "ctrl+p":
			if p.cursor > 0 {
				p.cursor--
			}
		case "g", "home":
			p.cursor = 0
		case "G", "end":
			if n := len(p.items); n > 0 {
				p.cursor = n - 1
			}
		}
		p.ensureVisible()
	}
	return p, nil
}

func (p *BookmarksPane) View() string {
	if len(p.items) == 0 {
		return lipgloss.NewStyle().Width(p.width).Render(
			listDimStyle.Render("no bookmarks — press b to add"),
		)
	}
	height := p.height
	if height <= 0 {
		height = 1
	}
	end := p.firstVisible + height
	if end > len(p.items) {
		end = len(p.items)
	}
	visible := p.items[p.firstVisible:end]

	var b strings.Builder
	rowWidth := p.width - 2
	if rowWidth < 10 {
		rowWidth = 10
	}
	for i, m := range visible {
		globalIdx := p.firstVisible + i
		row := ansi.Truncate(p.row(m), rowWidth, "…")
		if globalIdx == p.cursor {
			b.WriteString(listCursorStyle.Render("▶ " + row))
		} else {
			b.WriteString("  " + row)
		}
		if i < len(visible)-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (p *BookmarksPane) row(m session.Meta) string {
	tRel := humanDelta(p.starredAt[m.ID])
	id := m.ID
	if len(id) > 8 {
		id = id[:8]
	}
	proj := "-"
	if m.CWD != "" {
		proj = filepath.Base(m.CWD)
	}
	if len(proj) > 14 {
		proj = proj[:13] + "…"
	}
	prompt := flattenPrompt(m.FirstPrompt)
	if prompt == "" {
		prompt = listDimStyle.Render("(not loaded)")
	}
	return fmt.Sprintf("%3s  %-8s  %-14s  %s", tRel, id, proj, prompt)
}

func (p *BookmarksPane) sortItems() {
	sort.SliceStable(p.items, func(i, j int) bool {
		return p.starredAt[p.items[i].ID].After(p.starredAt[p.items[j].ID])
	})
}

func (p *BookmarksPane) rebuildIndex() {
	p.idIndex = make(map[string]int, len(p.items))
	for i, m := range p.items {
		p.idIndex[m.ID] = i
	}
}

func (p *BookmarksPane) ensureVisible() {
	height := p.height
	if height <= 0 {
		height = 1
	}
	if p.cursor < p.firstVisible {
		p.firstVisible = p.cursor
	}
	if p.cursor >= p.firstVisible+height {
		p.firstVisible = p.cursor - height + 1
	}
	if p.firstVisible < 0 {
		p.firstVisible = 0
	}
	maxFirst := len(p.items) - height
	if maxFirst < 0 {
		maxFirst = 0
	}
	if p.firstVisible > maxFirst {
		p.firstVisible = maxFirst
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -run BookmarksPane -v`
Expected: PASS for all four tests (+ the earlier empty-state test).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/bookmarks.go internal/ui/bookmarks_test.go
git commit -m "ui: render BookmarksPane rows with cursor + sort"
```

---

## Task 4: `BookmarksPane.Add` / `Remove` / `ReplaceItem`

**Files:**
- Modify: `internal/ui/bookmarks.go`
- Modify: `internal/ui/bookmarks_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/bookmarks_test.go`:

```go
func TestBookmarksPane_AddInsertsAtSortedPosition(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	p.SetItems(
		[]session.Meta{{ID: "old"}, {ID: "mid"}},
		map[string]time.Time{"old": time.Unix(100, 0), "mid": time.Unix(200, 0)},
	)
	p.Add(session.Meta{ID: "new"}, time.Unix(300, 0))
	got := p.Items()
	want := []string{"new", "mid", "old"}
	for i, id := range want {
		if got[i].ID != id {
			t.Errorf("items[%d] = %q; want %q", i, got[i].ID, id)
		}
	}
}

func TestBookmarksPane_AddPreservesCursorByID(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	p.SetItems(
		[]session.Meta{{ID: "a"}, {ID: "b"}},
		map[string]time.Time{"a": time.Unix(200, 0), "b": time.Unix(100, 0)},
	)
	// Cursor on "b" (index 1).
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if sel, _ := p.Selected(); sel.ID != "b" {
		t.Fatalf("precondition: selected = %q; want b", sel.ID)
	}
	// Add newer bookmark — goes to index 0, "b" shifts to index 2.
	p.Add(session.Meta{ID: "c"}, time.Unix(300, 0))
	if sel, _ := p.Selected(); sel.ID != "b" {
		t.Errorf("after Add: selected = %q; want b", sel.ID)
	}
}

func TestBookmarksPane_RemoveDropsByID(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	p.SetItems(
		[]session.Meta{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		map[string]time.Time{
			"a": time.Unix(300, 0), "b": time.Unix(200, 0), "c": time.Unix(100, 0),
		},
	)
	p.Remove("b")
	got := p.Items()
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Errorf("after Remove(b) = %+v; want [a, c]", idsOf(got))
	}
	if _, ok := p.IDIndex()["b"]; ok {
		t.Error("idIndex still has 'b' after Remove")
	}
}

func TestBookmarksPane_RemoveAdjustsCursor(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	p.SetItems(
		[]session.Meta{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		map[string]time.Time{
			"a": time.Unix(300, 0), "b": time.Unix(200, 0), "c": time.Unix(100, 0),
		},
	)
	// Cursor on "c" (index 2).
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	// Remove "a" — cursor should move to index 1 (still "c").
	p.Remove("a")
	if sel, _ := p.Selected(); sel.ID != "c" {
		t.Errorf("selected after Remove(a) = %q; want c", sel.ID)
	}
}

func TestBookmarksPane_ReplaceItemUpdatesInPlace(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	p.SetItems(
		[]session.Meta{{ID: "a"}},
		map[string]time.Time{"a": time.Unix(100, 0)},
	)
	p.ReplaceItem(session.Meta{ID: "a", FirstPrompt: "updated", Enriched: true})
	got := p.Items()
	if got[0].FirstPrompt != "updated" {
		t.Errorf("FirstPrompt = %q; want updated", got[0].FirstPrompt)
	}
}

func idsOf(metas []session.Meta) []string {
	out := make([]string, len(metas))
	for i, m := range metas {
		out[i] = m.ID
	}
	return out
}
```

Note: `IDIndex()` accessor is test-only — add it to the pane.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run BookmarksPane -v`
Expected: FAIL — `Add undefined`, `Remove undefined`, `ReplaceItem undefined`, `IDIndex undefined`.

- [ ] **Step 3: Add methods to `internal/ui/bookmarks.go`**

Add these methods (anywhere after `rebuildIndex`):

```go
// Add inserts or replaces a bookmark entry. Cursor tracks the selected
// session by ID across the resulting re-sort.
func (p *BookmarksPane) Add(m session.Meta, starredAt time.Time) {
	selID := ""
	if sel, ok := p.Selected(); ok {
		selID = sel.ID
	}
	if i, exists := p.idIndex[m.ID]; exists {
		p.items[i] = m
	} else {
		p.items = append(p.items, m)
	}
	p.starredAt[m.ID] = starredAt
	p.sortItems()
	p.rebuildIndex()
	if i, ok := p.idIndex[selID]; ok {
		p.cursor = i
	}
	p.ensureVisible()
}

// Remove drops the entry by ID. Cursor stays on the same session (if
// still present) or clamps into the remaining range.
func (p *BookmarksPane) Remove(id string) {
	i, ok := p.idIndex[id]
	if !ok {
		return
	}
	selID := ""
	if sel, ok := p.Selected(); ok && sel.ID != id {
		selID = sel.ID
	}
	p.items = append(p.items[:i], p.items[i+1:]...)
	delete(p.starredAt, id)
	p.rebuildIndex()
	if selID != "" {
		if j, ok := p.idIndex[selID]; ok {
			p.cursor = j
		}
	} else if p.cursor >= len(p.items) {
		if len(p.items) == 0 {
			p.cursor = 0
		} else {
			p.cursor = len(p.items) - 1
		}
	}
	p.ensureVisible()
}

// ReplaceItem updates the Meta for an existing bookmarked ID in place
// without changing its position. Used by BookmarkEnrichMsg.
func (p *BookmarksPane) ReplaceItem(m session.Meta) {
	if i, ok := p.idIndex[m.ID]; ok && i < len(p.items) {
		p.items[i] = m
	}
}

// IDIndex exposes the id→index map for tests. Callers must not mutate.
func (p *BookmarksPane) IDIndex() map[string]int { return p.idIndex }
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -run BookmarksPane -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/bookmarks.go internal/ui/bookmarks_test.go
git commit -m "ui: BookmarksPane Add/Remove/ReplaceItem with cursor-by-id"
```

---

## Task 5: `BookmarksPane` renders `(removed)` stubs

**Files:**
- Modify: `internal/ui/bookmarks.go`
- Modify: `internal/ui/bookmarks_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/bookmarks_test.go`:

```go
import "csess/internal/session"  // may already be imported

func TestBookmarksPane_MissingFileRendersRemovedMarker(t *testing.T) {
	p := NewBookmarksPane(120, 10)
	p.SetItems(
		[]session.Meta{{ID: "gone", LoadErr: session.ErrMissing}},
		map[string]time.Time{"gone": time.Unix(100, 0)},
	)
	v := p.View()
	if !strings.Contains(v, "(removed)") {
		t.Errorf("missing-file row should show (removed): %q", v)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run BookmarksPane_Missing -v`
Expected: FAIL — "(removed)" not present in output.

- [ ] **Step 3: Update `row` in `internal/ui/bookmarks.go`**

Replace the `row` method with:

```go
func (p *BookmarksPane) row(m session.Meta) string {
	tRel := humanDelta(p.starredAt[m.ID])
	id := m.ID
	if len(id) > 8 {
		id = id[:8]
	}
	if errors.Is(m.LoadErr, session.ErrMissing) {
		return listDimStyle.Render(fmt.Sprintf("%3s  %-8s  (removed)", tRel, id))
	}
	proj := "-"
	if m.CWD != "" {
		proj = filepath.Base(m.CWD)
	}
	if len(proj) > 14 {
		proj = proj[:13] + "…"
	}
	prompt := flattenPrompt(m.FirstPrompt)
	if prompt == "" {
		prompt = listDimStyle.Render("(not loaded)")
	}
	return fmt.Sprintf("%3s  %-8s  %-14s  %s", tRel, id, proj, prompt)
}
```

Add `"errors"` to the import block.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -run BookmarksPane -v`
Expected: PASS for all BookmarksPane tests.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/bookmarks.go internal/ui/bookmarks_test.go
git commit -m "ui: render (removed) marker on missing-file bookmark rows"
```

---

## Task 6: `App` — focus enum, Ctrl-J / Ctrl-K

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/app_test.go`:

```go
func TestApp_CtrlKSwitchesFocusToBookmarks(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	// Manually seed a bookmark so the pane is non-empty.
	app.bookmarks.SetItems(
		[]session.Meta{{ID: "bm1"}},
		map[string]time.Time{"bm1": time.Unix(1, 0)},
	)

	app.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if app.focus != focusBookmarks {
		t.Errorf("focus after Ctrl-K = %d; want focusBookmarks", app.focus)
	}
	// j should now drive the bookmarks pane, not the list.
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if app.list.Cursor() != 0 {
		t.Errorf("list cursor moved despite focusBookmarks: %d", app.list.Cursor())
	}
}

func TestApp_CtrlKEmptyBookmarksIsNoop(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	// bookmarks pane is empty by default.
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if app.focus != focusList {
		t.Errorf("focus after Ctrl-K with empty bookmarks = %d; want focusList", app.focus)
	}
}

func TestApp_CtrlJReturnsFocusToList(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	app.bookmarks.SetItems(
		[]session.Meta{{ID: "bm1"}},
		map[string]time.Time{"bm1": time.Unix(1, 0)},
	)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	if app.focus != focusList {
		t.Errorf("focus after Ctrl-J = %d; want focusList", app.focus)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestApp_Ctrl' -v`
Expected: FAIL — `app.focus undefined`, `app.bookmarks undefined`, etc.

- [ ] **Step 3: Add focus type + field + Ctrl-J/K handler in `internal/ui/app.go`**

Near the existing `mode` type block, add:

```go
type focus int

const (
	focusList focus = iota
	focusBookmarks
)
```

Extend the `App` struct (add to existing struct):

```go
	bookmarks   *BookmarksPane
	bookmarkIDs map[string]time.Time
	focus       focus
	prevFocus   focus
```

Update `NewApp` to initialize the pane (add before `app := &App{...}`):

```go
	bookmarks := NewBookmarksPane(cfg.Width, 5)
```

And in the `&App{...}` literal, include:

```go
		bookmarks:   bookmarks,
		bookmarkIDs: map[string]time.Time{},
```

In the `modeNormal` branch of `handleKey`, add a case before the existing `"q"` case:

```go
	case "ctrl+k":
		if len(a.bookmarks.Items()) == 0 {
			return a, nil
		}
		a.focus = focusBookmarks
		a.updatePreviewFromFocus()
		return a, a.loadTranscriptForFocus()
	case "ctrl+j":
		a.focus = focusList
		a.updatePreviewFromFocus()
		return a, a.loadTranscriptForFocus()
```

Add two helpers at the bottom of `app.go`:

```go
// focusedSelection returns the cursor's selected Meta from whichever
// pane currently has focus.
func (a *App) focusedSelection() (session.Meta, bool) {
	switch a.focus {
	case focusBookmarks:
		return a.bookmarks.Selected()
	default:
		return a.list.Selected()
	}
}

func (a *App) updatePreviewFromFocus() {
	sel, ok := a.focusedSelection()
	if !ok {
		a.preview.SetMeta(session.Meta{})
		return
	}
	a.preview.SetMeta(sel)
}

func (a *App) loadTranscriptForFocus() tea.Cmd {
	sel, ok := a.focusedSelection()
	if !ok {
		return nil
	}
	return a.loadTranscript(sel)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -run 'TestApp_Ctrl' -v`
Expected: PASS.

- [ ] **Step 5: Run the full UI suite to catch regressions**

Run: `go test ./internal/ui/ -v`
Expected: PASS across the board (existing tests untouched).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "ui: add focus enum and Ctrl-J/Ctrl-K pane switching"
```

---

## Task 7: `App` — route navigation keys by focus

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/app_test.go`:

```go
func TestApp_JInFocusBookmarksMovesBookmarkCursor(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	app.bookmarks.SetItems(
		[]session.Meta{{ID: "bm1"}, {ID: "bm2"}},
		map[string]time.Time{"bm1": time.Unix(200, 0), "bm2": time.Unix(100, 0)},
	)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if app.bookmarks.Cursor() != 1 {
		t.Errorf("bookmarks cursor after j = %d; want 1", app.bookmarks.Cursor())
	}
	if app.list.Cursor() != 0 {
		t.Errorf("list cursor changed despite focusBookmarks: %d", app.list.Cursor())
	}
}

func TestApp_EnterInFocusBookmarksResumesBookmarked(t *testing.T) {
	var resumed session.Meta
	app := NewApp(AppConfig{
		Width: 120, Height: 40, LoadTranscript: stubLoad,
		ResumeSelected: func(m session.Meta) tea.Cmd {
			resumed = m
			return func() tea.Msg { return nil }
		},
	})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	app.bookmarks.SetItems(
		[]session.Meta{{ID: "bm1", Enriched: true, CWD: "/work"}},
		map[string]time.Time{"bm1": time.Unix(1, 0)},
	)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter in focusBookmarks should return a resume cmd")
	}
	cmd()
	if resumed.ID != "bm1" {
		t.Errorf("resumed.ID = %q; want bm1", resumed.ID)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestApp_JInFocus|TestApp_EnterInFocusBookmarks' -v`
Expected: FAIL — j moves list cursor (not bookmarks), Enter resumes list's selection.

- [ ] **Step 3: Update `handleKey` in `internal/ui/app.go`**

In the `modeNormal` branch, change the cursor-movement and action cases so they dispatch by focus.

Replace the existing case:
```go
	case "j", "down", "ctrl+n", "k", "up", "ctrl+p", "g", "G", "home", "end":
		a.list.Update(km)
		a.updatePreviewFromSelection()
		return a, a.loadTranscriptForSelection()
```

with:
```go
	case "j", "down", "ctrl+n", "k", "up", "ctrl+p", "g", "G", "home", "end":
		if a.focus == focusBookmarks {
			a.bookmarks.Update(km)
		} else {
			a.list.Update(km)
		}
		a.updatePreviewFromFocus()
		return a, a.loadTranscriptForFocus()
```

Replace the existing `"enter"` case:

```go
	case "enter":
		sel, ok := a.focusedSelection()
		if !ok {
			return a, nil
		}
		if !sel.Enriched || sel.CWD == "" {
			a.banner = "session not ready (enrichment pending)"
			a.bannerExp = time.Now().Add(3 * time.Second)
			return a, nil
		}
		if a.cfg.ResumeSelected != nil {
			return a, a.cfg.ResumeSelected(sel)
		}
		return a, nil
```

Replace the existing `"y"` case (copy-id):

```go
	case "y":
		sel, ok := a.focusedSelection()
		if !ok {
			return a, nil
		}
		if a.cfg.CopySelected != nil {
			return a, a.cfg.CopySelected(sel)
		}
		return a, nil
```

Replace the existing `"d"` case:

```go
	case "d":
		sel, ok := a.focusedSelection()
		if !ok {
			return a, nil
		}
		a.confirm = NewConfirm(fmt.Sprintf("delete session %s?", sel.ID))
		a.mode = modeConfirm
		return a, nil
```

And in the `modeConfirm` branch, replace `sel, ok := a.list.Selected()` with `sel, ok := a.focusedSelection()`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -v`
Expected: PASS across the board.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "ui: route j/k/g/G/Enter/y/d by focused pane"
```

---

## Task 8: `App` — `b` key toggles bookmarks; SaveBookmarks cmd

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/app_test.go`:

```go
func TestApp_BKeyAddsToBookmarks(t *testing.T) {
	var saved []session.Bookmark
	app := NewApp(AppConfig{
		Width: 120, Height: 40, LoadTranscript: stubLoad,
		SaveBookmarks: func(bs []session.Bookmark) tea.Cmd {
			saved = bs
			return func() tea.Msg { return nil }
		},
	})
	app.Update(ScanMsg{Metas: []session.Meta{
		{ID: "a", Enriched: true, CWD: "/work"},
	}})
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if cmd == nil {
		t.Fatal("b should return a SaveBookmarks cmd")
	}
	cmd() // execute to populate `saved`
	if len(saved) != 1 || saved[0].ID != "a" {
		t.Errorf("saved = %+v; want [{ID:a, ...}]", saved)
	}
	if _, ok := app.bookmarkIDs["a"]; !ok {
		t.Error("bookmarkIDs should contain 'a'")
	}
	if len(app.bookmarks.Items()) != 1 {
		t.Errorf("bookmarks pane items = %d; want 1", len(app.bookmarks.Items()))
	}
}

func TestApp_BKeyRemovesExistingBookmark(t *testing.T) {
	var saved []session.Bookmark
	app := NewApp(AppConfig{
		Width: 120, Height: 40, LoadTranscript: stubLoad,
		SaveBookmarks: func(bs []session.Bookmark) tea.Cmd {
			saved = bs
			return func() tea.Msg { return nil }
		},
	})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a", Enriched: true, CWD: "/work"}}})
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}}) // add
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}}) // remove
	if cmd == nil {
		t.Fatal("second b should also return a SaveBookmarks cmd")
	}
	cmd()
	if len(saved) != 0 {
		t.Errorf("saved after toggle off = %+v; want empty", saved)
	}
	if _, ok := app.bookmarkIDs["a"]; ok {
		t.Error("bookmarkIDs should not contain 'a' after toggle off")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestApp_BKey' -v`
Expected: FAIL — `SaveBookmarks undefined`.

- [ ] **Step 3: Extend `AppConfig` and `handleKey` in `internal/ui/app.go`**

Add to `AppConfig`:

```go
	// SaveBookmarks writes the given set to disk and returns a
	// SaveBookmarksDoneMsg when finished. Nil in tests unless the test
	// wants to assert save behavior.
	SaveBookmarks func([]session.Bookmark) tea.Cmd
```

Add messages near the other `Msg` types:

```go
// SaveBookmarksDoneMsg is delivered after the bookmarks file is written.
type SaveBookmarksDoneMsg struct{ Err error }

// BookmarkEnrichMsg delivers a Meta for a bookmark that wasn't in the
// initial session scan (off-scope / different project).
type BookmarkEnrichMsg struct{ Meta session.Meta }
```

Handle `SaveBookmarksDoneMsg` in `Update`:

```go
	case SaveBookmarksDoneMsg:
		if m.Err != nil {
			a.banner = "bookmark save failed: " + m.Err.Error()
			a.bannerExp = time.Now().Add(5 * time.Second)
		}
		return a, nil
```

In `handleKey` under `modeNormal`, add a new case:

```go
	case "b":
		sel, ok := a.focusedSelection()
		if !ok || sel.ID == "" {
			return a, nil
		}
		if _, existed := a.bookmarkIDs[sel.ID]; existed {
			delete(a.bookmarkIDs, sel.ID)
			a.bookmarks.Remove(sel.ID)
		} else {
			now := time.Now()
			a.bookmarkIDs[sel.ID] = now
			a.bookmarks.Add(sel, now)
		}
		return a, a.saveBookmarksCmd()
```

Add helper at the bottom of `app.go`:

```go
// saveBookmarksCmd builds the ordered []Bookmark snapshot and delegates
// to the injected SaveBookmarks cmd (no-op if unset).
func (a *App) saveBookmarksCmd() tea.Cmd {
	if a.cfg.SaveBookmarks == nil {
		return nil
	}
	bs := make([]session.Bookmark, 0, len(a.bookmarkIDs))
	for id, t := range a.bookmarkIDs {
		bs = append(bs, session.Bookmark{ID: id, StarredAt: t})
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].StarredAt.After(bs[j].StarredAt) })
	return a.cfg.SaveBookmarks(bs)
}
```

Add `"sort"` to the imports.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "ui: b key toggles bookmark with SaveBookmarks cmd"
```

---

## Task 9: `ScanMsg` seeds bookmarks pane; `BookmarkEnrichMsg` updates stubs

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/ui/app_test.go`:

```go
func TestApp_ScanMsgSeedsBookmarksPane(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	// Pre-load bookmarkIDs as main.go would after reading the file.
	app.bookmarkIDs["a"] = time.Unix(200, 0)
	app.bookmarkIDs["offscope"] = time.Unix(100, 0)
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a", Enriched: true, CWD: "/work"}}})
	items := app.bookmarks.Items()
	if len(items) != 2 {
		t.Fatalf("bookmarks items = %d; want 2", len(items))
	}
	if items[0].ID != "a" { // newer StarredAt first
		t.Errorf("items[0] = %q; want a", items[0].ID)
	}
	if items[1].ID != "offscope" || items[1].Enriched {
		t.Errorf("items[1] = %+v; want stub for offscope", items[1])
	}
}

func TestApp_BookmarkEnrichMsgReplacesStub(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.bookmarkIDs["x"] = time.Unix(1, 0)
	app.Update(ScanMsg{Metas: nil})
	if !strings.Contains(app.bookmarks.View(), "(not loaded)") && !strings.Contains(app.bookmarks.View(), "x") {
		t.Fatalf("precondition: bookmarks view should show stub for x: %q", app.bookmarks.View())
	}
	app.Update(BookmarkEnrichMsg{Meta: session.Meta{
		ID: "x", FirstPrompt: "filled in", Enriched: true, CWD: "/other/proj",
	}})
	items := app.bookmarks.Items()
	if len(items) != 1 || items[0].FirstPrompt != "filled in" {
		t.Errorf("after enrich: items[0] = %+v; want FirstPrompt=filled in", items[0])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestApp_ScanMsgSeeds|TestApp_BookmarkEnrich' -v`
Expected: FAIL — `BookmarkEnrichMsg` not handled; bookmarks pane empty after ScanMsg.

- [ ] **Step 3: Extend `ScanMsg` and add `BookmarkEnrichMsg` handler in `internal/ui/app.go`**

Replace the existing `ScanMsg` case in `Update`:

```go
	case ScanMsg:
		a.allItems = m.Metas
		a.allIndex = make(map[string]int, len(m.Metas))
		for i, it := range m.Metas {
			a.allIndex[it.ID] = i
		}
		a.list.SetItems(m.Metas)
		a.seedBookmarks()
		a.updatePreviewFromFocus()
		return a, a.loadTranscriptForFocus()
```

Add `BookmarkEnrichMsg` case after the `EnrichDoneMsg` case:

```go
	case BookmarkEnrichMsg:
		a.bookmarks.ReplaceItem(m.Meta)
		if sel, ok := a.bookmarks.Selected(); ok && sel.ID == m.Meta.ID && a.focus == focusBookmarks {
			a.preview.UpdateMeta(m.Meta)
		}
		return a, nil
```

Add helper at the bottom:

```go
// seedBookmarks populates the bookmarks pane from a.bookmarkIDs after a
// ScanMsg. Known ids (in allIndex) get their full Meta; off-scope ids
// get a stub Meta{ID:id} that BookmarkEnrichMsg will later replace.
func (a *App) seedBookmarks() {
	items := make([]session.Meta, 0, len(a.bookmarkIDs))
	sa := make(map[string]time.Time, len(a.bookmarkIDs))
	for id, t := range a.bookmarkIDs {
		if i, ok := a.allIndex[id]; ok {
			items = append(items, a.allItems[i])
		} else {
			items = append(items, session.Meta{ID: id})
		}
		sa[id] = t
	}
	a.bookmarks.SetItems(items, sa)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "ui: seed bookmarks pane on ScanMsg; handle BookmarkEnrichMsg"
```

---

## Task 10: Delete auto-unbookmarks

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/app_test.go`:

```go
func TestApp_DeleteBookmarkedSessionAutoUnbookmarks(t *testing.T) {
	var savedAfter []session.Bookmark
	app := NewApp(AppConfig{
		Width: 120, Height: 40, LoadTranscript: stubLoad,
		SaveBookmarks: func(bs []session.Bookmark) tea.Cmd {
			savedAfter = bs
			return func() tea.Msg { return nil }
		},
	})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a", Enriched: true, CWD: "/w"}}})
	// Bookmark "a".
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	// Clear saved slot so we only see the post-delete save.
	savedAfter = nil
	_, cmd := app.Update(DeleteDoneMsg{ID: "a"})
	if cmd == nil {
		t.Fatal("DeleteDoneMsg for bookmarked id should return a SaveBookmarks cmd")
	}
	cmd()
	if _, ok := app.bookmarkIDs["a"]; ok {
		t.Error("bookmarkIDs still contains 'a' after delete")
	}
	if len(app.bookmarks.Items()) != 0 {
		t.Errorf("bookmarks items = %d; want 0", len(app.bookmarks.Items()))
	}
	if len(savedAfter) != 0 {
		t.Errorf("savedAfter = %+v; want empty", savedAfter)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestApp_DeleteBookmarkedSession -v`
Expected: FAIL — bookmark survives delete.

- [ ] **Step 3: Extend `DeleteDoneMsg` handler in `internal/ui/app.go`**

Locate the existing `DeleteDoneMsg` case. After the block that removes the session from `allItems`/`allIndex` and before `a.applyFilter()`, add:

```go
		var bookmarkSaveCmd tea.Cmd
		if _, existed := a.bookmarkIDs[m.ID]; existed {
			delete(a.bookmarkIDs, m.ID)
			a.bookmarks.Remove(m.ID)
			bookmarkSaveCmd = a.saveBookmarksCmd()
		}
```

Change `a.updatePreviewFromSelection()` in the DeleteDoneMsg handler to `a.updatePreviewFromFocus()` so the preview reflects whichever pane has focus.

Change the final `return` from:
```go
		return a, a.loadTranscriptForSelection()
```
to:
```go
		return a, tea.Batch(a.loadTranscriptForFocus(), bookmarkSaveCmd)
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "ui: auto-unbookmark when a bookmarked session is trashed"
```

---

## Task 11: Search mode hides bookmarks pane

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/app_test.go`:

```go
func TestApp_SearchHidesBookmarksPane(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	app.bookmarks.SetItems(
		[]session.Meta{{ID: "bm1", FirstPrompt: "uniqueBookmarkText"}},
		map[string]time.Time{"bm1": time.Unix(1, 0)},
	)
	// Confirm bookmark visible pre-search.
	if !strings.Contains(app.View(), "uniqueBookmarkText") {
		t.Fatal("precondition: bookmark row should be visible before /")
	}
	// Enter search.
	m, _ := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if strings.Contains(m.View(), "uniqueBookmarkText") {
		t.Error("bookmarks pane should be hidden during search mode")
	}
	// Esc exits search.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !strings.Contains(m.View(), "uniqueBookmarkText") {
		t.Error("bookmarks pane should reappear after Esc")
	}
}

func TestApp_SearchPreservesPriorFocus(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	app.bookmarks.SetItems(
		[]session.Meta{{ID: "bm1"}},
		map[string]time.Time{"bm1": time.Unix(1, 0)},
	)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if app.focus != focusBookmarks {
		t.Fatalf("precondition: focus = %d; want focusBookmarks", app.focus)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if app.focus != focusList {
		t.Errorf("during search: focus = %d; want focusList", app.focus)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if app.focus != focusBookmarks {
		t.Errorf("after esc: focus = %d; want focusBookmarks (restored)", app.focus)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestApp_SearchHidesBookmarks|TestApp_SearchPreservesPriorFocus' -v`
Expected: FAIL — bookmarks pane still rendered during search; focus not saved.

- [ ] **Step 3: Update `/` handler, `esc` handler, and `View()` in `internal/ui/app.go`**

In the `modeNormal` branch of `handleKey`, replace the `"/"` case:

```go
	case "/":
		a.prevFocus = a.focus
		a.focus = focusList
		a.mode = modeSearch
		return a, a.search.Focus()
```

In the `modeSearch` branch of `handleKey`, inside the `"esc"` case, add at the end (after `a.applyFilter()`):

```go
		a.focus = a.prevFocus
```

Replace the `View()` method body to skip the bookmarks pane during search:

```go
func (a *App) View() string {
	var left string
	if a.showMatches {
		left = a.matchList.View()
	} else {
		left = a.list.View()
	}
	right := a.preview.View()
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, lipgloss.NewStyle().Padding(0, 1).Render("│"), right)

	status := a.statusLine()
	if a.mode == modeSearch {
		return body + "\n" + status
	}
	top := a.bookmarks.View()
	return lipgloss.JoinVertical(lipgloss.Left, top, body, status)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "ui: hide bookmarks pane during search mode; stack vertically otherwise"
```

---

## Task 12: Enter on `(removed)` stub shows banner, skips resume

**Files:**
- Modify: `internal/ui/app.go`
- Modify: `internal/ui/app_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/app_test.go`:

```go
func TestApp_EnterOnRemovedStubBannersAndSkipsResume(t *testing.T) {
	var resumed session.Meta
	app := NewApp(AppConfig{
		Width: 120, Height: 40, LoadTranscript: stubLoad,
		ResumeSelected: func(m session.Meta) tea.Cmd {
			resumed = m
			return func() tea.Msg { return nil }
		},
	})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	app.bookmarks.SetItems(
		[]session.Meta{{ID: "gone", LoadErr: session.ErrMissing}},
		map[string]time.Time{"gone": time.Unix(1, 0)},
	)
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("Enter on removed stub should not return a resume cmd")
	}
	if resumed.ID != "" {
		t.Errorf("resumed.ID = %q; want empty (not called)", resumed.ID)
	}
	if !strings.Contains(app.banner, "session file missing") {
		t.Errorf("banner = %q; want contains 'session file missing'", app.banner)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestApp_EnterOnRemoved -v`
Expected: FAIL — Enter falls through to the existing "session not ready" path or calls ResumeSelected.

- [ ] **Step 3: Update `"enter"` case in `handleKey` in `internal/ui/app.go`**

Replace the `"enter"` case (added in Task 7):

```go
	case "enter":
		sel, ok := a.focusedSelection()
		if !ok {
			return a, nil
		}
		if errors.Is(sel.LoadErr, session.ErrMissing) {
			a.banner = "session file missing — press b to unbookmark"
			a.bannerExp = time.Now().Add(4 * time.Second)
			return a, nil
		}
		if !sel.Enriched || sel.CWD == "" {
			a.banner = "session not ready (enrichment pending)"
			a.bannerExp = time.Now().Add(3 * time.Second)
			return a, nil
		}
		if a.cfg.ResumeSelected != nil {
			return a, a.cfg.ResumeSelected(sel)
		}
		return a, nil
```

Add `"errors"` to the imports if not already present.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/ui/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "ui: banner on Enter for removed bookmarked session; skip resume"
```

---

## Task 13: `main.go` wiring — flag, store, save cmd, off-scope bridge

**Files:**
- Modify: `cmd/csess/main.go`

- [ ] **Step 1: Add the flag and load bookmarks**

Near the existing flag block (top of `main`), add:

```go
		bookmarksFile = flag.String("bookmarks-file", "", "override ~/.claude/csess/bookmarks.json")
```

After the block that defaults `*trashDir`, add:

```go
	if *bookmarksFile == "" {
		*bookmarksFile = filepath.Join(home, ".claude", "csess", "bookmarks.json")
	}
	bookmarkStore := session.NewBookmarkStore(*bookmarksFile)
	bookmarks, err := bookmarkStore.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bookmarks load: %v (continuing with empty set)\n", err)
		bookmarks = nil
	}
```

- [ ] **Step 2: Seed `bookmarkIDs` on the app**

After `app := ui.NewApp(cfg)`, add:

```go
	for _, b := range bookmarks {
		app.SetBookmark(b.ID, b.StarredAt)
	}
```

Add the helper in `internal/ui/app.go` (exposed so `main.go` can seed without reaching into internals):

```go
// SetBookmark seeds a bookmark from the persisted store during startup.
// Does not trigger Save (nothing has changed on disk). Callers must call
// it BEFORE the initial ScanMsg is dispatched.
func (a *App) SetBookmark(id string, starredAt time.Time) {
	a.bookmarkIDs[id] = starredAt
}
```

- [ ] **Step 3: Wire the save cmd**

In the `AppConfig` construction in `main.go`, add:

```go
		SaveBookmarks: buildSaveBookmarks(bookmarkStore),
```

Add the factory:

```go
func buildSaveBookmarks(store *session.BookmarkStore) func([]session.Bookmark) tea.Cmd {
	return func(bs []session.Bookmark) tea.Cmd {
		return func() tea.Msg {
			return ui.SaveBookmarksDoneMsg{Err: store.Save(bs)}
		}
	}
}
```

- [ ] **Step 4: Bridge off-scope enrichment**

After the existing `go bridgeEnrich(p, scanner, metas)` line, add:

```go
	go bridgeBookmarkEnrich(p, *projectsDir, bookmarks, metas)
```

Add the function:

```go
// bridgeBookmarkEnrich enriches any bookmarked id that isn't already in
// the main scan (off-scope — different project than the current scope).
// It walks project dirs under projectsDir looking for <id>.jsonl; on
// hit, it runs ExtractMeta and sends BookmarkEnrichMsg; on miss, it
// sends a stub with LoadErr=ErrMissing so the UI can mark the row.
func bridgeBookmarkEnrich(p *tea.Program, projectsDir string, bookmarks []session.Bookmark, scanned []session.Meta) {
	if len(bookmarks) == 0 {
		return
	}
	inScope := make(map[string]struct{}, len(scanned))
	for _, m := range scanned {
		inScope[m.ID] = struct{}{}
	}
	// Pre-walk all project dirs to build id -> path.
	paths := map[string]string{}
	projEntries, err := os.ReadDir(projectsDir)
	if err == nil {
		for _, pe := range projEntries {
			if !pe.IsDir() {
				continue
			}
			dir := filepath.Join(projectsDir, pe.Name())
			files, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, f := range files {
				if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
					continue
				}
				id := strings.TrimSuffix(f.Name(), ".jsonl")
				paths[id] = filepath.Join(dir, f.Name())
			}
		}
	}
	for _, b := range bookmarks {
		if _, ok := inScope[b.ID]; ok {
			continue // ScanMsg already supplied full Meta
		}
		path, ok := paths[b.ID]
		if !ok {
			p.Send(ui.BookmarkEnrichMsg{Meta: session.Meta{ID: b.ID, LoadErr: session.ErrMissing}})
			continue
		}
		meta := session.Meta{ID: b.ID, Path: path}
		f, err := os.Open(path)
		if err != nil {
			p.Send(ui.BookmarkEnrichMsg{Meta: session.Meta{ID: b.ID, LoadErr: session.ErrMissing}})
			continue
		}
		if err := session.ExtractMeta(f, &meta); err != nil && meta.LoadErr == nil {
			meta.LoadErr = err
		}
		f.Close()
		p.Send(ui.BookmarkEnrichMsg{Meta: meta})
	}
}
```

Add `"strings"` to imports if not already present.

- [ ] **Step 5: Build and run smoke check**

Run:
```bash
go build ./...
go test ./...
```
Expected: PASS across all packages.

- [ ] **Step 6: Manual smoke test**

Run `./csess` (or `go run ./cmd/csess`). With no prior bookmarks file, the bookmark pane renders `no bookmarks — press b to add`. Press `b` on a session; pane shows one entry. Press `b` again; pane empties, still padded to width. Press `Ctrl-K` to focus bookmarks; `j`/`k`/`Enter`/`y`/`d` operate on bookmark rows. Press `Ctrl-J` to return to list. Press `/`; bookmark pane hides. Press `Esc`; it reappears.

Verify `~/.claude/csess/bookmarks.json` exists and is valid JSON after a toggle.

- [ ] **Step 7: Commit**

```bash
git add cmd/csess/main.go internal/ui/app.go
git commit -m "main: wire BookmarkStore, save cmd, and off-scope enrichment bridge"
```

---

## Task 14: README note + status line hint

**Files:**
- Modify: `internal/ui/app.go` (status line)
- Modify: `README.md`

- [ ] **Step 1: Update status line hint**

In `statusLine()` in `internal/ui/app.go`, locate the `modeNormal` branch and update the help string:

Find:
```go
		parts = append(parts, "[/] filter  [Enter] resume  [y] copy id  [d] delete  [q] quit")
```

Replace with:
```go
		parts = append(parts, "[/] filter  [b] bookmark  [^K/^J] focus  [Enter] resume  [y] copy id  [d] delete  [q] quit")
```

- [ ] **Step 2: Add a bookmarks section to the README**

Open `README.md`. If it has a "Keys" or "Usage" section, append a paragraph like:

```markdown
### Bookmarks

Press `b` on any session to bookmark it. Bookmarked sessions appear in
a pane above the list, sorted by when you bookmarked them. Bookmarks
persist in `~/.claude/csess/bookmarks.json` (overridable via
`--bookmarks-file`) and are global — you'll see them regardless of
whether csess is running with `--here` or across all projects.

`Ctrl-K` / `Ctrl-J` moves keyboard focus between the bookmarks pane
and the session list. `Enter`, `y`, and `d` all operate on whichever
pane has focus. Deleting a bookmarked session auto-unbookmarks it.
```

If no such section exists, create a top-level `## Usage` or `## Keys`
section first.

- [ ] **Step 3: Build + test + commit**

Run:
```bash
go build ./...
go test ./...
```
Expected: PASS.

```bash
git add internal/ui/app.go README.md
git commit -m "docs: document b / ^K / ^J bookmark keys"
```

---

## Post-Implementation Sanity

After all tasks land:

- `go test ./...` passes with no skipped tests.
- `go build ./...` produces a working binary.
- Manual checks from Task 13 Step 6 pass.
- `~/.claude/csess/bookmarks.json` format matches the spec's example.
- No new TODOs or placeholder comments in production code.
