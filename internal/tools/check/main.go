package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
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
			}
		}
		b.WriteByte(c)
	}
	return b.String()
}

func main() {
	input := `90\t\n91\tfunc (p *Preview)`
	fmt.Printf("input:  %q\n", input)
	u := unescapeLiterals(input)
	fmt.Printf("after unescape: %q\n", u)
	w := ansi.Wordwrap(u, 80, " ,.-")
	fmt.Printf("after wrap:     %q\n", w)
}
