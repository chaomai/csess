package session

import (
	"testing"
	"testing/fstest"
	"time"
)

func TestScannerQuick_CurrentCWD(t *testing.T) {
	fsys := fstest.MapFS{
		"projects/-tmp-a/aaa.jsonl":  {Data: []byte("{}"), ModTime: time.Unix(100, 0)},
		"projects/-tmp-a/bbb.jsonl":  {Data: []byte("{}"), ModTime: time.Unix(200, 0)},
		"projects/-tmp-b/ccc.jsonl":  {Data: []byte("{}"), ModTime: time.Unix(300, 0)},
		"projects/-tmp-a/ignore.txt": {Data: []byte("x")},
	}
	s := NewScanner(fsys, "projects")

	got, err := s.Quick("/tmp/a")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2", len(got))
	}
	// Newest first
	if got[0].ID != "bbb" {
		t.Errorf("got[0].ID = %q; want bbb", got[0].ID)
	}
	if got[1].ID != "aaa" {
		t.Errorf("got[1].ID = %q; want aaa", got[1].ID)
	}
	if got[0].ProjectDir != "-tmp-a" {
		t.Errorf("ProjectDir = %q", got[0].ProjectDir)
	}
}

func TestScannerQuick_AllProjects(t *testing.T) {
	fsys := fstest.MapFS{
		"projects/-tmp-a/aaa.jsonl": {Data: []byte("{}"), ModTime: time.Unix(100, 0)},
		"projects/-tmp-b/ccc.jsonl": {Data: []byte("{}"), ModTime: time.Unix(300, 0)},
	}
	s := NewScanner(fsys, "projects")

	got, err := s.Quick("*")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2", len(got))
	}
}

func TestScannerQuick_MissingDir(t *testing.T) {
	s := NewScanner(fstest.MapFS{}, "projects")
	got, err := s.Quick("/tmp/nowhere")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d; want 0", len(got))
	}
}
