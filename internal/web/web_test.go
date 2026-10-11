package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestTools(t *testing.T, handler http.HandlerFunc) *WebTools {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	tools := New("secret")
	tools.searchURL = server.URL + "/search"
	tools.fetchURL = server.URL + "/fetch"
	tools.client = server.Client()
	return tools
}

func TestSearchBoundsAndTruncates(t *testing.T) {
	tools := newTestTools(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("authorization = %q", got)
		}
		results := make([]map[string]string, 3)
		for i := range results {
			results[i] = map[string]string{"title": "t", "url": "u", "content": strings.Repeat("x", 30_000)}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	})

	result := tools.Search(context.Background(), "hello", 2)
	if _, ok := result["error"]; ok {
		t.Fatalf("unexpected error: %v", result)
	}
	items, _ := result["results"].([]map[string]string)
	if len(items) != 2 {
		t.Fatalf("results = %d, want 2", len(items))
	}
	if result["truncated"] != true {
		t.Fatal("want truncated true")
	}
	if got := len([]rune(items[0]["content"])); got != maxResultContent/2 {
		t.Fatalf("content length = %d, want %d", got, maxResultContent/2)
	}
}

func TestSearchRejectsBlankQuery(t *testing.T) {
	tools := New("secret")
	result := tools.Search(context.Background(), "   ", 5)
	if result["error"] != "A non-empty search query is required." {
		t.Fatalf("error = %v", result["error"])
	}
}

func TestSearchRejectsBadMaxResults(t *testing.T) {
	tools := New("secret")
	if result := tools.Search(context.Background(), "q", 0); result["error"] != "max_results must be an integer from 1 to 10." {
		t.Fatalf("error = %v", result["error"])
	}
}

func TestFetchValidatesURL(t *testing.T) {
	tools := New("secret")
	for _, value := range []string{"", "ftp://example.com", "https://user:pass@example.com", "not a url"} {
		result := tools.Fetch(context.Background(), value)
		if result["error"] != "Enter a valid HTTP or HTTPS URL without credentials." {
			t.Fatalf("url %q -> %v", value, result["error"])
		}
	}
}

func TestFetchMapsAuthError(t *testing.T) {
	tools := newTestTools(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	result := tools.Fetch(context.Background(), "https://example.com")
	if result["error"] != "Ollama rejected web access. Check the API key and permissions." {
		t.Fatalf("error = %v", result["error"])
	}
}

func TestFetchTruncates(t *testing.T) {
	tools := newTestTools(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"title":   "title",
			"content": strings.Repeat("y", 25_000),
			"links":   []string{"https://example.com"},
		})
	})
	result := tools.Fetch(context.Background(), "https://example.com")
	if result["truncated"] != true {
		t.Fatal("want truncated true")
	}
	if got := len([]rune(result["content"].(string))); got != maxFetchContent {
		t.Fatalf("content length = %d", got)
	}
}

func TestSearchPostsFixedRequestAndMapsResults(t *testing.T) {
	var method, path, auth, contentType string
	var body map[string]any
	tools := newTestTools(t, func(w http.ResponseWriter, r *http.Request) {
		method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		contentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&body)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]string{
			{"title": "T", "url": "https://example.com", "content": "C"},
		}})
	})

	result := tools.Search(context.Background(), "  spaced out  ", 3)
	if method != http.MethodPost {
		t.Fatalf("method = %s", method)
	}
	if path != "/search" {
		t.Fatalf("path = %s", path)
	}
	if auth != "Bearer secret" {
		t.Fatalf("authorization = %q", auth)
	}
	if contentType != "application/json" {
		t.Fatalf("content type = %q", contentType)
	}
	if body["query"] != "spaced out" {
		t.Fatalf("query = %v", body["query"])
	}
	if body["max_results"] != float64(3) {
		t.Fatalf("max_results = %v", body["max_results"])
	}
	if result["query"] != "spaced out" || result["truncated"] != false {
		t.Fatalf("result = %v", result)
	}
	items := result["results"].([]map[string]string)
	if len(items) != 1 || items[0]["title"] != "T" || items[0]["url"] != "https://example.com" {
		t.Fatalf("items = %v", items)
	}
}

func TestSearchNoResultsIsNotTruncated(t *testing.T) {
	tools := newTestTools(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	})
	result := tools.Search(context.Background(), "q", 5)
	if result["truncated"] != false {
		t.Fatalf("empty results should not be truncated: %v", result)
	}
	if items := result["results"].([]map[string]string); len(items) != 0 {
		t.Fatalf("items = %v", items)
	}
}

func TestRejectedInputsNeverReachTheServer(t *testing.T) {
	requests := 0
	tools := newTestTools(t, func(w http.ResponseWriter, r *http.Request) { requests++ })
	tools.Search(context.Background(), "   ", 5)
	tools.Search(context.Background(), "q", 0)
	tools.Search(context.Background(), "q", 11)
	tools.Fetch(context.Background(), "")
	tools.Fetch(context.Background(), "ftp://example.com")
	tools.Fetch(context.Background(), "https://user:pass@example.com")
	tools.Fetch(context.Background(), "not a url")
	if requests != 0 {
		t.Fatalf("validation should happen before the request; got %d requests", requests)
	}
}

func TestFetchPostsURLAndBoundsLinks(t *testing.T) {
	var method, path string
	var body map[string]any
	tools := newTestTools(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		links := make([]string, maxLinks+5)
		for i := range links {
			links[i] = "https://example.com"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"title": "T", "content": "C", "links": links})
	})

	result := tools.Fetch(context.Background(), "https://example.com/page")
	if method != http.MethodPost || path != "/fetch" {
		t.Fatalf("request = %s %s", method, path)
	}
	if body["url"] != "https://example.com/page" {
		t.Fatalf("body url = %v", body["url"])
	}
	if result["url"] != "https://example.com/page" || result["title"] != "T" {
		t.Fatalf("result = %v", result)
	}
	if links := result["links"].([]string); len(links) != maxLinks {
		t.Fatalf("links = %d, want %d", len(links), maxLinks)
	}
	if result["truncated"] != true {
		t.Fatal("dropping links should mark the result truncated")
	}
}

func TestFetchRejectsOversizedURLBeforeRequest(t *testing.T) {
	requests := 0
	tools := newTestTools(t, func(w http.ResponseWriter, r *http.Request) { requests++ })
	long := "https://example.com/" + strings.Repeat("a", maxURL)
	result := tools.Fetch(context.Background(), long)
	if result["error"] != "The URL must be no longer than 2048 characters." {
		t.Fatalf("error = %v", result["error"])
	}
	if requests != 0 {
		t.Fatal("oversized URL should be rejected before the request")
	}
}

func TestHTTPErrorsAreSafeAndDoNotLeakTheBody(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusUnauthorized, "Ollama rejected web access. Check the API key and permissions."},
		{http.StatusForbidden, "Ollama rejected web access. Check the API key and permissions."},
		{http.StatusTooManyRequests, "Ollama web access is rate limited. Try again later."},
		{http.StatusBadRequest, "The Ollama web request failed."},
		{http.StatusNotFound, "The Ollama web request failed."},
		{http.StatusInternalServerError, "The Ollama web request failed."},
		{http.StatusServiceUnavailable, "The Ollama web request failed."},
	}
	for _, test := range cases {
		tools := newTestTools(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(test.status)
			_, _ = w.Write([]byte("SECRET_UPSTREAM_BODY"))
		})
		result := tools.Search(context.Background(), "q", 5)
		message, _ := result["error"].(string)
		if message != test.want {
			t.Fatalf("status %d -> %q, want %q", test.status, message, test.want)
		}
		if strings.Contains(message, "SECRET_UPSTREAM_BODY") {
			t.Fatalf("status %d leaked the response body", test.status)
		}
	}
}

func TestInvalidResponseIsReportedSafely(t *testing.T) {
	for _, payload := range []string{"not json", `[1,2,3]`, ""} {
		tools := newTestTools(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(payload))
		})
		if result := tools.Search(context.Background(), "q", 5); result["error"] != invalidWebResponse {
			t.Fatalf("search payload %q -> %v", payload, result["error"])
		}
		if result := tools.Fetch(context.Background(), "https://example.com"); result["error"] != invalidWebResponse {
			t.Fatalf("fetch payload %q -> %v", payload, result["error"])
		}
	}

	tools := newTestTools(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"results":"nope"}`))
	})
	if result := tools.Search(context.Background(), "q", 5); result["error"] != invalidWebResponse {
		t.Fatalf("wrong result type -> %v", result["error"])
	}

	links := newTestTools(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"links":"nope"}`))
	})
	if result := links.Fetch(context.Background(), "https://example.com"); result["error"] != invalidWebResponse {
		t.Fatalf("wrong links type -> %v", result["error"])
	}
}

func TestTimeoutAndNetworkErrorsAreSafe(t *testing.T) {
	slow := newTestTools(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if result := slow.Search(ctx, "q", 5); result["error"] != "The Ollama web request timed out." {
		t.Fatalf("timeout error = %v", result["error"])
	}

	offline := New("secret")
	offline.searchURL = "http://127.0.0.1:1/search"
	offline.fetchURL = "http://127.0.0.1:1/fetch"
	if result := offline.Search(context.Background(), "q", 5); result["error"] != "The Ollama web service could not be reached." {
		t.Fatalf("network error = %v", result["error"])
	}
	if result := offline.Fetch(context.Background(), "https://example.com"); result["error"] != "The Ollama web service could not be reached." {
		t.Fatalf("network error = %v", result["error"])
	}
}
