package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

const (
	defaultFetchTimeout  = 15
	defaultSearchTimeout = 10
	maxBodyBytes         = 512 * 1024 // 512 KB — avoids pulling full megabyte pages
)

// NewWebTools returns the web tool suite: web_fetch and web_search.
func NewWebTools(logger *slog.Logger) []Tool {
	return []Tool{
		newWebFetchTool(logger),
		newWebSearchTool(logger),
	}
}

// newWebFetchTool fetches a URL and returns its content as readable plain text.
// HTML is stripped to text; other content types are returned as-is up to the
// size cap.
func newWebFetchTool(logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to fetch.",
			},
			"timeout_seconds": map[string]any{
				"type":        "integer",
				"description": "Request timeout in seconds. Defaults to 15.",
			},
		},
		"required": []string{"url"},
	}

	return NewTool(
		"web_fetch",
		"Fetch the content of a URL and return it as plain text. HTML pages are converted to readable text. Use this to retrieve documentation, APIs, exchange rates, weather, or any web resource.",
		schema,
		func(params map[string]any) ToolResult {
			rawURL, ok := getString(params, "url")
			if !ok || strings.TrimSpace(rawURL) == "" {
				return NewToolResult("", fmt.Errorf("web_fetch: 'url' parameter is required"))
			}
			timeoutSec := getInt(params, "timeout_seconds", defaultFetchTimeout)

			logger.Debug("web_fetch", "url", rawURL)

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
			if err != nil {
				return NewToolResult("", fmt.Errorf("web_fetch: invalid request: %w", err))
			}
			req.Header.Set("User-Agent", "feino-agent/1.0 (+https://github.com/odinnordico/feino)")
			req.Header.Set("Accept", "text/html,application/xhtml+xml,application/json,text/plain;q=0.9,*/*;q=0.8")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return NewToolResult("", fmt.Errorf("web_fetch: request failed: %w", err))
			}
			defer resp.Body.Close()

			limited := io.LimitReader(resp.Body, maxBodyBytes+1)
			body, err := io.ReadAll(limited)
			if err != nil {
				return NewToolResult("", fmt.Errorf("web_fetch: reading body: %w", err))
			}

			truncated := len(body) > maxBodyBytes
			if truncated {
				body = body[:maxBodyBytes]
			}
			ct := resp.Header.Get("Content-Type")
			var text string
			if strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml") {
				text = htmlToText(string(body))
			} else {
				text = string(body)
			}
			if truncated {
				text += fmt.Sprintf("\n[truncated at %d KB — use a more specific URL or path to retrieve the full content]", maxBodyBytes/1024)
			}

			if resp.StatusCode >= 400 {
				return NewToolResult(text, fmt.Errorf("web_fetch: HTTP %d from %s", resp.StatusCode, rawURL))
			}
			return NewToolResult(text, nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// newWebSearchTool searches the web using the DuckDuckGo Instant Answer API.
// No API key required. Returns titles, URLs, and snippets as JSON.
func newWebSearchTool(logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return. Defaults to 10.",
			},
		},
		"required": []string{"query"},
	}

	return NewTool(
		"web_search",
		"Search the web using DuckDuckGo and return titles, URLs, and snippets. Use this to find current information, documentation, prices, news, or anything that requires a live web search.",
		schema,
		func(params map[string]any) ToolResult {
			query, ok := getString(params, "query")
			if !ok || strings.TrimSpace(query) == "" {
				return NewToolResult("", fmt.Errorf("web_search: 'query' parameter is required"))
			}
			maxResults := getInt(params, "max_results", 10)

			logger.Debug("web_search", "query", query)

			ctx, cancel := context.WithTimeout(context.Background(), defaultSearchTimeout*time.Second)
			defer cancel()

			apiURL := "https://api.duckduckgo.com/?q=" + url.QueryEscape(query) + "&format=json&no_html=1&no_redirect=1&skip_disambig=1"
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
			if err != nil {
				return NewToolResult("", fmt.Errorf("web_search: invalid request: %w", err))
			}
			req.Header.Set("User-Agent", "feino-agent/1.0 (+https://github.com/odinnordico/feino)")

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return NewToolResult("", fmt.Errorf("web_search: request failed: %w", err))
			}
			defer resp.Body.Close()

			var ddg duckduckGoResponse
			if err := json.NewDecoder(resp.Body).Decode(&ddg); err != nil {
				return NewToolResult("", fmt.Errorf("web_search: decode response: %w", err))
			}

			results := buildSearchResults(ddg, maxResults)
			if len(results) == 0 {
				return NewToolResult("No results found. Try web_fetch with a direct URL instead.", nil)
			}

			out, _ := json.MarshalIndent(results, "", "  ")
			return NewToolResult(string(out), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// duckduckGoResponse is a partial mapping of the DDG instant-answer JSON schema.
type duckduckGoResponse struct {
	AbstractText   string          `json:"AbstractText"`
	AbstractURL    string          `json:"AbstractURL"`
	AbstractSource string          `json:"AbstractSource"`
	Answer         string          `json:"Answer"`
	AnswerType     string          `json:"AnswerType"`
	RelatedTopics  []relatedTopic  `json:"RelatedTopics"`
	Results        []instantResult `json:"Results"`
}

type relatedTopic struct {
	Text     string `json:"Text"`
	FirstURL string `json:"FirstURL"`
	// Nested topics (sub-categories) are skipped.
	Topics []relatedTopic `json:"Topics,omitempty"`
}

type instantResult struct {
	Text     string `json:"Text"`
	FirstURL string `json:"FirstURL"`
}

type searchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

func buildSearchResults(ddg duckduckGoResponse, max int) []searchResult {
	var out []searchResult

	// Instant answer (e.g. calculator, unit conversion, currency).
	if ddg.Answer != "" {
		out = append(out, searchResult{
			Title:   ddg.AnswerType,
			URL:     "",
			Snippet: ddg.Answer,
		})
	}

	// Abstract (Wikipedia-style summary).
	if ddg.AbstractText != "" {
		out = append(out, searchResult{
			Title:   ddg.AbstractSource,
			URL:     ddg.AbstractURL,
			Snippet: ddg.AbstractText,
		})
	}

	// Direct results.
	for _, r := range ddg.Results {
		if len(out) >= max {
			break
		}
		if r.Text == "" {
			continue
		}
		out = append(out, searchResult{Snippet: r.Text, URL: r.FirstURL})
	}

	// Related topics.
	for _, rt := range ddg.RelatedTopics {
		if len(out) >= max {
			break
		}
		if rt.Text == "" {
			continue
		}
		out = append(out, searchResult{Snippet: rt.Text, URL: rt.FirstURL})
	}

	return out
}

// noise tags are always skipped regardless of which extraction strategy is used.
var noiseTags = map[string]bool{
	"script": true, "style": true, "head": true, "noscript": true,
	"nav": true, "footer": true, "aside": true, "header": true,
	"form": true, "button": true, "iframe": true, "svg": true,
}

// htmlToText extracts visible text from an HTML document.
//
// Strategy:
//  1. Parse the document.
//  2. Look for <main>, <article>, or <section> elements. If found, extract
//     text only from those subtrees (skipping noise tags).
//  3. If none are found, fall back to a full-document walk (still skipping
//     noise tags).
//
// The result has blank lines collapsed so it is readable in a context window.
func htmlToText(src string) string {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return src
	}

	// findSemanticRoots returns all <main> / <article> / <section> nodes.
	var findSemanticRoots func(*html.Node) []*html.Node
	findSemanticRoots = func(n *html.Node) []*html.Node {
		var roots []*html.Node
		if n.Type == html.ElementNode {
			switch n.Data {
			case "main", "article", "section":
				roots = append(roots, n)
				return roots // don't descend into nested semantic elements
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			roots = append(roots, findSemanticRoots(c)...)
		}
		return roots
	}

	extractText := func(root *html.Node, sb *strings.Builder) {
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.ElementNode && noiseTags[n.Data] {
				return
			}
			if n.Type == html.TextNode {
				if t := strings.TrimSpace(n.Data); t != "" {
					sb.WriteString(t)
					sb.WriteByte('\n')
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(root)
	}

	var sb strings.Builder
	if roots := findSemanticRoots(doc); len(roots) > 0 {
		for _, r := range roots {
			extractText(r, &sb)
		}
	} else {
		extractText(doc, &sb)
	}

	// Collapse blank lines.
	lines := strings.Split(sb.String(), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
