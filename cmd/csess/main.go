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
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"csess/internal/action"
	"csess/internal/session"
	"csess/internal/ui"
)

func main() {
	var (
		hereFlag    = flag.Bool("here", false, "only show sessions for the current directory (default: all projects)")
		projectsDir = flag.String("projects-dir", "", "override ~/.claude/projects")
		trashDir    = flag.String("trash-dir", "", "override ~/.claude/.trash")
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

	cwd, err := os.Getwd()
	if err != nil {
		fatal("cwd: %v", err)
	}

	allMode := !*hereFlag
	scope := "*"
	if *hereFlag {
		scope = cwd
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

	var pendingResume *session.Meta
	cfg := ui.AppConfig{
		Width: 120, Height: 40, AllMode: allMode, Scope: scope,
		LoadTranscript: buildLoadTranscript(fsys),
		ResumeSelected: buildResume(&pendingResume),
		CopySelected:   buildCopy(clip),
		TrashSelected:  buildTrash(*trashDir),
	}
	app := ui.NewApp(cfg)

	p := tea.NewProgram(app, tea.WithAltScreen())
	// Send the initial scan synthetically.
	go p.Send(ui.ScanMsg{Metas: metas})
	// Bridge enrich messages into the program.
	go bridgeEnrich(p, scanner, metas)

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
// tea.Program via p.Send.
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
