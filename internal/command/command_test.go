package command

import (
	"strings"
	"testing"
)

func TestParseRecognisesCommands(t *testing.T) {
	cases := []struct {
		line     string
		name     string
		argument string
		ok       bool
	}{
		{"/exit", "/exit", "", true},
		{"/exit\n", "/exit", "", true},
		{"  /exit  ", "/exit", "", true},
		{"/clear", "/clear", "", true},
		{"/start", "/start", "", true},
		{EnableWeb, EnableWeb, "", true},
		{"/model", "/model", "", true},
		{"/model gpt-oss:120b", "/model", "gpt-oss:120b", true},
		{"/model   gpt-oss:120b  ", "/model", "gpt-oss:120b", true},
		{"hello", "", "", false},
		{"hello /exit", "", "", false},
		{"", "", "", false},
		{"   ", "", "", false},
		{"/nope", "", "", false},
		{"/models", "", "", false},
		// A command that takes no argument must not swallow one.
		{"/clear now", "/clear", "", true},
	}
	for _, test := range cases {
		spec, argument, ok := Parse(test.line)
		if ok != test.ok || spec.Name != test.name || argument != test.argument {
			t.Errorf("Parse(%q) = (%q, %q, %v), want (%q, %q, %v)",
				test.line, spec.Name, argument, ok, test.name, test.argument, test.ok)
		}
	}
}

func TestIsCommandDistinguishesUnknownFromPrompt(t *testing.T) {
	for _, line := range []string{"/exit", "/nope", "  /x  "} {
		if !IsCommand(line) {
			t.Errorf("IsCommand(%q) = false, want true", line)
		}
	}
	for _, line := range []string{"hello", "", "1 + 1", "a/b"} {
		if IsCommand(line) {
			t.Errorf("IsCommand(%q) = true, want false", line)
		}
	}
}

func TestMatchIsAPrefixSearchOverTheSpecs(t *testing.T) {
	if got := Match("/"); len(got) != len(Specs) {
		t.Fatalf("a bare slash should list every command, got %v", got)
	}
	if got := Match("/c"); len(got) != 1 || got[0] != "/clear" {
		t.Fatalf("Match(/c) = %v", got)
	}
	if got := Match("/en"); len(got) != 1 || got[0] != EnableWeb {
		t.Fatalf("Match(/en) = %v", got)
	}
	if got := Match("/zz"); len(got) != 0 {
		t.Fatalf("Match(/zz) = %v, want none", got)
	}
}

func TestTableIsConsistent(t *testing.T) {
	descriptions := Descriptions()
	for _, spec := range Specs {
		if !strings.HasPrefix(spec.Name, "/") {
			t.Errorf("%q should start with a slash", spec.Name)
		}
		if spec.Description == "" {
			t.Errorf("%q has no description, so the palette would show a bare name", spec.Name)
		}
		if descriptions[spec.Name] != spec.Description {
			t.Errorf("%q description does not round-trip", spec.Name)
		}
		if spec.TakesArgument && spec.Argument == "" {
			t.Errorf("%q takes an argument but has no hint", spec.Name)
		}
		if !spec.TakesArgument && spec.Argument != "" {
			t.Errorf("%q has an argument hint but does not take one", spec.Name)
		}
	}

	seen := map[string]bool{}
	for _, name := range Names() {
		if seen[name] {
			t.Errorf("%q is listed twice", name)
		}
		seen[name] = true
	}
}

func TestHintAndListAndUnknownNotice(t *testing.T) {
	hint := Hint()
	if !strings.Contains(hint, "/exit") {
		t.Fatalf("Hint() = %q", hint)
	}
	if strings.Contains(hint, EnableWeb) {
		t.Fatalf("the long web tools name belongs on its own line: %q", hint)
	}

	if !strings.Contains(List(), EnableWeb) {
		t.Fatalf("List() should mention every command: %q", List())
	}

	notice := UnknownNotice()
	if !strings.Contains(notice, "/model") || !strings.HasPrefix(notice, "Unknown command") {
		t.Fatalf("UnknownNotice() = %q", notice)
	}
}
