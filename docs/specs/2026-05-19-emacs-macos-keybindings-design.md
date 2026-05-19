# csess — Emacs / macOS Keybindings for Movement, Paging, and Pane Focus

Design spec · 2026-05-19

## Goal

Layer emacs / macOS-conventional keybindings on top of the existing
vim-style keys for three concerns:

1. **Cursor movement** in the focused list pane (no functional change —
   `↑`/`↓`/`C-n`/`C-p` already work; this spec records the inventory).
2. **Page-by-page navigation** through long lists, which today is not
   possible — the user can only step one row at a time with `j`/`k`.
3. **Switching keyboard focus** between the bookmarks pane and the
   session list, so the user can use `Tab` instead of (or in addition
   to) the existing `C-j` / `C-k`.

Existing vim and macOS keys remain as aliases. No removals.

## Scope

In:

- `C-v` / `M-v` page the focused list pane (`internal/ui/list.go`,
  `internal/ui/bookmarks.go`, `internal/ui/matches.go`). Page size is
  `max(1, visibleHeight - 2)` rows. Boundary is a no-op (no wrap),
  matching existing `j`/`k` semantics.
- `Tab` / `Shift-Tab` switch pane focus, equivalent to the current
  `C-j` / `C-k`. With only two focusable panes (list, bookmarks),
  Tab and Shift-Tab cycle in opposite directions but have the same
  effect when the bookmarks pane is non-empty; both are no-ops when
  the bookmarks pane is empty.
- Both new bindings work in `modeNormal` and `modeSearch`. In
  `modeSearch`, `C-v` / `M-v` page whichever list (matches list when
  results are showing, otherwise the filtered session list) is
  receiving cursor movement today. `Tab` / `Shift-Tab` are
  explicitly consumed in `modeSearch` so they do not reach the
  search input.
- Preview pane scroll keeps `PgUp` / `PgDn` (a.k.a. macOS
  `Fn-↑` / `Fn-↓`) unchanged.
- README: extend the Keys table with the four new bindings and add a
  short note on enabling Option-as-Meta in Terminal.app, iTerm2, and
  Ghostty so `M-v` reaches the program. No runtime detection.

Out:

- `M-<` / `M->` to jump to top/bottom (rejected — `g`/`G`/`Home`/`End`
  already cover this).
- Removing any existing key. `j` / `k` / `g` / `G` / `C-j` / `C-k`
  remain as aliases.
- Fixing the pre-existing behaviour where `C-j` / `C-k` in
  `modeSearch` fall through to the search input (out of scope; called
  out so the new `Tab` / `Shift-Tab` handling explicitly does the
  right thing for itself, but the old keys are not touched).
- Auto-detection of terminal Meta-key configuration.
- Configurable keymaps. The map is hard-coded.

## Context

`csess` has a vim-flavoured keymap (`j`/`k`/`g`/`G`) augmented with
arrow keys and a few emacs aliases (`C-n`/`C-p`). Pane focus toggles
with `C-k` (up to bookmarks) / `C-j` (down to list); these collide
with the emacs / macOS muscle memory for `kill-line`, but the
collision is benign in a TUI without inline editing.

Two real gaps for emacs / macOS users today:

- **No page-step navigation in any list.** Long session lists must be
  walked one row at a time, or jumped to start/end. There is no
  middle ground.
- **No discoverable focus switch.** `C-j` / `C-k` is undocumented
  outside the README and unfamiliar to non-vim users. `Tab` is the
  near-universal "next focus" key in TUIs and forms.

The preview pane already pages cleanly via `PgUp` / `PgDn` and is not
in this spec's path.

## Final keymap

Cursor movement on the focused list pane (list / bookmarks / matches):

| Action     | Existing keys                  | New keys |
|------------|--------------------------------|----------|
| Up one     | `k`, `↑`, `C-p`                | —        |
| Down one   | `j`, `↓`, `C-n`                | —        |
| Top        | `g`, `Home`                    | —        |
| Bottom     | `G`, `End`                     | —        |
| Page up    | —                              | `M-v`    |
| Page down  | —                              | `C-v`    |

Preview pane scroll (unchanged):

| Action          | Keys                 |
|-----------------|----------------------|
| Preview page up | `PgUp` (= `Fn-↑`)    |
| Preview page dn | `PgDn` (= `Fn-↓`)    |

Pane focus:

| Action                       | Keys                       |
|------------------------------|----------------------------|
| Focus list pane (absolute)   | `C-j`                      |
| Focus bookmarks pane (abs.)  | `C-k`                      |
| Cycle to the other pane      | `Tab`, `Shift-Tab`         |

`Tab` and `Shift-Tab` are equivalent today because there are exactly
two focusable panes. They are named as a pair to keep the convention
discoverable for users who reach for `Shift-Tab` after `Tab`, and to
leave room for a third pane in the future without renaming.

## Architecture

### Page size

`visibleHeight` for each list-style pane is already tracked: `List`
and `MatchList` carry `height`, `BookmarksPane` carries the height it
gets via `SetSize`. Page size is computed inline in each `Update`:

```
page := visibleHeight - 2
if page < 1 {
    page = 1
}
```

The two-row overlap matches emacs's default
`next-screen-context-lines = 2` and is enough to keep a row visible
across a page jump on small panes.

### Cursor advance

`C-v` advances `cursor` by `page`, clamped to `len(items)-1`. `M-v`
retreats by `page`, clamped to `0`. Each call ends with
`ensureVisible()` so `firstVisible` re-anchors to the new cursor —
the existing helper already handles this for `j`/`k`/`g`/`G`. No
wrap.

### Pane focus dispatch

`app.go::handleKey` already has `case "ctrl+k"` and `case "ctrl+j"`
for focus toggling — they go to a specific pane (bookmarks and list
respectively), with no-ops when already focused or when the
bookmarks pane is empty.

Add a new `case "tab", "shift+tab":` branch that switches to the
*other* pane: if currently on the list, behave like `C-k` (go to
bookmarks, with the empty-bookmarks no-op guard); if currently on
bookmarks, behave like `C-j` (go to list). With two panes, `Tab` and
`Shift-Tab` are equivalent; sharing the case body avoids duplication
and signals the symmetry.

In `modeSearch`, add `case "tab", "shift+tab"` that returns
`(a, nil)` so the keys do not reach `a.search.Update`. This is the
narrowest fix: it does not touch the pre-existing `C-j` / `C-k`
fall-through behaviour in search mode.

### Page key dispatch

In `modeNormal`, the existing list-movement dispatch is one big
case:

```go
case "j", "down", "ctrl+n", "k", "up", "ctrl+p",
     "g", "G", "home", "end":
```

Add `"ctrl+v", "alt+v"` to that case. The branch already routes to
`a.bookmarks.Update(km)` or `a.list.Update(km)` based on focus, then
calls `updatePreviewFromFocus()` and `loadTranscriptForFocus()`; new
keys ride that machinery for free.

In `modeSearch`, the corresponding dispatch is:

```go
case "up", "down", "ctrl+p", "ctrl+n":
```

Add `"ctrl+v", "alt+v"` so paging routes to `a.matchList` when
matches are showing, otherwise to `a.list`, exactly like
`C-n`/`C-p`. `PgUp` / `PgDn` continues to scroll the preview via the
adjacent `case "pgup", "pgdown":` branch — untouched.

### Bubbletea key strings

Bubbletea reports `C-v` as `"ctrl+v"` and `M-v` as `"alt+v"` (when
the terminal sends ESC-prefixed Option presses). `Tab` is `"tab"`,
`Shift-Tab` is `"shift+tab"`. No surprises.

## Terminal Option-as-Meta

`M-v` only reaches the program when the terminal sends `ESC`+`v` for
`Option-v`. Defaults vary:

- **Terminal.app** — off by default. Preferences → Profiles → Keyboard
  → "Use Option as Meta key".
- **iTerm2** — off by default. Profiles → Keys → "Left Option key" /
  "Right Option key" → "Esc+".
- **Ghostty** — on by default; `M-v` works as shipped.

Without the setting, pressing `Option-v` on macOS US layout inserts
`√`. The README will note this and point users to the right
preference. No runtime detection — the app cannot reliably tell
whether `M-v` was intended or was a literal `√`.

## Testing

`internal/ui/list_test.go`, `bookmarks_test.go`, `matches_test.go`
each add three cases per pane:

- Mid-list page: cursor at index `5` with `visibleHeight = 10`,
  press `C-v`, expect cursor at `5 + 8 = 13` (or `len-1` if shorter).
- Top boundary: cursor at `0`, press `M-v`, expect cursor stays `0`.
- Bottom boundary: cursor at `len-1`, press `C-v`, expect cursor
  stays `len-1`.

`internal/ui/app_test.go` adds:

- `Tab` from list focus moves focus to bookmarks; second `Tab` (or
  `Shift-Tab`) returns to list. Equivalent under `C-k`/`C-j`.
- `Tab` is a no-op when the bookmarks pane is empty.
- In `modeSearch`, pressing `Tab` does not change the search query
  (`a.search.Query()` unchanged).
- In `modeSearch`, `C-v` advances the focused list cursor (matches
  list when results are showing, otherwise the session list).

## README

Append to the Keys table:

```
| `Tab` / `Shift-Tab` | cycle focus between bookmarks pane and session list |
| `^V` / `M-v`        | page down / page up in the focused list             |
```

Add a short subsection after the Keys table:

> ### macOS: Option-as-Meta
>
> `M-v` (page up in the focused list) requires the terminal to send
> `ESC` + `v` for `Option-v`. Terminal.app and iTerm2 ship with this
> off; enable it in:
>
> - **Terminal.app**: Preferences → Profiles → Keyboard → *Use
>   Option as Meta key*.
> - **iTerm2**: Profiles → Keys → set *Left Option key* (and/or
>   *Right Option key*) to *Esc+*.
> - **Ghostty**: works as shipped.

## Risks

- **`alt+v` on a terminal without Option-as-Meta** silently does
  nothing useful (inserts `√` as a literal in modes that take text).
  Mitigation: README note. Acceptable because `PgUp`, `M-v`, and
  `j`/`k`+`g`/`G` are all functionally redundant — the user has at
  least three other ways to navigate.
- **`Tab` colliding with future autocompletion** in the search input.
  Today the search input does not consume `Tab`; the explicit
  `case "tab"` no-op guards against accidentally feeding it later.
- **Page-size off-by-one on tiny panes** (`visibleHeight ≤ 2`). The
  `max(1, h-2)` clamp degrades to single-row stepping, which is
  correct.
