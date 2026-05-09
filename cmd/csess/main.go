package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"csess/internal/action"
	"csess/internal/search"
	"csess/internal/session"
	"csess/internal/ui"
)

func main() {
	var (
		hereFlag      = flag.Bool("here", false, "only show sessions for the current directory (default: all projects)")
		projectsDir   = flag.String("projects-dir", "", "override ~/.claude/projects")
		trashDir      = flag.String("trash-dir", "", "override ~/.claude/.trash")
		bookmarksFile = flag.String("bookmarks-file", "", "override ~/.claude/csess/bookmarks.json")
		contextN      = flag.Int("context", 3, "lines of context around each match in the context preview")
		maxMatches    = flag.Int("max-matches", 1000, "per-file match cap passed to rg (--max-count)")
	)
	flag.Parse()

	home, err := os.UserHomeDir()
	if err != nil {
		fatal("home dir: %v", err)
	}
	if *projectsDir == "" {
		*projectsDir = filepath.Join(home, ".claude", "projects")
	}
	if *trashDir == "" {
		*trashDir = filepath.Join(home, ".claude", ".trash")
	}
	if *bookmarksFile == "" {
		*bookmarksFile = filepath.Join(home, ".claude", "csess", "bookmarks.json")
	}
	bookmarkStore := session.NewBookmarkStore(*bookmarksFile)
	bookmarks, loadErr := bookmarkStore.Load()
	if loadErr != nil {
		fmt.Fprintf(os.Stderr, "bookmarks load: %v (continuing with empty set)\n", loadErr)
		bookmarks = nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		fatal("cwd: %v", err)
	}

	allMode := !*hereFlag
	scope := "*"
	if *hereFlag {
		scope = cwd
	}

	// In default (all-projects) mode, hide sessions whose cwd is the home
	// directory itself — they're usually accidental `claude` invocations
	// from a login shell and clutter the list. `--here` is an explicit
	// pointer, so we respect it and leave scope alone there.
	var hiddenProjectDir string
	if allMode {
		hiddenProjectDir = session.EncodeCWD(home)
	}

	// The scanner needs an fs.FS rooted at projectsDir's parent, with
	// "projects" as the root subpath. os.DirFS gives us that easily.
	parent, leaf := filepath.Split(*projectsDir)
	parent = filepath.Clean(parent)
	leaf = filepath.Clean(leaf)
	fsys := os.DirFS(parent)
	if _, err := fs.Stat(fsys, leaf); err != nil {
		fmt.Fprintf(os.Stderr, "no sessions found at %s\n", *projectsDir)
		os.Exit(0)
	}
	scanner := session.NewScanner(fsys, leaf)

	metas, err := scanner.Quick(scope)
	if err != nil {
		fatal("scan: %v", err)
	}
	if hiddenProjectDir != "" {
		kept := metas[:0]
		for _, m := range metas {
			if m.ProjectDir != hiddenProjectDir {
				kept = append(kept, m)
			}
		}
		metas = kept
	}
	if len(metas) == 0 {
		if *hereFlag {
			fmt.Fprintf(os.Stderr, "no sessions for %s. drop --here to see all projects\n", cwd)
		} else {
			fmt.Fprintf(os.Stderr, "no sessions found at %s\n", *projectsDir)
		}
		os.Exit(0)
	}

	clip := action.NewClipboard(action.ClipEnv{
		GOOS:    runtime.GOOS,
		TMUX:    os.Getenv("TMUX"),
		WAYLAND: os.Getenv("WAYLAND_DISPLAY"),
		DISPLAY: os.Getenv("DISPLAY"),
	}, os.Stderr)

	rgRunner := &search.Options{
		ProjectsDir: *projectsDir,
		Context:     *contextN,
		MaxMatches:  *maxMatches,
	}
	if hiddenProjectDir != "" {
		// Prefix with **/ so rg's globber anchors the match inside the
		// search root instead of treating the leading '-' as a flag-like
		// literal that fails to match any path component.
		rgRunner.ExcludeGlobs = []string{"!**/" + hiddenProjectDir + "/**"}
	}

	var pendingResume *session.Meta
	cfg := ui.AppConfig{
		Width: 120, Height: 40, AllMode: allMode, Scope: scope,
		LoadTranscript: buildLoadTranscript(fsys),
		ResumeSelected: buildResume(&pendingResume),
		CopySelected:   buildCopy(clip),
		TrashSelected:  buildTrash(*trashDir),
		RunSearch:      buildRunSearch(rgRunner),
		SaveBookmarks:  buildSaveBookmarks(bookmarkStore),
	}
	app := ui.NewApp(cfg)

	for _, b := range bookmarks {
		app.SetBookmark(b.ID, b.StarredAt)
	}

	p := tea.NewProgram(app, tea.WithAltScreen())
	// Send the initial scan synthetically.
	go p.Send(ui.ScanMsg{Metas: metas})
	// Bridge enrich messages into the program.
	go bridgeEnrich(p, scanner, metas)
	go bridgeBookmarkEnrich(p, *projectsDir, bookmarks, metas)

	if _, err := p.Run(); err != nil {
		fatal("tui: %v", err)
	}

	// After the TUI exits, replace this process with `claude --resume`
	// if the user chose a session. Using syscall.Exec so the shell sees
	// claude as the current foreground process — quitting claude returns
	// the user to the shell, not to csess.
	if pendingResume != nil {
		execClaude(*pendingResume)
	}
}

// bridgeEnrich runs Scanner.EnrichAll and forwards each EnrichMsg to the
// tea.Program via p.Send. A final EnrichDoneMsg is sent once all metas
// have been forwarded, so the UI can re-sort by UpdatedAt.
func bridgeEnrich(p *tea.Program, s *session.Scanner, metas []session.Meta) {
	ch := make(chan session.Meta, 16)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = s.EnrichAll(ctx, metas, ch)
		close(done)
	}()

	for {
		select {
		case m, ok := <-ch:
			if !ok {
				p.Send(ui.EnrichDoneMsg{})
				return
			}
			p.Send(ui.EnrichMsg{Meta: m})
		case <-done:
			// Drain remaining.
			for {
				select {
				case m := <-ch:
					p.Send(ui.EnrichMsg{Meta: m})
				default:
					p.Send(ui.EnrichDoneMsg{})
					return
				}
			}
		}
	}
}

func buildLoadTranscript(fsys fs.FS) func(seq int, m session.Meta, ctx context.Context) tea.Cmd {
	return func(seq int, m session.Meta, ctx context.Context) tea.Cmd {
		return func() tea.Msg {
			ch := make(chan session.Turn, 64)
			go func() {
				defer close(ch)
				_ = session.StreamTurns(ctx, fsys, m.Path, ch)
			}()
			// Blocking drain: return all turns as a batched message.
			var turns []session.Turn
			for t := range ch {
				turns = append(turns, t)
			}
			return ui.BatchTurnsMsg{Seq: seq, Turns: turns}
		}
	}
}

func buildResume(out **session.Meta) func(m session.Meta) tea.Cmd {
	return func(m session.Meta) tea.Cmd {
		if _, err := exec.LookPath("claude"); err != nil {
			return func() tea.Msg {
				return ui.BannerMsg{Text: "claude not in PATH", IsError: true, Until: time.Now().Add(5 * time.Second)}
			}
		}
		return func() tea.Msg {
			mc := m
			*out = &mc
			return tea.Quit()
		}
	}
}

// execClaude replaces the current process image with `claude --resume`
// rooted at the session's original working directory. On success this
// never returns; on failure it prints to stderr and exits non-zero.
func execClaude(m session.Meta) {
	claudePath, err := exec.LookPath("claude")
	if err != nil {
		fmt.Fprintf(os.Stderr, "claude not in PATH: %v\n", err)
		os.Exit(1)
	}
	if m.CWD != "" {
		if err := os.Chdir(m.CWD); err != nil {
			fmt.Fprintf(os.Stderr, "cd %s: %v\n", m.CWD, err)
			os.Exit(1)
		}
	}
	args := []string{"claude", "--resume", m.ID}
	if err := syscall.Exec(claudePath, args, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "exec claude: %v\n", err)
		os.Exit(1)
	}
}

func buildCopy(c *action.Clipboard) func(m session.Meta) tea.Cmd {
	return func(m session.Meta) tea.Cmd {
		return func() tea.Msg {
			if err := c.Copy(m.ID); err != nil {
				return ui.BannerMsg{Text: "copy failed: " + err.Error(), IsError: true}
			}
			return ui.BannerMsg{Text: "copied " + m.ID}
		}
	}
}

func buildTrash(trashDir string) func(m session.Meta) tea.Cmd {
	return func(m session.Meta) tea.Cmd {
		return func() tea.Msg {
			_, err := action.Trash(trashDir, m.Path, time.Now())
			return ui.DeleteDoneMsg{ID: m.ID, Err: err}
		}
	}
}

func fatal(f string, args ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", args...)
	os.Exit(1)
}

func buildRunSearch(opts *search.Options) func(ctx context.Context, query string) ([]search.Match, error) {
	return func(ctx context.Context, query string) ([]search.Match, error) {
		o := *opts // copy so each call can have its own query
		o.Query = query
		return search.Run(ctx, o)
	}
}

func buildSaveBookmarks(store *session.BookmarkStore) func([]session.Bookmark) tea.Cmd {
	return func(bs []session.Bookmark) tea.Cmd {
		return func() tea.Msg {
			return ui.SaveBookmarksDoneMsg{Err: store.Save(bs)}
		}
	}
}

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
