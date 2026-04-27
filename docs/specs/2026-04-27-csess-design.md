# csess — Claude Code Session Browser

Design spec · 2026-04-27

## Goal

A terminal UI tool for browsing and previewing Claude Code session files
stored under `~/.claude/projects/`, with one-key resume into the right
working directory.

## Scope

In:

- List sessions for the current `cwd` by default; `--all` to list across every project.
- Two-pane TUI: sessions list on the left, metadata header + full conversation preview on the right.
- Keys: `Enter` resume, `y` copy session id, `d` soft-delete, `/` filter, `a` expand truncated transcript, `q` quit.
- Lazy in-memory loading; no disk cache.

Out (explicitly):

- `--print` / headless output mode.
- Editing or merging sessions.
- Persistent cross-session index or search history.
- Reading from machines other than localhost.

## Context

Claude Code stores each conversation as a JSONL file at
`~/.claude/projects/<encoded-cwd>/<session-id>.jsonl`. Each line is one
record with a `type` field. Six types observed: `permission-mode`,
`attachment`, `file-history-snapshot`, `user`, `last-prompt`, `assistant`.
`claude --resume` today requires being in the original `cwd` to match
sessions — which is the main friction this tool exists to remove.

Current inventory on this machine:

- 34 project directories, 205 sessions in the current cwd alone, largest file ~8MB.

## Architecture

Three layers, one-way dependency (ui → action, ui → session; action and
session do not depend on each other or on ui).

```
cmd/csess/main.go           flag parsing → bubbletea program
internal/session/           pure reads over fs.FS, returns structs
  meta.go                   Meta struct, head/tail field extraction
  scanner.go                directory walk, concurrent enrichment
  transcript.go             streaming full-conversation parse
internal/action/            side effects, one concern per file
  resume.go                 build `bash -lc "cd <cwd> && exec claude -r <id>"`
  clipboard.go              pbcopy / wl-copy / xclip / tmux buffer / OSC52
  trash.go                  move to ~/.claude/.trash/<id>.<ts>.jsonl
internal/ui/                bubbletea MVU
  app.go                    root model, overlay state machine
  list.go                   left pane
  preview.go                right pane: header + scrolling conversation
  search.go                 / overlay
  confirm.go                d overlay
```

Rationale:

- `session` is pure I/O and parsing. No goroutines owned by it; it
  returns `tea.Cmd`-shaped callables that the UI schedules. Testable
  with `fstest.MapFS`.
- `action` wraps every platform-specific side effect. Swapping platforms
  or mocking for tests happens here, nowhere else.
- `ui` owns all state transitions. No side effects inline — everything
  goes through a `tea.Cmd`, which keeps the UI thread unblocked.

## Data model

```go
type Meta struct {
    // Cheap — from readdir + stat:
    ID         string
    Path       string
    ProjectDir string
    SizeBytes  int64
    ModTime    time.Time

    // Expensive — filled by EnrichAll:
    Enriched    bool
    CWD         string
    GitBranch   string
    Model       string
    Version     string
    StartedAt   time.Time
    UpdatedAt   time.Time
    UserMsgs    int
    AsstMsgs    int
    FirstPrompt string  // truncated to 200 chars
    LastPrompt  string  // truncated to 200 chars

    LoadErr error  // non-nil marks "corrupt" in UI
}

type Transcript struct {
    Meta  Meta
    Turns []Turn
}

type Turn struct {
    Role      string    // "user" | "assistant"
    Timestamp time.Time
    Text      string
}
```

### Rendering rules

Only `user` and `assistant` records become `Turn`s.

`assistant.message.content` is an array of blocks:

- `thinking` → dropped entirely, not even a placeholder line.
- `text` → kept verbatim.
- `tool_use` → rendered as a single line `→ ToolName(brief args)`;
  full args not expanded.

`user.message.content` may be string or array:

- string → kept verbatim.
- array → keep `text` blocks; `tool_result` long content kept to first
  line plus `…(N more lines)`.

`FirstPrompt` and `LastPrompt` are sourced only from `user` records
where `message.content` is a plain string and `userType == "external"`.
This filters out tool-result-dressed-as-user lines.

### Enrichment strategy

A single file open per session fills head/tail/count in one pass:

- Read head 40 lines → `permission-mode`, first external user
  (`StartedAt`, `FirstPrompt`), first few `assistant` (model, version,
  `CWD`, `GitBranch`).
- Stream the rest, counting `UserMsgs` / `AsstMsgs`.
- Keep a ring buffer of the last 20 `user` lines to capture
  `LastPrompt` / `UpdatedAt`.

Estimated cost: ~10–15ms for an 8MB file on SSD; well below perceptual
threshold when run in parallel.

## Data flow

### Startup

```
main → scope := cwd, or "*" if --all
     → tea.NewProgram(ui.New(scope)).Run()
ui.Init → cmdScanQuick(scope)  ⇒  ScanMsg{metas}
                                    list renders immediately with
                                    ID + mtime + size
                                    then cmdEnrichAll dispatched
```

### Enrichment

```
cmdEnrichAll:
  worker pool, size = min(NumCPU, 8)
  when --all, wrap file opens in semaphore(16) to bound open FDs
  each worker: one file open, fills head/tail/count
  sends EnrichMsg{meta} per completion

ui.Update(EnrichMsg):
  replace list.items[i] by ID
  resort by UpdatedAt desc (ModTime tie-break)
  update footer progress "done/total"
```

### Cursor movement → preview

```
list cursor moves →
  m.transcriptCtx.Cancel()      # any in-flight load stops
  ctx, seq := new(ctx), seq+1
  return cmdLoadTranscript(selected.Path, ctx, seq)

cmdLoadTranscript goroutine:
  for line in scanner:
    if ctx.Err() != nil: return
    turn, ok := parseTurn(line)
    if ok: send TurnMsg{seq, turn}

ui.Update(TurnMsg):
  if msg.seq != m.currentSeq: drop
  preview.turns = append(preview.turns, msg.turn)
  preview.viewport recompute
```

Two-layer race protection: ctx cancel stops the producer early; seq
filter drops stragglers already in flight.

### Action keys

```
Enter:
  preflight: exec.LookPath("claude"); meta.CWD != ""
  return tea.ExecProcess(
    exec.Command("bash", "-lc",
      fmt.Sprintf("cd %q && exec claude --resume %s", meta.CWD, meta.ID)),
    func(err) tea.Msg { return ResumeDoneMsg{err} })
  # bubbletea hands the terminal to claude, restores TUI on exit;
  # the outer shell's cwd is unchanged (cd happened in the child).

y:
  cmdCopyToClipboard(meta.ID) → BannerMsg("copied <id>")
  clipboard selection order:
    darwin           → pbcopy
    TMUX set         → tmux load-buffer -
    WAYLAND_DISPLAY  → wl-copy
    DISPLAY          → xclip -selection clipboard
    else             → OSC52 to stderr

d:
  m.mode = ModeConfirmDelete
  confirm overlay:
    Enter | y → cmdTrash(meta.Path) → BannerMsg + remove from list
    Esc       → back to Normal

/:
  m.mode = ModeSearch
  each keystroke → refilter m.allItems → m.items
  match weights (case-insensitive substring):
    ID prefix          3
    FirstPrompt        2
    CWD basename       2
    GitBranch          1
  stable sort by weight; Esc restores full list and prior cursor
```

## --all mode differences

- `scanner.Quick` walks `~/.claude/projects/*/` rather than a single directory.
- List adds a **project** column (CWD basename after enrich).
- Inventory can exceed several thousand; open-FD semaphore of 16 applies.
- Search across CWD basename is the primary discovery mechanism at this scale.

## Error handling

| Case | Behavior |
|---|---|
| Malformed JSON line | Skip, count; header shows `⚠ N bad lines skipped`. |
| Line >64KB | Scanner buffer up to 16MB; over that marks the line skipped. |
| Invalid UTF-8 | `ToValidUTF8` with `�`; no panic. |
| Empty / fully corrupt file | Listed; preview shows `corrupt: <err>`; Enter disabled. |
| Concurrent writes to a session | Rare; csess reads a snapshot and does not re-tail the file while open. |
| `~/.claude/projects/` missing | Print message, exit 0. |
| `EACCES` on a session | Banner + skip. |
| No sessions for cwd | Empty state: `no sessions for <cwd>. try --all`. |
| `claude` not on PATH | Preflight; banner; no exec. |
| `meta.CWD` not enriched yet | Banner `session not ready`. |
| `meta.CWD` directory removed | bash fails; `ResumeDoneMsg.err` shown as `cwd not found`. |
| claude interrupted / exits non-zero | Banner with exit code; TUI resumes. |
| Clipboard command fails | Banner; no panic. |

### Soft delete

Delete moves the file into `~/.claude/.trash/<id>.<YYYYMMDD-HHMMSS>.jsonl`.

- Directory is created with mode 0700.
- Timestamp suffix avoids collision on repeated delete+restore cycles.
- csess does not list trash contents; recovery is a manual `mv`.
- System trash CLIs (`trash`, `gio trash`) are deliberately not used — avoids per-platform branching and external tool dependency.

### Viewport cap

Transcripts with more than 5000 turns render in truncated form: the
oldest are folded to `… N earlier turns hidden, press 'a' to show all`.
This prevents pathological renders on very long sessions.

## Testing

### Unit: `internal/session/`

Fixtures under `internal/session/testdata/`:

- `happy.jsonl` — one of each record type, correct order.
- `thinking-heavy.jsonl` — many `thinking` blocks, confirms they are dropped.
- `long-line.jsonl` — single 5MB tool_result line.
- `corrupt-mid.jsonl` — three bad lines surrounded by good.
- `empty.jsonl` — zero bytes.
- `metadata-only.jsonl` — no user/assistant, only system records.
- `non-utf8.jsonl` — mixed Latin-1.

Tests (partial list):

- `meta_test.go`: ExtractFromHead respects `userType == "external"`; ignores tool-result-dressed-as-user; counts ignore system types; LoadErr survives corrupt lines.
- `scanner_test.go`: `Quick` touches only `stat`; `EnrichAll` caps at 8 workers; ctx cancel unwinds within 50ms.
- `transcript_test.go`: only user/assistant emitted; thinking dropped; tool_use single line; huge-line handling; cancel stops quickly.

All tests take `fs.FS`, use `fstest.MapFS`; no real file I/O in unit tests.

### Unit: `internal/action/`

- `resume_test.go`: asserts `exec.Cmd.Path/Args` for a shell-escaping case (`cwd = /tmp/has space/'quote`), including the `cd … && exec claude --resume <id>` shape. Does not fork.
- `clipboard_test.go`: picker selects pbcopy on darwin; tmux fallback when `$TMUX` set; OSC52 fallback bytes match spec; a failing command surfaces a typed error and does not panic.
- `trash_test.go`: file appears in trash dir with timestamped name; repeated deletion of the same id produces unique names; missing source returns an error.

### Unit: `internal/ui/`

Uses `teatest`. Drives `tm.Send(tea.KeyMsg{...})`, inspects final model
state and rendered output:

- `app_test.go`: Init emits scan cmd; EnrichMsg updates the matching item; cursor move cancels old transcript load; search filters case-insensitively; delete confirm Esc cancels; delete confirm Enter moves file and removes item.
- `preview_test.go`: header renders before any turn is loaded; turns append streaming; corrupt session renders error state.
- `list_test.go`: sort is UpdatedAt desc; `--all` surfaces a project column; empty state copy is exact.

### Golden snapshots

`internal/ui/testdata/golden/` holds frame captures for key states
(`list_initial.txt`, `preview_full.txt`, `search_two_matches.txt`,
`delete_confirm.txt`). `-update` flag regenerates; diffs are reviewed manually.

### Integration

`test/integration_test.go` runs against a `t.TempDir()` mirror of
`~/.claude/projects/<enc-cwd>/`, reusing `session/testdata` fixtures.
Covers the scan→enrich→select→preview pipeline and the delete→trash
move. The resume test asserts the built command, never forks `claude`.

### Explicitly not automated

- Real `claude --resume` spawn.
- Real clipboard side effects.
- Terminal color output (visual inspection).
- Large-session scroll feel (benchmarked, not asserted).

### CI

```
test:   go test ./... -race -timeout 60s
cover:  go test ./... -coverprofile=cover.out
lint:   golangci-lint run
bench:  go test ./internal/session/ -bench=. -benchtime=5s
```

Gate on `test` and `lint`. `cover` and `bench` report only.

## Performance targets

- Cold start, 205 sessions, SSD: list first paint <100ms; all enrichment <500ms.
- Cursor change → first turn painted: <50ms.
- Peak memory with one 8MB transcript loaded: ~30MB.

## Non-goals captured for the future

- Disk index cache (only worth it beyond ~5000 sessions).
- Multi-select, bulk operations.
- Watching `~/.claude/projects/` for live changes while TUI is open.
- Graphical renderer for markdown / code blocks inside conversation.
