// Package textutil holds small text helpers shared by the UI and the model
// layer.
package textutil

import (
	"fmt"
	"strings"
	"unicode"
)

// SafeLiteral makes control characters visible rather than writing terminal
// commands. Newlines are preserved; tabs and other C0/C1 controls are escaped.
func SafeLiteral(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch {
		case r == '\n':
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\t`)
		case unicode.Is(unicode.Cc, r):
			if r <= 0xFF {
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				fmt.Fprintf(&b, `\u%04x`, r)
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
