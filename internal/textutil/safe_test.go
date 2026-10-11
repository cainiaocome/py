package textutil

import "testing"

func TestSafeLiteral(t *testing.T) {
	got := SafeLiteral("a\tb\nc\x1bd\x7f")
	want := `a\tb` + "\n" + `c\x1bd\x7f`
	if got != want {
		t.Fatalf("SafeLiteral = %q, want %q", got, want)
	}
}

func TestSafeLiteralKeepsPrintable(t *testing.T) {
	if got := SafeLiteral("héllo 世界"); got != "héllo 世界" {
		t.Fatalf("SafeLiteral changed printable text: %q", got)
	}
}
