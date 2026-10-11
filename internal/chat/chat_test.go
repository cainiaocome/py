package chat

import (
	"testing"

	"github.com/cainiaocome/py/internal/llm"
)

func toolCall(name, arguments string) llm.ToolCall {
	return llm.ToolCall{Type: "function", Function: llm.FunctionCall{Name: name, Arguments: arguments}}
}

func TestToolActivity(t *testing.T) {
	cases := []struct {
		name string
		call llm.ToolCall
		want string
	}{
		{"search", toolCall("web_search", `{"query":"cats"}`), "Searching the web: cats"},
		{"fetch", toolCall("web_fetch", `{"url":"https://example.com"}`), "Fetching page: https://example.com"},
		{"unknown", toolCall("other", `{}`), ""},
		{"blank query", toolCall("web_search", `{"query":"  "}`), "Searching the web…"},
		{"control chars", toolCall("web_search", `{"query":"a\u0001b"}`), "Searching the web: a b"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := ToolActivity(test.call); got != test.want {
				t.Fatalf("ToolActivity = %q, want %q", got, test.want)
			}
		})
	}
}

func TestJoinText(t *testing.T) {
	if got := joinText([]string{"a", "", "b"}, "c"); got != "a\n\nb\n\nc" {
		t.Fatalf("joinText = %q", got)
	}
	if got := joinText(nil, ""); got != "" {
		t.Fatalf("joinText empty = %q", got)
	}
}
