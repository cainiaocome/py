package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/cainiaocome/py/internal/llm"
)

type capturedRequest struct {
	Model    string        `json:"model"`
	Stream   bool          `json:"stream"`
	Messages []llm.Message `json:"messages"`
	Tools    []llm.Tool    `json:"tools"`
}

func newTestChat(t *testing.T, handler http.HandlerFunc) *Chat {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewWithEndpoints("secret", "model-a", Endpoints{ChatBase: server.URL, WebBase: server.URL})
}

func capture(w http.ResponseWriter, r *http.Request, t *testing.T) capturedRequest {
	t.Helper()
	var request capturedRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Errorf("decode request: %v", err)
	}
	return request
}

func sseText(w http.ResponseWriter, parts ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, part := range parts {
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s}}]}\n\n", strconv.Quote(part))
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func sseToolCall(w http.ResponseWriter, id, name, arguments string) {
	w.Header().Set("Content-Type", "text/event-stream")
	fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":%s,\"function\":{\"name\":%s,\"arguments\":%s}}]}}]}\n\n",
		strconv.Quote(id), strconv.Quote(name), strconv.Quote(arguments))
	fmt.Fprint(w, "data: [DONE]\n\n")
}

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

// chatHandler routes the OpenAI chat endpoint and the Ollama web endpoints.
func chatHandler(t *testing.T, web http.HandlerFunc, completions func(call int, request capturedRequest, w http.ResponseWriter)) http.HandlerFunc {
	t.Helper()
	if web == nil {
		web = func(w http.ResponseWriter, r *http.Request) {}
	}
	calls := 0
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/web_search", "/api/web_fetch":
			web(w, r)
		case "/chat/completions":
			calls++
			completions(calls, capture(w, r, t), w)
		default:
			http.NotFound(w, r)
		}
	}
}

func TestStreamRetainsContextAndClearResets(t *testing.T) {
	var requests []capturedRequest
	c := newTestChat(t, chatHandler(t, nil, func(_ int, request capturedRequest, w http.ResponseWriter) {
		requests = append(requests, request)
		sseText(w, "ok")
	}))

	for _, prompt := range []string{"first", "second"} {
		if _, err := c.Stream(context.Background(), prompt, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(requests[1].Messages) != 4 {
		t.Fatalf("second request should carry the first exchange: %+v", requests[1].Messages)
	}
	if requests[1].Messages[1].Content != "first" || requests[1].Messages[2].Role != "assistant" {
		t.Fatalf("history = %+v", requests[1].Messages)
	}

	c.Clear()
	if _, err := c.Stream(context.Background(), "third", nil, nil); err != nil {
		t.Fatal(err)
	}
	if third := requests[2].Messages; len(third) != 2 || third[1].Content != "third" {
		t.Fatalf("Clear should forget history: %+v", third)
	}
}

func TestStreamFailedTurnPreservesHistory(t *testing.T) {
	var requests []capturedRequest
	c := newTestChat(t, chatHandler(t, nil, func(call int, request capturedRequest, w http.ResponseWriter) {
		if call == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		requests = append(requests, request)
		sseText(w, "ok")
	}))

	if _, err := c.Stream(context.Background(), "first", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stream(context.Background(), "second", nil, nil); err == nil {
		t.Fatal("want an error for the failed turn")
	}
	if _, err := c.Stream(context.Background(), "third", nil, nil); err != nil {
		t.Fatal(err)
	}
	last := requests[len(requests)-1].Messages
	if len(last) != 4 || last[1].Content != "first" || last[3].Content != "third" {
		t.Fatalf("failed turn leaked into history: %+v", last)
	}
}

func TestSwitchModelValidatesAndPreservesContext(t *testing.T) {
	var last capturedRequest
	c := newTestChat(t, chatHandler(t, nil, func(_ int, request capturedRequest, w http.ResponseWriter) {
		last = request
		sseText(w, "ok")
	}))

	if _, err := c.Stream(context.Background(), "first", nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.SwitchModel("   "); err == nil {
		t.Fatal("a blank model should be rejected")
	}
	if err := c.SwitchModel("two words"); err == nil {
		t.Fatal("a model with whitespace should be rejected")
	}
	if c.ModelName() != "model-a" {
		t.Fatalf("rejected switch changed the model: %q", c.ModelName())
	}
	if err := c.SwitchModel(" model-b "); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stream(context.Background(), "second", nil, nil); err != nil {
		t.Fatal(err)
	}
	if last.Model != "model-b" {
		t.Fatalf("model sent = %q", last.Model)
	}
	if len(last.Messages) != 4 {
		t.Fatalf("context was not retained across a switch: %+v", last.Messages)
	}
}

func TestSwitchModelKeepsWebTools(t *testing.T) {
	var last capturedRequest
	c := newTestChat(t, chatHandler(t, nil, func(_ int, request capturedRequest, w http.ResponseWriter) {
		last = request
		sseText(w, "ok")
	}))

	if !c.EnableWeb() {
		t.Fatal("the first enable should change state")
	}
	if err := c.SwitchModel("model-b"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stream(context.Background(), "hi", nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(last.Tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(last.Tools))
	}
}

func TestWebToolsAreOptInIdempotentAndPreserveContext(t *testing.T) {
	var last capturedRequest
	c := newTestChat(t, chatHandler(t, nil, func(_ int, request capturedRequest, w http.ResponseWriter) {
		last = request
		sseText(w, "ok")
	}))

	if _, err := c.Stream(context.Background(), "first", nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(last.Tools) != 0 {
		t.Fatalf("tools should be opt-in: %+v", last.Tools)
	}
	if !strings.Contains(last.Messages[0].Content, "Web access is disabled") {
		t.Fatalf("plain instructions missing: %q", last.Messages[0].Content)
	}

	if !c.EnableWeb() {
		t.Fatal("enable should change state")
	}
	if c.EnableWeb() {
		t.Fatal("a second enable should be a no-op")
	}
	if _, err := c.Stream(context.Background(), "second", nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(last.Tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(last.Tools))
	}
	if !strings.Contains(last.Messages[0].Content, "web search and page fetching") {
		t.Fatalf("web instructions missing: %q", last.Messages[0].Content)
	}
	if len(last.Messages) != 4 {
		t.Fatalf("enabling web should retain context: %+v", last.Messages)
	}
}

func TestStreamRunsToolAndFollowsUp(t *testing.T) {
	var followUp capturedRequest
	c := newTestChat(t, chatHandler(t,
		func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{
				{"title": "T", "url": "https://example.com", "content": "C"},
			}})
		},
		func(call int, request capturedRequest, w http.ResponseWriter) {
			if call == 1 {
				sseToolCall(w, "call_1", "web_search", `{"query":"cats"}`)
				return
			}
			followUp = request
			sseText(w, "Final")
		}))

	c.EnableWeb()
	var activity []string
	text, err := c.Stream(context.Background(), "find cats", func(status string) { activity = append(activity, status) }, nil)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Final" {
		t.Fatalf("text = %q", text)
	}
	if len(activity) != 1 || activity[0] != "Searching the web: cats" {
		t.Fatalf("activity = %v", activity)
	}
	if len(followUp.Messages) != 4 {
		t.Fatalf("messages = %+v", followUp.Messages)
	}
	tool := followUp.Messages[3]
	if tool.Role != "tool" || tool.ToolCallID != "call_1" {
		t.Fatalf("tool message = %+v", tool)
	}
	if !strings.Contains(tool.Content, "https://example.com") {
		t.Fatalf("tool content = %s", tool.Content)
	}
}

func TestStreamJoinsModelTurnsWithABlankLine(t *testing.T) {
	var text strings.Builder
	c := newTestChat(t, chatHandler(t, nil, func(call int, _ capturedRequest, w http.ResponseWriter) {
		if call == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"thinking\"}}]}\n\n")
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c\",\"function\":{\"name\":\"web_search\",\"arguments\":\"{\\\"query\\\":\\\"q\\\"}\"}}]}}]}\n\n")
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		sseText(w, "answer")
	}))

	c.EnableWeb()
	got, err := c.Stream(context.Background(), "q", nil, func(full string) {
		text.Reset()
		text.WriteString(full)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "thinking\n\nanswer" {
		t.Fatalf("text = %q", got)
	}
	if text.String() != "thinking\n\nanswer" {
		t.Fatalf("final callback = %q", text.String())
	}
}

func TestToolErrorsAreAvailableToTheModel(t *testing.T) {
	var followUp capturedRequest
	c := newTestChat(t, chatHandler(t,
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		func(call int, request capturedRequest, w http.ResponseWriter) {
			if call == 1 {
				sseToolCall(w, "c", "web_search", `{"query":"x"}`)
				return
			}
			followUp = request
			sseText(w, "ok")
		}))

	c.EnableWeb()
	if _, err := c.Stream(context.Background(), "q", nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(followUp.Messages[3].Content, "The Ollama web request failed.") {
		t.Fatalf("the model should see the tool error: %+v", followUp.Messages[3])
	}
}

func TestUnknownToolAndInvalidArgumentsReturnErrors(t *testing.T) {
	cases := []struct {
		name string
		call func(w http.ResponseWriter)
		want string
	}{
		{"unknown", func(w http.ResponseWriter) { sseToolCall(w, "c", "mystery", `{}`) }, "Unknown tool."},
		{"invalid args", func(w http.ResponseWriter) { sseToolCall(w, "c", "web_search", `{not json`) }, "A non-empty search query is required."},
	}
	for _, test := range cases {
		var followUp capturedRequest
		c := newTestChat(t, chatHandler(t, nil, func(call int, request capturedRequest, w http.ResponseWriter) {
			if call == 1 {
				test.call(w)
				return
			}
			followUp = request
			sseText(w, "ok")
		}))
		if _, err := c.Stream(context.Background(), "q", nil, nil); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(followUp.Messages[3].Content, test.want) {
			t.Fatalf("%s: tool = %+v", test.name, followUp.Messages[3])
		}
	}
}

func TestCancellationPropagatesAndPreservesHistory(t *testing.T) {
	var requests []capturedRequest
	c := newTestChat(t, chatHandler(t, nil, func(_ int, request capturedRequest, w http.ResponseWriter) {
		requests = append(requests, request)
		sseText(w, "ok")
	}))

	if _, err := c.Stream(context.Background(), "first", nil, nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Stream(ctx, "cancelled", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if _, err := c.Stream(context.Background(), "third", nil, nil); err != nil {
		t.Fatal(err)
	}
	last := requests[len(requests)-1].Messages
	if len(last) != 4 || last[3].Content != "third" {
		t.Fatalf("a cancelled turn should not be retained: %+v", last)
	}
}
