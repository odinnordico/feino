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
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	cdpdom "github.com/chromedp/cdproto/dom"
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
//
// Uses 127.0.0.1 explicitly rather than "localhost". Some hardened container
// images and mis-configured /etc/hosts entries make "localhost" resolve to
// something other than the loopback address (or fail to resolve at all),
// which silently turns the probe into "no browser found" and forces an
// unnecessary fresh launch.
func (pool *browserPool) probeWebSocketURL() string {
	url := fmt.Sprintf("http://127.0.0.1:%d/json/version", pool.debugPort)
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

// namedKey describes a non-printable key by the four fields the CDP
// Input.dispatchKeyEvent command expects. We carry all four because
// real keyboard event listeners commonly inspect more than one
// (e.g. checking event.code for layout independence and event.key for
// the printable representation).
type namedKey struct {
	Key                   string
	Code                  string
	WindowsVirtualKeyCode int64
	Text                  string
}

// namedKeys is the registry of named keys browser_key understands. Lookup
// is case- and whitespace-insensitive; common aliases ("up" → ArrowUp,
// "esc" → Escape, "return" → Enter) are accepted.
//
// Why this exists: chromedp.KeyEvent / SendKeys send characters, which
// works for printable text and the few non-printables that have ASCII
// representations (\r, \t, \b, \x1b). Arrow keys, Home, End, PageUp,
// PageDown have no such character — sending "" is a silent no-op.
// Dispatching DispatchKeyEvent with proper Key/Code/VKey makes JS
// listeners and browser default actions (cursor movement, scroll)
// fire correctly.
var namedKeys = map[string]namedKey{
	"enter":      {Key: "Enter", Code: "Enter", WindowsVirtualKeyCode: 13, Text: "\r"},
	"return":     {Key: "Enter", Code: "Enter", WindowsVirtualKeyCode: 13, Text: "\r"},
	"escape":     {Key: "Escape", Code: "Escape", WindowsVirtualKeyCode: 27},
	"esc":        {Key: "Escape", Code: "Escape", WindowsVirtualKeyCode: 27},
	"tab":        {Key: "Tab", Code: "Tab", WindowsVirtualKeyCode: 9, Text: "\t"},
	"backspace":  {Key: "Backspace", Code: "Backspace", WindowsVirtualKeyCode: 8},
	"delete":     {Key: "Delete", Code: "Delete", WindowsVirtualKeyCode: 46},
	"space":      {Key: " ", Code: "Space", WindowsVirtualKeyCode: 32, Text: " "},
	"arrowup":    {Key: "ArrowUp", Code: "ArrowUp", WindowsVirtualKeyCode: 38},
	"up":         {Key: "ArrowUp", Code: "ArrowUp", WindowsVirtualKeyCode: 38},
	"arrowdown":  {Key: "ArrowDown", Code: "ArrowDown", WindowsVirtualKeyCode: 40},
	"down":       {Key: "ArrowDown", Code: "ArrowDown", WindowsVirtualKeyCode: 40},
	"arrowleft":  {Key: "ArrowLeft", Code: "ArrowLeft", WindowsVirtualKeyCode: 37},
	"left":       {Key: "ArrowLeft", Code: "ArrowLeft", WindowsVirtualKeyCode: 37},
	"arrowright": {Key: "ArrowRight", Code: "ArrowRight", WindowsVirtualKeyCode: 39},
	"right":      {Key: "ArrowRight", Code: "ArrowRight", WindowsVirtualKeyCode: 39},
	"home":       {Key: "Home", Code: "Home", WindowsVirtualKeyCode: 36},
	"end":        {Key: "End", Code: "End", WindowsVirtualKeyCode: 35},
	"pageup":     {Key: "PageUp", Code: "PageUp", WindowsVirtualKeyCode: 33},
	"pagedown":   {Key: "PageDown", Code: "PageDown", WindowsVirtualKeyCode: 34},
}

// lookupNamedKey performs a case- and whitespace-insensitive lookup against
// namedKeys.
func lookupNamedKey(name string) (namedKey, bool) {
	nk, ok := namedKeys[strings.ToLower(strings.TrimSpace(name))]
	return nk, ok
}

// dispatchNamedKeyAction returns a chromedp.Action that fires a complete
// keyboard sequence (keyDown then keyUp) for the given named key. Both
// events carry the full Key/Code/VKey metadata so JS listeners that
// inspect event.key, event.code, or event.keyCode all see consistent
// values.
func dispatchNamedKeyAction(nk namedKey) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		down := cdpinput.DispatchKeyEvent(cdpinput.KeyDown).
			WithKey(nk.Key).
			WithCode(nk.Code).
			WithWindowsVirtualKeyCode(nk.WindowsVirtualKeyCode)
		if nk.Text != "" {
			down = down.WithText(nk.Text)
		}
		if err := down.Do(ctx); err != nil {
			return err
		}
		return cdpinput.DispatchKeyEvent(cdpinput.KeyUp).
			WithKey(nk.Key).
			WithCode(nk.Code).
			WithWindowsVirtualKeyCode(nk.WindowsVirtualKeyCode).
			Do(ctx)
	})
}

// ── Security helpers ──────────────────────────────────────────────────────────

// allowedNavigationSchemes restricts which URL schemes browser_navigate and
// browser_new_tab may load. Anything outside this set is rejected before it
// reaches the CDP layer.
//
// Why these three:
//   - http/https — the only schemes a browsing-agent realistically needs.
//   - about      — about:blank is the documented default for browser_new_tab,
//     and other about:* targets the Chromium block list will reject anyway.
//
// Why everything else is rejected:
//   - file://              — local-disk read; combined with browser_get_text
//     it becomes an arbitrary-file-read primitive.
//   - chrome:// / chrome-extension:// — exposes browser internals and any
//     extension storage attached to the user profile.
//   - javascript:          — script injection masquerading as navigation.
//   - data:                — can carry arbitrary HTML/JS payloads inline.
//   - custom OS protocols  — vscode:, mailto:, ms-cmd:, etc. punch out of
//     the browser sandbox into the host OS.
var allowedNavigationSchemes = map[string]bool{
	"http":  true,
	"https": true,
	"about": true,
}

// validateNavigationURL ensures rawURL is a syntactically valid URL whose
// scheme is in [allowedNavigationSchemes]. It returns nil on success or a
// descriptive error suitable for surfacing back to the LLM.
func validateNavigationURL(rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return fmt.Errorf("url is empty")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url %q: %w", rawURL, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme == "" {
		return fmt.Errorf("url %q has no scheme; include http:// or https://", rawURL)
	}
	if !allowedNavigationSchemes[scheme] {
		return fmt.Errorf("scheme %q is not permitted; allowed: http, https, about", u.Scheme)
	}
	return nil
}

// confineToTempDir validates a caller-supplied output path for any browser
// tool that writes to disk (screenshots, downloads, ...). The empty string
// signals "let the tool pick a temp file" and is returned as-is. Otherwise
// the path must be absolute and resolve (after [filepath.Clean]) to a
// location inside [os.TempDir].
//
// We deliberately do NOT allow writes elsewhere on the filesystem: the LLM
// supplies this parameter and a permissive allowlist is exactly the
// arbitrary-file-write vector we are closing. Callers that want a
// persistent location can read the returned path from the tool result and
// move the file themselves.
func confineToTempDir(rawPath string) (string, error) {
	if rawPath == "" {
		return "", nil
	}
	if !filepath.IsAbs(rawPath) {
		return "", fmt.Errorf("save_path must be absolute, got %q", rawPath)
	}
	cleaned := filepath.Clean(rawPath)
	tempDir := filepath.Clean(os.TempDir())
	// Match either the temp dir itself (unusual but technically valid) or any
	// descendant. Use the OS path separator so the prefix check is correct on
	// Windows (\) as well as POSIX (/).
	withSep := tempDir + string(os.PathSeparator)
	if cleaned != tempDir && !strings.HasPrefix(cleaned, withSep) {
		return "", fmt.Errorf("save_path must be inside %s, got %q", tempDir, cleaned)
	}
	return cleaned, nil
}

// validateCookieDomain checks that a caller-supplied cookie domain is a
// domain-suffix of the active page's host, in the sense of RFC 6265 §5.1.3.
// The empty domain is accepted (the browser will infer the active page's
// origin); anything else must satisfy:
//
//	host == domain  OR  host endswith "." + domain
//
// Leading "." on the cookie domain is stripped before comparison since
// `domain=.example.com` and `domain=example.com` are equivalent for matching.
func validateCookieDomain(domain, currentURL string) error {
	if domain == "" {
		return nil
	}
	u, err := url.Parse(currentURL)
	if err != nil {
		return fmt.Errorf("could not parse current page url %q: %w", currentURL, err)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("active tab has no host (current url: %q); cookies cannot be set on chrome:// or about:blank pages", currentURL)
	}
	cookieDomain := strings.ToLower(strings.TrimPrefix(domain, "."))
	if cookieDomain == "" {
		return fmt.Errorf("cookie domain %q is empty after stripping leading dot", domain)
	}
	if host == cookieDomain || strings.HasSuffix(host, "."+cookieDomain) {
		return nil
	}
	return fmt.Errorf("cookie domain %q does not match active page host %q; cookies may only be set on the current origin or one of its parent domains", domain, host)
}

// ── Frame resolution ──────────────────────────────────────────────────────────

// resolveFrame returns chromedp query options that scope subsequent
// selector-based DOM queries into the iframe identified by frameSelector.
// An empty frameSelector returns the top-frame option set
// (chromedp.ByQuery only).
//
// For cross-origin iframes — where the parent document cannot read the
// iframe's contentDocument — this returns a clear, actionable error rather
// than failing later with an opaque CDP message. Cross-origin iframe
// interaction would require attaching to a separate CDP target; that is
// not yet supported. As an alternative, callers can navigate directly to
// the iframe's URL.
//
// Must be invoked inside a chromedp ActionFunc because it issues CDP
// queries against the live DOM.
func resolveFrame(ctx context.Context, frameSelector string) ([]chromedp.QueryOption, error) {
	if frameSelector == "" {
		return []chromedp.QueryOption{chromedp.ByQuery}, nil
	}

	var nodeIDs []cdp.NodeID
	if err := chromedp.NodeIDs(frameSelector, &nodeIDs, chromedp.ByQuery).Do(ctx); err != nil {
		return nil, fmt.Errorf("find frame %q: %w", frameSelector, err)
	}
	if len(nodeIDs) == 0 {
		return nil, fmt.Errorf("no element matches frame selector %q", frameSelector)
	}

	node, err := cdpdom.DescribeNode().WithNodeID(nodeIDs[0]).WithPierce(true).Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("describe frame %q: %w", frameSelector, err)
	}
	tag := strings.ToUpper(node.NodeName)
	if tag != "IFRAME" && tag != "FRAME" {
		return nil, fmt.Errorf("frame selector %q matched <%s>, not an iframe/frame element", frameSelector, strings.ToLower(tag))
	}
	if node.ContentDocument == nil {
		return nil, fmt.Errorf("frame %q is cross-origin or not yet loaded; only same-origin iframes are supported. Navigate directly to the iframe's URL to interact with cross-origin content", frameSelector)
	}

	return []chromedp.QueryOption{chromedp.ByQuery, chromedp.FromNode(node.ContentDocument)}, nil
}

// frameSchemaProperty is the schema fragment for the `frame` parameter that
// every selector-using tool accepts. Defining it once here keeps the schema
// description consistent across the suite.
func frameSchemaProperty() map[string]any {
	return map[string]any{
		"type":        "string",
		"description": "Optional CSS selector for an iframe element to scope this query into. When set, `selector` runs against the iframe's content document instead of the top frame. Same-origin iframes only — cross-origin frames return an error. Default: top frame.",
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
		newBrowserUploadFileTool(pool, logger),
		newBrowserDownloadFileTool(pool, logger),
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
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "Full URL to navigate to (e.g. https://example.com). Only http, https, and about: schemes are accepted; file://, chrome://, javascript:, data:, and custom OS protocols are rejected for security.",
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
	return NewTool("browser_navigate", "Navigate the active browser tab to an http(s) or about: URL.", schema,
		func(params map[string]any) ToolResult {
			rawURL, ok := getString(params, "url")
			if !ok {
				return NewToolResult("", fmt.Errorf("browser_navigate: 'url' is required"))
			}
			if err := validateNavigationURL(rawURL); err != nil {
				return NewToolResult("", fmt.Errorf("browser_navigate: %w", err))
			}
			waitFor := getStringDefault(params, "wait_for", "")
			timeoutMs := getInt(params, "timeout_ms", int(browserOpTimeout/time.Millisecond))
			timeout := time.Duration(timeoutMs) * time.Millisecond

			var title, finalURL string
			actions := []chromedp.Action{
				chromedp.Navigate(rawURL),
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
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element to click.",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Click the first element whose visible text contains this string. Used when 'selector' is not provided. Note: text-based matching uses XPath dom.PerformSearch which is document-global; text + frame is not supported, use a CSS selector instead.",
			},
			"wait_visible": map[string]any{
				"type":        "boolean",
				"description": "Wait for the element to be visible before clicking (default true).",
			},
			"frame": frameSchemaProperty(),
		},
	}
	return NewTool("browser_click", "Click an element in the active browser tab.", schema,
		func(params map[string]any) ToolResult {
			selector := getStringDefault(params, "selector", "")
			text := getStringDefault(params, "text", "")
			frame := getStringDefault(params, "frame", "")
			waitVisible := getBool(params, "wait_visible", true)

			if selector == "" && text == "" {
				return NewToolResult("", fmt.Errorf("browser_click: provide 'selector' or 'text'"))
			}
			// Text-based clicks compile to XPath, which the CDP backend evaluates
			// via dom.PerformSearch — a document-global operation that doesn't
			// take a node scope. Reject the combination explicitly so the LLM
			// gets a clear pointer to the workaround.
			if text != "" && frame != "" {
				return NewToolResult("", fmt.Errorf("browser_click: text-based matching cannot be scoped to a frame (XPath search is document-global). Use a CSS `selector` with `frame` instead"))
			}

			// Build a safe XPath when matching by visible text.
			//
			// The expression has three intentional clauses:
			//   1. not(self::script|style|noscript) — never click into
			//      non-rendered elements whose text content can incidentally
			//      contain the search string.
			//   2. contains(normalize-space(.), …) — `normalize-space(.)`
			//      flattens descendant text and collapses whitespace, so
			//      `<a><span>Login</span></a>` matches a search for "Login";
			//      the previous `text()` form missed nested-text elements.
			//   3. not(.//*[contains(normalize-space(.), …)]) — pick the
			//      deepest (innermost) element that satisfies the match,
			//      not whatever ancestor wraps it. Without this, clicking
			//      "Login" in a page with <body><a>Login</a></body> targets
			//      <body> first, which is rarely what the agent wants.
			byXPath := false
			if selector == "" {
				escaped := xpathEscapeText(text)
				selector = fmt.Sprintf(
					"//*[not(self::script) and not(self::style) and not(self::noscript) "+
						"and contains(normalize-space(.),%s) "+
						"and not(.//*[contains(normalize-space(.),%s)])]",
					escaped, escaped,
				)
				byXPath = true
			}

			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				opts, err := resolveFrame(ctx, frame)
				if err != nil {
					return err
				}
				if byXPath {
					// XPath path is document-global; replace ByQuery with
					// BySearch and we already rejected the frame combination.
					opts = []chromedp.QueryOption{chromedp.BySearch}
				}
				if waitVisible {
					if err := chromedp.WaitVisible(selector, opts...).Do(ctx); err != nil {
						return err
					}
				}
				return chromedp.Click(selector, opts...).Do(ctx)
			})); err != nil {
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
		"type":                 "object",
		"additionalProperties": false,
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
			"frame": frameSchemaProperty(),
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
			frame := getStringDefault(params, "frame", "")
			clearFirst := getBool(params, "clear_first", true)

			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				opts, err := resolveFrame(ctx, frame)
				if err != nil {
					return err
				}
				if err := chromedp.WaitVisible(selector, opts...).Do(ctx); err != nil {
					return err
				}
				if clearFirst {
					if err := chromedp.Clear(selector, opts...).Do(ctx); err != nil {
						return err
					}
				}
				return chromedp.SendKeys(selector, text, opts...).Do(ctx)
			})); err != nil {
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
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the input or textarea.",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "Value to set (replaces current content; does not fire key events).",
			},
			"frame": frameSchemaProperty(),
		},
		"required": []string{"selector", "value"},
	}
	return NewTool("browser_fill",
		"Set the value of an input field directly (faster than browser_type; does not fire key events).",
		schema,
		func(params map[string]any) ToolResult {
			selector, _ := getString(params, "selector")
			value, _ := getString(params, "value")
			frame := getStringDefault(params, "frame", "")
			if selector == "" {
				return NewToolResult("", fmt.Errorf("browser_fill: 'selector' is required"))
			}
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				opts, err := resolveFrame(ctx, frame)
				if err != nil {
					return err
				}
				if err := chromedp.WaitVisible(selector, opts...).Do(ctx); err != nil {
					return err
				}
				return chromedp.SetValue(selector, value, opts...).Do(ctx)
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_fill: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Set value of %s", selector), nil)
		},
		WithPermissionLevel(PermLevelWrite),
		WithLogger(logger),
	)
}

// ── browser_upload_file ───────────────────────────────────────────────────────

func newBrowserUploadFileTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector for the <input type='file'> element. Hidden inputs are supported (file inputs are commonly hidden behind a styled <label>).",
			},
			"files": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"minItems":    1,
				"description": "Absolute paths to the files to upload. Each must exist and be a regular file (not a directory). Pass multiple paths only when the input has the `multiple` attribute.",
			},
			"frame": frameSchemaProperty(),
		},
		"required": []string{"selector", "files"},
	}
	return NewTool("browser_upload_file",
		"Set the files of an <input type='file'> element. Visibility is not required — file inputs are often hidden behind a styled label that triggers them on click.",
		schema,
		func(params map[string]any) ToolResult {
			selector, ok := getString(params, "selector")
			if !ok || selector == "" {
				return NewToolResult("", fmt.Errorf("browser_upload_file: 'selector' is required"))
			}
			frame := getStringDefault(params, "frame", "")

			files, err := stringSliceParam(params, "files")
			if err != nil {
				return NewToolResult("", fmt.Errorf("browser_upload_file: %w", err))
			}
			if len(files) == 0 {
				return NewToolResult("", fmt.Errorf("browser_upload_file: 'files' must not be empty"))
			}

			// Pre-validate every path before opening the CDP round-trip so a
			// typo or unreadable file fails immediately with a clear message
			// rather than mid-CDP with an opaque protocol error.
			for i, p := range files {
				if !filepath.IsAbs(p) {
					return NewToolResult("", fmt.Errorf("browser_upload_file: files[%d] must be an absolute path, got %q", i, p))
				}
				info, err := os.Stat(p)
				if err != nil {
					return NewToolResult("", fmt.Errorf("browser_upload_file: files[%d] cannot be read: %w", i, err))
				}
				if info.IsDir() {
					return NewToolResult("", fmt.Errorf("browser_upload_file: files[%d] is a directory, expected a file: %q", i, p))
				}
			}

			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				opts, err := resolveFrame(ctx, frame)
				if err != nil {
					return err
				}
				// Resolve the selector to a node id. Use the resolved opts so
				// a frame-scoped selector queries inside the iframe.
				var nodeIDs []cdp.NodeID
				if err := chromedp.NodeIDs(selector, &nodeIDs, opts...).Do(ctx); err != nil {
					return fmt.Errorf("locate element %q: %w", selector, err)
				}
				if len(nodeIDs) == 0 {
					return fmt.Errorf("no element matches selector %q", selector)
				}
				return cdpdom.SetFileInputFiles(files).WithNodeID(nodeIDs[0]).Do(ctx)
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_upload_file: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Uploaded %d file(s) to %s", len(files), selector), nil)
		},
		WithPermissionLevel(PermLevelWrite),
		WithLogger(logger),
	)
}

// stringSliceParam pulls a []string out of an arbitrary tool param map. The
// JSON decoder upstream always produces []any for arrays even when items are
// strings, so we unwrap that case explicitly. Everything else is rejected
// with a typed error so the LLM gets an immediately actionable message.
func stringSliceParam(params map[string]any, key string) ([]string, error) {
	raw, ok := params[key]
	if !ok {
		return nil, fmt.Errorf("'%s' is required", key)
	}
	switch v := raw.(type) {
	case []string:
		return v, nil
	case []any:
		out := make([]string, 0, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("'%s'[%d] is %T, want string", key, i, item)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("'%s' must be an array of strings, got %T", key, raw)
	}
}

// ── browser_download_file ─────────────────────────────────────────────────────

func newBrowserDownloadFileTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element whose click triggers the download (e.g. an <a> with the download attribute, or a button that issues an export).",
			},
			"save_path": map[string]any{
				"type":        "string",
				"description": "Optional absolute path to save the downloaded file. For security this must be inside the OS temp directory; paths elsewhere are rejected. Default: a fresh staging file under the OS temp directory using the server-suggested filename.",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"minimum":     1000,
				"description": "Maximum time to wait for the download to complete (default 60000).",
			},
			"frame": frameSchemaProperty(),
		},
		"required": []string{"selector"},
	}
	return NewTool("browser_download_file",
		"Click an element that triggers a file download and wait for the download to finish. Returns the saved file path, the server-suggested filename, and the size in bytes.",
		schema,
		func(params map[string]any) ToolResult {
			selector, ok := getString(params, "selector")
			if !ok || selector == "" {
				return NewToolResult("", fmt.Errorf("browser_download_file: 'selector' is required"))
			}
			rawSavePath := getStringDefault(params, "save_path", "")
			frame := getStringDefault(params, "frame", "")
			timeoutMs := getInt(params, "timeout_ms", 60000)
			if timeoutMs < 1000 {
				return NewToolResult("", fmt.Errorf("browser_download_file: timeout_ms must be at least 1000, got %d", timeoutMs))
			}

			savePath, err := confineToTempDir(rawSavePath)
			if err != nil {
				return NewToolResult("", fmt.Errorf("browser_download_file: %w", err))
			}

			// Each download gets its own staging directory under temp. Using
			// `AllowAndName` makes Chrome write the file as <staging>/<GUID>;
			// we rename it to the server-suggested filename after completion
			// (with sanitisation applied to defeat path-traversal attempts in
			// the suggested name).
			downloadDir, err := os.MkdirTemp("", "feino-download-*")
			if err != nil {
				return NewToolResult("", fmt.Errorf("browser_download_file: create staging dir: %w", err))
			}
			cleanupStaging := true
			defer func() {
				if cleanupStaging {
					_ = os.RemoveAll(downloadDir)
				}
			}()

			type downloadOutcome struct {
				guid     string
				filename string
				size     int64
				err      error
			}
			var outcome downloadOutcome

			timeout := time.Duration(timeoutMs) * time.Millisecond
			runErr := pool.run(timeout, chromedp.ActionFunc(func(ctx context.Context) error {
				// Order matters here:
				//   1. Register the listener BEFORE we tell Chrome to allow
				//      downloads, so we never miss a willBegin event.
				//   2. SetDownloadBehavior with AllowAndName + EventsEnabled.
				//   3. Click the selector, which (eventually) triggers a
				//      Browser.downloadWillBegin event followed by progress
				//      events ending in completed / canceled.
				done := make(chan downloadOutcome, 1)
				listenerCtx, listenerCancel := context.WithCancel(ctx)
				defer listenerCancel()

				var pending downloadOutcome
				chromedp.ListenBrowser(listenerCtx, func(ev any) {
					switch e := ev.(type) {
					case *browser.EventDownloadWillBegin:
						pending.guid = e.GUID
						pending.filename = sanitizeFilename(e.SuggestedFilename)
					case *browser.EventDownloadProgress:
						if pending.guid == "" || e.GUID != pending.guid {
							return
						}
						switch e.State {
						case browser.DownloadProgressStateCompleted:
							pending.size = int64(e.TotalBytes)
							select {
							case done <- pending:
							default:
							}
						case browser.DownloadProgressStateCanceled:
							select {
							case done <- downloadOutcome{err: fmt.Errorf("download was canceled by the page")}:
							default:
							}
						}
					}
				})

				if err := browser.SetDownloadBehavior(browser.SetDownloadBehaviorBehaviorAllowAndName).
					WithDownloadPath(downloadDir).
					WithEventsEnabled(true).
					Do(ctx); err != nil {
					return fmt.Errorf("set download behavior: %w", err)
				}

				clickOpts, err := resolveFrame(ctx, frame)
				if err != nil {
					return err
				}
				if err := chromedp.Click(selector, clickOpts...).Do(ctx); err != nil {
					return fmt.Errorf("click %q: %w", selector, err)
				}

				select {
				case r := <-done:
					if r.err != nil {
						return r.err
					}
					outcome = r
					return nil
				case <-ctx.Done():
					return fmt.Errorf("timed out after %s waiting for the download to start/complete", timeout)
				}
			}))

			if runErr != nil {
				return NewToolResult("", fmt.Errorf("browser_download_file: %w", runErr))
			}

			// Move the staged GUID-named file to its final destination. If the
			// caller didn't specify save_path we keep it in the staging dir
			// but renamed to the suggested filename — so we DON'T clean up
			// the staging dir in that case.
			srcPath := filepath.Join(downloadDir, outcome.guid)
			finalPath := savePath
			if finalPath == "" {
				name := outcome.filename
				if name == "" {
					name = "download.bin"
				}
				finalPath = filepath.Join(downloadDir, name)
				cleanupStaging = false // file ends up inside the staging dir
			}
			if err := os.Rename(srcPath, finalPath); err != nil {
				return NewToolResult("", fmt.Errorf("browser_download_file: rename: %w", err))
			}

			return NewToolResult(
				fmt.Sprintf("Downloaded %q to %s (%d bytes)", outcome.filename, finalPath, outcome.size),
				nil,
			)
		},
		WithPermissionLevel(PermLevelWrite),
		WithLogger(logger),
	)
}

// sanitizeFilename takes a server-supplied filename and returns a safe local
// filename: path components are stripped (so "../etc/passwd" becomes
// "passwd"), leading dots are removed (no "..foo" or hidden ".foo"), and
// characters that are reserved on common filesystems are dropped. Returns
// the empty string only if the entire input was unsafe.
func sanitizeFilename(name string) string {
	// Chrome may report Windows-style paths even on Unix runners; strip any
	// segment up to the last separator of either flavour.
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimLeft(name, ".")
	name = strings.Map(func(r rune) rune {
		switch r {
		case 0, ':', '<', '>', '"', '|', '?', '*':
			return -1
		default:
			return r
		}
	}, name)
	return name
}

// ── browser_select ────────────────────────────────────────────────────────────

func newBrowserSelectTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
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
			"frame": frameSchemaProperty(),
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
			frame := getStringDefault(params, "frame", "")

			if value == "" && text == "" {
				return NewToolResult("", fmt.Errorf("browser_select: provide 'value' or 'text'"))
			}

			var result any
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				// resolveFrame validates the iframe up-front (existence,
				// element type, same-origin) so the JS we send below can
				// safely assume contentDocument is reachable. The returned
				// opts also scope WaitVisible / SetValue into the iframe.
				opts, err := resolveFrame(ctx, frame)
				if err != nil {
					return err
				}
				if err := chromedp.WaitVisible(selector, opts...).Do(ctx); err != nil {
					return err
				}

				if value != "" {
					// Fast path: assign via CDP, then dispatch events from JS
					// so listeners on the page (or in the iframe) fire.
					if err := chromedp.SetValue(selector, value, opts...).Do(ctx); err != nil {
						return err
					}
					script := fmt.Sprintf(
						`(function(){%s var el=doc.querySelector(%q);`+
							`if(!el)return"error: element not found";`+
							`el.dispatchEvent(new Event('change',{bubbles:true}));`+
							`el.dispatchEvent(new Event('input',{bubbles:true}));`+
							`return"value: "+el.value})()`,
						selectDocScopeJS(frame), selector,
					)
					return chromedp.EvaluateAsDevTools(script, &result).Do(ctx)
				}

				// Text-based selection via JS to avoid XPath complexity.
				script := fmt.Sprintf(`(function(){
					%s
					var sel = doc.querySelector(%q);
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
				})()`, selectDocScopeJS(frame), selector, text)
				return chromedp.EvaluateAsDevTools(script, &result).Do(ctx)
			})); err != nil {
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

// selectDocScopeJS returns a JS statement that defines `doc` — either the
// top-frame document, or the iframe's contentDocument when frameSelector is
// non-empty. The statement is meant to be inlined at the start of an IIFE
// body so the early-return on a missing iframe propagates as the IIFE's
// return value.
//
// The Go-side resolveFrame helper has already validated that the frame
// exists and is same-origin before this script reaches the page, but we
// keep the in-JS guard as defence in depth — between Go validation and the
// JS fetching the contentDocument, the iframe could in principle be
// detached from the DOM (or navigate to a cross-origin URL) by other page
// scripts.
func selectDocScopeJS(frameSelector string) string {
	if frameSelector == "" {
		return "var doc = document;"
	}
	return fmt.Sprintf(
		`var __frame = document.querySelector(%q);`+
			`if (!__frame || !__frame.contentDocument) return "error: frame became inaccessible: " + %q;`+
			`var doc = __frame.contentDocument;`,
		frameSelector, frameSelector,
	)
}

// ── browser_key ───────────────────────────────────────────────────────────────

func newBrowserKeyTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
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
			"frame": frameSchemaProperty(),
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
			frame := getStringDefault(params, "frame", "")

			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				opts, err := resolveFrame(ctx, frame)
				if err != nil {
					return err
				}
				if selector != "" {
					if err := chromedp.WaitVisible(selector, opts...).Do(ctx); err != nil {
						return err
					}
					if err := chromedp.Focus(selector, opts...).Do(ctx); err != nil {
						return err
					}
				}
				// Named keys (Enter, ArrowUp, Home, …) are dispatched via the
				// CDP Input.dispatchKeyEvent command with full Key/Code/VKey
				// metadata — chromedp.KeyEvent of an empty rune (the previous
				// behaviour for arrow keys) is a silent no-op. Single
				// characters fall through to the regular SendKeys/KeyEvent
				// path. Once the iframe element has focus, page-level key
				// dispatch reaches it correctly.
				if nk, named := lookupNamedKey(key); named {
					return dispatchNamedKeyAction(nk).Do(ctx)
				}
				if selector != "" {
					return chromedp.SendKeys(selector, key, opts...).Do(ctx)
				}
				return chromedp.KeyEvent(key).Do(ctx)
			})); err != nil {
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
		"type":                 "object",
		"additionalProperties": false,
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
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector to screenshot. Omit for a full-page screenshot (which captures beyond the visible viewport).",
			},
			"save_path": map[string]any{
				"type":        "string",
				"description": "Absolute path where the image will be saved. For security this must be inside the OS temp directory; paths elsewhere are rejected. Omit to write to a fresh temp file (recommended). The file extension should match `format` but is not enforced.",
			},
			"format": map[string]any{
				"type":        "string",
				"enum":        []string{"png", "jpeg"},
				"description": "Image format. PNG (default) is lossless. JPEG is smaller but lossy and pairs with `quality`.",
			},
			"quality": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     100,
				"description": "JPEG quality 1–100 (default 90). Ignored when format is png — PNG has no lossy-quality knob.",
			},
		},
	}
	return NewTool("browser_screenshot",
		"Take a screenshot of the active tab or a specific element. Output is always written under the OS temp directory. Supports PNG (default, lossless) and JPEG (with adjustable quality).",
		schema,
		func(params map[string]any) ToolResult {
			selector := getStringDefault(params, "selector", "")
			rawSavePath := getStringDefault(params, "save_path", "")
			format := strings.ToLower(getStringDefault(params, "format", "png"))
			quality := getInt(params, "quality", 90)

			if format != "png" && format != "jpeg" {
				return NewToolResult("", fmt.Errorf("browser_screenshot: format must be 'png' or 'jpeg', got %q", format))
			}
			if quality < 1 || quality > 100 {
				return NewToolResult("", fmt.Errorf("browser_screenshot: quality must be between 1 and 100, got %d", quality))
			}

			savePath, err := confineToTempDir(rawSavePath)
			if err != nil {
				return NewToolResult("", fmt.Errorf("browser_screenshot: %w", err))
			}
			if savePath == "" {
				ext := ".png"
				if format == "jpeg" {
					ext = ".jpg"
				}
				f, err := os.CreateTemp("", "feino-browser-*"+ext)
				if err != nil {
					return NewToolResult("", fmt.Errorf("browser_screenshot: create temp file: %w", err))
				}
				f.Close()
				savePath = f.Name()
			}

			var buf []byte
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				var captErr error
				buf, captErr = captureScreenshot(ctx, selector, format, int64(quality))
				return captErr
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_screenshot: %w", err))
			}
			// Mode 0600: screenshots can carry sensitive page content (auth UIs,
			// PII), so don't make them world-readable on multi-user systems.
			if err := os.WriteFile(savePath, buf, 0o600); err != nil {
				return NewToolResult("", fmt.Errorf("browser_screenshot: write: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Screenshot saved: %s (%d bytes, %s)", savePath, len(buf), format), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// captureScreenshot runs inside the pool's CDP action context and returns
// the encoded image bytes. Two dimensions of behaviour:
//
//   - selector empty   → full-page capture (uses CDP's captureBeyondViewport
//     so content below the fold is included without a viewport-resize dance).
//   - selector present → element capture: scroll into view, read the element's
//     getBoundingClientRect, capture with a clip rectangle of those dimensions.
//
// PNG is the default; JPEG is requested via WithFormat+WithQuality. The
// previous implementation passed `quality` to chromedp.FullScreenshot which
// ignores it for PNG output — this fixes the schema lying about a knob that
// did nothing.
func captureScreenshot(ctx context.Context, selector, format string, quality int64) ([]byte, error) {
	capFmt := page.CaptureScreenshotFormatPng
	if format == "jpeg" {
		capFmt = page.CaptureScreenshotFormatJpeg
	}
	req := page.CaptureScreenshot().
		WithFormat(capFmt).
		WithCaptureBeyondViewport(true)
	if format == "jpeg" {
		req = req.WithQuality(quality)
	}

	if selector != "" {
		if err := chromedp.WaitReady(selector, chromedp.ByQuery).Do(ctx); err != nil {
			return nil, fmt.Errorf("element not ready: %w", err)
		}
		if err := chromedp.ScrollIntoView(selector, chromedp.ByQuery).Do(ctx); err != nil {
			return nil, fmt.Errorf("scroll into view: %w", err)
		}
		// Inline the selector via %q so any embedded quotes in the user's
		// input become Go-escaped JS string literals — same pattern as
		// browser_hover. (CSS selectors come from the LLM and need to be
		// safely embedded in JS.)
		getRect := fmt.Sprintf(
			`(function(){var e=document.querySelector(%q);if(!e)return"";var r=e.getBoundingClientRect();return JSON.stringify({x:r.x,y:r.y,width:r.width,height:r.height});})()`,
			selector,
		)
		var rectJSON string
		if err := chromedp.EvaluateAsDevTools(getRect, &rectJSON).Do(ctx); err != nil {
			return nil, fmt.Errorf("get element bounds: %w", err)
		}
		if rectJSON == "" {
			return nil, fmt.Errorf("element not found: %s", selector)
		}
		var rect struct {
			X, Y, Width, Height float64
		}
		if err := json.Unmarshal([]byte(rectJSON), &rect); err != nil {
			return nil, fmt.Errorf("parse rect: %w", err)
		}
		if rect.Width <= 0 || rect.Height <= 0 {
			return nil, fmt.Errorf("element has zero size: %s", selector)
		}
		req = req.WithClip(&page.Viewport{
			X:      rect.X,
			Y:      rect.Y,
			Width:  rect.Width,
			Height: rect.Height,
			Scale:  1,
		})
	}
	return req.Do(ctx)
}

// ── browser_get_text ──────────────────────────────────────────────────────────

func newBrowserGetTextTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector. Defaults to 'body' (full visible page text).",
			},
			"frame": frameSchemaProperty(),
		},
	}
	return NewTool("browser_get_text", "Get the visible text content of a page element.", schema,
		func(params map[string]any) ToolResult {
			selector := getStringDefault(params, "selector", "body")
			frame := getStringDefault(params, "frame", "")
			var text string
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				opts, err := resolveFrame(ctx, frame)
				if err != nil {
					return err
				}
				if err := chromedp.WaitReady(selector, opts...).Do(ctx); err != nil {
					return err
				}
				return chromedp.Text(selector, &text, opts...).Do(ctx)
			})); err != nil {
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
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector. Defaults to the full document.",
			},
			"frame": frameSchemaProperty(),
		},
	}
	return NewTool("browser_get_html", "Get the HTML source of a page element or the full document.", schema,
		func(params map[string]any) ToolResult {
			selector := getStringDefault(params, "selector", "html")
			frame := getStringDefault(params, "frame", "")
			var html string
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				opts, err := resolveFrame(ctx, frame)
				if err != nil {
					return err
				}
				if err := chromedp.WaitReady(selector, opts...).Do(ctx); err != nil {
					return err
				}
				return chromedp.OuterHTML(selector, &html, opts...).Do(ctx)
			})); err != nil {
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
		"type":                 "object",
		"additionalProperties": false,
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
		"type":                 "object",
		"additionalProperties": false,
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
		"type":                 "object",
		"additionalProperties": false,
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
				"description": "Cookie domain (e.g. .example.com). Must be a domain-suffix of the active tab's host — you cannot set cookies on origins other than the page you're currently on. Defaults to the current page's domain.",
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
		"Inject or overwrite a browser cookie on the active tab's origin. The cookie domain must match the current page's host; cross-origin cookie injection is blocked.",
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

			// Domain validation needs the active tab's URL, which only the page
			// itself can give us. Resolve it inside the same CDP action so the
			// validation and the SetCookie write race against the same tab —
			// avoids a TOCTOU where the tab navigates between two pool calls.
			var resolvedDomain string
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				var currentURL string
				if err := chromedp.Location(&currentURL).Do(ctx); err != nil {
					return fmt.Errorf("read current url: %w", err)
				}
				if err := validateCookieDomain(domain, currentURL); err != nil {
					return err
				}
				resolvedDomain = domain
				if resolvedDomain == "" {
					if u, err := url.Parse(currentURL); err == nil {
						resolvedDomain = u.Hostname()
					}
				}

				expr := network.SetCookie(name, value).WithPath(path).WithSecure(secure).WithHTTPOnly(httpOnly)
				if domain != "" {
					expr = expr.WithDomain(domain)
				}
				return expr.Do(ctx)
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_set_cookies: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Set cookie %q on %s", name, resolvedDomain), nil)
		},
		WithPermissionLevel(PermLevelBash),
		WithLogger(logger),
	)
}

// ── browser_wait ──────────────────────────────────────────────────────────────

func newBrowserWaitTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
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
			"frame": frameSchemaProperty(),
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
			frame := getStringDefault(params, "frame", "")
			timeoutMs := getInt(params, "timeout_ms", 10000)
			timeout := time.Duration(timeoutMs) * time.Millisecond

			if err := pool.run(timeout, chromedp.ActionFunc(func(ctx context.Context) error {
				opts, err := resolveFrame(ctx, frame)
				if err != nil {
					return err
				}
				switch state {
				case "ready":
					return chromedp.WaitReady(selector, opts...).Do(ctx)
				case "not_visible":
					return chromedp.WaitNotVisible(selector, opts...).Do(ctx)
				default:
					return chromedp.WaitVisible(selector, opts...).Do(ctx)
				}
			})); err != nil {
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
		"type":                 "object",
		"additionalProperties": false,
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
			"frame": frameSchemaProperty(),
		},
	}
	return NewTool("browser_scroll", "Scroll the page or scroll an element into view.", schema,
		func(params map[string]any) ToolResult {
			selector := getStringDefault(params, "selector", "")
			frame := getStringDefault(params, "frame", "")
			x := getInt(params, "x", 0)
			y := getInt(params, "y", 0)
			toBottom := getBool(params, "to_bottom", false)

			if selector != "" {
				if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
					opts, err := resolveFrame(ctx, frame)
					if err != nil {
						return err
					}
					return chromedp.ScrollIntoView(selector, opts...).Do(ctx)
				})); err != nil {
					return NewToolResult("", fmt.Errorf("browser_scroll: %w", err))
				}
				return NewToolResult(fmt.Sprintf("Scrolled %s into view", selector), nil)
			}
			// Window-scroll path (no selector) is top-frame only — frame is
			// silently ignored when no selector is given.

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
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]any{},
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
		"type":                 "object",
		"additionalProperties": false,
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
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "URL to open in the new tab. Defaults to about:blank. Only http, https, and about: schemes are accepted; file://, chrome://, javascript:, data:, and custom OS protocols are rejected for security.",
			},
		},
	}
	return NewTool("browser_new_tab", "Open a new tab and make it the active tab.", schema,
		func(params map[string]any) ToolResult {
			tabURL := getStringDefault(params, "url", "about:blank")
			if err := validateNavigationURL(tabURL); err != nil {
				return NewToolResult("", fmt.Errorf("browser_new_tab: %w", err))
			}

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
				chromedp.Navigate(tabURL),
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
		"type":                 "object",
		"additionalProperties": false,
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
		"type":                 "object",
		"additionalProperties": false,
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
		"type":                 "object",
		"additionalProperties": false,
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
		"type":                 "object",
		"additionalProperties": false,
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
