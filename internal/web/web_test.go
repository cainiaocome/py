package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
