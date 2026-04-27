// Package test_test is the integration suite; it reaches across the
// session, action, and ui packages with a real tempdir filesystem.
package test_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"csess/internal/action"
	"csess/internal/session"
)

func TestFullFlow_ScanEnrich(t *testing.T) {
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	projDir := filepath.Join(projects, "-tmp-x")
	os.MkdirAll(projDir, 0755)

	fixture, err := os.Open(filepath.Join("..", "internal", "session", "testdata", "happy.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()
	dst, _ := os.Create(filepath.Join(projDir, "sess-1.jsonl"))
	io.Copy(dst, fixture)
	dst.Close()

	fsys := os.DirFS(root)
	s := session.NewScanner(fsys, "projects")
	metas, err := s.Quick("/tmp/x")
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1 {
		t.Fatalf("scan: %d; want 1", len(metas))
	}

	ch := make(chan session.Meta, 1)
	if err := s.EnrichAll(context.Background(), metas, ch); err != nil {
		t.Fatal(err)
	}
	close(ch)
	got := <-ch
	if got.FirstPrompt != "hello claude" {
		t.Errorf("FirstPrompt = %q", got.FirstPrompt)
	}
}

func TestFullFlow_TrashMovesFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "abc.jsonl")
	os.WriteFile(src, []byte("x"), 0644)
	trashDir := filepath.Join(root, ".trash")

	dst, err := action.Trash(trashDir, src, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("src not removed")
	}
	if _, err := os.Stat(dst); err != nil {
		t.Error("dst missing")
	}
}

func TestFullFlow_ResumeCmdShape(t *testing.T) {
	cmd := action.BuildResumeCmd("/Users/me/proj", "id-1")
	if len(cmd.Args) != 3 {
		t.Fatalf("args = %v", cmd.Args)
	}
	if cmd.Args[2] == "" {
		t.Error("empty script")
	}
}
