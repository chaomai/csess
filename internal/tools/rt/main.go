package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"csess/internal/session"
)

func unescapeLiterals(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case 't':
				b.WriteByte('\t')
				i++
				continue
			case 'r':
				b.WriteByte('\r')
				i++
				continue
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func main() {
	path := os.Args[1]
	targetLine := 517
	ch := make(chan session.Turn, 64)
	go func() {
		defer close(ch)
		_ = session.StreamTurns(context.Background(), os.DirFS("/"), strings.TrimPrefix(path, "/"), ch)
	}()
	for t := range ch {
		if t.LineNo != targetLine { continue }
		fmt.Printf("=== turn role=%s line=%d ===\n", t.Role, t.LineNo)
		fmt.Printf("raw text (first 200 bytes, hex): ")
		for i := 0; i < 40 && i < len(t.Text); i++ {
			fmt.Printf("%02x ", t.Text[i])
		}
		fmt.Println()
		u := unescapeLiterals(t.Text)
		fmt.Printf("after unescape (first 40 bytes, hex): ")
		for i := 0; i < 40 && i < len(u); i++ {
			fmt.Printf("%02x ", u[i])
		}
		fmt.Println()
		w := ansi.Wordwrap(u, 130, " ,.-、。，")
		fmt.Println("after wrap (first 200 chars, quoted):")
		if len(w) > 200 { w = w[:200] }
		fmt.Printf("%q\n", w)
		break
	}
}
