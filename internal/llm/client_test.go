package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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

// A rejected request must say why: "request failed (HTTP 401)" alone leaves the
// reader with nothing to act on. The reason is written by the endpoint, so it is
// bounded and collapsed before it can reach a terminal.
func TestStreamChatHTTPErrorReportsABoundedReason(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "openai style error envelope",
			body: `{"error":{"message":"invalid api key","type":"invalid_request_error"}}`,
			want: "invalid api key",
		},
		{
			name: "plain text",
			body: "model not found",
			want: "model not found",
		},
		{
			name: "newlines and tabs collapse onto one line",
			body: "line one\n\tline two",
			want: "line one line two",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(testCase.body))
			}))
			defer server.Close()

			_, err := New(server.URL, "k").StreamChat(context.Background(), "m", nil, nil, nil)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), "HTTP 401") || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("err = %q, want it to carry %q", err, testCase.want)
			}
			if strings.Contains(err.Error(), "\n") {
				t.Fatalf("err = %q, want a single line", err)
			}
		})
	}
}

func TestStreamChatHTTPErrorTruncatesAHugeBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(strings.Repeat("boom ", 100_000)))
	}))
	defer server.Close()

	_, err := New(server.URL, "k").StreamChat(context.Background(), "m", nil, nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if runes := len([]rune(err.Error())); runes > 400 {
		t.Fatalf("err is %d runes, want it bounded", runes)
	}
}

// A transport failure has to name the endpoint: "dial tcp: connection refused"
// on its own does not say what could not be reached.
func TestStreamChatTransportFailureNamesTheHost(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close() // nothing is listening now, so dialling is refused

	_, err = New("http://"+address+"/v1", "k").StreamChat(context.Background(), "m", nil, nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), address) {
		t.Fatalf("err = %q, want it to name %s", err, address)
	}
}

func TestListModelsTransportFailureNamesTheHost(t *testing.T) {
	_, err := New("http://127.0.0.1:1/v1", "k").ListModels(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("err = %q, want it to name the host", err)
	}
}

func TestListModelsReportsTheServersReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
	}))
	defer server.Close()

	_, err := New(server.URL, "k").ListModels(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("err = %v, want it to carry the server's reason", err)
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

func TestPingReportsAnUnauthorizedKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("Ping sent %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Unauthorized","type":"api_error","param":null,"code":null}}`))
	}))
	defer server.Close()

	err := New(server.URL, "k").Ping(context.Background(), "m")
	if err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Fatalf("err = %v, want the server's reason", err)
	}
}

func TestPingSucceedsOnAnAcceptedKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer k" {
			t.Errorf("Authorization = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"max_tokens":1`) {
			t.Errorf("body = %s, want the smallest request", body)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"pong"}}]}`))
	}))
	defer server.Close()

	if err := New(server.URL, "k").Ping(context.Background(), "m"); err != nil {
		t.Fatalf("err = %v", err)
	}
}

// An endpoint that is down has to be named, or the report cannot be acted on.
func TestPingNamesAnUnreachableHost(t *testing.T) {
	err := New("http://127.0.0.1:1/v1", "k").Ping(context.Background(), "m")
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1:1") {
		t.Fatalf("err = %v, want it to name the host", err)
	}
}

func TestErrorReasonReadsTheOpenAIEnvelope(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"nested message", `{"error":{"message":"invalid api key","type":"x"}}`, "invalid api key"},
		{"bare string", `{"error":"rate limited"}`, "rate limited"},
		{"top level message", `{"message":"model not found"}`, "model not found"},
		{"not json", "plain   prose\nhere", "plain prose here"},
		{"empty", "", ""},
		{"unparseable object", `{"error":{`, `{"error":{`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := errorReason(strings.NewReader(testCase.body)); got != testCase.want {
				t.Fatalf("errorReason(%q) = %q, want %q", testCase.body, got, testCase.want)
			}
		})
	}
}
