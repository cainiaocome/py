package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStreamChatAssemblesTextAndToolCalls(t *testing.T) {
	var sawAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		frames := []string{
			`{"choices":[{"delta":{"content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"web_search","arguments":"{\"query\":"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"cats\"}"}}]}}]}`,
			`[DONE]`,
		}
		for _, frame := range frames {
			fmt.Fprintf(w, "data: %s\n\n", frame)
		}
	}))
	defer server.Close()

	client := New(server.URL, "secret")
	var text strings.Builder
	message, err := client.StreamChat(context.Background(), "model", []Message{{Role: "user", Content: "hi"}}, nil,
		func(delta string) { text.WriteString(delta) })
	if err != nil {
		t.Fatal(err)
	}
	if text.String() != "Hello" {
		t.Fatalf("text = %q", text.String())
	}
	if message.Content != "Hello" {
		t.Fatalf("content = %q", message.Content)
	}
	if sawAuth != "Bearer secret" {
		t.Fatalf("authorization = %q", sawAuth)
	}
	if len(message.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d", len(message.ToolCalls))
	}
	call := message.ToolCalls[0]
	if call.ID != "call_1" || call.Function.Name != "web_search" {
		t.Fatalf("tool call = %+v", call)
	}
	if call.Function.Arguments != `{"query":"cats"}` {
		t.Fatalf("arguments = %q", call.Function.Arguments)
	}
}

func TestListModelsDeduplicatesAndSorts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{
			{"id": "b"}, {"id": "a"}, {"id": "b"},
		}})
	}))
	defer server.Close()

	models, err := New(server.URL, "k").ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(models, ",") != "a,b" {
		t.Fatalf("models = %v", models)
	}
}

func TestStreamChatHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := New(server.URL, "k").StreamChat(context.Background(), "m", nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v", err)
	}
}

func TestStreamChatSendsRequestShape(t *testing.T) {
	var method, path string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	tools := []Tool{{Type: "function", Function: ToolFunction{Name: "web_search", Parameters: map[string]any{"type": "object"}}}}
	_, err := New(server.URL, "k").StreamChat(context.Background(), "m", []Message{{Role: "user", Content: "hi"}}, tools, nil)
	if err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/chat/completions" {
		t.Fatalf("request = %s %s", method, path)
	}
	if body["model"] != "m" || body["stream"] != true {
		t.Fatalf("body = %v", body)
	}
	if messages, _ := body["messages"].([]any); len(messages) != 1 {
		t.Fatalf("messages = %v", body["messages"])
	}
	if got, _ := body["tools"].([]any); len(got) != 1 {
		t.Fatalf("tools = %v", body["tools"])
	}
}

func TestStreamChatOmitsToolsWhenEmpty(t *testing.T) {
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	_, _ = New(server.URL, "k").StreamChat(context.Background(), "m", nil, nil, nil)
	if _, ok := body["tools"]; ok {
		t.Fatalf("tools should be omitted when empty: %v", body)
	}
}

func TestStreamChatIgnoresMalformedFramesAndFinishesWithoutDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, ": keep-alive\n\n")
		fmt.Fprint(w, "data: not json\n\n")
		fmt.Fprint(w, "data: {}\n\n")
		fmt.Fprint(w, "event: ping\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
		// No [DONE]: the stream simply ends.
	}))
	defer server.Close()

	var text strings.Builder
	message, err := New(server.URL, "k").StreamChat(context.Background(), "m", nil, nil,
		func(delta string) { text.WriteString(delta) })
	if err != nil {
		t.Fatal(err)
	}
	if text.String() != "ok" || message.Content != "ok" {
		t.Fatalf("text = %q, content = %q", text.String(), message.Content)
	}
}

func TestStreamChatAssemblesToolCallsInIndexOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":1,\"id\":\"c1\",\"function\":{\"name\":\"web_fetch\",\"arguments\":\"{}\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"c0\",\"function\":{\"name\":\"web_search\",\"arguments\":\"{}\"}}]}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()

	message, err := New(server.URL, "k").StreamChat(context.Background(), "m", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(message.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d", len(message.ToolCalls))
	}
	if message.ToolCalls[0].Function.Name != "web_search" || message.ToolCalls[1].Function.Name != "web_fetch" {
		t.Fatalf("tool calls out of order: %+v", message.ToolCalls)
	}
}

func TestStreamChatCancellationPropagates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := New(server.URL, "k").StreamChat(ctx, "m", nil, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestStreamChatHTTPErrorDoesNotLeakTheBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("SECRET_UPSTREAM_BODY"))
	}))
	defer server.Close()

	_, err := New(server.URL, "k").StreamChat(context.Background(), "m", nil, nil, nil)
	if err == nil || strings.Contains(err.Error(), "SECRET_UPSTREAM_BODY") {
		t.Fatalf("err = %v", err)
	}
}

func TestListModelsErrors(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer bad.Close()
	if _, err := New(bad.URL, "k").ListModels(context.Background()); err == nil {
		t.Fatal("want error for non-2xx")
	}

	invalid := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":"not a list"}`))
	}))
	defer invalid.Close()
	if _, err := New(invalid.URL, "k").ListModels(context.Background()); err == nil {
		t.Fatal("want error for invalid json")
	}
}
