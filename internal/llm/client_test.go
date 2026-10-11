package llm

import (
	"context"
	"encoding/json"
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
