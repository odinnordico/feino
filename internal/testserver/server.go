package testserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

// ToolCall describes a tool invocation the server returns in a response.
type ToolCall struct {
	// ID is the call identifier sent back to the client, e.g. "call_abc123".
	// Defaults to "call_<index>" when empty.
	ID        string
	Name      string // tool function name, e.g. "file_read"
	Arguments string // JSON-encoded arguments, e.g. `{"path":"/tmp/foo"}`
}

// Usage is the token accounting reported in the response.
// Zero values are replaced with sensible defaults (10 prompt, 5 completion).
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// RecordedResponse is the deterministic reply the server sends when a Record
// is matched. Set Text XOR ToolCalls — mixing both is unsupported.
type RecordedResponse struct {
	Text      string     // plain-text assistant reply
	ToolCalls []ToolCall // structured tool invocations
	Usage     Usage
}

// Record pairs a representative prompt string with a pre-recorded response.
// The Prompt field is used only for similarity matching and is never sent back.
type Record struct {
	Prompt   string
	Response RecordedResponse
}

// Option configures a SimulatedServer.
type Option func(*SimulatedServer)

// WithMinSimilarity sets the minimum cosine similarity score required to
// consider a Record a match. Requests that score below the threshold receive
// HTTP 500 with a descriptive JSON error. Default is 0.1.
func WithMinSimilarity(threshold float64) Option {
	return func(s *SimulatedServer) { s.minSimilarity = threshold }
}

// SimulatedServer is an in-process HTTP server that speaks the OpenAI
// /v1/chat/completions SSE protocol. Incoming prompts are routed to
// pre-recorded responses via cosine-similarity vector matching.
//
// Call URL() to obtain the base URL for configuring a provider, and Close()
// when the test is done.
type SimulatedServer struct {
	server        *httptest.Server
	index         *vectorIndex
	minSimilarity float64
}

// NewSimulatedServer creates and starts the simulated server with the given
// records. Panics if records is empty (a server with no corpus is a
// misconfiguration). Apply options to override defaults.
func NewSimulatedServer(records []Record, opts ...Option) *SimulatedServer {
	s := &SimulatedServer{
		index:         newVectorIndex(records),
		minSimilarity: 0.1,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// URL returns the base URL callers should use when configuring a provider.
// For the OpenAI provider: p.baseURL = server.URL() + "/v1/"
func (s *SimulatedServer) URL() string { return s.server.URL }

// Close shuts down the HTTP server. Call this in a defer after construction.
func (s *SimulatedServer) Close() { s.server.Close() }

// ── HTTP handler ──────────────────────────────────────────────────────────────

func (s *SimulatedServer) handle(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
		s.handleModels(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
		s.handleCompletions(w, r)
	default:
		http.Error(w, `{"error":{"message":"not found"}}`, http.StatusNotFound)
	}
}

func (s *SimulatedServer) handleModels(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = fmt.Fprintln(w, `{"object":"list","data":[{"id":"simulated-model","object":"model","created":1700000000,"owned_by":"testserver"}]}`)
}

// chatRequest is the subset of the OpenAI chat completions request we need.
type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (s *SimulatedServer) handleCompletions(w http.ResponseWriter, r *http.Request) {
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":{"message":"bad request"}}`, http.StatusBadRequest)
		return
	}

	// Build query from all message content fields — maximises similarity signal.
	var parts []string
	for _, m := range req.Messages {
		if m.Content != "" {
			parts = append(parts, m.Content)
		}
	}
	query := strings.Join(parts, " ")

	record, score := s.index.findBestMatch(query)
	if score < s.minSimilarity {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"simulated server: no matching record (best score: %.4f)","type":"test_no_match","code":500}}`, score)
		return
	}

	// Apply usage defaults.
	usage := record.Response.Usage
	if usage.PromptTokens == 0 {
		usage.PromptTokens = 10
	}
	if usage.CompletionTokens == 0 {
		usage.CompletionTokens = 5
	}

	modelName := req.Model
	if modelName == "" {
		modelName = "simulated-model"
	}

	if req.Stream {
		s.writeSSE(w, record.Response, usage)
	} else {
		s.writeJSON(w, modelName, record.Response, usage)
	}
}

// ── SSE response ──────────────────────────────────────────────────────────────

// sseChunk is the top-level streaming response object.
type sseChunk struct {
	ID      string      `json:"id"`
	Choices []sseChoice `json:"choices"`
	Usage   *sseUsage   `json:"usage,omitempty"`
}

type sseChoice struct {
	Index        int64    `json:"index"`
	Delta        sseDelta `json:"delta"`
	FinishReason *string  `json:"finish_reason"`
}

type sseDelta struct {
	Role      string        `json:"role,omitempty"`
	Content   string        `json:"content,omitempty"`
	ToolCalls []sseToolCall `json:"tool_calls,omitempty"`
}

type sseToolCall struct {
	Index    int64           `json:"index"`
	ID       string          `json:"id,omitempty"`
	Type     string          `json:"type,omitempty"`
	Function sseToolFunction `json:"function"`
}

type sseToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type sseUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (s *SimulatedServer) writeSSE(w http.ResponseWriter, resp RecordedResponse, usage Usage) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")

	flusher, ok := w.(http.Flusher)
	if !ok {
		// Fallback: just write without flushing (should not happen in tests).
		flusher = noopFlusher{}
	}

	emit := func(chunk sseChunk) {
		b, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	stopStr := "stop"
	toolCallsStr := "tool_calls"

	if len(resp.ToolCalls) > 0 {
		// Emit one chunk per tool call (name + arguments combined in a single
		// delta), then a finish_reason chunk, then the usage chunk.
		for i, tc := range resp.ToolCalls {
			id := tc.ID
			if id == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			role := ""
			if i == 0 {
				role = "assistant"
			}
			emit(sseChunk{
				ID: "chatcmpl-test",
				Choices: []sseChoice{{
					Index: int64(i),
					Delta: sseDelta{
						Role: role,
						ToolCalls: []sseToolCall{{
							Index: int64(i),
							ID:    id,
							Type:  "function",
							Function: sseToolFunction{
								Name:      tc.Name,
								Arguments: tc.Arguments,
							},
						}},
					},
					FinishReason: nil,
				}},
			})
		}
		// Finish reason chunk.
		emit(sseChunk{
			ID: "chatcmpl-test",
			Choices: []sseChoice{{
				Index:        0,
				Delta:        sseDelta{},
				FinishReason: &toolCallsStr,
			}},
		})
	} else {
		// Text response: content delta, then finish_reason.
		emit(sseChunk{
			ID: "chatcmpl-test",
			Choices: []sseChoice{{
				Index: 0,
				Delta: sseDelta{
					Role:    "assistant",
					Content: resp.Text,
				},
				FinishReason: nil,
			}},
		})
		emit(sseChunk{
			ID: "chatcmpl-test",
			Choices: []sseChoice{{
				Index:        0,
				Delta:        sseDelta{},
				FinishReason: &stopStr,
			}},
		})
	}

	// Usage chunk — empty choices slice, not nil.
	emit(sseChunk{
		ID:      "chatcmpl-test",
		Choices: []sseChoice{},
		Usage: &sseUsage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.PromptTokens + usage.CompletionTokens,
		},
	})

	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// ── Non-streaming JSON response ───────────────────────────────────────────────

type jsonCompletion struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Model   string       `json:"model"`
	Choices []jsonChoice `json:"choices"`
	Usage   sseUsage     `json:"usage"`
}

type jsonChoice struct {
	Index        int         `json:"index"`
	Message      jsonMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type jsonMessage struct {
	Role      string         `json:"role"`
	Content   string         `json:"content,omitempty"`
	ToolCalls []jsonToolCall `json:"tool_calls,omitempty"`
}

type jsonToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Function sseToolFunction `json:"function"`
}

func (s *SimulatedServer) writeJSON(w http.ResponseWriter, modelName string, resp RecordedResponse, usage Usage) {
	w.Header().Set("Content-Type", "application/json")

	msg := jsonMessage{Role: "assistant"}
	finishReason := "stop"

	if len(resp.ToolCalls) > 0 {
		finishReason = "tool_calls"
		for i, tc := range resp.ToolCalls {
			id := tc.ID
			if id == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			msg.ToolCalls = append(msg.ToolCalls, jsonToolCall{
				ID:   id,
				Type: "function",
				Function: sseToolFunction{
					Name:      tc.Name,
					Arguments: tc.Arguments,
				},
			})
		}
	} else {
		msg.Content = resp.Text
	}

	completion := jsonCompletion{
		ID:     "chatcmpl-test",
		Object: "chat.completion",
		Model:  modelName,
		Choices: []jsonChoice{{
			Index:        0,
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: sseUsage{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.PromptTokens + usage.CompletionTokens,
		},
	}

	_ = json.NewEncoder(w).Encode(completion)
}

// noopFlusher satisfies http.Flusher when the ResponseWriter does not.
type noopFlusher struct{}

func (noopFlusher) Flush() {}
