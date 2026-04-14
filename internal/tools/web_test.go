package tools

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── web_fetch ─────────────────────────────────────────────────────────────────

func TestWebFetch_PlainText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello from server"))
	}))
	defer srv.Close()

	tool := newWebFetchTool(slog.Default())
	result := tool.Run(map[string]any{"url": srv.URL})

	if result.GetError() != nil {
		t.Fatalf("unexpected error: %v", result.GetError())
	}
	got, _ := result.GetContent().(string)
	if !strings.Contains(got, "hello from server") {
		t.Errorf("expected body content, got %q", got)
	}
}

func TestWebFetch_HTMLStripping(t *testing.T) {
	body := `<!DOCTYPE html><html><head><title>T</title></head><body>
<nav>Menu items here</nav>
<main><article><p>Main article content</p></article></main>
<footer>Footer noise</footer>
</body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	tool := newWebFetchTool(slog.Default())
	result := tool.Run(map[string]any{"url": srv.URL})

	if result.GetError() != nil {
		t.Fatalf("unexpected error: %v", result.GetError())
	}
	got, _ := result.GetContent().(string)
	if !strings.Contains(got, "Main article content") {
		t.Errorf("expected article content in output, got %q", got)
	}
	if strings.Contains(got, "Menu items here") {
		t.Errorf("expected nav noise to be stripped, got %q", got)
	}
	if strings.Contains(got, "Footer noise") {
		t.Errorf("expected footer noise to be stripped, got %q", got)
	}
}

func TestWebFetch_HTMLFallbackNonSemantic(t *testing.T) {
	// Page with no <main>/<article>/<section> — falls back to full walk.
	body := `<!DOCTYPE html><html><body><div><p>Only content</p></div></body></html>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	tool := newWebFetchTool(slog.Default())
	result := tool.Run(map[string]any{"url": srv.URL})

	if result.GetError() != nil {
		t.Fatalf("unexpected error: %v", result.GetError())
	}
	got, _ := result.GetContent().(string)
	if !strings.Contains(got, "Only content") {
		t.Errorf("expected fallback text content, got %q", got)
	}
}

func TestWebFetch_HTTP4xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	tool := newWebFetchTool(slog.Default())
	result := tool.Run(map[string]any{"url": srv.URL})

	if result.GetError() == nil {
		t.Fatal("expected error for HTTP 404, got nil")
	}
	if !strings.Contains(result.GetError().Error(), "404") {
		t.Errorf("expected 404 in error, got %v", result.GetError())
	}
}

func TestWebFetch_TruncationSignal(t *testing.T) {
	// Serve a body larger than maxBodyBytes.
	large := strings.Repeat("x", maxBodyBytes+100)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(large))
	}))
	defer srv.Close()

	tool := newWebFetchTool(slog.Default())
	result := tool.Run(map[string]any{"url": srv.URL})

	got, _ := result.GetContent().(string)
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected truncation notice in output, got %q", got[:min(len(got), 200)])
	}
}

func TestWebFetch_MissingURL(t *testing.T) {
	tool := newWebFetchTool(slog.Default())
	result := tool.Run(map[string]any{})
	if result.GetError() == nil {
		t.Fatal("expected error for missing url param, got nil")
	}
}

// ── web_search ────────────────────────────────────────────────────────────────

func TestWebSearch_ReturnsInstantAnswer(t *testing.T) {
	// Simulate a DDG response with an instant answer.
	ddgResp := map[string]any{
		"Answer":         "1 USD = 0.92 EUR",
		"AnswerType":     "currency",
		"AbstractText":   "",
		"AbstractURL":    "",
		"AbstractSource": "",
		"RelatedTopics":  []any{},
		"Results":        []any{},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ddgResp)
	}))
	defer srv.Close()

	// Swap the DDG API URL by patching via the tool closure; instead, we call
	// the internal helper directly to keep the test self-contained.
	result := buildSearchResults(duckduckGoResponse{
		Answer:     "1 USD = 0.92 EUR",
		AnswerType: "currency",
	}, 10)

	if len(result) == 0 {
		t.Fatal("expected at least one result")
	}
	if !strings.Contains(result[0].Snippet, "0.92") {
		t.Errorf("expected instant answer in snippet, got %q", result[0].Snippet)
	}
	_ = srv // silence unused warning
}

func TestWebSearch_EmptyResultsMessage(t *testing.T) {
	// buildSearchResults with an empty response should return nothing.
	result := buildSearchResults(duckduckGoResponse{}, 10)
	if len(result) != 0 {
		t.Errorf("expected empty results for empty DDG response, got %d", len(result))
	}
}

func TestWebSearch_MaxResultsRespected(t *testing.T) {
	ddg := duckduckGoResponse{}
	for i := range 20 {
		ddg.RelatedTopics = append(ddg.RelatedTopics, relatedTopic{
			Text:     strings.Repeat("topic", i+1),
			FirstURL: "https://example.com",
		})
	}
	results := buildSearchResults(ddg, 5)
	if len(results) > 5 {
		t.Errorf("expected at most 5 results, got %d", len(results))
	}
}

func TestWebSearch_MissingQuery(t *testing.T) {
	tool := newWebSearchTool(slog.Default())
	result := tool.Run(map[string]any{})
	if result.GetError() == nil {
		t.Fatal("expected error for missing query param, got nil")
	}
}

// ── htmlToText ────────────────────────────────────────────────────────────────

func TestHTMLToText_PrefersSemanticElements(t *testing.T) {
	src := `<html><body>
<nav>Nav noise</nav>
<header>Header noise</header>
<main><p>Real content</p></main>
<footer>Footer noise</footer>
</body></html>`

	got := htmlToText(src)
	if !strings.Contains(got, "Real content") {
		t.Errorf("expected main content, got %q", got)
	}
	for _, noise := range []string{"Nav noise", "Header noise", "Footer noise"} {
		if strings.Contains(got, noise) {
			t.Errorf("expected %q to be stripped, got %q", noise, got)
		}
	}
}

func TestHTMLToText_FallsBackWithoutSemanticElements(t *testing.T) {
	src := `<html><body><div><p>Fallback content</p></div></body></html>`
	got := htmlToText(src)
	if !strings.Contains(got, "Fallback content") {
		t.Errorf("expected fallback content, got %q", got)
	}
}

func TestHTMLToText_ScriptsAndStylesStripped(t *testing.T) {
	src := `<html><body><script>alert(1)</script><style>.a{}</style><p>Visible</p></body></html>`
	got := htmlToText(src)
	if strings.Contains(got, "alert") || strings.Contains(got, ".a{}") {
		t.Errorf("expected script/style stripped, got %q", got)
	}
	if !strings.Contains(got, "Visible") {
		t.Errorf("expected visible text, got %q", got)
	}
}
