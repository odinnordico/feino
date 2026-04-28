package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	httpDefaultTimeoutSec = 30
	httpMaxBodyKB         = 512
	httpDefaultMaxBodyKB  = httpMaxBodyKB
)

// httpDoer is the HTTP client interface used by http_request.
// Replaced in tests with a lightweight fake.
var httpDoer interface {
	Do(*http.Request) (*http.Response, error)
} = http.DefaultClient

// validMethods is the set of HTTP methods http_request accepts.
var validMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true,
	"DELETE": true, "HEAD": true, "OPTIONS": true,
}

// NewHTTPTools returns the http_request tool.
func NewHTTPTools(logger *slog.Logger) []Tool {
	return []Tool{newHTTPRequestTool(logger)}
}

// httpResponse is the JSON payload returned to the agent.
type httpResponse struct {
	StatusCode    int               `json:"status_code"`
	Status        string            `json:"status"`
	Headers       map[string]string `json:"headers"`
	Body          string            `json:"body"`
	BodyTruncated bool              `json:"body_truncated,omitempty"`
	BodyBase64    string            `json:"body_base64,omitempty"` // set when body is not valid UTF-8
	LatencyMs     float64           `json:"latency_ms"`
}

func newHTTPRequestTool(logger *slog.Logger) Tool {
	return NewTool(
		"http_request",
		"Send an HTTP request and return the response status, headers, and body. "+
			"Supports all standard methods (GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS), "+
			"custom headers, JSON/text/form bodies, and Bearer token authentication. "+
			"Use this instead of web_fetch when you need full request control: "+
			"POST with a JSON body, multipart uploads, custom auth headers, or REST API calls.",
		map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"url": map[string]any{
					"type":        "string",
					"description": "The request URL.",
				},
				"method": map[string]any{
					"type":        "string",
					"description": `HTTP method. One of GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS. Defaults to "GET".`,
					"enum":        []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
				},
				"headers": map[string]any{
					"type":                 "object",
					"description":          "Request headers as a JSON object mapping header names to values.",
					"additionalProperties": map[string]any{"type": "string"},
				},
				"body": map[string]any{
					"type":        "string",
					"description": "Request body as a UTF-8 string. For JSON payloads pass the JSON text directly.",
				},
				"body_base64": map[string]any{
					"type":        "string",
					"description": "Request body as standard base64. Use for binary payloads. Mutually exclusive with body.",
				},
				"form": map[string]any{
					"type":                 "object",
					"description":          "Form fields to send as application/x-www-form-urlencoded. Sets Content-Type automatically. Mutually exclusive with body.",
					"additionalProperties": map[string]any{"type": "string"},
				},
				"timeout_seconds": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Request timeout in seconds (1–120). Defaults to %d.", httpDefaultTimeoutSec),
				},
				"follow_redirects": map[string]any{
					"type":        "boolean",
					"description": "Follow HTTP redirects (3xx). Defaults to true.",
				},
				"max_response_kb": map[string]any{
					"type":        "integer",
					"description": fmt.Sprintf("Cap response body size in KiB (1–4096). Defaults to %d.", httpDefaultMaxBodyKB),
				},
			},
			"required": []string{"url"},
		},
		func(params map[string]any) ToolResult {
			rawURL, ok := getString(params, "url")
			if !ok || strings.TrimSpace(rawURL) == "" {
				return NewToolResult("", fmt.Errorf("http_request: url is required"))
			}

			method := strings.ToUpper(getStringDefault(params, "method", "GET"))
			if !validMethods[method] {
				return NewToolResult("", fmt.Errorf("http_request: unsupported method %q", method))
			}

			timeoutSec := min(max(getInt(params, "timeout_seconds", httpDefaultTimeoutSec), 1), 120)
			maxKB := min(max(getInt(params, "max_response_kb", httpDefaultMaxBodyKB), 1), 4096)

			followRedirects := getBool(params, "follow_redirects", true)

			// ── build request body ────────────────────────────────────────────
			var bodyReader io.Reader
			contentTypeOverride := ""

			switch {
			case params["body_base64"] != nil:
				enc, _ := getString(params, "body_base64")
				decoded, err := base64.StdEncoding.DecodeString(enc)
				if err != nil {
					return NewToolResult("", fmt.Errorf("http_request: invalid body_base64: %w", err))
				}
				bodyReader = bytes.NewReader(decoded)

			case params["body"] != nil:
				bodyStr, _ := getString(params, "body")
				bodyReader = strings.NewReader(bodyStr)

			case params["form"] != nil:
				formVals, err := extractStringMap(params, "form")
				if err != nil {
					return NewToolResult("", fmt.Errorf("http_request: form: %w", err))
				}
				encoded := encodeForm(formVals)
				bodyReader = strings.NewReader(encoded)
				contentTypeOverride = "application/x-www-form-urlencoded"
			}

			// ── build request ─────────────────────────────────────────────────
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
			if err != nil {
				return NewToolResult("", fmt.Errorf("http_request: build request: %w", err))
			}
			req.Header.Set("User-Agent", "feino-agent/1.0 (+https://github.com/odinnordico/feino)")

			if contentTypeOverride != "" {
				req.Header.Set("Content-Type", contentTypeOverride)
			}

			// Apply caller-supplied headers (may override Content-Type / User-Agent).
			if hdrs, hdrErr := extractStringMap(params, "headers"); hdrErr == nil {
				for k, v := range hdrs {
					req.Header.Set(k, v)
				}
			}

			// ── choose client ─────────────────────────────────────────────────
			//nolint:bodyclose // false positive because the linter doesn't trace interface returns properly, closed below
			client := doerToClient(httpDoer, followRedirects)

			start := time.Now()
			resp, err := client.Do(req)
			if err != nil {
				if resp != nil && resp.Body != nil {
					_ = resp.Body.Close()
				}
				return NewToolResult("", fmt.Errorf("http_request: %w", err))
			}
			defer func() { _ = resp.Body.Close() }()
			elapsed := time.Since(start)

			// ── read response body ────────────────────────────────────────────
			limitBytes := int64(maxKB) * 1024
			limited := io.LimitReader(resp.Body, limitBytes+1) // +1 to detect truncation
			rawBody, err := io.ReadAll(limited)
			if err != nil {
				return NewToolResult("", fmt.Errorf("http_request: read response body: %w", err))
			}
			truncated := int64(len(rawBody)) > limitBytes
			if truncated {
				rawBody = rawBody[:limitBytes]
			}

			// ── collect response headers ──────────────────────────────────────
			respHeaders := make(map[string]string, len(resp.Header))
			for k := range resp.Header {
				respHeaders[k] = resp.Header.Get(k)
			}

			// ── build result ──────────────────────────────────────────────────
			result := httpResponse{
				StatusCode:    resp.StatusCode,
				Status:        resp.Status,
				Headers:       respHeaders,
				LatencyMs:     float64(elapsed.Milliseconds()),
				BodyTruncated: truncated,
			}

			if utf8.Valid(rawBody) {
				result.Body = string(rawBody)
			} else {
				result.BodyBase64 = base64.StdEncoding.EncodeToString(rawBody)
			}

			if truncated {
				suffix := fmt.Sprintf("\n[truncated — response exceeded %d KiB; increase max_response_kb to retrieve more]", maxKB)
				if result.Body != "" {
					result.Body += suffix
				}
			}

			safeLogger(logger).Debug("http_request",
				"method", method,
				"url", rawURL,
				"status", resp.StatusCode,
				"latency_ms", result.LatencyMs,
			)

			out, _ := json.MarshalIndent(result, "", "  ")
			return NewToolResult(string(out), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// doerToClient wraps an httpDoer in an *http.Client that optionally disables
// redirect following. When followRedirects is false, CheckRedirect returns
// http.ErrUseLastResponse so the 3xx is returned as-is.
func doerToClient(doer interface {
	Do(*http.Request) (*http.Response, error)
}, followRedirects bool) interface {
	Do(*http.Request) (*http.Response, error)
} {
	if followRedirects {
		return doer
	}
	// Extract the underlying transport so that the new *http.Client's
	// CheckRedirect policy actually fires — wrapping doer.Do would let the
	// doer's own redirect logic run before we could intercept it.
	transport := http.DefaultTransport
	if c, ok := doer.(*http.Client); ok && c.Transport != nil {
		transport = c.Transport
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// extractStringMap reads a map[string]string from params[key].
// The value arrives as map[string]any from JSON decoding.
func extractStringMap(params map[string]any, key string) (map[string]string, error) {
	v, ok := params[key]
	if !ok {
		return nil, nil
	}
	raw, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a JSON object", key)
	}
	out := make(map[string]string, len(raw))
	for k, val := range raw {
		switch s := val.(type) {
		case string:
			out[k] = s
		default:
			out[k] = fmt.Sprintf("%v", val)
		}
	}
	return out, nil
}

// encodeForm URL-encodes a map into application/x-www-form-urlencoded format.
func encodeForm(fields map[string]string) string {
	vals := url.Values{}
	for k, v := range fields {
		vals.Set(k, v)
	}
	return vals.Encode() // already sorts by key
}
