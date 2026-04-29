package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"csess/internal/session"
)

func main() {
	path := os.Args[1]
	ch := make(chan session.Turn, 64)
	go func() {
		defer close(ch)
		_ = session.StreamTurns(context.Background(), os.DirFS("/"), strings.TrimPrefix(path, "/"), ch)
	}()
	for t := range ch {
		if t.LineNo < 510 || t.LineNo > 520 { continue }
		fmt.Printf("=== turn role=%s line=%d ===\n", t.Role, t.LineNo)
		txt := t.Text
		if len(txt) > 300 { txt = txt[:300] + "..." }
		fmt.Printf("Text: %q\n\n", txt)
	}
}
