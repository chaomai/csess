# csess

A terminal UI for browsing and resuming Claude Code sessions.

## Install

```bash
git clone <this repo>
cd csess
go install ./cmd/csess
```

## Use

```bash
csess           # list sessions for the current directory
csess --all     # list sessions across every project
```

### Keys

| Key | Action |
|-----|--------|
| `j` / `k` | move cursor |
| `g` / `G` | top / bottom |
| `Enter` | resume session (cd to its cwd, exec `claude --resume`) |
| `y` | copy session id to clipboard |
| `d` | soft-delete (moves to `~/.claude/.trash/`) |
| `/` | filter |
| `a` | expand truncated transcript |
| `q` | quit |

## How it works

Claude Code stores each conversation as a JSONL file at
`~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`. `csess` reads those
files directly — no claude runtime, no API.

- Start-up scans the directory (`readdir` + `stat`) and paints the list.
- Metadata (first/last prompt, model, cwd, counts) is filled in by a
  bounded worker pool in the background.
- Cursor moves stream the selected session's full conversation on
  demand. Moving again cancels the previous load.
- Enter launches `bash -lc "cd <cwd> && exec claude --resume <id>"`.
  Your outer shell's working directory is unchanged.

## Layout

```
cmd/csess/        entrypoint
internal/session/ parsing, scanning, enrichment
internal/action/  resume, clipboard, trash
internal/ui/      bubbletea models
```

## Develop

```bash
make test       # go test -race
make cover
make lint       # requires golangci-lint
make bench
make build
```
