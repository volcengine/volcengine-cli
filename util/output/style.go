package output

// Table styling.
//
// AWS CLI colorizes table cells through ColorizedStyler (table.py). Note that
// AWS itself has disabled title and header styling — both methods return the
// text unchanged — leaving only row content colored. This file follows the same
// conservative shape: headers get bold, cells get a subtle color, nothing else.
//
// Styling is opt-in via Options.Color and must never affect layout: every width
// calculation runs on stripANSI(text), so a colored table aligns exactly like an
// uncolored one.

import "strings"

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiCyan   = "\x1b[36m"
	ansiEscape = '\x1b'
)

func (o Options) styleHeader(s string) string {
	if !o.Color || s == "" {
		return s
	}
	return ansiBold + s + ansiReset
}

func (o Options) styleCell(s string) string {
	if !o.Color || s == "" {
		return s
	}
	return ansiCyan + s + ansiReset
}

// stripANSI removes CSI sequences so display width reflects visible characters.
//
// Cell content itself can never contain ESC — escapeCellString rewrites control
// characters as visible escapes — so the only sequences here are the ones added
// by styleHeader/styleCell.
func stripANSI(s string) string {
	if !strings.ContainsRune(s, ansiEscape) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] != ansiEscape {
			b.WriteRune(runes[i])
			continue
		}
		// Skip "ESC [ ... <final byte>"; a final byte is in the range @ to ~.
		j := i + 1
		if j < len(runes) && runes[j] == '[' {
			j++
			for j < len(runes) && !(runes[j] >= '@' && runes[j] <= '~') {
				j++
			}
			i = j
			continue
		}
		i = j
	}
	return b.String()
}
