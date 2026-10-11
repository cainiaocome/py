// Package web is a small client for Ollama Cloud's web search and fetch APIs.
// It mirrors the Python implementation's validation, bounding, and error text.
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	searchURL          = "https://ollama.com/api/web_search"
	fetchURL           = "https://ollama.com/api/web_fetch"
	requestTimeout     = 30 * time.Second
	maxResultsLimit    = 10
	maxResultContent   = 20_000
	maxFetchContent    = 20_000
	maxTitle           = 512
	maxURL             = 2_048
	maxLinks           = 20
	maxResponseBytes   = 8 << 20
	invalidWebResponse = "Ollama returned an invalid web response."
)

type searchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

type searchResponse struct {
	Results []searchResult `json:"results"`
}

type fetchResponse struct {
	Title   string   `json:"title"`
	Content string   `json:"content"`
	Links   []string `json:"links"`
}

// WebTools calls Ollama's fixed web endpoints using a private API key.
type WebTools struct {
	apiKey    string
	client    *http.Client
	searchURL string
	fetchURL  string
}

// New builds a client with the production endpoints and a 30 second timeout.
func New(apiKey string) *WebTools {
	return &WebTools{
		apiKey:    apiKey,
		client:    &http.Client{Timeout: requestTimeout},
		searchURL: searchURL,
		fetchURL:  fetchURL,
	}
}

// Search returns bounded titles, URLs, and snippets, or an {"error": ...} map.
func (w *WebTools) Search(ctx context.Context, query string, maxResults int) map[string]any {
	query = strings.TrimSpace(query)
	if query == "" {
		return errorResult("A non-empty search query is required.")
	}
	if maxResults < 1 || maxResults > maxResultsLimit {
		return errorResult("max_results must be an integer from 1 to 10.")
	}

	data, err := w.post(ctx, w.searchURL, map[string]any{"query": query, "max_results": maxResults})
	if err != nil {
		return errorResult(err.Error())
	}
	var response searchResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return errorResult(invalidWebResponse)
	}

	results := response.Results
	truncated := false
	if len(results) > maxResults {
		results = results[:maxResults]
		truncated = true
	}
	contentLimit := 0
	if len(results) > 0 {
		contentLimit = maxResultContent / len(results)
	}
	bounded := make([]map[string]string, 0, len(results))
	for _, item := range results {
		title, titleCut := truncate(item.Title, maxTitle)
		link, linkCut := truncate(item.URL, maxURL)
		content, contentCut := truncate(item.Content, contentLimit)
		truncated = truncated || titleCut || linkCut || contentCut
		bounded = append(bounded, map[string]string{"title": title, "url": link, "content": content})
	}
	return map[string]any{
		"query":     query,
		"results":   bounded,
		"truncated": truncated,
	}
}

// Fetch returns a bounded page through Ollama's web API, never directly from
// its URL, or an {"error": ...} map.
func (w *WebTools) Fetch(ctx context.Context, rawURL string) map[string]any {
	if !validURL(rawURL) {
		return errorResult("Enter a valid HTTP or HTTPS URL without credentials.")
	}
	if len([]rune(rawURL)) > maxURL {
		return errorResult("The URL must be no longer than 2048 characters.")
	}

	data, err := w.post(ctx, w.fetchURL, map[string]any{"url": rawURL})
	if err != nil {
		return errorResult(err.Error())
	}
	var response fetchResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return errorResult(invalidWebResponse)
	}

	title, titleCut := truncate(response.Title, maxTitle)
	content, contentCut := truncate(response.Content, maxFetchContent)
	truncated := titleCut || contentCut
	links := make([]string, 0, maxLinks)
	for i, link := range response.Links {
		if i >= maxLinks {
			truncated = true
			break
		}
		cut, wasCut := truncate(link, maxURL)
		truncated = truncated || wasCut
		links = append(links, cut)
	}
	return map[string]any{
		"url":       rawURL,
		"title":     title,
		"content":   content,
		"links":     links,
		"truncated": truncated,
	}
}

func (w *WebTools) post(ctx context.Context, endpoint string, payload map[string]any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, errors.New("The Ollama web request failed.")
	}
	requestCtx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("The Ollama web request failed.")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+w.apiKey)

	response, err := w.client.Do(request)
	if err != nil {
		if errors.Is(requestCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("The Ollama web request timed out.")
		}
		return nil, errors.New("The Ollama web service could not be reached.")
	}
	defer response.Body.Close()

	switch {
	case response.StatusCode == http.StatusUnauthorized, response.StatusCode == http.StatusForbidden:
		return nil, errors.New("Ollama rejected web access. Check the API key and permissions.")
	case response.StatusCode == http.StatusTooManyRequests:
		return nil, errors.New("Ollama web access is rate limited. Try again later.")
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return nil, errors.New("The Ollama web request failed.")
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if err != nil {
		return nil, errors.New(invalidWebResponse)
	}
	return data, nil
}

func errorResult(message string) map[string]any {
	return map[string]any{"error": message}
}

// truncate cuts s to limit runes, reporting whether anything was removed.
func truncate(s string, limit int) (string, bool) {
	if limit < 0 {
		limit = 0
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s, false
	}
	return string(runes[:limit]), true
}

func validURL(raw string) bool {
	if raw == "" {
		return false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(parsed.Scheme)
	return (scheme == "http" || scheme == "https") && parsed.Hostname() != "" && parsed.User == nil
}
