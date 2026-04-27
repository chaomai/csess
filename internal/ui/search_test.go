package ui

import (
	"testing"

	"csess/internal/session"
)

func TestSearchFilter_MatchesPrompt(t *testing.T) {
	all := []session.Meta{
		{ID: "aaa", FirstPrompt: "fix login bug", Enriched: true},
		{ID: "bbb", FirstPrompt: "write tests", Enriched: true},
		{ID: "ccc", FirstPrompt: "debug LOGIN again", Enriched: true},
	}
	got := FilterMetas(all, "login")
	if len(got) != 2 {
		t.Fatalf("len = %d; want 2", len(got))
	}
	// Case-insensitive; relative order preserved (stable)
	if got[0].ID != "aaa" || got[1].ID != "ccc" {
		t.Errorf("order = %v", []string{got[0].ID, got[1].ID})
	}
}

func TestSearchFilter_MatchesCWDAndBranch(t *testing.T) {
	all := []session.Meta{
		{ID: "a", CWD: "/Users/me/proj-one", Enriched: true},
		{ID: "b", CWD: "/Users/me/other", GitBranch: "proj-one-feature", Enriched: true},
		{ID: "c", CWD: "/tmp", Enriched: true},
	}
	got := FilterMetas(all, "proj-one")
	if len(got) != 2 {
		t.Errorf("got %d; want 2", len(got))
	}
}

func TestSearchFilter_IDPrefix(t *testing.T) {
	all := []session.Meta{
		{ID: "02c753f2-xxx", FirstPrompt: "something", Enriched: true},
		{ID: "039f60b8-xxx", FirstPrompt: "02c753 prefix in prompt", Enriched: true},
	}
	got := FilterMetas(all, "02c753")
	if got[0].ID != "02c753f2-xxx" {
		t.Errorf("ID-prefix should rank first; got %q", got[0].ID)
	}
}

func TestSearchFilter_EmptyQueryReturnsAll(t *testing.T) {
	all := []session.Meta{{ID: "a"}, {ID: "b"}}
	got := FilterMetas(all, "")
	if len(got) != 2 {
		t.Errorf("len = %d", len(got))
	}
}
