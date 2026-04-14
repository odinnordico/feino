package tools

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── test server helpers ───────────────────────────────────────────────────────

// echoServer returns an httptest.Server that responds with a JSON object
// containing the method, path, headers, and body it received.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		hdrs := map[string]string{}
		for k := range r.Header {
			hdrs[k] = r.Header.Get(k)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Test-Header", "test-value")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"method":  r.Method,
			"path":    r.URL.Path,
			"headers": hdrs,
			"body":    string(body),
		})
	}))
}

// statusServer returns a server that always responds with the given status code
// and body.
func statusServer(t *testing.T, code int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
}

// redirectServer returns a server that redirects once to /final, then serves
// "final" at /final.
func redirectServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("final"))
	})
	srv = httptest.NewServer(mux)
	return srv
}

// withHTTPDoer swaps httpDoer for the duration of the test.
func withHTTPDoer(t *testing.T, c *http.Client) {
	t.Helper()
	orig := httpDoer
	httpDoer = c
	t.Cleanup(func() { httpDoer = orig })
}

// callHTTP is a shorthand for running the tool with given params.
func callHTTP(params map[string]any) httpResponse {
	tool := newHTTPRequestTool(nil)
	res := tool.Run(params)
	content, _ := res.GetContent().(string)
	var out httpResponse
	_ = json.Unmarshal([]byte(content), &out)
	return out
}

// callHTTPRaw returns the error message or raw content string for assertions.
func callHTTPRaw(params map[string]any) string {
	tool := newHTTPRequestTool(nil)
	res := tool.Run(params)
	if err := res.GetError(); err != nil {
		return err.Error()
	}
	s, _ := res.GetContent().(string)
	return s
}

// ── basic GET ─────────────────────────────────────────────────────────────────

func TestHTTPRequest_GET_200(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{"url": srv.URL + "/hello"})
	if r.StatusCode != 200 {
		t.Errorf("status: want 200, got %d", r.StatusCode)
	}
	if !strings.Contains(r.Body, `"method":"GET"`) {
		t.Errorf("body should contain method:GET, got: %s", r.Body)
	}
	if r.LatencyMs < 0 {
		t.Error("latency_ms should be non-negative")
	}
}

func TestHTTPRequest_ResponseHeaders(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{"url": srv.URL})
	if r.Headers["X-Test-Header"] != "test-value" {
		t.Errorf("response headers: want X-Test-Header=test-value, got %v", r.Headers)
	}
}

// ── methods ───────────────────────────────────────────────────────────────────

func TestHTTPRequest_POST_WithBody(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{
		"url":    srv.URL,
		"method": "POST",
		"body":   `{"key":"value"}`,
		"headers": map[string]any{
			"Content-Type": "application/json",
		},
	})
	if r.StatusCode != 200 {
		t.Errorf("status: want 200, got %d", r.StatusCode)
	}
	// The echo server JSON-encodes the received body, so quotes are escaped.
	if !strings.Contains(r.Body, `\"key\":\"value\"`) {
		t.Errorf("echoed body should contain posted content, got: %s", r.Body)
	}
}

func TestHTTPRequest_PUT(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{"url": srv.URL, "method": "PUT", "body": "update"})
	if !strings.Contains(r.Body, `"method":"PUT"`) {
		t.Errorf("expected PUT method echoed, got: %s", r.Body)
	}
}

func TestHTTPRequest_DELETE(t *testing.T) {
	srv := statusServer(t, 204, "")
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{"url": srv.URL, "method": "DELETE"})
	if r.StatusCode != 204 {
		t.Errorf("status: want 204, got %d", r.StatusCode)
	}
}

func TestHTTPRequest_HEAD(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{"url": srv.URL, "method": "HEAD"})
	// HEAD responses have no body by spec; status should still be 200.
	if r.StatusCode != 200 {
		t.Errorf("status: want 200, got %d", r.StatusCode)
	}
}

// ── form body ─────────────────────────────────────────────────────────────────

func TestHTTPRequest_FormBody(t *testing.T) {
	srv := echoServer(t)
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{
		"url":    srv.URL,
		"method": "POST",
		"form":   map[string]any{"user": "alice", "token": "abc123"},
	})
	if r.StatusCode != 200 {
		t.Fatalf("status: want 200, got %d", r.StatusCode)
	}
	// The echo body should contain the URL-encoded form.
	if !strings.Contains(r.Body, "token=abc123") && !strings.Contains(r.Body, "user=alice") {
		t.Errorf("form body not echoed correctly: %s", r.Body)
	}
}

func TestHTTPRequest_FormSetsContentType(t *testing.T) {
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		w.WriteHeader(200)
	}))
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	callHTTP(map[string]any{
		"url":    srv.URL,
		"method": "POST",
		"form":   map[string]any{"a": "b"},
	})
	if gotCT != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type: want application/x-www-form-urlencoded, got %q", gotCT)
	}
}

// ── base64 body ───────────────────────────────────────────────────────────────

func TestHTTPRequest_Base64Body(t *testing.T) {
	payload := []byte{0x00, 0x01, 0x02, 0xFF}
	srv := echoServer(t)
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{
		"url":         srv.URL,
		"method":      "POST",
		"body_base64": base64.StdEncoding.EncodeToString(payload),
	})
	if r.StatusCode != 200 {
		t.Errorf("status: want 200, got %d", r.StatusCode)
	}
}

func TestHTTPRequest_InvalidBase64(t *testing.T) {
	raw := callHTTPRaw(map[string]any{
		"url":         "http://localhost:1",
		"body_base64": "!!!not-base64!!!",
	})
	if !strings.Contains(raw, "invalid body_base64") {
		t.Errorf("expected base64 error, got %q", raw)
	}
}

// ── headers ───────────────────────────────────────────────────────────────────

func TestHTTPRequest_CustomHeaders(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
	}))
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	callHTTP(map[string]any{
		"url":     srv.URL,
		"headers": map[string]any{"Authorization": "Bearer tok123"},
	})
	if gotAuth != "Bearer tok123" {
		t.Errorf("Authorization: want %q, got %q", "Bearer tok123", gotAuth)
	}
}

// ── redirects ─────────────────────────────────────────────────────────────────

func TestHTTPRequest_FollowRedirects_Default(t *testing.T) {
	srv := redirectServer(t)
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{"url": srv.URL + "/"})
	if r.StatusCode != 200 {
		t.Errorf("should follow redirect, want 200, got %d", r.StatusCode)
	}
	if r.Body != "final" {
		t.Errorf("body: want %q, got %q", "final", r.Body)
	}
}

func TestHTTPRequest_NoFollowRedirects(t *testing.T) {
	srv := redirectServer(t)
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{
		"url":              srv.URL + "/",
		"follow_redirects": false,
	})
	if r.StatusCode != 302 {
		t.Errorf("should NOT follow redirect, want 302, got %d", r.StatusCode)
	}
}

// ── error status codes ────────────────────────────────────────────────────────

func TestHTTPRequest_404(t *testing.T) {
	srv := statusServer(t, 404, "not found")
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{"url": srv.URL})
	if r.StatusCode != 404 {
		t.Errorf("status: want 404, got %d", r.StatusCode)
	}
	// Tool returns the body regardless of status code.
	if r.Body != "not found" {
		t.Errorf("body: want %q, got %q", "not found", r.Body)
	}
}

func TestHTTPRequest_500(t *testing.T) {
	srv := statusServer(t, 500, "internal error")
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{"url": srv.URL})
	if r.StatusCode != 500 {
		t.Errorf("status: want 500, got %d", r.StatusCode)
	}
}

// ── body truncation ───────────────────────────────────────────────────────────

func TestHTTPRequest_BodyTruncation(t *testing.T) {
	large := strings.Repeat("x", 3*1024) // 3 KiB
	srv := statusServer(t, 200, large)
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{"url": srv.URL, "max_response_kb": 1})
	if !r.BodyTruncated {
		t.Error("body_truncated should be true")
	}
	if len(r.Body) > 1024+200 { // 1 KiB + truncation notice
		t.Errorf("body longer than expected: %d bytes", len(r.Body))
	}
	if !strings.Contains(r.Body, "truncated") {
		t.Error("truncated body should contain truncation notice")
	}
}

// ── binary (non-UTF-8) response ───────────────────────────────────────────────

func TestHTTPRequest_BinaryResponse_Base64(t *testing.T) {
	bin := []byte{0xFF, 0xFE, 0x00, 0x01}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(bin)
	}))
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{"url": srv.URL})
	if r.Body != "" {
		t.Error("body should be empty for non-UTF-8 response")
	}
	if r.BodyBase64 == "" {
		t.Error("body_base64 should be set for binary response")
	}
	decoded, err := base64.StdEncoding.DecodeString(r.BodyBase64)
	if err != nil {
		t.Fatalf("body_base64 is not valid base64: %v", err)
	}
	if string(decoded) != string(bin) {
		t.Errorf("decoded body mismatch")
	}
}

// ── validation ────────────────────────────────────────────────────────────────

func TestHTTPRequest_MissingURL(t *testing.T) {
	raw := callHTTPRaw(map[string]any{})
	if !strings.Contains(raw, "url is required") {
		t.Errorf("expected url error, got %q", raw)
	}
}

func TestHTTPRequest_InvalidMethod(t *testing.T) {
	raw := callHTTPRaw(map[string]any{"url": "http://x", "method": "BREW"})
	if !strings.Contains(raw, "unsupported method") {
		t.Errorf("expected method error, got %q", raw)
	}
}

func TestHTTPRequest_TimeoutClamped(t *testing.T) {
	// Should not panic or error on out-of-range values; just clamp.
	srv := statusServer(t, 200, "ok")
	defer srv.Close()
	withHTTPDoer(t, srv.Client())

	r := callHTTP(map[string]any{"url": srv.URL, "timeout_seconds": 999})
	if r.StatusCode != 200 {
		t.Errorf("clamped timeout should still work, got status %d", r.StatusCode)
	}
}

// ── helper functions ──────────────────────────────────────────────────────────

func TestEncodeForm(t *testing.T) {
	tests := []struct {
		input map[string]string
		want  string
	}{
		{map[string]string{"a": "1"}, "a=1"},
		{map[string]string{"a": "hello world"}, "a=hello+world"},
		{map[string]string{"z": "last", "a": "first"}, "a=first&z=last"},
	}
	for _, tc := range tests {
		got := encodeForm(tc.input)
		if got != tc.want {
			t.Errorf("encodeForm(%v): want %q, got %q", tc.input, tc.want, got)
		}
	}
}

func TestExtractStringMap_Valid(t *testing.T) {
	params := map[string]any{
		"hdrs": map[string]any{"Content-Type": "application/json", "X-Foo": "bar"},
	}
	m, err := extractStringMap(params, "hdrs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["Content-Type"] != "application/json" {
		t.Errorf("Content-Type: want application/json, got %q", m["Content-Type"])
	}
}

func TestExtractStringMap_Missing(t *testing.T) {
	m, err := extractStringMap(map[string]any{}, "missing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != nil {
		t.Errorf("want nil for missing key, got %v", m)
	}
}

func TestExtractStringMap_WrongType(t *testing.T) {
	_, err := extractStringMap(map[string]any{"key": "not-a-map"}, "key")
	if err == nil {
		t.Error("expected error for wrong type")
	}
}

// ── permission level + registration ──────────────────────────────────────────

func TestHTTPRequestTool_PermissionLevel(t *testing.T) {
	tool := newHTTPRequestTool(nil)
	c, ok := tool.(Classified)
	if !ok {
		t.Fatal("http_request tool does not implement Classified")
	}
	if c.PermissionLevel() != PermLevelRead {
		t.Errorf("want PermLevelRead (%d), got %d", PermLevelRead, c.PermissionLevel())
	}
}

func TestNewNativeTools_IncludesHTTPRequest(t *testing.T) {
	tools := NewNativeTools(nil)
	for _, tool := range tools {
		if tool.GetName() == "http_request" {
			return
		}
	}
	t.Error("http_request not found in NewNativeTools output")
}
