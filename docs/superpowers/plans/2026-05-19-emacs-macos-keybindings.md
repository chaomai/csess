# Emacs / macOS Keybindings Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `C-v` / `M-v` page-by-page navigation to the focused list pane and `Tab` / `Shift-Tab` pane-focus cycling, alongside the existing vim aliases. Extend README accordingly.

**Architecture:** Each list-style pane (`*List`, `*BookmarksPane`, `*MatchList`) gets two new cases in its own `Update` switch — `ctrl+v` advances the cursor by `pageStep = max(1, height-2)` rows, `alt+v` retreats by the same. `internal/ui/app.go` extends two existing dispatch branches (modeNormal list-movement and modeSearch list-movement) to forward those keys, and adds a `tab`/`shift+tab` case that flips focus to the *other* pane (cycle semantics — the two keys are siblings, not antonyms, while there are exactly two focusable panes). In modeSearch, `tab`/`shift+tab` are explicitly consumed as no-ops so they don't leak into the search input. README's Keys table picks up the four new bindings; a new "macOS: Option-as-Meta" subsection notes the terminal setting required for `M-v`.

**Tech Stack:** Go 1.22, bubbletea TUI framework. Tests use the table-driven style already present in `internal/ui/*_test.go`. Build/test via `make test` and `make build`.

**Reference spec:** `docs/specs/2026-05-19-emacs-macos-keybindings-design.md`.

---

## Files

- Modify: `internal/ui/list.go:124-144` — extend `(*List).Update` switch with `ctrl+v` / `alt+v`.
- Modify: `internal/ui/list_test.go` — append page-step tests.
- Modify: `internal/ui/bookmarks.go:93-112` — extend `(*BookmarksPane).Update` switch with `ctrl+v` / `alt+v`.
- Modify: `internal/ui/bookmarks_test.go` — append page-step tests.
- Modify: `internal/ui/matches.go:60-79` — extend `(*MatchList).Update` switch with `ctrl+v` / `alt+v`.
- Modify: `internal/ui/matches_test.go` — append page-step tests.
- Modify: `internal/ui/app.go:432-528` (modeNormal) and `:345-408` (modeSearch) — add `ctrl+v`/`alt+v` to the list-movement dispatch branches; add `tab`/`shift+tab` cycle in modeNormal; add no-op `tab`/`shift+tab` in modeSearch.
- Modify: `internal/ui/app_test.go` — append Tab cycle tests, search-mode no-op test, search-mode `C-v` paging test.
- Modify: `README.md:50-63` — extend Keys table; add macOS Option-as-Meta subsection after the table.

---

## Task 1: List paging — `C-v` / `M-v`

**Files:**
- Modify: `internal/ui/list.go:124-144`
- Test: `internal/ui/list_test.go` (append at end of file)

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/list_test.go`:

```go
func TestList_CtrlVPagesDown(t *testing.T) {
	items := make([]session.Meta, 20)
	for i := range items {
		items[i] = session.Meta{ID: fmt.Sprintf("s%02d", i), UpdatedAt: time.Unix(int64(100-i), 0), Enriched: true}
	}
	m := NewList(80, 10, false) // visible height = 10 → pageStep = 8
	m.SetItems(items)

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if m.Cursor() != 8 {
		t.Errorf("cursor after ctrl+v = %d; want 8", m.Cursor())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if m.Cursor() != 16 {
		t.Errorf("cursor after second ctrl+v = %d; want 16", m.Cursor())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if m.Cursor() != 19 {
		t.Errorf("cursor at bottom should clamp to len-1; got %d, want 19", m.Cursor())
	}
}

func TestList_AltVPagesUp(t *testing.T) {
	items := make([]session.Meta, 20)
	for i := range items {
		items[i] = session.Meta{ID: fmt.Sprintf("s%02d", i), UpdatedAt: time.Unix(int64(100-i), 0), Enriched: true}
	}
	m := NewList(80, 10, false) // pageStep = 8
	m.SetItems(items)
	m.Update(tea.KeyMsg{Type: tea.KeyEnd}) // cursor = 19

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if m.Cursor() != 11 {
		t.Errorf("cursor after alt+v from bottom = %d; want 11", m.Cursor())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if m.Cursor() != 3 {
		t.Errorf("cursor after second alt+v = %d; want 3", m.Cursor())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if m.Cursor() != 0 {
		t.Errorf("cursor at top should clamp to 0; got %d", m.Cursor())
	}
}

func TestList_PageStepClampsToOneOnTinyPane(t *testing.T) {
	items := make([]session.Meta, 5)
	for i := range items {
		items[i] = session.Meta{ID: fmt.Sprintf("s%d", i), UpdatedAt: time.Unix(int64(100-i), 0), Enriched: true}
	}
	m := NewList(80, 1, false) // pageStep = max(1, 1-2) = 1
	m.SetItems(items)

	m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if m.Cursor() != 1 {
		t.Errorf("ctrl+v on h=1 should advance by 1; got %d", m.Cursor())
	}
}
```

- [ ] **Step 2: Run the new tests and confirm they fail**

Run: `go test ./internal/ui/ -run 'TestList_CtrlVPagesDown|TestList_AltVPagesUp|TestList_PageStepClampsToOneOnTinyPane' -v`

Expected: three FAILs because the cases don't exist in the switch — `Cursor()` stays at 0 (or 19 for the alt+v test) and the assertions fire.

- [ ] **Step 3: Implement `ctrl+v` / `alt+v` in `(*List).Update`**

Edit `internal/ui/list.go`. Replace the `Update` function body's switch block (current lines 126-140) with:

```go
func (l *List) Update(msg tea.Msg) (*List, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "j", "down", "ctrl+n":
			if l.cursor < len(l.items)-1 {
				l.cursor++
			}
		case "k", "up", "ctrl+p":
			if l.cursor > 0 {
				l.cursor--
			}
		case "g", "home":
			l.cursor = 0
		case "G", "end":
			l.cursor = len(l.items) - 1
		case "ctrl+v":
			step := pageStep(l.height)
			l.cursor += step
			if l.cursor > len(l.items)-1 {
				l.cursor = len(l.items) - 1
			}
		case "alt+v":
			step := pageStep(l.height)
			l.cursor -= step
			if l.cursor < 0 {
				l.cursor = 0
			}
		}
		l.ensureVisible()
	}
	return l, nil
}
```

Below the function, add the shared helper (before `View`):

```go
// pageStep returns the cursor step size for C-v / M-v paging. It mirrors
// emacs's next-screen-context-lines = 2 and degrades to 1 on tiny panes.
func pageStep(height int) int {
	if height-2 < 1 {
		return 1
	}
	return height - 2
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/ui/ -run 'TestList_CtrlVPagesDown|TestList_AltVPagesUp|TestList_PageStepClampsToOneOnTinyPane' -v`

Expected: PASS for all three.

- [ ] **Step 5: Run the full ui package tests to confirm nothing regressed**

Run: `go test ./internal/ui/ -race`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/list.go internal/ui/list_test.go
git commit -m "ui: page list cursor with C-v / M-v"
```

---

## Task 2: Bookmarks pane paging — `C-v` / `M-v`

**Files:**
- Modify: `internal/ui/bookmarks.go:93-112`
- Test: `internal/ui/bookmarks_test.go` (append at end of file)

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/bookmarks_test.go`:

```go
func TestBookmarksPane_CtrlVPagesDown(t *testing.T) {
	p := NewBookmarksPane(80, 10) // pageStep = 8
	items := make([]session.Meta, 20)
	sa := map[string]time.Time{}
	for i := range items {
		id := fmt.Sprintf("id%02d", i)
		items[i] = session.Meta{ID: id}
		sa[id] = time.Unix(int64(1000-i), 0) // id00 newest
	}
	p.SetItems(items, sa)

	p.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if p.Cursor() != 8 {
		t.Errorf("cursor after ctrl+v = %d; want 8", p.Cursor())
	}
	p.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	p.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if p.Cursor() != 19 {
		t.Errorf("cursor at bottom should clamp to len-1; got %d, want 19", p.Cursor())
	}
}

func TestBookmarksPane_AltVPagesUp(t *testing.T) {
	p := NewBookmarksPane(80, 10) // pageStep = 8
	items := make([]session.Meta, 20)
	sa := map[string]time.Time{}
	for i := range items {
		id := fmt.Sprintf("id%02d", i)
		items[i] = session.Meta{ID: id}
		sa[id] = time.Unix(int64(1000-i), 0)
	}
	p.SetItems(items, sa)
	p.Update(tea.KeyMsg{Type: tea.KeyEnd}) // cursor = 19

	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if p.Cursor() != 11 {
		t.Errorf("cursor after alt+v from bottom = %d; want 11", p.Cursor())
	}
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if p.Cursor() != 0 {
		t.Errorf("cursor at top should clamp to 0; got %d", p.Cursor())
	}
}
```

- [ ] **Step 2: Run the new tests and confirm they fail**

Run: `go test ./internal/ui/ -run 'TestBookmarksPane_CtrlVPagesDown|TestBookmarksPane_AltVPagesUp' -v`

Expected: FAIL — cursor stays at 0 (or 19) because the cases don't exist.

- [ ] **Step 3: Implement `ctrl+v` / `alt+v` in `(*BookmarksPane).Update`**

Edit `internal/ui/bookmarks.go`. Replace the body of `Update` (lines 93-112) with:

```go
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
			p.cursor = maxInt(0, len(p.items)-1)
		case "ctrl+v":
			step := pageStep(p.height)
			p.cursor = minInt(p.cursor+step, maxInt(0, len(p.items)-1))
		case "alt+v":
			step := pageStep(p.height)
			p.cursor = maxInt(0, p.cursor-step)
		}
		p.ensureVisible()
	}
	return p, nil
}
```

`pageStep` is the helper added in Task 1 (lives in `list.go`, package-level). `maxInt` already exists; `minInt` is used here — confirm it exists in the package; if not, add it next to `maxInt`. (Quick check: `grep -n 'func minInt\|func maxInt' internal/ui/`.)

If `minInt` is missing, add it near `maxInt` in `internal/ui/list.go` (or wherever `maxInt` lives):

```go
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/ui/ -run 'TestBookmarksPane_CtrlVPagesDown|TestBookmarksPane_AltVPagesUp' -v`

Expected: PASS.

- [ ] **Step 5: Run the full ui package tests**

Run: `go test ./internal/ui/ -race`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/bookmarks.go internal/ui/bookmarks_test.go internal/ui/list.go
git commit -m "ui: page bookmarks pane cursor with C-v / M-v"
```

(`internal/ui/list.go` is included only if `minInt` was added there; otherwise drop it from the `git add` command.)

---

## Task 3: Match list paging — `C-v` / `M-v`

**Files:**
- Modify: `internal/ui/matches.go:60-79`
- Test: `internal/ui/matches_test.go` (append at end of file)

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/matches_test.go`:

```go
func TestMatchList_CtrlVPagesDown(t *testing.T) {
	items := make([]search.Match, 20)
	for i := range items {
		items[i] = search.Match{
			SessionID: "id",
			LineNo:    i + 1,
			Line:      fmt.Sprintf("line %d", i+1),
			SortTime:  time.Unix(int64(1000-i), 0),
		}
	}
	ml := NewMatchList(80, 10) // pageStep = 8
	ml.SetItems(items)

	ml.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if ml.Cursor() != 8 {
		t.Errorf("cursor after ctrl+v = %d; want 8", ml.Cursor())
	}
	ml.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	ml.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if ml.Cursor() != 19 {
		t.Errorf("cursor at bottom should clamp to len-1; got %d, want 19", ml.Cursor())
	}
}

func TestMatchList_AltVPagesUp(t *testing.T) {
	items := make([]search.Match, 20)
	for i := range items {
		items[i] = search.Match{
			SessionID: "id",
			LineNo:    i + 1,
			Line:      fmt.Sprintf("line %d", i+1),
			SortTime:  time.Unix(int64(1000-i), 0),
		}
	}
	ml := NewMatchList(80, 10)
	ml.SetItems(items)
	ml.Update(tea.KeyMsg{Type: tea.KeyEnd}) // cursor = 19

	ml.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if ml.Cursor() != 11 {
		t.Errorf("cursor after alt+v = %d; want 11", ml.Cursor())
	}
	ml.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	ml.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if ml.Cursor() != 0 {
		t.Errorf("cursor at top should clamp to 0; got %d", ml.Cursor())
	}
}
```

- [ ] **Step 2: Run the new tests and confirm they fail**

Run: `go test ./internal/ui/ -run 'TestMatchList_CtrlVPagesDown|TestMatchList_AltVPagesUp' -v`

Expected: FAIL.

- [ ] **Step 3: Implement `ctrl+v` / `alt+v` in `(*MatchList).Update`**

Edit `internal/ui/matches.go`. Replace `Update` (lines 60-79) with:

```go
func (ml *MatchList) Update(msg tea.Msg) (*MatchList, tea.Cmd) {
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "j", "down", "ctrl+n":
			if ml.cursor < len(ml.items)-1 {
				ml.cursor++
			}
		case "k", "up", "ctrl+p":
			if ml.cursor > 0 {
				ml.cursor--
			}
		case "g", "home":
			ml.cursor = 0
		case "G", "end":
			ml.cursor = maxInt(0, len(ml.items)-1)
		case "ctrl+v":
			step := pageStep(ml.height)
			ml.cursor = minInt(ml.cursor+step, maxInt(0, len(ml.items)-1))
		case "alt+v":
			step := pageStep(ml.height)
			ml.cursor = maxInt(0, ml.cursor-step)
		}
		ml.ensureVisible()
	}
	return ml, nil
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/ui/ -run 'TestMatchList_CtrlVPagesDown|TestMatchList_AltVPagesUp' -v`

Expected: PASS.

- [ ] **Step 5: Run the full ui package tests**

Run: `go test ./internal/ui/ -race`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/matches.go internal/ui/matches_test.go
git commit -m "ui: page match list cursor with C-v / M-v"
```

---

## Task 4: Wire `C-v` / `M-v` into `App.handleKey` (modeNormal)

**Files:**
- Modify: `internal/ui/app.go:515` (the list-movement dispatch case in modeNormal)
- Test: `internal/ui/app_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/app_test.go`:

```go
func TestApp_CtrlVPagesListInModeNormal(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	metas := make([]session.Meta, 30)
	for i := range metas {
		metas[i] = session.Meta{
			ID:        fmt.Sprintf("s%02d", i),
			Path:      fmt.Sprintf("s%02d.jsonl", i),
			UpdatedAt: time.Unix(int64(1000-i), 0),
		}
	}
	app.Update(ScanMsg{Metas: metas})

	before := app.list.Cursor()
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if app.list.Cursor() == before {
		t.Errorf("ctrl+v did not advance list cursor (still %d)", app.list.Cursor())
	}
	if cmd == nil {
		t.Error("ctrl+v should schedule LoadTranscript cmd like j does")
	}
}

func TestApp_AltVPagesListInModeNormal(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	metas := make([]session.Meta, 30)
	for i := range metas {
		metas[i] = session.Meta{
			ID:        fmt.Sprintf("s%02d", i),
			Path:      fmt.Sprintf("s%02d.jsonl", i),
			UpdatedAt: time.Unix(int64(1000-i), 0),
		}
	}
	app.Update(ScanMsg{Metas: metas})
	app.Update(tea.KeyMsg{Type: tea.KeyEnd})
	before := app.list.Cursor()
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if app.list.Cursor() >= before {
		t.Errorf("alt+v did not retreat list cursor (was %d, now %d)", before, app.list.Cursor())
	}
	if cmd == nil {
		t.Error("alt+v should schedule LoadTranscript cmd like k does")
	}
}
```

`fmt` is already imported in this file (used by `makeTestSearchMatches`); if not, add it.

- [ ] **Step 2: Run the new tests and confirm they fail**

Run: `go test ./internal/ui/ -run 'TestApp_CtrlVPagesListInModeNormal|TestApp_AltVPagesListInModeNormal' -v`

Expected: FAIL — `ctrl+v` and `alt+v` aren't handled in `app.go` modeNormal yet, so `Update` returns `(a, nil)` and the cursor stays put.

- [ ] **Step 3: Extend the modeNormal list-movement case**

Edit `internal/ui/app.go`. Find the existing case (around line 515):

```go
case "j", "down", "ctrl+n", "k", "up", "ctrl+p", "g", "G", "home", "end":
```

Add `"ctrl+v"` and `"alt+v"` to the list:

```go
case "j", "down", "ctrl+n", "k", "up", "ctrl+p", "g", "G", "home", "end", "ctrl+v", "alt+v":
```

The branch body already routes the key to `a.list.Update` or `a.bookmarks.Update` based on focus and triggers `updatePreviewFromFocus()` + `loadTranscriptForFocus()`. No further changes needed.

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/ui/ -run 'TestApp_CtrlVPagesListInModeNormal|TestApp_AltVPagesListInModeNormal' -v`

Expected: PASS.

- [ ] **Step 5: Run the full ui package tests**

Run: `go test ./internal/ui/ -race`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "ui: route ctrl+v / alt+v to focused list in modeNormal"
```

---

## Task 5: Wire `C-v` / `M-v` into `App.handleKey` (modeSearch)

**Files:**
- Modify: `internal/ui/app.go:364` (the modeSearch list-movement dispatch case)
- Test: `internal/ui/app_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/app_test.go`:

```go
// In search mode with no rg results yet (matches list hidden), C-v / M-v
// should page the underlying filtered session list — same dispatch path
// the existing C-n / C-p take.
func TestApp_CtrlVPagesListInModeSearch(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	metas := make([]session.Meta, 30)
	for i := range metas {
		metas[i] = session.Meta{
			ID:        fmt.Sprintf("s%02d", i),
			Path:      fmt.Sprintf("s%02d.jsonl", i),
			UpdatedAt: time.Unix(int64(1000-i), 0),
			Enriched:  true,
		}
	}
	app.Update(ScanMsg{Metas: metas})
	// Enter search mode; matches list stays hidden (no query → no rg run).
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	if app.mode != modeSearch {
		t.Fatalf("precondition: mode = %v; want modeSearch", app.mode)
	}

	before := app.list.Cursor()
	app.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if app.list.Cursor() == before {
		t.Errorf("ctrl+v in search mode did not page list (cursor still %d)", app.list.Cursor())
	}
}

func TestApp_AltVPagesListInModeSearch(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	metas := make([]session.Meta, 30)
	for i := range metas {
		metas[i] = session.Meta{
			ID:        fmt.Sprintf("s%02d", i),
			Path:      fmt.Sprintf("s%02d.jsonl", i),
			UpdatedAt: time.Unix(int64(1000-i), 0),
			Enriched:  true,
		}
	}
	app.Update(ScanMsg{Metas: metas})
	// Move cursor down before entering search so alt+v has somewhere to go.
	app.Update(tea.KeyMsg{Type: tea.KeyEnd})
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})

	before := app.list.Cursor()
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}, Alt: true})
	if app.list.Cursor() >= before {
		t.Errorf("alt+v in search mode did not retreat list (was %d, now %d)", before, app.list.Cursor())
	}
}
```

- [ ] **Step 2: Run the new tests and confirm they fail**

Run: `go test ./internal/ui/ -run 'TestApp_CtrlVPagesListInModeSearch|TestApp_AltVPagesListInModeSearch' -v`

Expected: FAIL — in search mode, `ctrl+v` / `alt+v` currently fall through to the `default` branch and get fed to the search input, leaving the list cursor unchanged.

- [ ] **Step 3: Extend the modeSearch list-movement case**

Edit `internal/ui/app.go`. Find the existing case (around line 364):

```go
case "up", "down", "ctrl+p", "ctrl+n":
```

Add `"ctrl+v"` and `"alt+v"`:

```go
case "up", "down", "ctrl+p", "ctrl+n", "ctrl+v", "alt+v":
```

The branch body already routes to `a.matchList.Update` when matches are showing or `a.list.Update` otherwise — no further changes needed.

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/ui/ -run 'TestApp_CtrlVPagesListInModeSearch|TestApp_AltVPagesListInModeSearch' -v`

Expected: PASS.

- [ ] **Step 5: Run the full ui package tests**

Run: `go test ./internal/ui/ -race`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "ui: route ctrl+v / alt+v through search mode too"
```

---

## Task 6: `Tab` / `Shift-Tab` cycle pane focus (modeNormal)

**Files:**
- Modify: `internal/ui/app.go:432-448` (the `ctrl+k` / `ctrl+j` block)
- Test: `internal/ui/app_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/app_test.go`:

```go
func TestApp_TabFromListMovesFocusToBookmarks(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	app.bookmarks.SetItems(
		[]session.Meta{{ID: "bm1"}},
		map[string]time.Time{"bm1": time.Unix(1, 0)},
	)
	if app.focus != focusList {
		t.Fatalf("precondition: focus = %d; want focusList", app.focus)
	}

	app.Update(tea.KeyMsg{Type: tea.KeyTab})
	if app.focus != focusBookmarks {
		t.Errorf("focus after Tab = %d; want focusBookmarks", app.focus)
	}
}

func TestApp_TabFromBookmarksMovesFocusToList(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	app.bookmarks.SetItems(
		[]session.Meta{{ID: "bm1"}},
		map[string]time.Time{"bm1": time.Unix(1, 0)},
	)
	app.Update(tea.KeyMsg{Type: tea.KeyTab}) // → bookmarks
	app.Update(tea.KeyMsg{Type: tea.KeyTab}) // → back to list
	if app.focus != focusList {
		t.Errorf("focus after second Tab = %d; want focusList", app.focus)
	}
}

func TestApp_ShiftTabCyclesLikeTab(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	app.bookmarks.SetItems(
		[]session.Meta{{ID: "bm1"}},
		map[string]time.Time{"bm1": time.Unix(1, 0)},
	)
	app.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if app.focus != focusBookmarks {
		t.Errorf("focus after Shift-Tab from list = %d; want focusBookmarks", app.focus)
	}
	app.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if app.focus != focusList {
		t.Errorf("focus after second Shift-Tab = %d; want focusList", app.focus)
	}
}

func TestApp_TabIsNoopWithEmptyBookmarks(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a"}}})
	// bookmarks pane is empty.
	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyTab})
	if app.focus != focusList {
		t.Errorf("focus after Tab with empty bookmarks = %d; want focusList", app.focus)
	}
	if cmd != nil {
		t.Error("Tab with empty bookmarks should return nil cmd (no preview reload)")
	}
}
```

- [ ] **Step 2: Run the new tests and confirm they fail**

Run: `go test ./internal/ui/ -run 'TestApp_TabFromList|TestApp_TabFromBookmarks|TestApp_ShiftTabCyclesLikeTab|TestApp_TabIsNoopWithEmptyBookmarks' -v`

Expected: FAIL — `tab`/`shift+tab` aren't handled, so focus never changes.

- [ ] **Step 3: Add the `tab` / `shift+tab` case**

Edit `internal/ui/app.go`. After the existing `case "ctrl+j":` block (which ends around line 448, just before `case "q", "ctrl+c":`), insert:

```go
case "tab", "shift+tab":
	// Cycle focus to the OTHER pane. With two focusable panes, Tab
	// and Shift-Tab are equivalent; both fall back to the same body
	// rather than the directional C-j / C-k.
	if a.focus == focusList {
		if len(a.bookmarks.Items()) == 0 {
			return a, nil
		}
		a.focus = focusBookmarks
	} else {
		a.focus = focusList
	}
	a.applyFocus()
	a.updatePreviewFromFocus()
	return a, a.loadTranscriptForFocus()
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/ui/ -run 'TestApp_TabFromList|TestApp_TabFromBookmarks|TestApp_ShiftTabCyclesLikeTab|TestApp_TabIsNoopWithEmptyBookmarks' -v`

Expected: PASS.

- [ ] **Step 5: Run the full ui package tests**

Run: `go test ./internal/ui/ -race`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "ui: cycle pane focus with Tab / Shift-Tab"
```

---

## Task 7: `Tab` / `Shift-Tab` no-op in modeSearch

**Files:**
- Modify: `internal/ui/app.go` (modeSearch branch, around line 348)
- Test: `internal/ui/app_test.go` (append)

- [ ] **Step 1: Write the failing test**

Append to `internal/ui/app_test.go`:

```go
// Tab in search mode must NOT reach the search input. The bubbles
// textinput typically ignores Tab anyway, but an explicit no-op guards
// against future versions that might bind it to autocomplete.
func TestApp_TabIsNoopInModeSearch(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a", Enriched: true}}})
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	// Type a query so we can see whether Tab pollutes it.
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h', 'i'}})
	before := app.search.Query()

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyTab})
	if got := app.search.Query(); got != before {
		t.Errorf("Tab in search mode changed query: %q -> %q", before, got)
	}
	if cmd != nil {
		t.Errorf("Tab in search mode should be a no-op (nil cmd); got %T", cmd)
	}
}

func TestApp_ShiftTabIsNoopInModeSearch(t *testing.T) {
	app := NewApp(AppConfig{Width: 120, Height: 40, LoadTranscript: stubLoad})
	app.Update(ScanMsg{Metas: []session.Meta{{ID: "a", Enriched: true}}})
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h', 'i'}})
	before := app.search.Query()

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if got := app.search.Query(); got != before {
		t.Errorf("Shift-Tab in search mode changed query: %q -> %q", before, got)
	}
	if cmd != nil {
		t.Errorf("Shift-Tab in search mode should be a no-op; got %T", cmd)
	}
}
```

- [ ] **Step 2: Run the new tests and confirm they fail or pass-by-accident**

Run: `go test ./internal/ui/ -run 'TestApp_TabIsNoopInModeSearch|TestApp_ShiftTabIsNoopInModeSearch' -v`

Expected: They likely PASS today because bubbles' textinput ignores Tab — but rerun after the implementation step to confirm the explicit no-op still holds. If they FAIL today (e.g., Tab is interpreted as "\t" by the search input), the implementation step below will fix it.

If the tests pass at this step, that's fine — the next step makes the no-op intentional rather than incidental, which is the point.

- [ ] **Step 3: Add explicit `tab` / `shift+tab` no-op in modeSearch**

Edit `internal/ui/app.go`. In the `case modeSearch:` block, find the existing `case "esc":` (around line 349). After the `case "enter":` block that ends near line 399 and before the `// Default: pass keystroke to search input` comment, insert:

```go
case "tab", "shift+tab":
	// Cycle-pane keys are absorbed in search mode so they never reach
	// the search input. Pane focus cycling is a normal-mode-only
	// concern; in search mode focus is forced to the list anyway.
	return a, nil
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `go test ./internal/ui/ -run 'TestApp_TabIsNoopInModeSearch|TestApp_ShiftTabIsNoopInModeSearch' -v`

Expected: PASS.

- [ ] **Step 5: Run the full ui package tests**

Run: `go test ./internal/ui/ -race`

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/app.go internal/ui/app_test.go
git commit -m "ui: absorb Tab / Shift-Tab in search mode"
```

---

## Task 8: README — document the new keys

**Files:**
- Modify: `README.md:50-63` (Keys table) and append a new subsection after it.

- [ ] **Step 1: Edit the Keys table**

Open `README.md`. Locate the Keys table (header at line 50). After the existing row:

```
| `^K` / `^J` | move focus between bookmarks pane and session list           |
```

Insert two new rows so the relevant section reads:

```
| `^K` / `^J` | move focus between bookmarks pane and session list           |
| `Tab` / `Shift-Tab` | cycle focus between bookmarks pane and session list (alias of `^K` / `^J`) |
| `^V` / `M-v` | page down / page up in the focused list                     |
```

- [ ] **Step 2: Add the macOS Option-as-Meta subsection**

Right after the Keys table (before the `## Bookmarks` section), insert:

```markdown
### macOS: Option-as-Meta

`M-v` (page up in the focused list) requires the terminal to send
`ESC`+`v` for `Option-v`. Defaults vary by terminal:

- **Terminal.app**: Preferences → Profiles → Keyboard → *Use Option as
  Meta key*.
- **iTerm2**: Profiles → Keys → set *Left Option key* (and/or *Right
  Option key*) to *Esc+*.
- **Ghostty**: works as shipped.

Without this setting, `Option-v` inserts `√` (US layout) and the page-up
binding will not fire. `PgUp` (= `Fn-↑`) and `k` / `↑` / `^P` keep
working regardless.
```

- [ ] **Step 3: Verify the README still renders**

Run: `go build ./cmd/csess && rm -f csess` (sanity check that nothing in the package broke; README has no compile-time effect, but the task is the right place to confirm everything still builds.)

Or, just visually open `README.md` and check the table is well-formed.

- [ ] **Step 4: Run the full test suite one more time**

Run: `make test`

Expected: PASS, no race warnings.

- [ ] **Step 5: Commit**

```bash
git add README.md
git commit -m "docs: document Tab / Shift-Tab and C-v / M-v keys"
```

---

## Final verification

- [ ] **Run the whole suite**

Run: `make test && make build`

Expected: PASS, binary built.

- [ ] **Smoke test in the terminal**

Run: `./csess` and verify, by hand:

- `Tab` / `Shift-Tab` cycle focus between the list and bookmarks pane (need at least one bookmark for the move *to* bookmarks to be visible).
- `C-v` advances the list cursor by roughly a page; `M-v` retreats. (`M-v` requires Option-as-Meta if you're on macOS Terminal.app or iTerm2.)
- `j` / `k` / `g` / `G` / `↑` / `↓` / `Home` / `End` / `C-n` / `C-p` / `C-k` / `C-j` all still work.
- `PgUp` / `PgDn` still scroll the preview pane (unchanged).

- [ ] **Clean up the binary if you don't want it tracked**

```bash
make clean
```
