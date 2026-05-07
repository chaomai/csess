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
			return []Bookmark{}, nil
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
	if f.Bookmarks == nil {
		return []Bookmark{}, nil
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
	dir, err := os.Open(filepath.Dir(s.path))
	if err != nil {
		return fmt.Errorf("open bookmarks dir: %w", err)
	}
	if err := dir.Sync(); err != nil {
		dir.Close()
		return fmt.Errorf("fsync bookmarks dir: %w", err)
	}
	if err := dir.Close(); err != nil {
		return fmt.Errorf("close bookmarks dir: %w", err)
	}
	return nil
}
