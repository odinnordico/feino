# Package `internal/tools`

The `tools` package defines the `Tool` interface and ships the complete native tool suite that the agent uses for function calling. It also implements the plugin system for loading external executables as tools.

---

## Core interfaces

### Tool

```go
type Tool interface {
    GetName() string
    GetDescription() string
    GetParameters() map[string]any  // JSON Schema describing inputs
    Run(parameters map[string]any) ToolResult
    GetLogger() *slog.Logger
}
```

### Classified

Optional interface a tool can implement to declare its own permission level:

```go
type Classified interface {
    PermissionLevel() int  // negative = not declared
}
```

The `SecurityGate` queries this before falling back to external classification maps.

### ToolResult

```go
type ToolResult interface {
    GetContent() any
    GetError() error
}

// Construct a result:
result := tools.NewToolResult(content, err)
```

---

## Creating a tool

```go
myTool := tools.NewTool(
    "my_tool",
    "Does something useful",
    map[string]any{
        "type": "object",
        "properties": map[string]any{
            "input": map[string]any{"type": "string", "description": "Input text"},
        },
        "required": []string{"input"},
    },
    func(params map[string]any) tools.ToolResult {
        input, _ := params["input"].(string)
        return tools.NewToolResult("processed: "+input, nil)
    },
    tools.WithPermissionLevel(tools.PermLevelRead),
    tools.WithLogger(logger),
)
```

### Permission level constants

```go
const (
    PermLevelRead       = 0  // non-mutating reads
    PermLevelWrite      = 1  // file or state mutations
    PermLevelBash       = 2  // shell execution
    PermLevelDangerZone = 3  // no restrictions
)
```

---

## Native tool factories

```go
// All native tools in one slice.
tools.NewNativeTools(logger)

// Individual categories:
tools.NewShellTools(logger)    // shell_exec
tools.NewFileTools(logger)     // list_files, file_read, file_write, file_edit, file_search
tools.NewGitTools(logger)      // git_status, git_log, git_diff, git_blame
tools.NewWebTools(logger)      // web_search, web_fetch
tools.NewHTTPTools(logger)     // http_get, http_post, http_put, http_delete
tools.NewCurrencyTools(logger) // currency_convert
tools.NewWeatherTools(logger)  // weather_get_forecast
tools.NewSysInfoTools(logger)  // sysinfo_get
tools.NewNotifyTools(logger)   // notify_send
tools.NewMemoryTools(logger)   // memory_write, memory_read, memory_list, memory_delete
tools.NewBrowserTools(logger, debugPort) // browser_* suite (22 tools)
```

### Tool reference

| Tool | Level | Description |
|------|-------|-------------|
| `shell_exec` | Bash | Run a shell command with timeout; captures stdout/stderr |
| `list_files` | Read | Directory listing with optional recursive tree walk |
| `file_read` | Read | Read file contents (up to 1 MB) |
| `file_write` | Write | Atomic file write |
| `file_edit` | Write | Pattern-based find-and-replace within a file |
| `file_search` | Read | Regex search across files in a directory |
| `git_status` | Read | Working tree status |
| `git_log` | Read | Commit history with optional range and format |
| `git_diff` | Read | Diff between refs or working tree |
| `git_blame` | Read | Annotate file lines with commit info |
| `web_search` | Read | Web search |
| `web_fetch` | Read | Fetch a URL and return its text content |
| `http_get` | Read | HTTP GET with optional headers |
| `http_post` | Write | HTTP POST with JSON body |
| `http_put` | Write | HTTP PUT |
| `http_delete` | Write | HTTP DELETE |
| `currency_convert` | Read | Exchange rate conversion |
| `weather_get_forecast` | Read | Weather forecast |
| `sysinfo_get` | Read | CPU, memory, disk, OS information |
| `notify_send` | Write | Desktop system notification |
| `memory_write` | Write | Write an entry to the agent memory store |
| `memory_read` | Read | Search the agent memory store |
| `memory_list` | Read | List all memory entries |
| `memory_delete` | Write | Delete a memory entry |

---

## Browser tools

`NewBrowserTools(logger, debugPort)` returns 18 tools that control a Chromium-based browser (Chrome, Chromium, Edge, Brave) via the Chrome DevTools Protocol. Pass `debugPort=0` to use the default port (9222).

### Connection strategy

On the first tool invocation the pool:

1. Checks `http://localhost:<port>/json/version` to see if a browser is already listening.
2. If found, attaches to the first non-`chrome://` page tab via WebSocket — cookies, sessions, and extensions remain intact.
3. If not found, launches a new Chromium process using the user's own profile directory (`~/.config/google-chrome` on Linux, `~/Library/Application Support/Google/Chrome` on macOS, `%LOCALAPPDATA%\Google\Chrome\User Data` on Windows).

All 18 tools share the same pool and active-tab context. The connection is re-established automatically when a stale context is detected.

### Browser tool reference

| Tool | Level | Description |
|------|-------|-------------|
| `browser_navigate` | Read | Navigate to a URL; optional CSS `wait_for` selector and `timeout_ms` |
| `browser_back` | Read | Go back in browser history; optional `wait_for` selector |
| `browser_forward` | Read | Go forward in browser history; optional `wait_for` selector |
| `browser_reload` | Read | Reload the page; `ignore_cache=true` for hard reload (Shift+F5) |
| `browser_click` | Write | Click by CSS selector or by visible text (XPath-safe) |
| `browser_type` | Write | Type text with real key events (`clear_first` default true) |
| `browser_fill` | Write | Set input value directly without firing key events |
| `browser_select` | Write | Select a `<select>` option by `value` attribute or visible `text` |
| `browser_key` | Write | Dispatch a keyboard event (Enter, Escape, Tab, arrow keys, …) |
| `browser_hover` | Read | Hover via CDP `Input.dispatchMouseEvent` (triggers CSS `:hover`) + JS synthetic events |
| `browser_screenshot` | Read | Screenshot full page or element; configurable `quality` (1–100) |
| `browser_get_text` | Read | Get visible text content of a selector (defaults to `body`) |
| `browser_get_html` | Read | Get outer HTML of a selector; waits for DOM readiness |
| `browser_eval` | Bash | Execute JavaScript (sync or async/await); awaits Promises automatically |
| `browser_get_cookies` | Bash | List cookies; values redacted by default (`include_values=true` to reveal) |
| `browser_set_cookies` | Bash | Inject or overwrite a cookie (name, value, domain, path, secure, http_only) |
| `browser_wait` | Read | Wait for a selector to reach `visible`, `ready`, or `not_visible` |
| `browser_scroll` | Read | Scroll element into view, scroll by offset, or `to_bottom=true` |
| `browser_info` | Read | Current URL, title, and list of all open tabs with their IDs |
| `browser_switch_tab` | Read | Switch active tab by `tab_id` or `title_contains` substring |
| `browser_new_tab` | Read | Open a new tab at a URL and make it the active tab |
| `browser_close_tab` | Read | Close a tab by `tab_id`, or close the current active tab |

### Attaching to an existing browser

Start Chrome or Chromium with remote debugging enabled:

```bash
google-chrome --remote-debugging-port=9222
# or
chromium --remote-debugging-port=9222
```

FEINO will attach automatically on the next browser tool call. Your existing logins and cookies are fully available.

### Security notes

- **`browser_eval`** and **`browser_get_cookies`** / **`browser_set_cookies`** require `PermLevelBash` because they provide unrestricted access to the browser's JavaScript context and credential store.
- **`browser_eval` awaits Promises** — scripts using `async/await` or returning a `fetch()`/Promise are resolved before the result is returned. Wrap synchronous scripts in `return` or just write an expression; both work.
- Cookie **values are redacted by default** in `browser_get_cookies` results to prevent accidental leakage of session tokens and auth credentials into tool logs. Pass `include_values=true` only when the value itself is needed.
- **`browser_hover` moves the real cursor** via CDP `Input.dispatchMouseEvent`, which activates CSS `:hover` pseudo-selectors. It also fires synthetic JS `mouseover`/`mouseenter` events for JS-driven menus.
- `--no-sandbox` is only added to the launched browser when the process is running as root (e.g., inside a container). For normal user sessions the OS sandbox remains active.
- **Stale connection retry** — `pool.run` detects a cancelled tab context at call time and reconnects transparently before retrying, so transient CDP disconnects don't surface as errors to the agent.

---

## Plugin system

External executables in `~/.feino/plugins/` (or `config.Context.PluginsDir`) are loaded as tools automatically.

### Directory layout

```
~/.feino/plugins/
├── my_tool          ← executable (any language, must have the execute bit)
└── my_tool.json     ← JSON manifest
```

### Manifest format

```json
{
  "name": "my_tool",
  "description": "Does something useful",
  "permission_level": "read",
  "parameters": {
    "type": "object",
    "properties": {
      "input": {"type": "string"}
    },
    "required": ["input"]
  },
  "timeout_seconds": 30
}
```

`permission_level` must be one of: `read`, `write`, `bash`, `danger_zone`.

### Protocol

The plugin receives parameters as JSON on stdin and must write a JSON response to stdout:

```
stdin  → {"input": "hello"}
stdout → {"content": "processed: hello"}              ← success
stdout → {"content": "error text", "is_error": true}  ← failure
```

Non-JSON stdout is treated as plain-text success content. Stderr is appended to the error message on non-zero exit.

### Loading plugins

```go
tools, err := tools.LoadPlugins(pluginsDir, logger)
```

---

## Best practices

- **Declare permission levels on all native tools.** The security gate defaults unclassified tools to `DangerZone`. `WithPermissionLevel` makes the intent explicit and avoids over-restriction.
- **Return structured errors from `Run`.** Use `tools.NewToolResult(content, fmt.Errorf("..."))` rather than embedding error text in `content`. The agent interprets `GetError() != nil` as a failed tool call and may retry.
- **Keep `Run` functions synchronous.** The agent's verify phase runs tool calls sequentially. Long-running tools should respect context cancellation via `context.WithTimeout`.
- **Plugin timeouts are enforced.** A plugin that exceeds `timeout_seconds` is killed with SIGTERM. Always set a realistic timeout in the manifest.
- **Browser tools share a single pool.** Do not call `NewBrowserTools` more than once per session; the second call creates a competing CDP connection on the same port.
- **`browser_new_tab` is generation-safe.** If the browser reconnects while a new tab is being navigated, the stale tab is discarded and an error is returned rather than corrupting the pool state. The caller should retry once.

---

## Extending

### Adding a new native tool

1. Create (or add to) a `_tools.go` file in `internal/tools/`.
2. Implement the factory function: `func NewMyTools(logger *slog.Logger) []Tool { ... }`.
3. Call it from `NewNativeTools` in `native.go`.
4. Add a test case in `*_test.go` covering the parameter schema and at least one success and one failure scenario.

### Adding a new browser tool

1. Write a `newBrowser<Name>Tool(pool *browserPool, logger *slog.Logger) Tool` function in `browser.go`.
2. Use `pool.runDefault(actions...)` for standard 30 s operations, or `pool.run(timeout, actions...)` when a custom timeout is needed.
3. Register it in the `NewBrowserTools` return slice.
4. Pick the correct permission level: most read-only DOM operations are `PermLevelRead`; anything that mutates page state is `PermLevelWrite`; JS eval, cookie access, and network interception are `PermLevelBash`.

### Adding a new tool category factory

Follow the pattern in `shell.go`, `files.go`, `git.go`, etc.:

```go
func NewMyTools(logger *slog.Logger) []Tool {
    return []Tool{
        NewTool("my_tool", "...", schema, func(params map[string]any) ToolResult {
            // implementation
        }, WithPermissionLevel(PermLevelRead), WithLogger(logger)),
    }
}
```

### Extending the plugin protocol

The plugin protocol is intentionally minimal (stdin/stdout JSON). To add bidirectional streaming, consider switching to a proper MCP server (`internal/mcp`) instead of extending the plugin protocol.
