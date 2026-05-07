# csess — Bookmarked Sessions Pane

Design spec · 2026-05-07

## Goal

Add a third, navigable pane above the existing list/preview split that
shows sessions the user has bookmarked. Bookmarks persist across runs
and are global — they survive `--here` scope changes and project
changes, so the user can jump back to any pinned session from any cwd.

## Scope

In:

- New `BookmarksPane` rendered above the existing two-pane body; stacks
  vertically with `lipgloss.JoinVertical`.
- Key `b`: toggle bookmark on the focused pane's cursor row.
- Keys `Ctrl-J` / `Ctrl-K`: switch keyboard focus between the list pane
  and the bookmark pane. Cursor movement, Enter-resume, `y` copy-id,
  `d` delete all dispatch to the focused pane.
- Persistence: `~/.claude/csess/bookmarks.json`, overridable via
  `--bookmarks-file <path>`. Atomic write via `tmp` + `rename`.
- Auto-height with internal scroll, capped at ~½ window height.
- Cross-project: the bookmark pane shows every bookmarked session
  regardless of `--here` / `--all` scope.
- Hide the bookmark pane during search mode (`/`); restore on `esc`.

Out:

- Reordering bookmarks by hand.
- Bookmark categories, tags, or labels.
- Import / export formats beyond the JSON file.
- Cross-machine sync (the file is plain text; user handles it).
- Bookmarking search matches directly — `b` operates on sessions only.

## Context

Today csess shows two panes: a session list on the left and a preview
on the right. With many projects and hundreds of sessions per project,
returning to a specific past conversation takes scrolling or a search.
Bookmarks give O(1) return-to-pinned-session, persistent across runs
and cwds. User-facing term is **bookmark** throughout; the toggle key
is `b` and the pane title reads `bookmarks`.

The session scanner already skips subdirectories (so subagent files
are excluded) and the rg search was recently narrowed to top-level
`*.jsonl`. Bookmarks reference only top-level session IDs.

## Architecture

One-way dependency preserved: `ui → session → fs`. No new cross-package
imports in the opposite direction.

```
internal/session/
  bookmarks.go        # BookmarkStore: Load/Save over a JSON file
  bookmarks_test.go

internal/ui/
  bookmarks.go        # BookmarksPane model (mirrors List/MatchList)
  bookmarks_test.go
  app.go              # +focus enum, +bookmarks field, +key routing,
                      # vertical layout composition
  app_test.go         # +focus switch, +toggle, +off-scope enrich

cmd/csess/main.go     # +load bookmarks.json on startup,
                      # +goroutine bridge for off-scope enrichment
```

Net: two new files (~200 LOC combined), surgical edits to `app.go` and
`main.go`. No changes to `scanner.go`, `preview.go`, `matches.go`,
`search.go`.

## Components

### `session.BookmarkStore`

```go
type Bookmark struct {
    ID        string    `json:"id"`
    StarredAt time.Time `json:"starredAt"`
}

type BookmarkStore struct{ path string }

func NewBookmarkStore(path string) *BookmarkStore
func (s *BookmarkStore) Load() ([]Bookmark, error)
func (s *BookmarkStore) Save(bookmarks []Bookmark) error
```

Contract:
- `Load`: missing file → `([]Bookmark{}, nil)`. Malformed JSON,
  unknown version, decode error → `(nil, err)`.
- `Save`: atomic (tmp file in same dir, `fsync`, `rename`). Creates
  parent directory if absent.
- Ordering: stored in `StarredAt` desc; `Save` expects the caller to
  sort.
- File format:
  ```json
  {
    "version": 1,
    "bookmarks": [
      {"id": "abc123", "starredAt": "2026-05-07T12:34:56Z"}
    ]
  }
  ```

### `ui.BookmarksPane`

```go
type BookmarksPane struct {
    width, height        int
    items                []session.Meta
    idIndex              map[string]int
    cursor, firstVisible int
}

NewBookmarksPane(w, h int) *BookmarksPane
SetItems(items []session.Meta)       // sorted by StarredAt desc
Add(m session.Meta, starredAt time.Time)
Remove(id string)
ReplaceItem(m session.Meta)          // for BookmarkEnrichMsg
Selected() (session.Meta, bool)
Items() []session.Meta
Update(msg tea.Msg) (*BookmarksPane, tea.Cmd)   // j/k/g/G, same as List
View() string                        // padded to width; empty shows
                                     // "no bookmarks — press b to add"
DesiredHeight(maxRows int) int       // min(1 + len(items), maxRows)
```

Row format: the same 4-field form used by `List` in `allMode`
(`{rel-time}  {id-8}  {project-14}  {first-prompt}`), regardless of
csess's `--here` / `--all` flag — bookmarks cross projects.

A Meta carrying `LoadErr = session.ErrMissing` renders dimmed with a
trailing `(removed)` marker instead of a prompt snippet.

`ErrMissing` is a new exported sentinel in `internal/session` (same
file as `BookmarkStore`):

```go
var ErrMissing = errors.New("session file missing")
```

`StarredAt` is stored alongside `items` on the pane as
`starredAt map[string]time.Time` (id → time). Sorting `items` and
choosing insertion position in `Add` read from this map. `SetItems`
accepts a pre-sorted slice plus a full starredAt map.

Cursor behavior on mutation:
- `Add`: inserts at the correct sorted position. Cursor follows its
  existing item by id (not by index).
- `Remove`: drops by id. If the removed item was the cursor, cursor
  stays at the same index (clamped). If the removed item was before
  the cursor, cursor decrements by 1.

### App additions

```go
type focus int
const (
    focusList focus = iota
    focusBookmarks
)

type App struct {
    // existing fields unchanged
    bookmarks     *BookmarksPane
    bookmarkIDs   map[string]time.Time   // id -> starredAt
    bookmarkStore *session.BookmarkStore
    focus         focus
    prevFocus     focus                  // saved across search mode
}

// New message
type BookmarkEnrichMsg struct{ Meta session.Meta }
```

`AppConfig` gains one optional command-maker:

```go
SaveBookmarks func([]session.Bookmark) tea.Cmd
```

Injected from `main.go`, nil in tests unless a test wants to assert
save behavior (then the test supplies a recording double).

## Data flow

### Startup

1. `main.go`:
   - `BookmarkStore.Load()` → `[]Bookmark`.
   - Construct `App` with the loaded list in `bookmarkIDs`.
   - Fire `ScanMsg` as today.
2. On `ScanMsg` in `App.Update`:
   - Build `allItems` / `allIndex` as today.
   - For each `(id, starredAt)` in `bookmarkIDs`: if `id ∈ allIndex`,
     call `bookmarks.Add(allItems[i], starredAt)`; else call
     `bookmarks.Add(Meta{ID:id}, starredAt)` as a stub.
3. `main.go` concurrently walks the projects dir for any stub id —
   opens the file, runs `ExtractMeta`, sends one `BookmarkEnrichMsg`
   per id. Missing files deliver `Meta{ID:id, LoadErr:ErrMissing}`.
4. On each `BookmarkEnrichMsg`: `bookmarks.ReplaceItem(m.Meta)`.

### Toggle (`b`)

1. Read `sel, ok := focusedPane().Selected()`.
2. If `!ok` or `sel.ID == ""`, no-op.
3. If `sel.ID ∈ bookmarkIDs`: `delete(...)`, `bookmarks.Remove(sel.ID)`.
4. Else: `bookmarkIDs[sel.ID] = time.Now()`, `bookmarks.Add(sel, now)`.
5. Return `a.cfg.SaveBookmarks(sortedBookmarks(bookmarkIDs))`.
6. Save errors surface as a banner; in-memory state keeps the new
   state so the user can retry.

### Focus switch (`Ctrl-J` / `Ctrl-K`)

- `Ctrl-K`: move focus up (list → bookmarks). If bookmarks pane is
  empty, no-op.
- `Ctrl-J`: move focus down (bookmarks → list).
- On successful switch: `updatePreviewFromSelection()` against the new
  focus's `Selected()`, kick `loadTranscript(...)` for it.

### Normal-mode key routing

| Key | Target |
|---|---|
| `j k g G home end` | focused pane (then update preview + load transcript) |
| `Enter` | focused pane's `Selected()` → `ResumeSelected` |
| `y` | focused pane's `Selected()` → `CopySelected` |
| `d` | focused pane's `Selected()` → confirm + `TrashSelected` |
| `b` | focused pane's `Selected()` → toggle |
| `Ctrl-J` / `Ctrl-K` | focus switch |
| `/` | enter search mode |
| `q ctrl+c` | quit |
| `a` | preview expand (unchanged) |
| `pgup pgdown` | preview scroll (unchanged) |

### Search mode

- On `/`: `a.prevFocus = a.focus; a.focus = focusList`. Bookmark pane
  is hidden in `View()` — `JoinVertical` skips it when
  `a.mode == modeSearch`.
- On `esc`: `a.focus = a.prevFocus`; bookmark pane reappears.
- No bookmark operations during search.

### Delete integration (`d`)

In `DeleteDoneMsg` handler, after the existing removal from
`allItems`/`allIndex`: if `id ∈ bookmarkIDs`, also drop from
`bookmarkIDs` and `bookmarks.Remove(id)`, and return a `SaveBookmarks`
cmd.

### Resume from a `(removed)` stub

In the Enter handler, if `sel.LoadErr == ErrMissing || sel.Path == ""`,
banner "session file missing — press b to unbookmark" and return nil;
do not call `ResumeSelected`.

## Persistence format

Location: `~/.claude/csess/bookmarks.json`.
Override: `--bookmarks-file <path>`.

```json
{
  "version": 1,
  "bookmarks": [
    {"id": "abc123-…", "starredAt": "2026-05-07T12:34:56Z"}
  ]
}
```

- Sorted by `starredAt` desc on write.
- Atomic write: `<path>.tmp` → `fsync` → `rename` → `fsync` parent dir.
- Parent dir created on first save with mode `0755`; file mode `0644`.
- `version: 1` is the only recognized version; anything else is
  treated like malformed (fallback to empty set, log to stderr).

## Error handling

| Failure | Behavior |
|---|---|
| `Load` — file absent | Start with empty bookmarks. |
| `Load` — malformed / wrong version | Log to stderr, start empty. Do not overwrite; let the user fix or delete. |
| `Save` — write failure | Banner "bookmark save failed: <err>". Keep in-memory state; user can retry next toggle. |
| Off-scope enrich — file missing | Deliver `ErrMissing` stub; pane renders `(removed)`. |
| Off-scope enrich — read error | Same as above; treat any read error as "missing" for the user (the bookmark is unusable). |
| Bookmarked session trashed via `d` | Auto-unbookmark + save. |
| Duplicate `b` on bookmarked row | Unbookmark (toggle semantics). |
| `b` on an empty pane / no selection | No-op. |
| Two csess instances running | Last save wins; documented in `--help`. |

## Layout

```
┌────────────────────── bookmarks (2) ────────────────────────┐
│ ▶  3h  abc12345  rta-server    fix auth middleware bug        │
│    1d  def45678  dmp-etl       refactor job runner            │
├──────────────────┬───────────────────────────────────────────┤
│ sessions         │ preview                                   │
│   3h  abc12345…  │ session abc12345                          │
│   4h  def45678…  │ cwd /work/rta-server                      │
│   …              │ ───                                       │
└──────────────────┴───────────────────────────────────────────┘
status:  N sessions   [/] filter  [Enter] resume  [b] bookmark  [^J/^K] focus  …
```

Height: `BookmarksPane.DesiredHeight(cap)` where
`cap = max(3, (totalHeight - statusLine - divider) / 2)`. Internal
scroll takes over when bookmarks exceed the cap. When empty, one row
says `no bookmarks — press b to add`.

## Testing

### `session/bookmarks_test.go`

- Load of missing file → `([], nil)`.
- Load of malformed / wrong-version JSON → `(nil, err)`.
- Round-trip: Save then Load preserves order + timestamps.
- Save creates parent dir if absent.
- Atomic write: after `Save` returns, the real file is fully written
  and valid. If a leftover `.tmp` file exists (e.g. from a previous
  killed run), it's ignored on `Load` and overwritten on the next
  `Save`.

### `ui/bookmarks_test.go`

- `SetItems` sorts by `StarredAt` desc.
- `Add` inserts at the correct position, updates `idIndex`.
- `Remove` drops by id, fixes `idIndex`, clamps cursor.
- `ReplaceItem` updates a stub in place without changing position.
- `View` with empty items is padded to width.
- `View` with a `(removed)` stub renders the marker.
- `DesiredHeight` caps at the supplied max.
- `Update`: j/k/g/G move cursor with the usual bounds.

### `ui/app_test.go` additions

- `b` on focused list pane → Meta added to BookmarksPane, `SaveBookmarks` called with the new set.
- `b` on focused bookmark pane → entry removed, `SaveBookmarks` called.
- `Ctrl-K` with non-empty bookmarks → focus = `focusBookmarks`; subsequent `j` moves bookmarks pane cursor.
- `Ctrl-K` with empty bookmarks → focus unchanged.
- `Ctrl-J` from bookmarks → focus = `focusList`.
- `DeleteDoneMsg` for a bookmarked id → auto-unbookmark + save.
- `BookmarkEnrichMsg` for a stub → pane now holds full Meta at same position.
- `/` while on bookmarks pane → focus saved, forced to list; View doesn't render bookmarks.
- `esc` → restores prior focus and re-renders bookmarks.
- Enter on a `(removed)` stub → banner set, `ResumeSelected` NOT called.

Total new tests: ~15. Existing tests unchanged.

## Non-goals

- Manual reorder of bookmarks.
- Tags / categories / colored marks.
- Cross-machine sync logic inside csess.
- Multi-cursor editing.
- Bookmark a search match directly (matches map to sessions; bookmark
  the parent session instead).
