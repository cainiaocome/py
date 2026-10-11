package tui

import "testing"

func TestTextWidthHelpersUseDisplayCells(t *testing.T) {
	if got := runeLen("\u4f60\u597d"); got != 2 {
		t.Fatalf("runeLen = %d, want 2", got)
	}
	if got := displayWidth("\u4f60\u597d"); got != 4 {
		t.Fatalf("displayWidth = %d, want 4", got)
	}
	if got := firstRunes("\u4f60\u597d", 1); got != "\u4f60" {
		t.Fatalf("firstRunes = %q, want the first glyph", got)
	}

	if got := padRight("\u4f60\u597d", 5); displayWidth(got) != 5 {
		t.Fatalf("padRight -> %q (%d cells), want 5 cells", got, displayWidth(got))
	}
	if got := padRight("abc", 2); got != "abc" {
		t.Fatalf("padRight should never trim: %q", got)
	}

	if got := truncateRunes("\u4f60\u597d", 3); got != "\u4f60" {
		t.Fatalf("truncateRunes should not split a wide glyph: %q", got)
	}
	if got := truncateRunes("abc", 5); got != "abc" {
		t.Fatalf("truncateRunes should keep short strings: %q", got)
	}
	if got := truncateRunes("abc", -1); got != "" {
		t.Fatalf("truncateRunes(-1) = %q, want empty", got)
	}
}
