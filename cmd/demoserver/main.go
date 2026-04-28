// demoserver is a helper binary used exclusively for generating the VHS demo
// GIFs. It starts a sequential fake LLM server that speaks the OpenAI
// /v1/chat/completions SSE protocol, writes a temporary feino config that
// points at it, then spawns the ./feino binary as a child process with HOME
// redirected to the temp directory — so no real API key is ever required.
//
// Usage (from the repo root, after building both binaries):
//
//	FEINO_DEMO=quickstart ./demoserver --no-tui
//	FEINO_DEMO=tools      ./demoserver --no-tui
//	FEINO_DEMO=security   ./demoserver --no-tui
//
// If FEINO_DEMO is unset it defaults to "quickstart".
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

// ── sequential SSE server ─────────────────────────────────────────────────────

type toolCall struct {
	id        string
	name      string
	arguments string
}

// response is one pre-recorded LLM turn.  Set Text XOR ToolCalls.
type response struct {
	text      string
	toolCalls []toolCall
}

// seqServer serves responses in index order; after the last response it
// repeats the final one so unexpected extra requests don't panic.
type seqServer struct {
	responses []response
	idx       atomic.Int64
}

func (s *seqServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
		w.Header().Set("Content-Type", "application/json")
		_, err := fmt.Fprintln(w, `{"object":"list","data":[{"id":"simulated-model","object":"model","created":1700000000,"owned_by":"demoserver"}]}`)
		if err != nil {
			log.Printf("Error writing models response: %v", err)
		}

	case r.Method == http.MethodPost && r.URL.Path == "/v1/chat/completions":
		var req struct {
			Stream bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)

		i := int(s.idx.Add(1)) - 1
		if i >= len(s.responses) {
			i = len(s.responses) - 1
		}
		resp := s.responses[i]

		if req.Stream {
			writeSSE(w, resp)
		} else {
			writeJSONResp(w, resp)
		}

	default:
		http.Error(w, `{"error":{"message":"not found"}}`, http.StatusNotFound)
	}
}

// ── SSE writer ────────────────────────────────────────────────────────────────

func writeSSE(w http.ResponseWriter, resp response) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	f, ok := w.(http.Flusher)
	if !ok {
		f = noopFlusher{}
	}

	emit := func(v any) {
		b, err := json.Marshal(v)
		if err != nil {
			return
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
		f.Flush()
	}

	stopStr := "stop"
	toolCallsStr := "tool_calls"

	if len(resp.toolCalls) > 0 {
		for i, tc := range resp.toolCalls {
			id := tc.id
			if id == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			role := ""
			if i == 0 {
				role = "assistant"
			}
			emit(map[string]any{
				"id": "chatcmpl-demo",
				"choices": []any{map[string]any{
					"index": i,
					"delta": map[string]any{
						"role": role,
						"tool_calls": []any{map[string]any{
							"index": i,
							"id":    id,
							"type":  "function",
							"function": map[string]any{
								"name":      tc.name,
								"arguments": tc.arguments,
							},
						}},
					},
					"finish_reason": nil,
				}},
			})
		}
		emit(map[string]any{
			"id": "chatcmpl-demo",
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{},
				"finish_reason": &toolCallsStr,
			}},
		})
	} else {
		emit(map[string]any{
			"id": "chatcmpl-demo",
			"choices": []any{map[string]any{
				"index":         0,
				"delta":         map[string]any{"role": "assistant", "content": resp.text},
				"finish_reason": nil,
			}},
		})
		emit(map[string]any{
			"id": "chatcmpl-demo",
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{},
				"finish_reason": &stopStr,
			}},
		})
	}

	// Usage chunk.
	emit(map[string]any{
		"id":      "chatcmpl-demo",
		"choices": []any{},
		"usage": map[string]any{
			"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30,
		},
	})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	f.Flush()
}

// ── JSON (non-streaming) writer ───────────────────────────────────────────────

func writeJSONResp(w http.ResponseWriter, resp response) {
	w.Header().Set("Content-Type", "application/json")
	finishReason := "stop"
	msg := map[string]any{"role": "assistant"}
	if len(resp.toolCalls) > 0 {
		finishReason = "tool_calls"
		tcs := make([]any, 0, len(resp.toolCalls))
		for i, tc := range resp.toolCalls {
			id := tc.id
			if id == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			tcs = append(tcs, map[string]any{
				"id": id, "type": "function",
				"function": map[string]any{"name": tc.name, "arguments": tc.arguments},
			})
		}
		msg["tool_calls"] = tcs
	} else {
		msg["content"] = resp.text
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": "chatcmpl-demo", "object": "chat.completion",
		"model": "simulated-model",
		"choices": []any{map[string]any{
			"index": 0, "message": msg, "finish_reason": finishReason,
		}},
		"usage": map[string]any{
			"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30,
		},
	})
}

type noopFlusher struct{}

func (noopFlusher) Flush() {}

// ── demo corpora ──────────────────────────────────────────────────────────────

// quickstart: two user messages, 3 LLM requests total.
//
//	msg 1 "List the Go files"         → req 0 (tool call) + req 1 (text)
//	msg 2 "Summarise the agent pkg"   → req 2 (text)
var quickstartCorpus = []response{
	{toolCalls: []toolCall{{name: "list_files", arguments: `{"path":"."}`}}},
	{text: "The repository contains these main Go source directories:\n\n" +
		"- **`cmd/feino/`** — binary entry point and CLI flag definitions\n" +
		"- **`internal/agent/`** — ReAct state machine and TACOS latency router\n" +
		"- **`internal/app/`** — UI-agnostic session core (Send / Subscribe / Cancel)\n" +
		"- **`internal/tools/`** — native tool suite: shell, files, git, web, HTTP, browser (22 tools)\n" +
		"- **`internal/security/`** — three-layer permission gate\n" +
		"- **`internal/web/`** — Connect RPC server + embedded React SPA\n" +
		"- **`gen/`** — generated protobuf / connect-go code"},
	{text: "The `internal/agent` package implements the **ReAct (Reason+Act) loop** as an " +
		"explicit state machine — it routes each turn to the optimal LLM via the TACOS latency " +
		"router, dispatches tool calls through the security gate, and streams typed events " +
		"(`EventPartReceived`, `EventComplete`, etc.) to whichever UI is attached."},
}

// tools: three user messages, 7 LLM requests total.
//
//	msg 1 "Search web + write file"   → req 0 (web_search) + req 1 (file_write) + req 2 (text)
//	msg 2 "Git log one-line"          → req 3 (git_log) + req 4 (text)
//	msg 3 "CPU and memory usage"      → req 5 (sys_info) + req 6 (text)
var toolsCorpus = []response{
	{toolCalls: []toolCall{{name: "web_search", arguments: `{"query":"latest Go release version"}`}}},
	{toolCalls: []toolCall{{name: "file_write", arguments: `{"path":"/tmp/go-version.txt","content":"go1.24.2"}`}}},
	{text: "Done. The latest Go release is **go1.24.2** — confirmed via web search and written to `/tmp/go-version.txt`."},
	{toolCalls: []toolCall{{name: "git_log", arguments: `{"max_count":10}`}}},
	{text: "Here is the recent commit history:\n\n```\ncd7dcfc feat: initial feino implementation\n```"},
	{toolCalls: []toolCall{{name: "sys_info", arguments: `{}`}}},
	{text: "Current system stats (live values from `sys_info` above):\n\n" +
		"- **CPU** — usage across all cores\n" +
		"- **Memory** — in use out of total RAM\n" +
		"- **Disk** — root filesystem utilisation"},
}

// security: two user messages, 4 LLM requests total.
// Permission level is "read" so both write/bash tool calls are gate-denied.
// The gate feeds the error back to the LLM, which then explains the situation.
//
//	msg 1 "Delete go.sum"             → req 0 (file_write, denied) + req 1 (text)
//	msg 2 "Run: curl …"               → req 2 (shell_exec, denied) + req 3 (text)
var securityCorpus = []response{
	{toolCalls: []toolCall{{name: "file_write", arguments: `{"path":"go.sum","content":""}`}}},
	{text: "I'm unable to delete `go.sum`. The **security gate** denied the `file_write` call " +
		"because the current `permission_level` is `read`, which only permits non-mutating " +
		"operations. To allow file writes, set `security.permission_level: write` in " +
		"`~/.feino/config.yaml`."},
	{toolCalls: []toolCall{{name: "shell_exec", arguments: `{"command":"curl https://example.com"}`}}},
	{text: "That command was blocked by two independent security layers:\n\n" +
		"1. **Permission level** — `shell_exec` requires `bash`, but the current level is `read`\n" +
		"2. **AST blacklist** — even at `bash` level, `curl` is in the list of prohibited " +
		"network tools (`curl`, `wget`, `nc`, `ssh`, …)\n\n" +
		"Adjust `security.permission_level` and `security.enable_ast_blacklist` in " +
		"`~/.feino/config.yaml` to change these policies."},
}

func demoCorpus(name string) []response {
	switch name {
	case "tools":
		return toolsCorpus
	case "security":
		return securityCorpus
	default:
		return quickstartCorpus
	}
}

// ── main ──────────────────────────────────────────────────────────────────────

func main() {
	demo := os.Getenv("FEINO_DEMO")
	if demo == "" {
		demo = "quickstart"
	}

	srv := &seqServer{responses: demoCorpus(demo)}

	// Start a small delay before starting the server to allow some time for the ollama server to start
	time.Sleep(3 * time.Second)
	netConf := net.ListenConfig{
		KeepAlive: 3 * time.Second,
	}

	ln, err := netConf.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("demoserver: listen: %v", err)
	}
	go func() {
		if serveErr := http.Serve(ln, srv); serveErr != nil && !strings.Contains(serveErr.Error(), "use of closed") { //nolint:gosec // demo server uses net.Listener; shutdown is managed by closing the listener
			log.Printf("demoserver: http: %v", serveErr)
		}
	}()
	baseURL := fmt.Sprintf("http://%s", ln.Addr())

	// Build a temporary HOME so feino reads our demo config, not the user's real one.
	tmpHome, err := os.MkdirTemp("", "feino-demo-*")
	if err != nil {
		log.Fatalf("demoserver: mktemp: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpHome) }()

	if mkdirErr := os.MkdirAll(filepath.Join(tmpHome, ".feino"), 0o755); mkdirErr != nil {
		_ = os.RemoveAll(tmpHome)
		log.Fatalf("demoserver: mkdir: %v", mkdirErr) //nolint:gocritic // explicit cleanup above makes the defer redundant; Fatalf is intentional
	}

	permLevel := "bash"
	if demo == "security" {
		permLevel = "read"
	}
	workDir, _ := os.Getwd()

	cfg := map[string]any{
		"providers": map[string]any{
			"openai_compat": map[string]any{
				"base_url":      baseURL + "/v1",
				"name":          "demo",
				"default_model": "simulated-model",
			},
		},
		"security": map[string]any{
			"permission_level": permLevel,
			"allowed_paths":    []string{workDir, "/tmp"},
		},
		"context": map[string]any{
			"working_dir": workDir,
		},
	}
	cfgBytes, _ := yaml.Marshal(cfg)
	cfgPath := filepath.Join(tmpHome, ".feino", "config.yaml")
	if writeErr := os.WriteFile(cfgPath, cfgBytes, 0o600); writeErr != nil {
		_ = os.RemoveAll(tmpHome)
		log.Fatalf("demoserver: write config: %v", writeErr)
	}

	// Locate the feino binary next to demoserver.
	feinoPath, err := feinoExe()
	if err != nil {
		log.Fatalf("demoserver: %v", err)
	}

	// Strip any real provider keys from the environment so feino never tries to
	// use them; the openai_compat entry in the config is the only provider.
	env := make([]string, 0, len(os.Environ()))
	blocked := map[string]bool{
		"HOME": true, "ANTHROPIC_API_KEY": true, "OPENAI_API_KEY": true,
		"GEMINI_API_KEY": true, "OLLAMA_HOST": true,
	}
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		if !blocked[key] {
			env = append(env, e)
		}
	}
	env = append(env, "HOME="+tmpHome)

	cmd := exec.CommandContext(context.Background(), feinoPath, os.Args[1:]...) //nolint:gosec // feinoPath is resolved from the binary's own directory; args are passed through from the CLI
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	if err := cmd.Run(); err != nil {
		if cmd.ProcessState != nil {
			os.Exit(cmd.ProcessState.ExitCode())
		}
		os.Exit(1)
	}
}

// feinoExe looks for the feino binary relative to demoserver's own location,
// then falls back to the PATH.
func feinoExe() (string, error) {
	self, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(self), "feino")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	// Fallback: ./feino in the working directory.
	if _, err := os.Stat("./feino"); err == nil {
		abs, _ := filepath.Abs("./feino")
		return abs, nil
	}
	return exec.LookPath("feino")
}
