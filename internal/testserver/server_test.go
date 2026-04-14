package testserver

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// ── Vector unit tests ─────────────────────────────────────────────────────────

func TestTokenize(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"Hello, world!", []string{"hello", "world"}},
		{"go test ./...", []string{"go", "test"}},
		{"", nil},
		{"a", nil},               // single char filtered
		{"I am", []string{"am"}}, // "I" is 1 char, filtered
		{"READ the FILE", []string{"read", "the", "file"}},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := tokenize(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("tokenize(%q) = %v, want %v", tc.input, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("token[%d]: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestTermFreq(t *testing.T) {
	cases := []struct {
		tokens []string
		want   map[string]float64
	}{
		{[]string{"a", "b", "a"}, map[string]float64{"a": 2, "b": 1}},
		{[]string{}, map[string]float64{}},
		{nil, map[string]float64{}},
	}
	for _, tc := range cases {
		got := termFreq(tc.tokens)
		if len(got) != len(tc.want) {
			t.Errorf("termFreq(%v) len = %d, want %d", tc.tokens, len(got), len(tc.want))
			continue
		}
		for k, wv := range tc.want {
			if gv := got[k]; gv != wv {
				t.Errorf("termFreq: key %q = %.0f, want %.0f", k, gv, wv)
			}
		}
	}
}

func TestCosineSimilarity(t *testing.T) {
	a := map[string]float64{"x": 1}
	b := map[string]float64{"x": 1}
	if got := cosineSimilarity(a, b); got != 1.0 {
		t.Errorf("identical vectors: got %.4f, want 1.0", got)
	}

	c := map[string]float64{"y": 1}
	if got := cosineSimilarity(a, c); got != 0.0 {
		t.Errorf("disjoint vectors: got %.4f, want 0.0", got)
	}

	// {"a":1} vs {"a":1,"b":1} → dot=1, |a|=1, |b|=sqrt(2) → 1/sqrt(2) ≈ 0.707
	d := map[string]float64{"a": 1}
	e := map[string]float64{"a": 1, "b": 1}
	got := cosineSimilarity(d, e)
	if got < 0.70 || got > 0.72 {
		t.Errorf("partial overlap: got %.4f, want ~0.707", got)
	}

	if got := cosineSimilarity(map[string]float64{}, b); got != 0.0 {
		t.Errorf("empty a: got %.4f, want 0.0", got)
	}
	if got := cosineSimilarity(a, map[string]float64{}); got != 0.0 {
		t.Errorf("empty b: got %.4f, want 0.0", got)
	}
}

func TestVectorIndex_FindsBestMatch(t *testing.T) {
	records := []Record{
		{Prompt: "read file from disk", Response: RecordedResponse{Text: "file content"}},
		{Prompt: "execute shell command in terminal", Response: RecordedResponse{Text: "exit 0"}},
		{Prompt: "write data to file on disk", Response: RecordedResponse{Text: "wrote"}},
	}
	idx := newVectorIndex(records)

	r, score := idx.findBestMatch("read the contents of a file from disk")
	if r.Response.Text != "file content" {
		t.Errorf("expected 'file content' record, got %q (score %.4f)", r.Response.Text, score)
	}
	if score <= 0 {
		t.Errorf("expected positive score, got %.4f", score)
	}

	r2, _ := idx.findBestMatch("run a shell command in the terminal")
	if r2.Response.Text != "exit 0" {
		t.Errorf("expected 'exit 0' record, got %q", r2.Response.Text)
	}
}

func TestVectorIndex_ScoreRange(t *testing.T) {
	records := []Record{
		{Prompt: "file read operation", Response: RecordedResponse{Text: "ok"}},
	}
	idx := newVectorIndex(records)
	_, score := idx.findBestMatch("totally unrelated xyzyzy qwerty zxcvbn")
	if score < 0 || score > 1 {
		t.Errorf("score out of [0,1]: %.6f", score)
	}
}

// ── Server integration tests ──────────────────────────────────────────────────

func TestSimulatedServer_ModelsEndpoint(t *testing.T) {
	srv := NewSimulatedServer([]Record{{Prompt: "x", Response: RecordedResponse{Text: "y"}}})
	defer srv.Close()

	resp, err := http.Get(srv.URL() + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "simulated-model") {
		t.Errorf("expected simulated-model in body, got: %s", body)
	}
}

func TestSimulatedServer_TextResponse(t *testing.T) {
	srv := NewSimulatedServer([]Record{{
		Prompt:   "what is the capital of France",
		Response: RecordedResponse{Text: "Paris"},
	}})
	defer srv.Close()

	chunks := postCompletion(t, srv.URL(), "what is the capital of France", true)
	fullText := extractTextFromSSE(t, chunks)
	if !strings.Contains(fullText, "Paris") {
		t.Errorf("expected 'Paris' in response, got: %q", fullText)
	}
}

func TestSimulatedServer_ToolCallResponse(t *testing.T) {
	srv := NewSimulatedServer([]Record{{
		Prompt: "read file at /tmp/test.txt",
		Response: RecordedResponse{
			ToolCalls: []ToolCall{{
				ID:        "call_1",
				Name:      "file_read",
				Arguments: `{"path":"/tmp/test.txt"}`,
			}},
		},
	}})
	defer srv.Close()

	chunks := postCompletion(t, srv.URL(), "read file at /tmp/test.txt", true)

	var foundName, foundArgs bool
	for _, chunk := range chunks {
		for _, ch := range chunk["choices"].([]any) {
			choice := ch.(map[string]any)
			delta, ok := choice["delta"].(map[string]any)
			if !ok {
				continue
			}
			tcs, ok := delta["tool_calls"].([]any)
			if !ok {
				continue
			}
			for _, tc := range tcs {
				tcMap := tc.(map[string]any)
				fn, ok := tcMap["function"].(map[string]any)
				if !ok {
					continue
				}
				if fn["name"] == "file_read" {
					foundName = true
				}
				if strings.Contains(fn["arguments"].(string), "/tmp/test.txt") {
					foundArgs = true
				}
			}
		}
	}
	if !foundName {
		t.Error("expected tool call with name 'file_read'")
	}
	if !foundArgs {
		t.Error("expected tool call arguments containing '/tmp/test.txt'")
	}
}

func TestSimulatedServer_NoMatch(t *testing.T) {
	srv := NewSimulatedServer([]Record{{
		Prompt:   "read a file",
		Response: RecordedResponse{Text: "ok"},
	}})
	defer srv.Close()

	body := postRaw(t, srv.URL(), "xyzyzy totally unrelated gibberish zxcvbnm asdfgh", true)
	if !strings.Contains(body, "no matching record") {
		t.Errorf("expected 'no matching record' in error body, got: %s", body)
	}
}

func TestSimulatedServer_CustomThreshold(t *testing.T) {
	srv := NewSimulatedServer(
		[]Record{{Prompt: "read a file", Response: RecordedResponse{Text: "ok"}}},
		WithMinSimilarity(0.99),
	)
	defer srv.Close()

	// "list a file" shares some words but won't score 0.99 against "read a file"
	body := postRaw(t, srv.URL(), "list a file", true)
	if !strings.Contains(body, "no matching record") {
		t.Errorf("expected 'no matching record' with high threshold, got: %s", body)
	}
}

func TestSimulatedServer_UsageReported(t *testing.T) {
	srv := NewSimulatedServer([]Record{{
		Prompt: "test prompt",
		Response: RecordedResponse{
			Text:  "response",
			Usage: Usage{PromptTokens: 20, CompletionTokens: 8},
		},
	}})
	defer srv.Close()

	chunks := postCompletion(t, srv.URL(), "test prompt", true)
	usage := extractUsageFromSSE(t, chunks)

	if usage["prompt_tokens"].(float64) != 20 {
		t.Errorf("expected prompt_tokens=20, got %v", usage["prompt_tokens"])
	}
	if usage["completion_tokens"].(float64) != 8 {
		t.Errorf("expected completion_tokens=8, got %v", usage["completion_tokens"])
	}
	if usage["total_tokens"].(float64) != 28 {
		t.Errorf("expected total_tokens=28, got %v", usage["total_tokens"])
	}
}

func TestSimulatedServer_DefaultUsage(t *testing.T) {
	srv := NewSimulatedServer([]Record{{
		Prompt:   "test prompt",
		Response: RecordedResponse{Text: "response"}, // zero Usage
	}})
	defer srv.Close()

	chunks := postCompletion(t, srv.URL(), "test prompt", true)
	usage := extractUsageFromSSE(t, chunks)

	if usage["prompt_tokens"].(float64) != 10 {
		t.Errorf("expected default prompt_tokens=10, got %v", usage["prompt_tokens"])
	}
	if usage["completion_tokens"].(float64) != 5 {
		t.Errorf("expected default completion_tokens=5, got %v", usage["completion_tokens"])
	}
}

func TestSimulatedServer_NonStreamingResponse(t *testing.T) {
	srv := NewSimulatedServer([]Record{{
		Prompt:   "hello world",
		Response: RecordedResponse{Text: "hi there"},
	}})
	defer srv.Close()

	body := postRawStream(t, srv.URL(), "hello world", false)

	// Should be a single JSON object, not SSE
	if strings.Contains(body, "data:") {
		t.Error("non-streaming response should not contain SSE data: prefix")
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		t.Fatalf("non-streaming response is not valid JSON: %v\nbody: %s", err, body)
	}
	choices, ok := obj["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatal("expected choices array in non-streaming response")
	}
	choice := choices[0].(map[string]any)
	msg := choice["message"].(map[string]any)
	if msg["content"] != "hi there" {
		t.Errorf("expected content 'hi there', got %v", msg["content"])
	}
}

// ── Test helpers ──────────────────────────────────────────────────────────────

// postCompletion sends a streaming chat completion request and returns parsed
// JSON chunks (excluding the [DONE] terminator).
func postCompletion(t *testing.T, baseURL, message string, stream bool) []map[string]any {
	t.Helper()
	reqBody, _ := json.Marshal(map[string]any{
		"model":    "test-model",
		"stream":   stream,
		"messages": []map[string]any{{"role": "user", "content": message}},
	})
	resp, err := http.Post(baseURL+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
	}

	var chunks []map[string]any
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			t.Fatalf("invalid SSE chunk JSON: %v\nchunk: %s", err, data)
		}
		chunks = append(chunks, chunk)
	}
	return chunks
}

// postRaw sends a streaming request and returns the raw response body for
// error-case inspection (e.g. 500 responses).
func postRaw(t *testing.T, baseURL, message string, stream bool) string {
	t.Helper()
	return postRawStream(t, baseURL, message, stream)
}

func postRawStream(t *testing.T, baseURL, message string, stream bool) string {
	t.Helper()
	reqBody, _ := json.Marshal(map[string]any{
		"model":    "test-model",
		"stream":   stream,
		"messages": []map[string]any{{"role": "user", "content": message}},
	})
	resp, err := http.Post(baseURL+"/v1/chat/completions", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

// extractTextFromSSE concatenates all content delta values across SSE chunks.
func extractTextFromSSE(t *testing.T, chunks []map[string]any) string {
	t.Helper()
	var sb strings.Builder
	for _, chunk := range chunks {
		choices, ok := chunk["choices"].([]any)
		if !ok {
			continue
		}
		for _, ch := range choices {
			choice, ok := ch.(map[string]any)
			if !ok {
				continue
			}
			delta, ok := choice["delta"].(map[string]any)
			if !ok {
				continue
			}
			if content, ok := delta["content"].(string); ok {
				sb.WriteString(content)
			}
		}
	}
	return sb.String()
}

// extractUsageFromSSE finds the chunk with an empty choices array and returns
// its usage map.
func extractUsageFromSSE(t *testing.T, chunks []map[string]any) map[string]any {
	t.Helper()
	for _, chunk := range chunks {
		choices, ok := chunk["choices"].([]any)
		if !ok || len(choices) != 0 {
			continue
		}
		usage, ok := chunk["usage"].(map[string]any)
		if !ok {
			continue
		}
		return usage
	}
	t.Fatal("no usage chunk found in SSE response")
	return nil
}
