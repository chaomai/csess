package session

import (
	"context"
	"errors"
	"io/fs"
	"path"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// Scanner reads session metadata from a Claude projects directory.
//
// The fs.FS passed in is the parent of the projects directory (typically
// ~/.claude); root is the subpath to the projects dir (typically "projects").
type Scanner struct {
	fsys fs.FS
	root string
}

func NewScanner(fsys fs.FS, root string) *Scanner {
	return &Scanner{fsys: fsys, root: root}
}

// Quick lists sessions without opening any jsonl. Scope is either an
// absolute cwd to filter by (e.g. "/Users/x/proj") or "*" for all
// projects. Results are sorted by ModTime desc.
func (s *Scanner) Quick(scope string) ([]Meta, error) {
	var dirs []string
	if scope == "*" {
		entries, err := fs.ReadDir(s.fsys, s.root)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, nil
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				dirs = append(dirs, e.Name())
			}
		}
	} else {
		dirs = []string{EncodeCWD(scope)}
	}

	var out []Meta
	for _, d := range dirs {
		entries, err := fs.ReadDir(s.fsys, path.Join(s.root, d))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			out = append(out, Meta{
				ID:         strings.TrimSuffix(e.Name(), ".jsonl"),
				Path:       path.Join(s.root, d, e.Name()),
				ProjectDir: d,
				SizeBytes:  info.Size(),
				ModTime:    info.ModTime(),
			})
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ModTime.After(out[j].ModTime)
	})
	return out, nil
}

const defaultWorkers = 8

// EnrichAll concurrently fills ExtractMeta fields for each meta and sends
// each completed Meta on ch. It returns when all metas have been attempted
// or ctx is cancelled. It does not close ch.
func (s *Scanner) EnrichAll(ctx context.Context, metas []Meta, ch chan<- Meta) error {
	workers := runtime.NumCPU()
	if workers > defaultWorkers {
		workers = defaultWorkers
	}
	if len(metas) < workers {
		workers = len(metas)
	}
	if workers == 0 {
		return nil
	}

	jobs := make(chan Meta)
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := range jobs {
				if ctx.Err() != nil {
					return
				}
				enriched := s.enrichOne(m)
				select {
				case ch <- enriched:
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	for _, m := range metas {
		select {
		case jobs <- m:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	return nil
}

func (s *Scanner) enrichOne(m Meta) Meta {
	f, err := s.fsys.Open(m.Path)
	if err != nil {
		m.LoadErr = err
		return m
	}
	defer f.Close()
	if err := ExtractMeta(f, &m); err != nil && m.LoadErr == nil {
		m.LoadErr = err
	}
	return m
}
