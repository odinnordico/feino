// Package tools — browser automation via Chrome DevTools Protocol.
//
// NewBrowserTools returns a suite of tools that connect to a running
// Chromium-based browser (Chrome, Chromium, Edge, Brave) on its remote-debugging
// port. If no browser is listening on the configured port the tool set
// automatically launches one using the user's existing profile so that all
// cookies, sessions, and extensions are intact.
//
// The connection is lazy: the first actual tool invocation triggers the
// connect-or-launch logic. All tools in the suite share the same browser
// context and active tab.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	cdpinput "github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/storage"
	cdptarget "github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

const (
	defaultDebugPort   = 9222
	browserOpTimeout   = 30 * time.Second
	browserConnTimeout = 5 * time.Second
)

// browserPool manages a shared CDP browser connection.
// A single instance is shared across all tools returned by NewBrowserTools.
type browserPool struct {
	mu        sync.Mutex
	logger    *slog.Logger
	debugPort int

	// gen is incremented on every reconnect so concurrent operations that
	// snapshotted an older generation can detect the pool changed under them.
	gen uint64

	// allocCtx owns the allocator lifetime (process or remote connection).
	allocCtx  context.Context
	allocCncl context.CancelFunc

	// tabCtx is the currently active tab context.
	tabCtx  context.Context
	tabCncl context.CancelFunc
}

func newBrowserPool(logger *slog.Logger, debugPort int) *browserPool {
	if debugPort <= 0 {
		debugPort = defaultDebugPort
	}
	return &browserPool{logger: logger, debugPort: debugPort}
}

// ensureConnected connects to an existing browser or launches a new one.
// Must be called with pool.mu held. Temporarily releases the lock during slow
// network and process-launch operations so other tool calls are not stalled.
func (pool *browserPool) ensureConnected() error {
	if pool.tabCtx != nil {
		select {
		case <-pool.tabCtx.Done():
			pool.close()
		default:
			return nil
		}
	}

	// Release the lock while probing for a running browser — the HTTP call
	// blocks for up to browserConnTimeout and must not stall other tools.
	pool.mu.Unlock()
	wsURL := pool.probeWebSocketURL()
	pool.mu.Lock()

	// Re-check: another goroutine may have connected while we had the lock released.
	if pool.tabCtx != nil {
		select {
		case <-pool.tabCtx.Done():
			pool.close()
		default:
			return nil
		}
	}

	if wsURL != "" && pool.connectRemote(wsURL) {
		return nil
	}
	return pool.launch()
}

// probeWebSocketURL checks whether a browser is already listening on the debug
// port and returns its WebSocket debugger URL, or "" if none is found.
// Safe to call without pool.mu held.
func (pool *browserPool) probeWebSocketURL() string {
	url := fmt.Sprintf("http://localhost:%d/json/version", pool.debugPort)
	client := &http.Client{Timeout: browserConnTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var info struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return ""
	}
	return info.WebSocketDebuggerURL
}

// connectRemote attaches to a browser via its WebSocket debugger URL.
// Must be called with pool.mu held.
func (pool *browserPool) connectRemote(wsURL string) bool {
	pool.logger.Info("browser: attaching to existing browser", "port", pool.debugPort, "ws", wsURL)

	allocCtx, allocCncl := chromedp.NewRemoteAllocator(context.Background(), wsURL)

	// List existing page targets; reuse the first non-chrome:// page.
	var targets []*cdptarget.Info
	probeCtx, probeCancel := chromedp.NewContext(allocCtx)
	probeErr := chromedp.Run(probeCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		targets, err = chromedp.Targets(ctx)
		return err
	}))
	probeCancel()
	if probeErr != nil {
		allocCncl()
		return false
	}

	var targetID cdptarget.ID
	for _, t := range targets {
		if t.Type == "page" && !strings.HasPrefix(t.URL, "chrome://") {
			targetID = t.TargetID
			break
		}
	}

	var tabCtx context.Context
	var tabCncl context.CancelFunc
	if targetID != "" {
		tabCtx, tabCncl = chromedp.NewContext(allocCtx,
			chromedp.WithTargetID(targetID),
			chromedp.WithLogf(pool.logf),
		)
	} else {
		tabCtx, tabCncl = chromedp.NewContext(allocCtx, chromedp.WithLogf(pool.logf))
	}

	if err := chromedp.Run(tabCtx); err != nil {
		tabCncl()
		allocCncl()
		return false
	}

	pool.allocCtx = allocCtx
	pool.allocCncl = allocCncl
	pool.tabCtx = tabCtx
	pool.tabCncl = tabCncl
	return true
}

// launch starts a new Chromium process with the user's profile.
func (pool *browserPool) launch() error {
	profileDir := userChromeDataDir()
	pool.logger.Info("browser: launching Chromium", "profile", profileDir, "debug_port", pool.debugPort)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("remote-debugging-port", fmt.Sprintf("%d", pool.debugPort)),
		chromedp.Flag("disable-background-networking", false),
		chromedp.Flag("no-restore-last-session", true),
	)
	// no-sandbox disables OS-level sandboxing; only needed when running as root.
	if os.Getuid() == 0 {
		opts = append(opts, chromedp.Flag("no-sandbox", true))
	}
	if profileDir != "" {
		opts = append(opts, chromedp.UserDataDir(profileDir))
	}

	allocCtx, allocCncl := chromedp.NewExecAllocator(context.Background(), opts...)
	tabCtx, tabCncl := chromedp.NewContext(allocCtx, chromedp.WithLogf(pool.logf))

	if err := chromedp.Run(tabCtx); err != nil {
		tabCncl()
		allocCncl()
		return fmt.Errorf("browser: launch failed: %w", err)
	}

	pool.allocCtx = allocCtx
	pool.allocCncl = allocCncl
	pool.tabCtx = tabCtx
	pool.tabCncl = tabCncl
	return nil
}

func (pool *browserPool) logf(format string, args ...any) {
	pool.logger.Debug("cdp: " + fmt.Sprintf(format, args...))
}

// run executes CDP actions on the active tab with the given timeout.
// If the tab context was already cancelled when captured (stale connection),
// it reconnects and retries once transparently.
func (pool *browserPool) run(timeout time.Duration, actions ...chromedp.Action) error {
	for attempt := range 2 {
		pool.mu.Lock()
		if err := pool.ensureConnected(); err != nil {
			pool.mu.Unlock()
			return err
		}
		ctx := pool.tabCtx
		pool.mu.Unlock()

		opCtx, cancel := context.WithTimeout(ctx, timeout)
		err := chromedp.Run(opCtx, actions...)
		cancel()
		if err == nil {
			return nil
		}
		// Retry only when the tab context itself was already done at capture time.
		if attempt == 0 {
			select {
			case <-ctx.Done():
				continue
			default:
				return err
			}
		}
		return err
	}
	return nil
}

// runDefault executes CDP actions with the standard per-operation timeout.
func (pool *browserPool) runDefault(actions ...chromedp.Action) error {
	return pool.run(browserOpTimeout, actions...)
}

// switchTab replaces the active tab context with one pointing at targetID.
func (pool *browserPool) switchTab(targetID cdptarget.ID) error {
	// Phase 1: snapshot allocator and generation under lock.
	pool.mu.Lock()
	if err := pool.ensureConnected(); err != nil {
		pool.mu.Unlock()
		return err
	}
	allocCtx := pool.allocCtx
	snapGen := pool.gen
	pool.mu.Unlock()

	// Phase 2: attach to the target outside the lock (CDP round-trip).
	newCtx, newCncl := chromedp.NewContext(allocCtx,
		chromedp.WithTargetID(targetID),
		chromedp.WithLogf(pool.logf),
	)
	if err := chromedp.Run(newCtx); err != nil {
		newCncl()
		return fmt.Errorf("browser: switch to tab %s: %w", targetID, err)
	}

	// Phase 3: swap atomically; abort if the pool reconnected under us.
	pool.mu.Lock()
	if pool.gen != snapGen {
		pool.mu.Unlock()
		newCncl()
		return fmt.Errorf("browser: switch to tab %s: browser reconnected during switch; try again", targetID)
	}
	oldCncl := pool.tabCncl
	pool.tabCtx = newCtx
	pool.tabCncl = newCncl
	pool.mu.Unlock()

	if oldCncl != nil {
		oldCncl()
	}
	return nil
}

// listTargets returns all page targets from the current allocator.
func (pool *browserPool) listTargets() ([]*cdptarget.Info, error) {
	pool.mu.Lock()
	if err := pool.ensureConnected(); err != nil {
		pool.mu.Unlock()
		return nil, err
	}
	ctx := pool.tabCtx
	pool.mu.Unlock()

	var targets []*cdptarget.Info
	opCtx, cancel := context.WithTimeout(ctx, browserOpTimeout)
	defer cancel()
	err := chromedp.Run(opCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		targets, err = chromedp.Targets(ctx)
		return err
	}))
	return targets, err
}

// close releases all browser resources (does not terminate a remote browser).
// Must be called with pool.mu held.
func (pool *browserPool) close() {
	pool.gen++
	if pool.tabCncl != nil {
		pool.tabCncl()
		pool.tabCncl = nil
		pool.tabCtx = nil
	}
	if pool.allocCncl != nil {
		pool.allocCncl()
		pool.allocCncl = nil
		pool.allocCtx = nil
	}
}

// userChromeDataDir returns the first Chromium-family profile directory found.
func userChromeDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	var candidates []string
	switch runtime.GOOS {
	case "linux":
		candidates = []string{
			filepath.Join(home, ".config", "google-chrome"),
			filepath.Join(home, ".config", "chromium"),
			filepath.Join(home, ".config", "microsoft-edge"),
			filepath.Join(home, ".config", "brave-browser"),
		}
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support")
		candidates = []string{
			filepath.Join(base, "Google", "Chrome"),
			filepath.Join(base, "Chromium"),
			filepath.Join(base, "Microsoft Edge"),
			filepath.Join(base, "BraveSoftware", "Brave-Browser"),
		}
	default: // windows
		local := os.Getenv("LOCALAPPDATA")
		candidates = []string{
			filepath.Join(local, "Google", "Chrome", "User Data"),
			filepath.Join(local, "Chromium", "User Data"),
			filepath.Join(local, "Microsoft", "Edge", "User Data"),
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// xpathEscapeText returns an XPath expression that safely matches the given
// string regardless of embedded single or double quotes.
func xpathEscapeText(s string) string {
	if !strings.Contains(s, "'") {
		return "'" + s + "'"
	}
	if !strings.Contains(s, `"`) {
		return `"` + s + `"`
	}
	// Both quote types present: use XPath concat().
	parts := strings.Split(s, "'")
	exprs := make([]string, 0, len(parts)*2)
	for i, p := range parts {
		if i > 0 {
			exprs = append(exprs, `"'"`)
		}
		if p != "" {
			exprs = append(exprs, "'"+p+"'")
		}
	}
	return "concat(" + strings.Join(exprs, ",") + ")"
}

// keyChar maps human-readable key names to the characters chromedp.KeyEvent accepts.
func keyChar(key string) string {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "enter", "return":
		return "\r"
	case "escape", "esc":
		return "\x1b"
	case "tab":
		return "\t"
	case "backspace":
		return "\b"
	case "delete":
		return "\x7f"
	case "space":
		return " "
	case "arrowup", "up":
		return ""
	case "arrowdown", "down":
		return ""
	case "arrowleft", "left":
		return ""
	case "arrowright", "right":
		return ""
	case "home":
		return ""
	case "end":
		return ""
	case "pageup":
		return ""
	case "pagedown":
		return ""
	default:
		return key // pass through for single chars and anything else
	}
}

// ── Tool factories ────────────────────────────────────────────────────────────

// NewBrowserTools returns the full browser automation tool suite.
// All tools share a single lazy browser connection managed by an internal pool.
//
// The pool tries to attach to a browser already running with remote-debugging
// on debugPort (default 9222). If none is found it launches Chromium using the
// user's own profile directory so cookies and login sessions are available.
//
// Pass debugPort=0 to use the default port (9222).
func NewBrowserTools(logger *slog.Logger, debugPort int) []Tool {
	pool := newBrowserPool(logger, debugPort)
	return []Tool{
		newBrowserNavigateTool(pool, logger),
		newBrowserBackTool(pool, logger),
		newBrowserForwardTool(pool, logger),
		newBrowserReloadTool(pool, logger),
		newBrowserClickTool(pool, logger),
		newBrowserTypeTool(pool, logger),
		newBrowserFillTool(pool, logger),
		newBrowserSelectTool(pool, logger),
		newBrowserKeyTool(pool, logger),
		newBrowserHoverTool(pool, logger),
		newBrowserScreenshotTool(pool, logger),
		newBrowserGetTextTool(pool, logger),
		newBrowserGetHTMLTool(pool, logger),
		newBrowserEvalTool(pool, logger),
		newBrowserGetCookiesTool(pool, logger),
		newBrowserSetCookiesTool(pool, logger),
		newBrowserWaitTool(pool, logger),
		newBrowserScrollTool(pool, logger),
		newBrowserInfoTool(pool, logger),
		newBrowserSwitchTabTool(pool, logger),
		newBrowserNewTabTool(pool, logger),
		newBrowserCloseTabTool(pool, logger),
	}
}

// ── browser_navigate ──────────────────────────────────────────────────────────

func newBrowserNavigateTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "Full URL to navigate to (e.g. https://example.com).",
			},
			"wait_for": map[string]any{
				"type":        "string",
				"description": "Optional CSS selector to wait for after navigation (up to 10 s).",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "Navigation timeout in milliseconds (default 30000).",
			},
		},
		"required": []string{"url"},
	}
	return NewTool("browser_navigate", "Navigate the active browser tab to a URL.", schema,
		func(params map[string]any) ToolResult {
			url, ok := getString(params, "url")
			if !ok || url == "" {
				return NewToolResult("", fmt.Errorf("browser_navigate: 'url' is required"))
			}
			waitFor := getStringDefault(params, "wait_for", "")
			timeoutMs := getInt(params, "timeout_ms", int(browserOpTimeout/time.Millisecond))
			timeout := time.Duration(timeoutMs) * time.Millisecond

			var title, finalURL string
			actions := []chromedp.Action{
				chromedp.Navigate(url),
				chromedp.Title(&title),
				chromedp.Location(&finalURL),
			}
			if waitFor != "" {
				actions = append(actions, chromedp.WaitVisible(waitFor, chromedp.ByQuery))
			}

			if err := pool.run(timeout, actions...); err != nil {
				return NewToolResult("", fmt.Errorf("browser_navigate: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Navigated to: %s\nTitle: %s", finalURL, title), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── browser_click ─────────────────────────────────────────────────────────────

func newBrowserClickTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element to click.",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Click the first element whose visible text contains this string. Used when 'selector' is not provided.",
			},
			"wait_visible": map[string]any{
				"type":        "boolean",
				"description": "Wait for the element to be visible before clicking (default true).",
			},
		},
	}
	return NewTool("browser_click", "Click an element in the active browser tab.", schema,
		func(params map[string]any) ToolResult {
			selector := getStringDefault(params, "selector", "")
			text := getStringDefault(params, "text", "")
			waitVisible := getBool(params, "wait_visible", true)

			if selector == "" && text == "" {
				return NewToolResult("", fmt.Errorf("browser_click: provide 'selector' or 'text'"))
			}

			// Build a safe XPath when matching by visible text.
			byXPath := false
			if selector == "" {
				selector = fmt.Sprintf("//*[contains(text(),%s)]", xpathEscapeText(text))
				byXPath = true
			}

			queryOption := chromedp.ByQuery
			if byXPath {
				queryOption = chromedp.BySearch
			}

			var actions []chromedp.Action
			if waitVisible {
				actions = append(actions, chromedp.WaitVisible(selector, queryOption))
			}
			actions = append(actions, chromedp.Click(selector, queryOption))

			if err := pool.runDefault(actions...); err != nil {
				return NewToolResult("", fmt.Errorf("browser_click: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Clicked: %s", selector), nil)
		},
		WithPermissionLevel(PermLevelWrite),
		WithLogger(logger),
	)
}

// ── browser_type ──────────────────────────────────────────────────────────────

func newBrowserTypeTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the input element.",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Text to type (dispatches real key events — triggers JS listeners).",
			},
			"clear_first": map[string]any{
				"type":        "boolean",
				"description": "Clear the field before typing (default true).",
			},
		},
		"required": []string{"selector", "text"},
	}
	return NewTool("browser_type", "Type text into an input element (fires real keyboard events).", schema,
		func(params map[string]any) ToolResult {
			selector, ok := getString(params, "selector")
			if !ok {
				return NewToolResult("", fmt.Errorf("browser_type: 'selector' is required"))
			}
			text, ok := getString(params, "text")
			if !ok {
				return NewToolResult("", fmt.Errorf("browser_type: 'text' is required"))
			}
			clearFirst := getBool(params, "clear_first", true)

			var actions []chromedp.Action
			actions = append(actions, chromedp.WaitVisible(selector, chromedp.ByQuery))
			if clearFirst {
				actions = append(actions, chromedp.Clear(selector, chromedp.ByQuery))
			}
			actions = append(actions, chromedp.SendKeys(selector, text, chromedp.ByQuery))

			if err := pool.runDefault(actions...); err != nil {
				return NewToolResult("", fmt.Errorf("browser_type: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Typed into %s", selector), nil)
		},
		WithPermissionLevel(PermLevelWrite),
		WithLogger(logger),
	)
}

// ── browser_fill ──────────────────────────────────────────────────────────────

func newBrowserFillTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the input or textarea.",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "Value to set (replaces current content; does not fire key events).",
			},
		},
		"required": []string{"selector", "value"},
	}
	return NewTool("browser_fill",
		"Set the value of an input field directly (faster than browser_type; does not fire key events).",
		schema,
		func(params map[string]any) ToolResult {
			selector, _ := getString(params, "selector")
			value, _ := getString(params, "value")
			if selector == "" {
				return NewToolResult("", fmt.Errorf("browser_fill: 'selector' is required"))
			}
			if err := pool.runDefault(
				chromedp.WaitVisible(selector, chromedp.ByQuery),
				chromedp.SetValue(selector, value, chromedp.ByQuery),
			); err != nil {
				return NewToolResult("", fmt.Errorf("browser_fill: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Set value of %s", selector), nil)
		},
		WithPermissionLevel(PermLevelWrite),
		WithLogger(logger),
	)
}

// ── browser_select ────────────────────────────────────────────────────────────

func newBrowserSelectTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the <select> element.",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "Select the option with this value attribute.",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Select the option whose visible text matches (exact first, then partial). Used when 'value' is not provided.",
			},
		},
		"required": []string{"selector"},
	}
	return NewTool("browser_select", "Select an option in a <select> dropdown by value or visible text.", schema,
		func(params map[string]any) ToolResult {
			selector, ok := getString(params, "selector")
			if !ok || selector == "" {
				return NewToolResult("", fmt.Errorf("browser_select: 'selector' is required"))
			}
			value := getStringDefault(params, "value", "")
			text := getStringDefault(params, "text", "")

			if value == "" && text == "" {
				return NewToolResult("", fmt.Errorf("browser_select: provide 'value' or 'text'"))
			}

			var result any
			var actions []chromedp.Action

			if value != "" {
				// Direct value assignment — fast path.
				actions = []chromedp.Action{
					chromedp.WaitVisible(selector, chromedp.ByQuery),
					chromedp.SetValue(selector, value, chromedp.ByQuery),
					// Dispatch change/input so JS listeners fire.
					chromedp.EvaluateAsDevTools(fmt.Sprintf(
						`(function(){var el=document.querySelector(%q);`+
							`el.dispatchEvent(new Event('change',{bubbles:true}));`+
							`el.dispatchEvent(new Event('input',{bubbles:true}));`+
							`return el.value})()`, selector), &result),
				}
			} else {
				// Text-based selection via JS to avoid XPath complexity.
				script := fmt.Sprintf(`(function(){
					var sel = document.querySelector(%q);
					if (!sel) return "error: element not found";
					var opts = Array.from(sel.options);
					var want = %q;
					var opt = opts.find(function(o){return o.text.trim()===want;})
					         || opts.find(function(o){return o.text.trim().toLowerCase().includes(want.toLowerCase());});
					if (!opt) return "error: option not found: " + want;
					sel.value = opt.value;
					sel.dispatchEvent(new Event('change',{bubbles:true}));
					sel.dispatchEvent(new Event('input',{bubbles:true}));
					return "selected: " + opt.text.trim();
				})()`, selector, text)
				actions = []chromedp.Action{
					chromedp.WaitVisible(selector, chromedp.ByQuery),
					chromedp.EvaluateAsDevTools(script, &result),
				}
			}

			if err := pool.runDefault(actions...); err != nil {
				return NewToolResult("", fmt.Errorf("browser_select: %w", err))
			}
			msg := fmt.Sprintf("%v", result)
			if strings.HasPrefix(msg, "error:") {
				return NewToolResult("", fmt.Errorf("browser_select: %s", msg))
			}
			return NewToolResult(msg, nil)
		},
		WithPermissionLevel(PermLevelWrite),
		WithLogger(logger),
	)
}

// ── browser_key ───────────────────────────────────────────────────────────────

func newBrowserKeyTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key": map[string]any{
				"type": "string",
				"description": "Key to dispatch. Named keys: Enter, Escape, Tab, Backspace, Delete, Space, " +
					"ArrowUp, ArrowDown, ArrowLeft, ArrowRight, Home, End, PageUp, PageDown. " +
					"Or pass a single character directly.",
			},
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector to focus before sending the key. When omitted the key goes to the active element.",
			},
		},
		"required": []string{"key"},
	}
	return NewTool("browser_key", "Dispatch a keyboard event (Enter, Escape, Tab, arrow keys, etc.).", schema,
		func(params map[string]any) ToolResult {
			key, ok := getString(params, "key")
			if !ok || key == "" {
				return NewToolResult("", fmt.Errorf("browser_key: 'key' is required"))
			}
			selector := getStringDefault(params, "selector", "")
			ch := keyChar(key)

			var actions []chromedp.Action
			if selector != "" {
				actions = append(actions,
					chromedp.WaitVisible(selector, chromedp.ByQuery),
					chromedp.Focus(selector, chromedp.ByQuery),
					chromedp.SendKeys(selector, ch, chromedp.ByQuery),
				)
			} else {
				actions = append(actions, chromedp.KeyEvent(ch))
			}

			if err := pool.runDefault(actions...); err != nil {
				return NewToolResult("", fmt.Errorf("browser_key: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Sent key %q", key), nil)
		},
		WithPermissionLevel(PermLevelWrite),
		WithLogger(logger),
	)
}

// ── browser_hover ─────────────────────────────────────────────────────────────

func newBrowserHoverTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element to hover over.",
			},
		},
		"required": []string{"selector"},
	}
	return NewTool("browser_hover", "Hover over a DOM element, triggering both CSS :hover and JS mouseover handlers.", schema,
		func(params map[string]any) ToolResult {
			selector, ok := getString(params, "selector")
			if !ok || selector == "" {
				return NewToolResult("", fmt.Errorf("browser_hover: 'selector' is required"))
			}

			// Step 1: get element center in viewport coordinates.
			posScript := fmt.Sprintf(`(function(){
				var el = document.querySelector(%q);
				if (!el) return "";
				var r = el.getBoundingClientRect();
				return JSON.stringify({x: r.left + r.width/2, y: r.top + r.height/2});
			})()`, selector)

			// Step 2: dispatch JS mouse events (for JS-driven menus).
			jsScript := fmt.Sprintf(`(function(){
				var el = document.querySelector(%q);
				if (!el) return;
				["mouseover","mouseenter","mousemove"].forEach(function(t){
					el.dispatchEvent(new MouseEvent(t,{bubbles:true,cancelable:true}));
				});
			})()`, selector)

			var posJSON string
			if err := pool.runDefault(
				chromedp.WaitVisible(selector, chromedp.ByQuery),
				chromedp.EvaluateAsDevTools(posScript, &posJSON),
			); err != nil {
				return NewToolResult("", fmt.Errorf("browser_hover: %w", err))
			}
			if posJSON == "" {
				return NewToolResult("", fmt.Errorf("browser_hover: element not found: %s", selector))
			}

			var pos struct {
				X float64 `json:"x"`
				Y float64 `json:"y"`
			}
			if err := json.Unmarshal([]byte(posJSON), &pos); err != nil {
				return NewToolResult("", fmt.Errorf("browser_hover: parse position: %w", err))
			}

			// Step 3: move the real mouse cursor via CDP (triggers CSS :hover) and
			// fire JS synthetic events in the same action sequence.
			var ignored any
			if err := pool.runDefault(
				chromedp.ActionFunc(func(ctx context.Context) error {
					return cdpinput.DispatchMouseEvent(cdpinput.MouseMoved, pos.X, pos.Y).Do(ctx)
				}),
				chromedp.EvaluateAsDevTools(jsScript, &ignored),
			); err != nil {
				return NewToolResult("", fmt.Errorf("browser_hover: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Hovered over: %s (%.0f, %.0f)", selector, pos.X, pos.Y), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── browser_screenshot ────────────────────────────────────────────────────────

func newBrowserScreenshotTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector to screenshot. Omit for full-page screenshot.",
			},
			"save_path": map[string]any{
				"type":        "string",
				"description": "File path where the PNG will be saved. Must be inside the OS temp directory or an absolute path the user controls. Defaults to a temp file.",
			},
			"quality": map[string]any{
				"type":        "integer",
				"description": "JPEG quality 1–100 for full-page screenshots (default 90). Ignored for element screenshots (always PNG).",
			},
		},
	}
	return NewTool("browser_screenshot", "Take a screenshot of the active tab or a specific element.", schema,
		func(params map[string]any) ToolResult {
			selector := getStringDefault(params, "selector", "")
			savePath := getStringDefault(params, "save_path", "")
			quality := max(1, min(100, getInt(params, "quality", 90)))

			if savePath == "" {
				f, err := os.CreateTemp("", "feino-browser-*.png")
				if err != nil {
					return NewToolResult("", fmt.Errorf("browser_screenshot: create temp file: %w", err))
				}
				f.Close()
				savePath = f.Name()
			}

			var buf []byte
			var action chromedp.Action
			if selector != "" {
				action = chromedp.Screenshot(selector, &buf, chromedp.ByQuery)
			} else {
				action = chromedp.FullScreenshot(&buf, quality)
			}

			if err := pool.runDefault(action); err != nil {
				return NewToolResult("", fmt.Errorf("browser_screenshot: %w", err))
			}
			if err := os.WriteFile(savePath, buf, 0644); err != nil {
				return NewToolResult("", fmt.Errorf("browser_screenshot: write: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Screenshot saved: %s (%d bytes)", savePath, len(buf)), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── browser_get_text ──────────────────────────────────────────────────────────

func newBrowserGetTextTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector. Defaults to 'body' (full visible page text).",
			},
		},
	}
	return NewTool("browser_get_text", "Get the visible text content of a page element.", schema,
		func(params map[string]any) ToolResult {
			selector := getStringDefault(params, "selector", "body")
			var text string
			if err := pool.runDefault(
				chromedp.WaitReady(selector, chromedp.ByQuery),
				chromedp.Text(selector, &text, chromedp.ByQuery),
			); err != nil {
				return NewToolResult("", fmt.Errorf("browser_get_text: %w", err))
			}
			return NewToolResult(text, nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── browser_get_html ──────────────────────────────────────────────────────────

func newBrowserGetHTMLTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector. Defaults to the full document.",
			},
		},
	}
	return NewTool("browser_get_html", "Get the HTML source of a page element or the full document.", schema,
		func(params map[string]any) ToolResult {
			selector := getStringDefault(params, "selector", "html")
			var html string
			if err := pool.runDefault(
				chromedp.WaitReady(selector, chromedp.ByQuery),
				chromedp.OuterHTML(selector, &html, chromedp.ByQuery),
			); err != nil {
				return NewToolResult("", fmt.Errorf("browser_get_html: %w", err))
			}
			return NewToolResult(html, nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── browser_eval ─────────────────────────────────────────────────────────────

func newBrowserEvalTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"script": map[string]any{
				"type": "string",
				"description": "JavaScript to execute. Use 'return value' to return data. " +
					"Example: \"return document.querySelectorAll('a').length\".",
			},
		},
		"required": []string{"script"},
	}
	return NewTool("browser_eval", "Execute JavaScript in the active tab and return the result. Supports async/await and Promises.", schema,
		func(params map[string]any) ToolResult {
			script, ok := getString(params, "script")
			if !ok || script == "" {
				return NewToolResult("", fmt.Errorf("browser_eval: 'script' is required"))
			}
			// Wrap in async IIFE: handles bare return, expressions, and async/await equally.
			expr := "(async function(){\n" + script + "\n})()"
			var raw json.RawMessage
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				res, exp, err := cdpruntime.Evaluate(expr).
					WithAwaitPromise(true).
					WithReturnByValue(true).
					Do(ctx)
				if err != nil {
					return err
				}
				if exp != nil {
					if exp.Exception != nil && exp.Exception.Description != "" {
						return fmt.Errorf("JS exception: %s", exp.Exception.Description)
					}
					return fmt.Errorf("JS exception: %s", exp.Text)
				}
				if res != nil && len(res.Value) > 0 {
					raw = json.RawMessage(res.Value)
				}
				return nil
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_eval: %w", err))
			}
			if raw == nil {
				return NewToolResult("null", nil)
			}
			return NewToolResult(string(raw), nil)
		},
		WithPermissionLevel(PermLevelBash),
		WithLogger(logger),
	)
}

// ── browser_get_cookies ───────────────────────────────────────────────────────

func newBrowserGetCookiesTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"filter": map[string]any{
				"type":        "string",
				"description": "Case-insensitive substring filter on cookie name or domain. Returns all cookies when empty.",
			},
			"include_values": map[string]any{
				"type":        "boolean",
				"description": "Include cookie values in the response (default false). Values may contain session tokens and credentials — only enable when needed.",
			},
		},
	}
	return NewTool("browser_get_cookies",
		"Return browser cookies (including HttpOnly). Values are redacted by default; set include_values=true to reveal them.",
		schema,
		func(params map[string]any) ToolResult {
			filter := strings.ToLower(getStringDefault(params, "filter", ""))
			includeValues := getBool(params, "include_values", false)

			var cookies []*network.Cookie
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				var err error
				cookies, err = storage.GetCookies().Do(ctx)
				return err
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_get_cookies: %w", err))
			}

			type cookieSummary struct {
				Name     string `json:"name"`
				Domain   string `json:"domain"`
				Path     string `json:"path"`
				Value    string `json:"value,omitempty"`
				Expires  string `json:"expires,omitempty"`
				Secure   bool   `json:"secure"`
				HttpOnly bool   `json:"http_only"`
				SameSite string `json:"same_site,omitempty"`
			}
			var out []cookieSummary
			for _, c := range cookies {
				if filter != "" &&
					!strings.Contains(strings.ToLower(c.Name), filter) &&
					!strings.Contains(strings.ToLower(c.Domain), filter) {
					continue
				}
				cs := cookieSummary{
					Name:     c.Name,
					Domain:   c.Domain,
					Path:     c.Path,
					Secure:   c.Secure,
					HttpOnly: c.HTTPOnly,
					SameSite: string(c.SameSite),
				}
				if includeValues {
					cs.Value = c.Value
				} else {
					cs.Value = "<redacted>"
				}
				if c.Expires > 0 {
					cs.Expires = time.Unix(int64(c.Expires), 0).UTC().Format(time.RFC3339)
				}
				out = append(out, cs)
			}
			b, _ := json.MarshalIndent(out, "", "  ")
			return NewToolResult(string(b), nil)
		},
		WithPermissionLevel(PermLevelBash),
		WithLogger(logger),
	)
}

// ── browser_set_cookies ───────────────────────────────────────────────────────

func newBrowserSetCookiesTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Cookie name.",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "Cookie value.",
			},
			"domain": map[string]any{
				"type":        "string",
				"description": "Cookie domain (e.g. .example.com). Defaults to the current page's domain.",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "Cookie path (default '/').",
			},
			"secure": map[string]any{
				"type":        "boolean",
				"description": "Mark cookie as Secure (default false).",
			},
			"http_only": map[string]any{
				"type":        "boolean",
				"description": "Mark cookie as HttpOnly (default false).",
			},
		},
		"required": []string{"name", "value"},
	}
	return NewTool("browser_set_cookies",
		"Inject or overwrite a browser cookie. Useful for session replay and testing authenticated flows.",
		schema,
		func(params map[string]any) ToolResult {
			name, ok := getString(params, "name")
			if !ok || name == "" {
				return NewToolResult("", fmt.Errorf("browser_set_cookies: 'name' is required"))
			}
			value, _ := getString(params, "value")
			domain := getStringDefault(params, "domain", "")
			path := getStringDefault(params, "path", "/")
			secure := getBool(params, "secure", false)
			httpOnly := getBool(params, "http_only", false)

			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				expr := network.SetCookie(name, value).WithPath(path).WithSecure(secure).WithHTTPOnly(httpOnly)
				if domain != "" {
					expr = expr.WithDomain(domain)
				}
				return expr.Do(ctx)
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_set_cookies: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Set cookie %q on %s", name, domain), nil)
		},
		WithPermissionLevel(PermLevelBash),
		WithLogger(logger),
	)
}

// ── browser_wait ──────────────────────────────────────────────────────────────

func newBrowserWaitTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector to wait for.",
			},
			"state": map[string]any{
				"type":        "string",
				"enum":        []string{"visible", "ready", "not_visible"},
				"description": "Condition: visible (default), ready (present in DOM), not_visible.",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"description": "Maximum wait in milliseconds (default 10000).",
			},
		},
		"required": []string{"selector"},
	}
	return NewTool("browser_wait", "Wait for a DOM element to reach a given state.", schema,
		func(params map[string]any) ToolResult {
			selector, ok := getString(params, "selector")
			if !ok || selector == "" {
				return NewToolResult("", fmt.Errorf("browser_wait: 'selector' is required"))
			}
			state := getStringDefault(params, "state", "visible")
			timeoutMs := getInt(params, "timeout_ms", 10000)
			timeout := time.Duration(timeoutMs) * time.Millisecond

			var action chromedp.Action
			switch state {
			case "ready":
				action = chromedp.WaitReady(selector, chromedp.ByQuery)
			case "not_visible":
				action = chromedp.WaitNotVisible(selector, chromedp.ByQuery)
			default:
				action = chromedp.WaitVisible(selector, chromedp.ByQuery)
			}

			if err := pool.run(timeout, action); err != nil {
				return NewToolResult("", fmt.Errorf("browser_wait: element %q did not reach state %q: %w", selector, state, err))
			}
			return NewToolResult(fmt.Sprintf("Element %q is %s", selector, state), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── browser_scroll ────────────────────────────────────────────────────────────

func newBrowserScrollTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "Scroll this element into view. When provided, x/y/to_bottom are ignored.",
			},
			"x": map[string]any{
				"type":        "integer",
				"description": "Horizontal scroll offset in pixels (window scroll).",
			},
			"y": map[string]any{
				"type":        "integer",
				"description": "Vertical scroll offset in pixels (window scroll).",
			},
			"to_bottom": map[string]any{
				"type":        "boolean",
				"description": "Scroll the window to the very bottom of the page (default false).",
			},
		},
	}
	return NewTool("browser_scroll", "Scroll the page or scroll an element into view.", schema,
		func(params map[string]any) ToolResult {
			selector := getStringDefault(params, "selector", "")
			x := getInt(params, "x", 0)
			y := getInt(params, "y", 0)
			toBottom := getBool(params, "to_bottom", false)

			if selector != "" {
				if err := pool.runDefault(chromedp.ScrollIntoView(selector, chromedp.ByQuery)); err != nil {
					return NewToolResult("", fmt.Errorf("browser_scroll: %w", err))
				}
				return NewToolResult(fmt.Sprintf("Scrolled %s into view", selector), nil)
			}

			var script string
			if toBottom {
				script = "window.scrollTo(0, document.body.scrollHeight)"
			} else {
				script = fmt.Sprintf("window.scrollBy(%d, %d)", x, y)
			}
			var ignored any
			if err := pool.runDefault(chromedp.EvaluateAsDevTools(script, &ignored)); err != nil {
				return NewToolResult("", fmt.Errorf("browser_scroll: %w", err))
			}
			if toBottom {
				return NewToolResult("Scrolled to bottom", nil)
			}
			return NewToolResult(fmt.Sprintf("Scrolled window by (%d, %d)", x, y), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── browser_info ─────────────────────────────────────────────────────────────

func newBrowserInfoTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	return NewTool("browser_info",
		"Return the current URL, page title, and a list of all open tabs.",
		schema,
		func(params map[string]any) ToolResult {
			var title, currentURL string
			if err := pool.runDefault(
				chromedp.Title(&title),
				chromedp.Location(&currentURL),
			); err != nil {
				return NewToolResult("", fmt.Errorf("browser_info: %w", err))
			}

			targets, err := pool.listTargets()
			if err != nil {
				return NewToolResult("", fmt.Errorf("browser_info: list targets: %w", err))
			}

			type tabInfo struct {
				ID    string `json:"id"`
				Title string `json:"title"`
				URL   string `json:"url"`
			}
			var tabs []tabInfo
			for _, t := range targets {
				if t.Type == "page" {
					tabs = append(tabs, tabInfo{
						ID:    string(t.TargetID),
						Title: t.Title,
						URL:   t.URL,
					})
				}
			}

			result := map[string]any{
				"current_url":   currentURL,
				"current_title": title,
				"tabs":          tabs,
				"tab_count":     len(tabs),
			}
			b, _ := json.MarshalIndent(result, "", "  ")
			return NewToolResult(string(b), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── browser_switch_tab ────────────────────────────────────────────────────────

func newBrowserSwitchTabTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tab_id": map[string]any{
				"type":        "string",
				"description": "Target ID of the tab to switch to (from browser_info).",
			},
			"title_contains": map[string]any{
				"type":        "string",
				"description": "Switch to the first tab whose title contains this string (case-insensitive).",
			},
		},
	}
	return NewTool("browser_switch_tab", "Switch the active tab by ID or by title substring.", schema,
		func(params map[string]any) ToolResult {
			tabID := getStringDefault(params, "tab_id", "")
			titleMatch := strings.ToLower(getStringDefault(params, "title_contains", ""))

			if tabID == "" && titleMatch == "" {
				return NewToolResult("", fmt.Errorf("browser_switch_tab: provide 'tab_id' or 'title_contains'"))
			}

			if tabID == "" {
				targets, err := pool.listTargets()
				if err != nil {
					return NewToolResult("", fmt.Errorf("browser_switch_tab: %w", err))
				}
				for _, t := range targets {
					if t.Type == "page" && strings.Contains(strings.ToLower(t.Title), titleMatch) {
						tabID = string(t.TargetID)
						break
					}
				}
				if tabID == "" {
					return NewToolResult("", fmt.Errorf("browser_switch_tab: no tab with title containing %q", titleMatch))
				}
			}

			if err := pool.switchTab(cdptarget.ID(tabID)); err != nil {
				return NewToolResult("", err)
			}
			return NewToolResult(fmt.Sprintf("Switched to tab %s", tabID), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── browser_new_tab ───────────────────────────────────────────────────────────

func newBrowserNewTabTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "URL to open in the new tab. Defaults to about:blank.",
			},
		},
	}
	return NewTool("browser_new_tab", "Open a new tab and make it the active tab.", schema,
		func(params map[string]any) ToolResult {
			url := getStringDefault(params, "url", "about:blank")

			// Phase 1: ensure connection, snapshot allocator and generation.
			pool.mu.Lock()
			if err := pool.ensureConnected(); err != nil {
				pool.mu.Unlock()
				return NewToolResult("", err)
			}
			allocCtx := pool.allocCtx
			snapGen := pool.gen
			pool.mu.Unlock()

			// Phase 2: create and navigate the new tab outside the lock so we
			// don't block concurrent tool calls for the entire navigation.
			newTabCtx, newTabCncl := chromedp.NewContext(allocCtx, chromedp.WithLogf(pool.logf))
			var title, finalURL string
			if err := chromedp.Run(newTabCtx,
				chromedp.Navigate(url),
				chromedp.Title(&title),
				chromedp.Location(&finalURL),
			); err != nil {
				newTabCncl()
				return NewToolResult("", fmt.Errorf("browser_new_tab: %w", err))
			}

			// Phase 3: swap in the new tab only if the pool hasn't reconnected
			// since phase 1. A changed generation means close() ran and the
			// allocCtx we used is now stale — installing that tab would break
			// the freshly reconnected pool.
			pool.mu.Lock()
			if pool.gen != snapGen {
				pool.mu.Unlock()
				newTabCncl()
				return NewToolResult("", fmt.Errorf("browser_new_tab: browser reconnected during navigation; try again"))
			}
			oldTabCncl := pool.tabCncl
			pool.tabCtx = newTabCtx
			pool.tabCncl = newTabCncl
			pool.mu.Unlock()

			if oldTabCncl != nil {
				oldTabCncl()
			}

			return NewToolResult(fmt.Sprintf("New tab: %s | Title: %s", finalURL, title), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── browser_close_tab ─────────────────────────────────────────────────────────

func newBrowserCloseTabTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tab_id": map[string]any{
				"type":        "string",
				"description": "Target ID of the tab to close (from browser_info). Defaults to the current active tab.",
			},
		},
	}
	return NewTool("browser_close_tab", "Close a browser tab by ID, or close the current active tab.", schema,
		func(params map[string]any) ToolResult {
			tabID := getStringDefault(params, "tab_id", "")

			// Resolve target ID and close in a single action sequence to avoid
			// a TOCTOU race where the active tab changes between two separate calls.
			var resolvedID cdptarget.ID
			if tabID != "" {
				resolvedID = cdptarget.ID(tabID)
			}
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				if resolvedID == "" {
					// Discover the attached page target.
					infos, err := chromedp.Targets(ctx)
					if err != nil {
						return err
					}
					for _, t := range infos {
						if t.Attached && t.Type == "page" {
							resolvedID = t.TargetID
							break
						}
					}
					if resolvedID == "" {
						return fmt.Errorf("no attached page target found")
					}
				}
				return cdptarget.CloseTarget(resolvedID).Do(ctx)
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_close_tab: %w", err))
			}

			// Reset pool so the next call reconnects to a live tab.
			pool.mu.Lock()
			pool.close()
			pool.mu.Unlock()

			return NewToolResult(fmt.Sprintf("Closed tab %s", tabID), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── browser_back / browser_forward / browser_reload ───────────────────────────

func newBrowserBackTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"wait_for": map[string]any{
				"type":        "string",
				"description": "Optional CSS selector to wait for after navigation.",
			},
		},
	}
	return NewTool("browser_back", "Navigate the active tab back in browser history.", schema,
		func(params map[string]any) ToolResult {
			waitFor := getStringDefault(params, "wait_for", "")
			var title, finalURL string
			actions := []chromedp.Action{
				chromedp.NavigateBack(),
				chromedp.Title(&title),
				chromedp.Location(&finalURL),
			}
			if waitFor != "" {
				actions = append(actions, chromedp.WaitVisible(waitFor, chromedp.ByQuery))
			}
			if err := pool.runDefault(actions...); err != nil {
				if strings.Contains(err.Error(), "beginning of navigation history") {
					return NewToolResult("", fmt.Errorf("browser_back: already at the beginning of history"))
				}
				return NewToolResult("", fmt.Errorf("browser_back: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Back to: %s\nTitle: %s", finalURL, title), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

func newBrowserForwardTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"wait_for": map[string]any{
				"type":        "string",
				"description": "Optional CSS selector to wait for after navigation.",
			},
		},
	}
	return NewTool("browser_forward", "Navigate the active tab forward in browser history.", schema,
		func(params map[string]any) ToolResult {
			waitFor := getStringDefault(params, "wait_for", "")
			var title, finalURL string
			actions := []chromedp.Action{
				chromedp.NavigateForward(),
				chromedp.Title(&title),
				chromedp.Location(&finalURL),
			}
			if waitFor != "" {
				actions = append(actions, chromedp.WaitVisible(waitFor, chromedp.ByQuery))
			}
			if err := pool.runDefault(actions...); err != nil {
				if strings.Contains(err.Error(), "end of navigation history") {
					return NewToolResult("", fmt.Errorf("browser_forward: already at the end of history"))
				}
				return NewToolResult("", fmt.Errorf("browser_forward: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Forward to: %s\nTitle: %s", finalURL, title), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

func newBrowserReloadTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ignore_cache": map[string]any{
				"type":        "boolean",
				"description": "Force reload bypassing cache (hard reload, default false).",
			},
			"wait_for": map[string]any{
				"type":        "string",
				"description": "Optional CSS selector to wait for after reload.",
			},
		},
	}
	return NewTool("browser_reload", "Reload the active tab, optionally bypassing cache.", schema,
		func(params map[string]any) ToolResult {
			ignoreCache := getBool(params, "ignore_cache", false)
			waitFor := getStringDefault(params, "wait_for", "")
			var title, finalURL string
			actions := []chromedp.Action{
				page.Reload().WithIgnoreCache(ignoreCache),
			}
			actions = append(actions,
				chromedp.Title(&title),
				chromedp.Location(&finalURL),
			)
			if waitFor != "" {
				actions = append(actions, chromedp.WaitVisible(waitFor, chromedp.ByQuery))
			}
			if err := pool.runDefault(actions...); err != nil {
				return NewToolResult("", fmt.Errorf("browser_reload: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Reloaded: %s\nTitle: %s", finalURL, title), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}
