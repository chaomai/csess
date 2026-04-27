package action

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTrash_MovesFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "abc.jsonl")
	os.MkdirAll(filepath.Dir(src), 0755)
	os.WriteFile(src, []byte("hi"), 0644)

	trash := filepath.Join(dir, ".trash")
	now := time.Date(2026, 4, 27, 15, 4, 5, 0, time.UTC)

	dst, err := Trash(trash, src, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source still exists")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("dst missing: %v", err)
	}
	name := filepath.Base(dst)
	if !strings.HasPrefix(name, "abc.20260427-150405.") {
		t.Errorf("dst name = %q", name)
	}
}

func TestTrash_CollisionGetsUniqueName(t *testing.T) {
	dir := t.TempDir()
	trash := filepath.Join(dir, ".trash")

	write := func(name string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("x"), 0644)
		return p
	}

	now := time.Date(2026, 4, 27, 15, 4, 5, 0, time.UTC)
	first, err := Trash(trash, write("a.jsonl"), now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Trash(trash, write("a.jsonl"), now)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Errorf("same path: %s", first)
	}
}

func TestTrash_SourceMissing(t *testing.T) {
	dir := t.TempDir()
	_, err := Trash(filepath.Join(dir, ".trash"), filepath.Join(dir, "nope.jsonl"), time.Now())
	if err == nil {
		t.Error("expected error")
	}
}
