# csess

A fast terminal UI for browsing, searching, and resuming your Claude Code sessions.

If you use Claude Code, every conversation is saved as a JSONL file under
`~/.claude/projects/`. Over time you accumulate hundreds of them across many
projects, and the built-in `claude --resume` picker only shows sessions for
the current directory. `csess` gives you a single place to see them all,
full-text search across them, bookmark the ones you care about, and jump
back into any of them with one keystroke.

## What you can do

- **Browse all sessions** across every project, or just the current directory (`--here`).
- **Full-text search** across every transcript — powered by `ripgrep`, with a context preview.
- **Resume** a session by pressing `Enter`: csess `cd`s into the session's original `cwd` and execs `claude --resume <id>`.
- **Bookmark** sessions you want to return to. Bookmarks are global and persist across runs.
- **Soft-delete** sessions you no longer need (moved to `~/.claude/.trash/`, recoverable).
- **Copy** a session id to the clipboard with `y`.

No Claude runtime or API access is needed — csess reads the JSONL files directly.

## Install

Requires Go 1.22+ and [`ripgrep`](https://github.com/BurntSushi/ripgrep) (`rg`) on your `PATH` for search.

```bash
git clone https://github.com/chaomai/csess
cd csess
go install ./cmd/csess
```

## Usage

```bash
csess           # list sessions across all projects
csess --here    # only sessions whose cwd matches the current directory
```

Useful flags:

```
--projects-dir   override ~/.claude/projects
--trash-dir      override ~/.claude/.trash
--bookmarks-file override ~/.claude/csess/bookmarks.json
--context N      lines of context around each search match (default 3)
--max-matches N  per-file match cap passed to rg (default 1000)
```

## Keys

| Key         | Action                                                       |
|-------------|--------------------------------------------------------------|
| `j` / `k`   | move cursor                                                  |
| `g` / `G`   | top / bottom                                                 |
| `Enter`     | resume the focused session (`cd` to its cwd + `claude --resume`) |
| `y`         | copy session id to clipboard                                 |
| `d`         | soft-delete (moves the JSONL to `~/.claude/.trash/`)         |
| `/`         | search — full-text via `rg`, or session-id prefix if hex     |
| `b`         | bookmark / unbookmark the focused session                    |
| `^K` / `^J` | move focus between bookmarks pane and session list           |
| `a`         | expand a truncated transcript in the preview                 |
| `q`         | quit                                                         |

## Bookmarks

Press `b` to bookmark the session under the cursor. Bookmarked sessions
appear in a pane above the list, sorted by when you bookmarked them.
Bookmarks live in `~/.claude/csess/bookmarks.json` (override with
`--bookmarks-file`) and are global — they show regardless of `--here`.

`Ctrl-K` / `Ctrl-J` moves focus between the bookmarks pane and the session
list. `Enter`, `y`, and `d` operate on whichever pane has focus. Deleting a
bookmarked session auto-removes the bookmark.

## How it works

Claude Code stores each conversation as a JSONL file at
`~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`. csess reads those
files directly:

- Start-up scans the directory (`readdir` + `stat`) and paints the list immediately.
- Metadata (first/last prompt, model, cwd, message counts) is filled in by a
  bounded worker pool in the background as you scroll.
- Cursor moves stream the focused session's full transcript on demand.
  Moving again cancels the previous load.
- Search shells out to `rg --json` over the JSONL files, debounced, with the
  matched turn snippet shown in the preview pane.
- `Enter` launches `bash -lc "cd <cwd> && exec claude --resume <id>"`, so
  your outer shell's working directory is unchanged.

## Layout

```
cmd/csess/        entrypoint
internal/session/ parsing, scanning, enrichment, bookmarks
internal/search/  ripgrep wrapper, match parsing
internal/action/  resume, clipboard, trash
internal/ui/      bubbletea models (list, preview, search, bookmarks)
```

## Develop

```bash
make test       # go test -race
make cover      # coverage report
make lint       # requires golangci-lint
make bench
make build
```
