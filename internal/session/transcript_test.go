package session

import (
	"context"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestStreamTurns_Happy(t *testing.T) {
	data, _ := os.ReadFile("testdata/happy.jsonl")
	fsys := fstest.MapFS{"s.jsonl": {Data: data}}

	ch := make(chan Turn, 16)
	err := StreamTurns(context.Background(), fsys, "s.jsonl", ch)
	close(ch)
	if err != nil {
		t.Fatal(err)
	}

	var turns []Turn
	for tn := range ch {
		turns = append(turns, tn)
	}
	if len(turns) != 4 {
		t.Fatalf("got %d turns; want 4 (2 user + 2 asst)", len(turns))
	}
	// order: user "hello claude", asst "Hi there!", user "run ls", asst "Running." + tool_use line
	if turns[0].Role != "user" || turns[0].Text != "hello claude" {
		t.Errorf("turn0 = %+v", turns[0])
	}
	if turns[1].Role != "assistant" || turns[1].Text != "Hi there!" {
		t.Errorf("turn1 = %+v", turns[1])
	}
	if turns[2].Role != "user" || turns[2].Text != "run ls" {
		t.Errorf("turn2 = %+v", turns[2])
	}
	// asst msg2: thinking dropped, text "Running.", tool_use one-line
	if turns[3].Role != "assistant" {
		t.Errorf("turn3.Role = %q", turns[3].Role)
	}
	if !strings.Contains(turns[3].Text, "Running.") {
		t.Errorf("missing text: %q", turns[3].Text)
	}
	if !strings.Contains(turns[3].Text, "→ Bash(") {
		t.Errorf("missing tool_use line: %q", turns[3].Text)
	}
	if strings.Contains(turns[3].Text, "I should run ls") {
		t.Errorf("thinking leaked into text: %q", turns[3].Text)
	}
}

func TestStreamTurns_CancelStops(t *testing.T) {
	// Large synthetic file
	var b strings.Builder
	for i := 0; i < 10000; i++ {
		b.WriteString(`{"type":"user","message":{"role":"user","content":"x"},"userType":"external","timestamp":"2026-01-15T10:00:00.000Z"}` + "\n")
	}
	fsys := fstest.MapFS{"big.jsonl": {Data: []byte(b.String())}}

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan Turn) // unbuffered
	done := make(chan error, 1)
	go func() { done <- StreamTurns(ctx, fsys, "big.jsonl", ch) }()

	<-ch        // read exactly one
	cancel()
	// drain whatever is in flight
	go func() { for range ch {} }()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("StreamTurns did not honor cancel within 200ms")
	}
}
