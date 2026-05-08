package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBookmarkStore_LoadMissingReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	store := NewBookmarkStore(path)
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load missing = %v; want nil err", err)
	}
	if got == nil {
		t.Error("Load missing = nil slice; want empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("Load missing = %d entries; want 0", len(got))
	}
}

func TestBookmarkStore_LoadMalformedReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	store := NewBookmarkStore(path)
	if _, err := store.Load(); err == nil {
		t.Error("Load malformed = nil err; want error")
	}
}

func TestBookmarkStore_LoadWrongVersionReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.json")
	body, _ := json.Marshal(map[string]any{
		"version":   2,
		"bookmarks": []Bookmark{},
	})
	if err := os.WriteFile(path, body, 0644); err != nil {
		t.Fatal(err)
	}
	store := NewBookmarkStore(path)
	if _, err := store.Load(); err == nil {
		t.Error("Load wrong version = nil err; want error")
	}
}

func TestBookmarkStore_SaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bm.json")
	store := NewBookmarkStore(path)
	want := []Bookmark{
		{ID: "abc", StarredAt: time.Unix(200, 0).UTC()},
		{ID: "def", StarredAt: time.Unix(100, 0).UTC()},
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("loaded %d; want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID || !got[i].StarredAt.Equal(want[i].StarredAt) {
			t.Errorf("entry[%d] = %+v; want %+v", i, got[i], want[i])
		}
	}
}

func TestBookmarkStore_SaveCreatesParentDir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "subdir", "bm.json")
	store := NewBookmarkStore(path)
	if err := store.Save([]Bookmark{{ID: "x", StarredAt: time.Unix(1, 0)}}); err != nil {
		t.Fatalf("Save with missing parent: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func TestBookmarkStore_LoadIgnoresLeftoverTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bm.json")
	store := NewBookmarkStore(path)
	if err := store.Save([]Bookmark{{ID: "real", StarredAt: time.Unix(1, 0)}}); err != nil {
		t.Fatal(err)
	}
	// Simulate interrupted prior save: leftover .tmp file with garbage.
	if err := os.WriteFile(path+".tmp", []byte("garbage"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(got) != 1 || got[0].ID != "real" {
		t.Errorf("Load after .tmp leftover = %+v; want single 'real' entry", got)
	}
}

func TestErrMissingIsExported(t *testing.T) {
	var err error = ErrMissing
	if !errors.Is(err, ErrMissing) {
		t.Error("ErrMissing should satisfy errors.Is against itself")
	}
	if err.Error() == "" {
		t.Error("ErrMissing.Error() should be non-empty")
	}
}
