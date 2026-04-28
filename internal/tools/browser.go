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

	cdpaccessibility "github.com/chromedp/cdproto/accessibility"
	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	cdpdom "github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/emulation"
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

	// console buffers console.* messages and uncaught exceptions captured
	// from each tab the pool attaches to. Survives tab switches so the
	// browser_console_log tool can return everything that happened across
	// a multi-step session.
	console *consoleBuffer

	// consoleMu guards consoleCncl. It is separate from mu so that
	// attachConsoleCapture can be called from both lock-held and non-lock-held
	// call sites without risk of deadlock.
	consoleMu   sync.Mutex
	consoleCncl *consoleListenerToken

	// networkBuf captures HTTP requests/responses for browser_network.
	// networkMu / networkCncl follow the same pattern as consoleMu / consoleCncl.
	networkBuf  *networkBuffer
	networkMu   sync.Mutex
	networkCncl *consoleListenerToken
}

// consoleListenerToken wraps a context.CancelFunc so that identity comparisons
// (pointer equality) can distinguish the current token from a stale one.
type consoleListenerToken struct{ cancel context.CancelFunc }

// consoleBufferDefaultMax bounds the per-pool console-message ring buffer.
// 500 entries comfortably covers a debugging session without unbounded
// growth on chatty pages (e.g. apps that log every render).
const consoleBufferDefaultMax = 500

func newBrowserPool(logger *slog.Logger, debugPort int) *browserPool {
	if debugPort <= 0 {
		debugPort = defaultDebugPort
	}
	return &browserPool{
		logger:     logger,
		debugPort:  debugPort,
		console:    newConsoleBuffer(consoleBufferDefaultMax),
		networkBuf: newNetworkBuffer(networkBufferDefaultMax),
	}
}

// ── Console-message buffer ────────────────────────────────────────────────────

// consoleEntry is one captured console.* call or uncaught exception.
type consoleEntry struct {
	Level     string    `json:"level"`
	Text      string    `json:"text"`
	Source    string    `json:"source,omitempty"`
	Line      int       `json:"line,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// consoleBuffer is a bounded ring of console entries with its own mutex so
// the chromedp event-loop goroutine can write to it without blocking on
// the pool's main mutex.
type consoleBuffer struct {
	mu      sync.Mutex
	entries []consoleEntry
	maxSize int
}

func newConsoleBuffer(maxSize int) *consoleBuffer {
	if maxSize <= 0 {
		maxSize = consoleBufferDefaultMax
	}
	return &consoleBuffer{maxSize: maxSize}
}

// add records one entry, dropping the oldest when at capacity.
func (b *consoleBuffer) add(e consoleEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.entries) >= b.maxSize {
		b.entries = b.entries[1:]
	}
	b.entries = append(b.entries, e)
}

// consoleQuery filters [consoleBuffer.read]'s output.
type consoleQuery struct {
	// level is "" or "all" → no level filter; otherwise must match exactly.
	level string
	// substring is matched case-insensitively against entry.Text. Empty = no
	// filter. Caller must lower-case before passing.
	substring string
	// clear empties the buffer (after collecting matches) when true.
	clear bool
}

// read returns a fresh slice of matching entries in chronological order
// (oldest first). When q.clear is true the buffer is emptied even if the
// matched slice excludes some entries — `clear` is "consume everything".
func (b *consoleBuffer) read(q consoleQuery) []consoleEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]consoleEntry, 0, len(b.entries))
	for _, e := range b.entries {
		if q.level != "" && q.level != "all" && e.Level != q.level {
			continue
		}
		if q.substring != "" && !strings.Contains(strings.ToLower(e.Text), q.substring) {
			continue
		}
		out = append(out, e)
	}
	if q.clear {
		b.entries = b.entries[:0]
	}
	return out
}

// attachConsoleCapture enables Runtime events on the given tab context and
// registers a chromedp listener that copies every console.* call and
// uncaught exception into pool.console. Failure to enable the runtime
// domain is logged as a warning rather than fatal — the rest of the pool
// still works, just without console capture for this tab.
//
// Each call cancels the previous listener before registering the new one,
// so at most one active console listener exists at any time.
func (pool *browserPool) attachConsoleCapture(tabCtx context.Context) {
	// Cancel any previously registered listener immediately rather than
	// relying on chromedp's lazy cleanup (which only fires on the next event).
	pool.consoleMu.Lock()
	if pool.consoleCncl != nil {
		pool.consoleCncl.cancel()
	}
	listenerCtx, listenerCncl := context.WithCancel(tabCtx)
	tok := &consoleListenerToken{cancel: listenerCncl}
	pool.consoleCncl = tok
	pool.consoleMu.Unlock()

	enableCtx, enableCancel := context.WithTimeout(tabCtx, browserOpTimeout)
	defer enableCancel()
	if err := chromedp.Run(enableCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdpruntime.Enable().Do(ctx)
	})); err != nil {
		// Roll back: clear consoleCncl only if it still points to our token.
		pool.consoleMu.Lock()
		if pool.consoleCncl == tok {
			pool.consoleCncl = nil
		}
		pool.consoleMu.Unlock()
		listenerCncl()
		pool.logger.Warn("browser: cannot enable runtime events for console capture", "error", err)
		return
	}
	chromedp.ListenTarget(listenerCtx, func(ev any) {
		switch e := ev.(type) {
		case *cdpruntime.EventConsoleAPICalled:
			pool.console.add(consoleEntryFromAPICall(e))
		case *cdpruntime.EventExceptionThrown:
			pool.console.add(consoleEntryFromException(e))
		}
	})
}

// ── Network capture buffer ────────────────────────────────────────────────────

const networkBufferDefaultMax = 500

// networkEntry records one completed (or failed) HTTP request/response pair.
type networkEntry struct {
	RequestID  string    `json:"request_id"`
	URL        string    `json:"url"`
	Method     string    `json:"method"`
	Status     int       `json:"status,omitempty"`
	MimeType   string    `json:"mime_type,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	DurationMs float64   `json:"duration_ms,omitempty"`
	Done       bool      `json:"done"`
	Failed     bool      `json:"failed,omitempty"`
	FailReason string    `json:"fail_reason,omitempty"`
}

// networkBuffer is a bounded ring of completed requests plus a pending map
// for in-flight ones. Both are guarded by a single mutex that is only held
// for pointer swaps and slice appends — never across I/O.
type networkBuffer struct {
	mu        sync.Mutex
	completed []networkEntry
	pending   map[network.RequestID]*networkEntry
	maxSize   int
}

func newNetworkBuffer(maxSize int) *networkBuffer {
	if maxSize <= 0 {
		maxSize = networkBufferDefaultMax
	}
	return &networkBuffer{
		pending: make(map[network.RequestID]*networkEntry),
		maxSize: maxSize,
	}
}

func (b *networkBuffer) onRequest(e *network.EventRequestWillBeSent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.pending[e.RequestID] = &networkEntry{
		RequestID: string(e.RequestID),
		URL:       e.Request.URL,
		Method:    e.Request.Method,
		Timestamp: time.Now(),
	}
}

func (b *networkBuffer) onResponse(e *network.EventResponseReceived) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if entry, ok := b.pending[e.RequestID]; ok {
		entry.Status = int(e.Response.Status)
		entry.MimeType = e.Response.MimeType
	}
}

func (b *networkBuffer) onFinished(e *network.EventLoadingFinished) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if entry, ok := b.pending[e.RequestID]; ok {
		entry.Done = true
		entry.DurationMs = float64(time.Since(entry.Timestamp).Nanoseconds()) / 1e6
		delete(b.pending, e.RequestID)
		b.addLocked(*entry)
	}
}

func (b *networkBuffer) onFailed(e *network.EventLoadingFailed) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if entry, ok := b.pending[e.RequestID]; ok {
		entry.Done = true
		entry.Failed = true
		entry.FailReason = e.ErrorText
		entry.DurationMs = float64(time.Since(entry.Timestamp).Nanoseconds()) / 1e6
		delete(b.pending, e.RequestID)
		b.addLocked(*entry)
	}
}

func (b *networkBuffer) addLocked(e networkEntry) {
	if len(b.completed) >= b.maxSize {
		b.completed = b.completed[1:]
	}
	b.completed = append(b.completed, e)
}

type networkQuery struct {
	urlFilter      string
	method         string
	statusMin      int
	statusMax      int
	limit          int
	includePending bool
	clear          bool
}

func (b *networkBuffer) read(q networkQuery) []networkEntry {
	b.mu.Lock()
	defer b.mu.Unlock()

	var result []networkEntry
	for _, e := range b.completed {
		if networkEntryMatches(e, q) {
			result = append(result, e)
		}
	}
	if q.includePending {
		for _, e := range b.pending {
			if networkEntryMatches(*e, q) {
				result = append(result, *e)
			}
		}
	}
	if q.limit > 0 && len(result) > q.limit {
		result = result[len(result)-q.limit:]
	}
	if q.clear {
		b.completed = nil
	}
	return result
}

func networkEntryMatches(e networkEntry, q networkQuery) bool {
	if q.urlFilter != "" && !strings.Contains(strings.ToLower(e.URL), strings.ToLower(q.urlFilter)) {
		return false
	}
	if q.method != "" && !strings.EqualFold(e.Method, q.method) {
		return false
	}
	if q.statusMin > 0 && e.Status < q.statusMin {
		return false
	}
	if q.statusMax > 0 && e.Status > q.statusMax {
		return false
	}
	return true
}

// attachNetworkCapture enables Network events and registers a lifetime
// listener that writes request/response pairs into pool.networkBuf.
// At most one active listener exists at any time (same token pattern as
// attachConsoleCapture).
func (pool *browserPool) attachNetworkCapture(tabCtx context.Context) {
	pool.networkMu.Lock()
	if pool.networkCncl != nil {
		pool.networkCncl.cancel()
	}
	listenerCtx, listenerCncl := context.WithCancel(tabCtx)
	tok := &consoleListenerToken{cancel: listenerCncl}
	pool.networkCncl = tok
	pool.networkMu.Unlock()

	enableCtx, enableCancel := context.WithTimeout(tabCtx, browserOpTimeout)
	defer enableCancel()
	if err := chromedp.Run(enableCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return network.Enable().Do(ctx)
	})); err != nil {
		pool.networkMu.Lock()
		if pool.networkCncl == tok {
			pool.networkCncl = nil
		}
		pool.networkMu.Unlock()
		listenerCncl()
		pool.logger.Warn("browser: cannot enable network events for capture", "error", err)
		return
	}
	chromedp.ListenTarget(listenerCtx, func(ev any) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			pool.networkBuf.onRequest(e)
		case *network.EventResponseReceived:
			pool.networkBuf.onResponse(e)
		case *network.EventLoadingFinished:
			pool.networkBuf.onFinished(e)
		case *network.EventLoadingFailed:
			pool.networkBuf.onFailed(e)
		}
	})
}

// remoteObjectToString reduces a CDP RemoteObject to a readable string.
// Primitives (strings, numbers, bools, null) come through Value as JSON;
// strings get unwrapped so console.log("hi") yields "hi" rather than `"hi"`.
// Objects/functions don't carry a Value (they're handles); fall back to
// Description, which is what DevTools shows.
func remoteObjectToString(arg *cdpruntime.RemoteObject) string {
	if arg == nil {
		return ""
	}
	if len(arg.Value) > 0 {
		var s string
		if err := json.Unmarshal(arg.Value, &s); err == nil {
			return s
		}
		return string(arg.Value)
	}
	return arg.Description
}

func consoleEntryFromAPICall(e *cdpruntime.EventConsoleAPICalled) consoleEntry {
	parts := make([]string, 0, len(e.Args))
	for _, a := range e.Args {
		parts = append(parts, remoteObjectToString(a))
	}
	entry := consoleEntry{
		Level:     string(e.Type),
		Text:      strings.Join(parts, " "),
		Timestamp: time.Now().UTC(),
	}
	if e.StackTrace != nil && len(e.StackTrace.CallFrames) > 0 {
		cf := e.StackTrace.CallFrames[0]
		entry.Source = cf.URL
		entry.Line = int(cf.LineNumber)
	}
	return entry
}

func consoleEntryFromException(e *cdpruntime.EventExceptionThrown) consoleEntry {
	entry := consoleEntry{
		Level:     "error",
		Timestamp: time.Now().UTC(),
	}
	if e.ExceptionDetails == nil {
		entry.Text = "(unknown JS exception)"
		return entry
	}
	entry.Text = e.ExceptionDetails.Text
	if msg := remoteObjectToString(e.ExceptionDetails.Exception); msg != "" && msg != entry.Text {
		entry.Text = entry.Text + ": " + msg
	}
	entry.Source = e.ExceptionDetails.URL
	entry.Line = int(e.ExceptionDetails.LineNumber)
	return entry
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
	pool.attachConsoleCapture(tabCtx)
	pool.attachNetworkCapture(tabCtx)
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
	pool.attachConsoleCapture(tabCtx)
	pool.attachNetworkCapture(tabCtx)
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

	pool.attachConsoleCapture(newCtx)
	pool.attachNetworkCapture(newCtx)

	if oldCncl != nil {
		oldCncl()
	}
	return nil
}

// activeTargetID returns the chromedp Target.TargetID currently associated
// with the active tab context, or "" when no tab is attached or chromedp
// has not yet populated the Target on the context (e.g. before the first
// chromedp.Run completes).
//
// Reads pool.tabCtx — caller is responsible for any locking when consistency
// with other pool fields matters.
func (pool *browserPool) activeTargetID() cdptarget.ID {
	if pool.tabCtx == nil {
		return ""
	}
	cdpCtx := chromedp.FromContext(pool.tabCtx)
	if cdpCtx == nil || cdpCtx.Target == nil {
		return ""
	}
	return cdpCtx.Target.TargetID
}

// closeTab closes the tab identified by targetID. The behaviour depends on
// whether the closed tab was the pool's active one:
//
//   - Closing a non-active tab: the pool is untouched. The active tab and
//     its context remain valid for subsequent calls.
//   - Closing the active tab: the active context is replaced with a fresh
//     one attached to another remaining page (preferring non-chrome:// URLs).
//   - Closing the active tab when no other pages remain: the pool is fully
//     torn down (ensureConnected on the next call relaunches a browser).
//
// The previous implementation used [browserPool.close] unconditionally,
// which cancelled the entire allocator after every close — even closes of
// non-active tabs forced the next call to reconnect. This split preserves
// the long-lived allocator and only swaps the small tab-context portion.
func (pool *browserPool) closeTab(targetID cdptarget.ID) error {
	// Phase 1: snapshot under lock.
	pool.mu.Lock()
	if err := pool.ensureConnected(); err != nil {
		pool.mu.Unlock()
		return err
	}
	activeID := pool.activeTargetID()
	tabCtx := pool.tabCtx
	allocCtx := pool.allocCtx
	snapGen := pool.gen
	pool.mu.Unlock()

	isActive := targetID == activeID

	// Phase 2: enumerate remaining targets BEFORE closing if we're closing
	// the active tab — afterwards the tab context becomes unusable for
	// further CDP commands. For non-active closes we don't need this.
	var remaining []*cdptarget.Info
	if isActive {
		listCtx, listCancel := context.WithTimeout(tabCtx, browserOpTimeout)
		listErr := chromedp.Run(listCtx, chromedp.ActionFunc(func(ctx context.Context) error {
			t, err := chromedp.Targets(ctx)
			remaining = t
			return err
		}))
		listCancel()
		if listErr != nil {
			return fmt.Errorf("list remaining targets: %w", listErr)
		}
	}

	// Phase 3: close the target. CloseTarget is a browser-domain command, so
	// we can issue it on any tab context — including the one being closed.
	closeCtx, closeCancel := context.WithTimeout(tabCtx, browserOpTimeout)
	defer closeCancel()
	if err := chromedp.Run(closeCtx, chromedp.ActionFunc(func(ctx context.Context) error {
		return cdptarget.CloseTarget(targetID).Do(ctx)
	})); err != nil {
		return fmt.Errorf("close target %s: %w", targetID, err)
	}

	if !isActive {
		// Non-active close: the pool's active tab is unchanged, nothing else
		// to do.
		return nil
	}

	// Phase 4: pick a successor active tab (skip the one we just closed and
	// any chrome:// pages — those are internal browser tabs the user almost
	// never wants the agent attached to).
	var newActive cdptarget.ID
	for _, t := range remaining {
		if t.Type == "page" && t.TargetID != targetID && !strings.HasPrefix(t.URL, "chrome://") {
			newActive = t.TargetID
			break
		}
	}
	if newActive == "" {
		// No other usable pages — full pool teardown is the right answer
		// (next call will probe / launch a fresh browser).
		pool.mu.Lock()
		pool.close()
		pool.mu.Unlock()
		return nil
	}

	// Phase 5: attach a new chromedp context to the successor target. Same
	// TOCTOU-safe protocol as switchTab — abort if the pool reconnected
	// while we were doing CDP work outside the lock.
	newCtx, newCncl := chromedp.NewContext(allocCtx,
		chromedp.WithTargetID(newActive),
		chromedp.WithLogf(pool.logf),
	)
	if err := chromedp.Run(newCtx); err != nil {
		newCncl()
		return fmt.Errorf("attach to remaining tab %s: %w", newActive, err)
	}

	pool.mu.Lock()
	if pool.gen != snapGen {
		pool.mu.Unlock()
		newCncl()
		return fmt.Errorf("browser reconnected during close; try again")
	}
	oldCncl := pool.tabCncl
	pool.tabCtx = newCtx
	pool.tabCncl = newCncl
	pool.mu.Unlock()

	pool.attachConsoleCapture(newCtx)
	pool.attachNetworkCapture(newCtx)

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
		newBrowserDragTool(pool, logger),
		newBrowserScreenshotTool(pool, logger),
		newBrowserGetTextTool(pool, logger),
		newBrowserGetHTMLTool(pool, logger),
		newBrowserEvalTool(pool, logger),
		newBrowserConsoleLogTool(pool, logger),
		newBrowserGetCookiesTool(pool, logger),
		newBrowserSetCookiesTool(pool, logger),
		newBrowserDeleteCookiesTool(pool, logger),
		newBrowserStorageTool(pool, logger),
		newBrowserWaitTool(pool, logger),
		newBrowserScrollTool(pool, logger),
		newBrowserSetViewportTool(pool, logger),
		newBrowserInfoTool(pool, logger),
		newBrowserSwitchTabTool(pool, logger),
		newBrowserNewTabTool(pool, logger),
		newBrowserCloseTabTool(pool, logger),
		newBrowserDialogTool(pool, logger),
		newBrowserNetworkTool(pool, logger),
		newBrowserAccessibilityTool(pool, logger),
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
				"description": "Select a single option by its value attribute. Ignored when 'values' is provided.",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Select a single option by visible text (exact match first, then partial). Ignored when 'values' is provided.",
			},
			"values": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"minItems":    1,
				"description": "Select one or more options by their value attributes. Use for <select multiple> elements or to select several values at once. Unselects all currently selected options first. Takes precedence over 'value' and 'text'.",
			},
			"frame": frameSchemaProperty(),
		},
		"required": []string{"selector"},
		// Require at least one of value / text / values so the error surfaces
		// before the tool call reaches the browser.
		"anyOf": []any{
			map[string]any{"required": []string{"value"}},
			map[string]any{"required": []string{"text"}},
			map[string]any{"required": []string{"values"}},
		},
	}
	return NewTool("browser_select",
		"Select option(s) in a <select> dropdown. Exactly one of 'value' (string), 'text' (string), or 'values' (array) must be provided.",
		schema,
		func(params map[string]any) ToolResult {
			selector, ok := getString(params, "selector")
			if !ok || selector == "" {
				return NewToolResult("", fmt.Errorf("browser_select: 'selector' is required"))
			}
			value := getStringDefault(params, "value", "")
			text := getStringDefault(params, "text", "")
			frame := getStringDefault(params, "frame", "")

			// Extract 'values' array (JSON arrays arrive as []any after decode).
			var multiValues []string
			if raw, ok := params["values"]; ok {
				switch v := raw.(type) {
				case []any:
					for _, item := range v {
						if s, ok := item.(string); ok {
							multiValues = append(multiValues, s)
						}
					}
				case []string:
					multiValues = v
				}
			}

			if len(multiValues) == 0 && value == "" && text == "" {
				return NewToolResult("", fmt.Errorf("browser_select: provide 'values', 'value', or 'text'"))
			}

			var result any
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				opts, err := resolveFrame(ctx, frame)
				if err != nil {
					return err
				}
				if err := chromedp.WaitVisible(selector, opts...).Do(ctx); err != nil {
					return err
				}

				// Multi-select: reset all selected options then select each
				// matching value, then dispatch change/input events.
				if len(multiValues) > 0 {
					targetsJSON, _ := json.Marshal(multiValues)
					script := fmt.Sprintf(`(function(){
						%s
						var sel = doc.querySelector(%q);
						if (!sel) return "error: element not found";
						var targets = %s;
						Array.from(sel.options).forEach(function(opt){ opt.selected = false; });
						var matched = [];
						Array.from(sel.options).forEach(function(opt){
							if (targets.indexOf(opt.value) >= 0) {
								opt.selected = true;
								matched.push(opt.value);
							}
						});
						if (matched.length === 0) return "error: no options matched: " + targets.join(", ");
						sel.dispatchEvent(new Event('change',{bubbles:true}));
						sel.dispatchEvent(new Event('input',{bubbles:true}));
						return "selected " + matched.length + " option(s): " + matched.join(", ");
					})()`, selectDocScopeJS(frame), selector, targetsJSON)
					return chromedp.EvaluateAsDevTools(script, &result).Do(ctx)
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

// hoverAbsPosJS returns a JS IIFE that resolves the absolute viewport centre
// of elementSelector. When frameSelector is empty it queries the top frame
// directly; when set it sums the iframe's top-frame rect with the inner
// element rect so CDP receives outer-viewport coordinates.
func hoverAbsPosJS(frameSelector, elementSelector string) string {
	if frameSelector == "" {
		return fmt.Sprintf(`(function(){
			var el=document.querySelector(%q);
			if(!el)return"";
			var r=el.getBoundingClientRect();
			return JSON.stringify({x:r.left+r.width/2,y:r.top+r.height/2});
		})()`, elementSelector)
	}
	return fmt.Sprintf(`(function(){
		var __frame=document.querySelector(%q);
		if(!__frame||!__frame.contentDocument)return"error: frame inaccessible";
		var fr=__frame.getBoundingClientRect();
		var el=__frame.contentDocument.querySelector(%q);
		if(!el)return"";
		var er=el.getBoundingClientRect();
		return JSON.stringify({x:fr.left+er.left+er.width/2,y:fr.top+er.top+er.height/2});
	})()`, frameSelector, elementSelector)
}

// hoverEventsJS returns a JS IIFE that dispatches synthetic mouseover /
// mouseenter / mousemove events. When frameSelector is set the events are
// created with the iframe's own contentWindow.MouseEvent so iframe-internal
// listeners fire in the correct JS realm.
func hoverEventsJS(frameSelector, elementSelector string) string {
	if frameSelector == "" {
		return fmt.Sprintf(`(function(){
			var el=document.querySelector(%q);
			if(!el)return;
			["mouseover","mouseenter","mousemove"].forEach(function(t){
				el.dispatchEvent(new MouseEvent(t,{bubbles:true,cancelable:true}));
			});
		})()`, elementSelector)
	}
	return fmt.Sprintf(`(function(){
		var __frame=document.querySelector(%q);
		if(!__frame||!__frame.contentDocument)return;
		var el=__frame.contentDocument.querySelector(%q);
		if(!el)return;
		var __win=__frame.contentWindow;
		["mouseover","mouseenter","mousemove"].forEach(function(t){
			el.dispatchEvent(new __win.MouseEvent(t,{bubbles:true,cancelable:true}));
		});
	})()`, frameSelector, elementSelector)
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
			"frame": frameSchemaProperty(),
		},
		"required": []string{"selector"},
	}
	return NewTool("browser_hover", "Hover over a DOM element, triggering both CSS :hover and JS mouseover handlers.", schema,
		func(params map[string]any) ToolResult {
			selector, ok := getString(params, "selector")
			if !ok || selector == "" {
				return NewToolResult("", fmt.Errorf("browser_hover: 'selector' is required"))
			}
			frame := getStringDefault(params, "frame", "")

			// Step 1: validate frame (same-origin check) + wait for element +
			// read absolute viewport centre. hoverAbsPosJS translates iframe-local
			// rect to outer-viewport coords when frame is set.
			var posJSON string
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				opts, err := resolveFrame(ctx, frame)
				if err != nil {
					return err
				}
				if err := chromedp.WaitVisible(selector, opts...).Do(ctx); err != nil {
					return err
				}
				return chromedp.EvaluateAsDevTools(hoverAbsPosJS(frame, selector), &posJSON).Do(ctx)
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_hover: %w", err))
			}
			if posJSON == "" {
				return NewToolResult("", fmt.Errorf("browser_hover: element not found: %s", selector))
			}
			if strings.HasPrefix(posJSON, "error:") {
				return NewToolResult("", fmt.Errorf("browser_hover: %s", posJSON))
			}

			var pos struct {
				X float64 `json:"x"`
				Y float64 `json:"y"`
			}
			if err := json.Unmarshal([]byte(posJSON), &pos); err != nil {
				return NewToolResult("", fmt.Errorf("browser_hover: parse position: %w", err))
			}

			// Step 2: move real mouse cursor (triggers CSS :hover) and dispatch
			// synthetic JS events. hoverEventsJS uses the iframe's own
			// contentWindow.MouseEvent realm so iframe listeners fire correctly.
			var ignored any
			if err := pool.runDefault(
				chromedp.ActionFunc(func(ctx context.Context) error {
					return cdpinput.DispatchMouseEvent(cdpinput.MouseMoved, pos.X, pos.Y).Do(ctx)
				}),
				chromedp.EvaluateAsDevTools(hoverEventsJS(frame, selector), &ignored),
			); err != nil {
				return NewToolResult("", fmt.Errorf("browser_hover: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Hovered over: %s (%.0f, %.0f)", selector, pos.X, pos.Y), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── browser_drag ──────────────────────────────────────────────────────────────

func newBrowserDragTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"source": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element to drag from.",
			},
			"target": map[string]any{
				"type":        "string",
				"description": "CSS selector of the element to drop onto.",
			},
			"steps": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     100,
				"description": "Number of intermediate dragover positions simulated between source and target (default 10). Higher values produce a smoother path that fires more dragover events — useful when an element only activates mid-drag.",
			},
		},
		"required": []string{"source", "target"},
	}
	return NewTool("browser_drag",
		"Drag an element from a source selector to a target selector using CDP drag events. "+
			"Works with HTML5 drag-and-drop, sortable lists, kanban boards, and file drop zones.",
		schema,
		func(params map[string]any) ToolResult {
			source, ok := getString(params, "source")
			if !ok || source == "" {
				return NewToolResult("", fmt.Errorf("browser_drag: 'source' is required"))
			}
			target, ok2 := getString(params, "target")
			if !ok2 || target == "" {
				return NewToolResult("", fmt.Errorf("browser_drag: 'target' is required"))
			}
			steps := max(1, min(100, getInt(params, "steps", 10)))

			// Step 1: resolve the centre viewport coords of both elements.
			type elemPos struct{ X, Y float64 }
			var src, dst elemPos

			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				getCenter := func(sel string) (float64, float64, error) {
					script := fmt.Sprintf(
						`(function(){var e=document.querySelector(%q);if(!e)return"";`+
							`var r=e.getBoundingClientRect();`+
							`return JSON.stringify({x:r.left+r.width/2,y:r.top+r.height/2});})()`,
						sel,
					)
					var out string
					if err := chromedp.EvaluateAsDevTools(script, &out).Do(ctx); err != nil {
						return 0, 0, err
					}
					if out == "" {
						return 0, 0, fmt.Errorf("element not found: %s", sel)
					}
					var p elemPos
					if err := json.Unmarshal([]byte(out), &p); err != nil {
						return 0, 0, fmt.Errorf("parse rect for %s: %w", sel, err)
					}
					return p.X, p.Y, nil
				}
				var err error
				src.X, src.Y, err = getCenter(source)
				if err != nil {
					return fmt.Errorf("source: %w", err)
				}
				dst.X, dst.Y, err = getCenter(target)
				if err != nil {
					return fmt.Errorf("target: %w", err)
				}
				return nil
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_drag: %w", err))
			}

			// Step 2: full CDP drag sequence.
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				// Enable drag interception so Chrome fires EventDragIntercepted
				// (carrying the DataTransfer populated by the page's dragstart
				// handler) instead of handling the drag natively.
				if err := cdpinput.SetInterceptDrags(true).Do(ctx); err != nil {
					return fmt.Errorf("enable drag interception: %w", err)
				}
				defer cdpinput.SetInterceptDrags(false).Do(ctx) //nolint:errcheck

				ch := make(chan *cdpinput.EventDragIntercepted, 1)
				chromedp.ListenTarget(ctx, func(ev any) {
					if e, ok := ev.(*cdpinput.EventDragIntercepted); ok {
						select {
						case ch <- e:
						default:
						}
					}
				})

				// Press at source to start the drag gesture.
				if err := cdpinput.DispatchMouseEvent(cdpinput.MousePressed, src.X, src.Y).
					WithButton(cdpinput.Left).
					WithButtons(1).
					WithClickCount(1).
					Do(ctx); err != nil {
					return fmt.Errorf("mousedown on source: %w", err)
				}

				// Nudge the mouse to trigger the browser's dragstart event.
				if err := cdpinput.DispatchMouseEvent(cdpinput.MouseMoved, src.X+4, src.Y+4).
					WithButton(cdpinput.Left).
					WithButtons(1).
					Do(ctx); err != nil {
					return fmt.Errorf("trigger drag: %w", err)
				}

				// Collect drag data from the page's dragstart handler.
				// Fall back to a minimal text/plain payload for pages that
				// use drag visuals only (no dataTransfer.setData call).
				var dragData *cdpinput.DragData
				select {
				case ev := <-ch:
					dragData = ev.Data
				case <-time.After(500 * time.Millisecond):
					dragData = &cdpinput.DragData{
						Items: []*cdpinput.DragDataItem{
							{MimeType: "text/plain", Data: ""},
						},
					}
				}

				// Simulate a smooth drag path from source to target so that
				// intermediate dragover handlers fire at each step.
				for i := range steps {
					t := float64(i+1) / float64(steps)
					ix := src.X + (dst.X-src.X)*t
					iy := src.Y + (dst.Y-src.Y)*t
					if err := cdpinput.DispatchDragEvent(cdpinput.DragOver, ix, iy, dragData).Do(ctx); err != nil {
						return fmt.Errorf("dragover step %d: %w", i+1, err)
					}
				}

				// Enter and drop at the target.
				if err := cdpinput.DispatchDragEvent(cdpinput.DragEnter, dst.X, dst.Y, dragData).Do(ctx); err != nil {
					return fmt.Errorf("dragenter: %w", err)
				}
				if err := cdpinput.DispatchDragEvent(cdpinput.DragOver, dst.X, dst.Y, dragData).Do(ctx); err != nil {
					return fmt.Errorf("dragover target: %w", err)
				}
				if err := cdpinput.DispatchDragEvent(cdpinput.Drop, dst.X, dst.Y, dragData).Do(ctx); err != nil {
					return fmt.Errorf("drop: %w", err)
				}

				// Release the mouse button to complete the gesture.
				return cdpinput.DispatchMouseEvent(cdpinput.MouseReleased, dst.X, dst.Y).
					WithButton(cdpinput.Left).
					WithButtons(0).
					Do(ctx)
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_drag: %w", err))
			}

			return NewToolResult(
				fmt.Sprintf("Dragged %q (%.0f,%.0f) → %q (%.0f,%.0f) via %d steps",
					source, src.X, src.Y, target, dst.X, dst.Y, steps),
				nil,
			)
		},
		WithPermissionLevel(PermLevelWrite),
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
			"frame": frameSchemaProperty(),
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
		"Take a screenshot of the active tab or a specific element. Output is always written under the OS temp directory. Supports PNG (default, lossless) and JPEG (with adjustable quality). Use `frame` to capture an element inside a same-origin iframe.",
		schema,
		func(params map[string]any) ToolResult {
			selector := getStringDefault(params, "selector", "")
			frame := getStringDefault(params, "frame", "")
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
				buf, captErr = captureScreenshot(ctx, frame, selector, format, int64(quality))
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
// the encoded image bytes. Three dimensions of behaviour:
//
//   - selector empty → full-page capture (captureBeyondViewport).
//   - selector set, frameSelector empty → element capture in top frame.
//   - selector set, frameSelector set → element inside same-origin iframe;
//     the clip rect is translated to outer-viewport coordinates by summing
//     the iframe's bounding rect with the inner element's bounding rect.
//
// PNG is the default; JPEG is requested via WithFormat+WithQuality.
func captureScreenshot(ctx context.Context, frameSelector, selector, format string, quality int64) ([]byte, error) {
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
		opts, err := resolveFrame(ctx, frameSelector)
		if err != nil {
			return nil, err
		}
		if err := chromedp.WaitReady(selector, opts...).Do(ctx); err != nil {
			return nil, fmt.Errorf("element not ready: %w", err)
		}
		if err := chromedp.ScrollIntoView(selector, opts...).Do(ctx); err != nil {
			return nil, fmt.Errorf("scroll into view: %w", err)
		}

		// Build the rect-query JS. When an iframe is involved, translate the
		// element's iframe-local rect to outer-viewport coordinates by adding
		// the iframe element's own getBoundingClientRect offset.
		var getRect string
		if frameSelector == "" {
			getRect = fmt.Sprintf(
				`(function(){var e=document.querySelector(%q);if(!e)return"";var r=e.getBoundingClientRect();return JSON.stringify({x:r.x,y:r.y,width:r.width,height:r.height});})()`,
				selector,
			)
		} else {
			getRect = fmt.Sprintf(
				`(function(){`+
					`var __frame=document.querySelector(%q);`+
					`if(!__frame||!__frame.contentDocument)return"error: frame inaccessible";`+
					`var fr=__frame.getBoundingClientRect();`+
					`var el=__frame.contentDocument.querySelector(%q);`+
					`if(!el)return"";`+
					`var er=el.getBoundingClientRect();`+
					`return JSON.stringify({x:fr.left+er.left,y:fr.top+er.top,width:er.width,height:er.height});`+
					`})()`,
				frameSelector, selector,
			)
		}

		var rectJSON string
		if err := chromedp.EvaluateAsDevTools(getRect, &rectJSON).Do(ctx); err != nil {
			return nil, fmt.Errorf("get element bounds: %w", err)
		}
		if rectJSON == "" {
			return nil, fmt.Errorf("element not found: %s", selector)
		}
		if strings.HasPrefix(rectJSON, "error:") {
			return nil, fmt.Errorf("%s", rectJSON)
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

// ── browser_dialog ─────────────────────────────────────────────────────────────

func newBrowserDialogTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"accept", "dismiss"},
				"description": "Whether to accept (OK/Yes/Confirm) or dismiss (Cancel/No) the dialog.",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Text to type into a prompt dialog before accepting. Ignored for alert and confirm dialogs.",
			},
			"wait_ms": map[string]any{
				"type":        "integer",
				"minimum":     0,
				"maximum":     25000,
				"description": "Milliseconds to wait for a dialog to appear if none is currently open (default 5000). Pass 0 to only handle an already-open dialog.",
			},
		},
		"required": []string{"action"},
	}
	return NewTool("browser_dialog",
		"Accept or dismiss a JavaScript dialog (alert, confirm, prompt, or beforeunload). "+
			"If a dialog is already open it is handled immediately; otherwise the tool waits up to wait_ms for one to appear. "+
			"Returns the dialog type and message so the agent knows what the page was asking.",
		schema,
		func(params map[string]any) ToolResult {
			action, ok := getString(params, "action")
			if !ok || (action != "accept" && action != "dismiss") {
				return NewToolResult("", fmt.Errorf("browser_dialog: 'action' must be 'accept' or 'dismiss'"))
			}
			promptText := getStringDefault(params, "text", "")
			waitMs := max(0, min(25000, getInt(params, "wait_ms", 5000)))
			accept := action == "accept"

			var dialogType, dialogMsg string

			err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				// Register the listener BEFORE any attempt to handle so a
				// dialog that fires during this action window is not missed.
				ch := make(chan *page.EventJavascriptDialogOpening, 1)
				chromedp.ListenTarget(ctx, func(ev any) {
					if e, ok := ev.(*page.EventJavascriptDialogOpening); ok {
						select {
						case ch <- e:
						default:
						}
					}
				})

				// Try to handle a dialog that is already open. Chrome returns
				// -32000 ("No dialog is showing") when nothing is pending.
				req := page.HandleJavaScriptDialog(accept)
				if promptText != "" {
					req = req.WithPromptText(promptText)
				}
				if err := req.Do(ctx); err == nil {
					dialogType = "unknown"
					dialogMsg = "(dialog was already open when tool was called; type not available)"
					return nil
				}

				if waitMs == 0 {
					return fmt.Errorf("no dialog is currently open")
				}

				timer := time.NewTimer(time.Duration(waitMs) * time.Millisecond)
				defer timer.Stop()
				select {
				case ev := <-ch:
					dialogType = string(ev.Type)
					dialogMsg = ev.Message
					req2 := page.HandleJavaScriptDialog(accept)
					if promptText != "" {
						req2 = req2.WithPromptText(promptText)
					}
					return req2.Do(ctx)
				case <-timer.C:
					return fmt.Errorf("no dialog appeared within %dms", waitMs)
				case <-ctx.Done():
					return ctx.Err()
				}
			}))

			if err != nil {
				return NewToolResult("", fmt.Errorf("browser_dialog: %w", err))
			}

			result := map[string]any{
				"action":  action,
				"type":    dialogType,
				"message": dialogMsg,
			}
			if accept && promptText != "" {
				result["prompt_input"] = promptText
			}
			b, _ := json.MarshalIndent(result, "", "  ")
			return NewToolResult(string(b), nil)
		},
		WithPermissionLevel(PermLevelWrite),
		WithLogger(logger),
	)
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
			"frame": map[string]any{
				"type":        "string",
				"description": "Optional CSS selector for an iframe element. When set, the script runs inside that iframe's JavaScript realm — `document` and `window` reference the iframe, and (for same-origin frames) the script can access the iframe's variables and DOM directly. Cross-origin iframes return a runtime error.",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"minimum":     100,
				"maximum":     25000,
				"description": "Maximum milliseconds Chrome allows the script to run before terminating it (default 5000). Protects against infinite loops blocking the browser session. Increase for genuinely long-running async work.",
			},
		},
		"required": []string{"script"},
	}
	return NewTool("browser_eval",
		"Execute JavaScript in the active tab (or in an iframe via `frame`) and return the result. Supports async/await and Promises.",
		schema,
		func(params map[string]any) ToolResult {
			script, ok := getString(params, "script")
			if !ok || script == "" {
				return NewToolResult("", fmt.Errorf("browser_eval: 'script' is required"))
			}
			frame := getStringDefault(params, "frame", "")
			timeoutMs := max(100, min(25000, getInt(params, "timeout_ms", 5000)))
			expr := buildEvalExpression(frame, script)

			var raw json.RawMessage
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				res, exp, err := cdpruntime.Evaluate(expr).
					WithAwaitPromise(true).
					WithReturnByValue(true).
					WithTimeout(cdpruntime.TimeDelta(timeoutMs)).
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

// buildEvalExpression returns the JS expression that browser_eval submits to
// Runtime.evaluate. The empty-frame case is the same single-line async IIFE
// that wraps the user's script in the top-frame realm. The non-empty case
// adds an outer wrapper that:
//
//  1. Locates the iframe element by CSS selector (and verifies it's an
//     <iframe>/<frame> tag at runtime, not whatever else the selector matched).
//  2. Reads `contentWindow` / `contentDocument` inside a try/catch so a
//     cross-origin SecurityError surfaces as a JS Error with a useful
//     message rather than crashing the evaluation.
//  3. Constructs a Function in the iframe's JS realm via
//     `new contentWindow.Function('document', 'window', body)`. Inside that
//     function, `document` and `window` (passed as arguments) reference the
//     iframe's globals; lexical references to `fetch`, `setTimeout`, etc.
//     resolve to the iframe's realm. This is the standard same-origin
//     same-page-iframe technique used by Playwright/Puppeteer.
//
// Cross-origin iframes still error here at runtime — the cross-origin
// SecurityError on contentDocument access is caught and rethrown with a
// clearer message. Cross-origin iframe support requires attaching to a
// separate CDP target and is a separate feature.
func buildEvalExpression(frameSelector, script string) string {
	if frameSelector == "" {
		return "(async function(){\n" + script + "\n})()"
	}
	innerBody := fmt.Sprintf("return (async function(){\n%s\n})()", script)
	return fmt.Sprintf(`(async function(){
	var __f = document.querySelector(%q);
	if (!__f) throw new Error("frame not found: " + %q);
	var tag = __f.tagName;
	if (tag !== "IFRAME" && tag !== "FRAME") {
		throw new Error("frame selector " + %q + " matched <" + tag.toLowerCase() + ">, not an iframe/frame");
	}
	var __win, __doc;
	try {
		__win = __f.contentWindow;
		__doc = __f.contentDocument;
	} catch (e) {
		throw new Error("frame is cross-origin (" + e.message + "); only same-origin iframes are supported");
	}
	if (!__win || !__doc) {
		throw new Error("frame has no accessible document (cross-origin or detached): " + %q);
	}
	var __fn = new __win.Function('document', 'window', %q);
	return await __fn.call(__win, __doc, __win);
})()`,
		frameSelector, frameSelector, frameSelector, frameSelector, innerBody,
	)
}

// ── browser_console_log ───────────────────────────────────────────────────────

func newBrowserConsoleLogTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"level": map[string]any{
				"type":        "string",
				"enum":        []string{"all", "log", "info", "warn", "error", "debug"},
				"description": "Filter by log level. Default: all. Note: uncaught JS exceptions are surfaced as level=\"error\".",
			},
			"filter": map[string]any{
				"type":        "string",
				"description": "Optional case-insensitive substring filter on the message text.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": "Maximum number of entries to return — most recent ones win when there are more matches than the limit. Default: 100.",
			},
			"clear": map[string]any{
				"type":        "boolean",
				"description": "Clear the buffer after reading (default false). Useful between phases of a test to consume only the most recent activity.",
			},
		},
	}
	return NewTool("browser_console_log",
		"Return console.log/info/warn/error/debug messages and uncaught exceptions captured from the active tab. Messages are buffered as they happen across the session (up to 500 entries, oldest dropped first).",
		schema,
		func(params map[string]any) ToolResult {
			levelParam := getStringDefault(params, "level", "all")
			filterParam := strings.ToLower(getStringDefault(params, "filter", ""))
			limitParam := getInt(params, "limit", 100)
			clearParam := getBool(params, "clear", false)

			validLevels := map[string]bool{
				"all": true, "log": true, "info": true, "warn": true,
				"error": true, "debug": true,
			}
			if !validLevels[levelParam] {
				return NewToolResult("", fmt.Errorf("browser_console_log: level must be one of all/log/info/warn/error/debug, got %q", levelParam))
			}
			if limitParam < 1 {
				return NewToolResult("", fmt.Errorf("browser_console_log: limit must be ≥1, got %d", limitParam))
			}

			// Make sure the pool is attached so the console listener is wired up.
			// Without this first call there's no active tab and nothing has been
			// captured yet.
			pool.mu.Lock()
			if err := pool.ensureConnected(); err != nil {
				pool.mu.Unlock()
				return NewToolResult("", fmt.Errorf("browser_console_log: %w", err))
			}
			pool.mu.Unlock()

			entries := pool.console.read(consoleQuery{
				level:     levelParam,
				substring: filterParam,
				clear:     clearParam,
			})
			// Most-recent-N truncation. Easier on the LLM than oldest-N when a
			// chatty page produces hundreds of messages.
			if len(entries) > limitParam {
				entries = entries[len(entries)-limitParam:]
			}
			b, _ := json.MarshalIndent(entries, "", "  ")
			return NewToolResult(string(b), nil)
		},
		WithPermissionLevel(PermLevelRead),
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
				HTTPOnly bool   `json:"http_only"`
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
					HTTPOnly: c.HTTPOnly,
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

// ── browser_storage ───────────────────────────────────────────────────────────

func newBrowserStorageTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"enum": []string{"get", "set", "remove", "clear", "get_all"},
				"description": "Storage operation: " +
					"get — read a value (returns null when key is absent); " +
					"set — write key=value; " +
					"remove — delete a single key; " +
					"clear — delete every key in this storage; " +
					"get_all — return all key-value pairs as a JSON object.",
			},
			"storage": map[string]any{
				"type":        "string",
				"enum":        []string{"local", "session"},
				"description": "Which Web Storage to target: 'local' (localStorage, persists across sessions, default) or 'session' (sessionStorage, tab-lifetime only).",
			},
			"key": map[string]any{
				"type":        "string",
				"description": "Storage key. Required for get, set, and remove.",
			},
			"value": map[string]any{
				"type":        "string",
				"description": "Value to write. Required for set. May be an empty string.",
			},
		},
		"required": []string{"action"},
	}
	return NewTool("browser_storage",
		"Read or write Web Storage (localStorage or sessionStorage) on the active tab's origin. "+
			"Useful for inspecting auth tokens, feature flags, and other SPA state without navigating.",
		schema,
		func(params map[string]any) ToolResult {
			action, ok := getString(params, "action")
			if !ok {
				return NewToolResult("", fmt.Errorf("browser_storage: 'action' is required"))
			}
			storageType := getStringDefault(params, "storage", "local")
			key := getStringDefault(params, "key", "")
			value := getStringDefault(params, "value", "")

			storeJS := "window.localStorage"
			if storageType == "session" {
				storeJS = "window.sessionStorage"
			}

			switch action {
			case "get", "set", "remove":
				if key == "" {
					return NewToolResult("", fmt.Errorf("browser_storage: 'key' is required for action %q", action))
				}
			case "clear", "get_all":
				// no key required
			default:
				return NewToolResult("", fmt.Errorf("browser_storage: unknown action %q", action))
			}

			var out string
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				var expr string
				switch action {
				case "get":
					// JSON.stringify so null (missing key) is distinguishable from
					// an empty-string value: missing → "null", "" → `""`
					expr = fmt.Sprintf(`JSON.stringify(%s.getItem(%q))`, storeJS, key)
				case "set":
					expr = fmt.Sprintf(`(function(){ %s.setItem(%q, %q); return "ok"; })()`, storeJS, key, value)
				case "remove":
					expr = fmt.Sprintf(`(function(){ %s.removeItem(%q); return "ok"; })()`, storeJS, key)
				case "clear":
					expr = fmt.Sprintf(`(function(){ %s.clear(); return "ok"; })()`, storeJS)
				case "get_all":
					expr = fmt.Sprintf(
						`JSON.stringify(Object.fromEntries(Object.keys(%s).map(k=>[k,%s.getItem(k)])))`,
						storeJS, storeJS,
					)
				}
				return chromedp.EvaluateAsDevTools(expr, &out).Do(ctx)
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_storage: %w", err))
			}

			switch action {
			case "get":
				var val *string
				if err := json.Unmarshal([]byte(out), &val); err != nil {
					return NewToolResult("", fmt.Errorf("browser_storage: parse result: %w", err))
				}
				storeName := storageType + "Storage"
				if val == nil {
					return NewToolResult(fmt.Sprintf("%s[%q] = null (key not set)", storeName, key), nil)
				}
				return NewToolResult(fmt.Sprintf("%s[%q] = %q", storeName, key, *val), nil)
			case "get_all":
				return NewToolResult(out, nil)
			default:
				return NewToolResult(fmt.Sprintf("%sStorage: %s %q ok", storageType, action, key), nil)
			}
		},
		WithPermissionLevel(PermLevelBash),
		WithLogger(logger),
	)
}

// ── browser_delete_cookies ─────────────────────────────────────────────────────

func newBrowserDeleteCookiesTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "Name of the cookie to delete.",
			},
			"domain": map[string]any{
				"type":        "string",
				"description": "Domain to scope deletion (e.g. .example.com). Must be a domain-suffix of the active tab's host. Omit to target the current page's URL (most precise match).",
			},
		},
		"required": []string{"name"},
	}
	return NewTool("browser_delete_cookies",
		"Delete a browser cookie by name on the active tab's origin. "+
			"Use browser_get_cookies first to discover which cookies exist and their domains.",
		schema,
		func(params map[string]any) ToolResult {
			name, ok := getString(params, "name")
			if !ok || name == "" {
				return NewToolResult("", fmt.Errorf("browser_delete_cookies: 'name' is required"))
			}
			domain := getStringDefault(params, "domain", "")

			var deletedOn string
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				var currentURL string
				if err := chromedp.Location(&currentURL).Do(ctx); err != nil {
					return fmt.Errorf("read current url: %w", err)
				}
				if err := validateCookieDomain(domain, currentURL); err != nil {
					return err
				}
				if domain != "" {
					deletedOn = domain
					return network.DeleteCookies(name).WithDomain(domain).Do(ctx)
				}
				deletedOn = currentURL
				return network.DeleteCookies(name).WithURL(currentURL).Do(ctx)
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_delete_cookies: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Deleted cookie %q (scope: %s)", name, deletedOn), nil)
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
				"description": "CSS selector to wait for. Required for all states except network_idle.",
			},
			"state": map[string]any{
				"type": "string",
				"enum": []string{"visible", "ready", "not_visible", "network_idle"},
				"description": "Condition to wait for: " +
					"visible — element exists and is visible (default); " +
					"ready — element is present in DOM (may be hidden); " +
					"not_visible — element is gone or hidden; " +
					"network_idle — no HTTP requests in flight for quiet_ms ms (no selector needed, useful after SPA navigation or form submission).",
			},
			"timeout_ms": map[string]any{
				"type":        "integer",
				"minimum":     100,
				"maximum":     120000,
				"description": "Maximum wait in milliseconds (100–120000, default 10000).",
			},
			"quiet_ms": map[string]any{
				"type":        "integer",
				"minimum":     100,
				"maximum":     5000,
				"description": "Used with network_idle: how many consecutive milliseconds with zero in-flight requests counts as idle (default 500).",
			},
			"frame": frameSchemaProperty(),
		},
	}
	return NewTool("browser_wait",
		"Wait for a DOM element to reach a given visibility state, or for all network activity to go quiet. "+
			"Use network_idle after triggering SPA navigation or form submissions that do not change the URL.",
		schema,
		func(params map[string]any) ToolResult {
			selector := getStringDefault(params, "selector", "")
			state := getStringDefault(params, "state", "visible")
			frame := getStringDefault(params, "frame", "")
			timeoutMs := max(100, min(120000, getInt(params, "timeout_ms", 10000)))
			quietMs := max(100, min(5000, getInt(params, "quiet_ms", 500)))
			timeout := time.Duration(timeoutMs) * time.Millisecond

			if state == "network_idle" {
				quietPeriod := time.Duration(quietMs) * time.Millisecond
				if err := pool.run(timeout, chromedp.ActionFunc(func(ctx context.Context) error {
					if err := network.Enable().Do(ctx); err != nil {
						return fmt.Errorf("enable network events: %w", err)
					}

					var mu sync.Mutex
					inFlight := 0
					lastActivity := time.Now()

					chromedp.ListenTarget(ctx, func(ev any) {
						mu.Lock()
						defer mu.Unlock()
						switch ev.(type) {
						case *network.EventRequestWillBeSent:
							inFlight++
							lastActivity = time.Now()
						case *network.EventLoadingFinished, *network.EventLoadingFailed:
							if inFlight > 0 {
								inFlight--
							}
							lastActivity = time.Now()
						}
					})

					ticker := time.NewTicker(50 * time.Millisecond)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							mu.Lock()
							idle := inFlight == 0 && time.Since(lastActivity) >= quietPeriod
							mu.Unlock()
							if idle {
								return nil
							}
						case <-ctx.Done():
							return ctx.Err()
						}
					}
				})); err != nil {
					return NewToolResult("", fmt.Errorf("browser_wait: network_idle: %w", err))
				}
				return NewToolResult(fmt.Sprintf("Network idle (quiet for %dms)", quietMs), nil)
			}

			if selector == "" {
				return NewToolResult("", fmt.Errorf("browser_wait: 'selector' is required for state %q", state))
			}

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
				"description": "Horizontal scroll offset in pixels. Applied to the top window, or to the iframe window when 'frame' is set.",
			},
			"y": map[string]any{
				"type":        "integer",
				"description": "Vertical scroll offset in pixels. Applied to the top window, or to the iframe window when 'frame' is set.",
			},
			"to_bottom": map[string]any{
				"type":        "boolean",
				"description": "Scroll to the very bottom of the page (or frame, if 'frame' is set). Default false.",
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

			// Window-scroll path: use doc.defaultView so that 'frame' is respected.
			var script string
			if frame != "" {
				if toBottom {
					script = fmt.Sprintf(`(function(){%s doc.defaultView.scrollTo(0, doc.body.scrollHeight)})()`, selectDocScopeJS(frame))
				} else {
					script = fmt.Sprintf(`(function(){%s doc.defaultView.scrollBy(%d, %d)})()`, selectDocScopeJS(frame), x, y)
				}
			} else {
				if toBottom {
					script = "window.scrollTo(0, document.body.scrollHeight)"
				} else {
					script = fmt.Sprintf("window.scrollBy(%d, %d)", x, y)
				}
			}
			var ignored any
			if err := pool.runDefault(chromedp.EvaluateAsDevTools(script, &ignored)); err != nil {
				return NewToolResult("", fmt.Errorf("browser_scroll: %w", err))
			}
			if toBottom {
				if frame != "" {
					return NewToolResult(fmt.Sprintf("Scrolled frame %q to bottom", frame), nil)
				}
				return NewToolResult("Scrolled to bottom", nil)
			}
			if frame != "" {
				return NewToolResult(fmt.Sprintf("Scrolled frame %q by (%d, %d)", frame, x, y), nil)
			}
			return NewToolResult(fmt.Sprintf("Scrolled window by (%d, %d)", x, y), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── browser_set_viewport ──────────────────────────────────────────────────────

func newBrowserSetViewportTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"width": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": "Viewport width in CSS pixels. Required unless reset is true.",
			},
			"height": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": "Viewport height in CSS pixels. Required unless reset is true.",
			},
			"device_scale_factor": map[string]any{
				"type":        "number",
				"minimum":     0.1,
				"maximum":     10,
				"description": "Device pixel ratio (default 1.0). Use 2.0 to simulate a Retina / HiDPI display.",
			},
			"mobile": map[string]any{
				"type":        "boolean",
				"description": "Emulate a mobile device: enables touch events, mobile viewport meta handling, and device-like scroll behaviour (default false).",
			},
			"reset": map[string]any{
				"type":        "boolean",
				"description": "Clear any active viewport override and restore the browser's natural window size (default false). When true, all other parameters are ignored.",
			},
		},
	}
	return NewTool("browser_set_viewport",
		"Override the browser viewport size and optionally enable mobile device emulation. "+
			"Useful for testing responsive layouts or simulating mobile screens. "+
			"Call with reset=true to restore the browser's natural dimensions.",
		schema,
		func(params map[string]any) ToolResult {
			reset := getBool(params, "reset", false)

			if reset {
				if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
					return emulation.ClearDeviceMetricsOverride().Do(ctx)
				})); err != nil {
					return NewToolResult("", fmt.Errorf("browser_set_viewport: reset: %w", err))
				}
				return NewToolResult("Viewport override cleared (restored browser default)", nil)
			}

			width := getInt(params, "width", 0)
			height := getInt(params, "height", 0)
			if width <= 0 || height <= 0 {
				return NewToolResult("", fmt.Errorf("browser_set_viewport: 'width' and 'height' are required (both must be > 0) unless reset is true"))
			}

			dsf := getFloat(params, "device_scale_factor", 1.0)
			if dsf < 0.1 || dsf > 10 {
				dsf = 1.0
			}
			mobile := getBool(params, "mobile", false)

			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				return emulation.SetDeviceMetricsOverride(int64(width), int64(height), dsf, mobile).Do(ctx)
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_set_viewport: %w", err))
			}
			return NewToolResult(
				fmt.Sprintf("Viewport set to %dx%d (scale=%.1f, mobile=%v)", width, height, dsf, mobile),
				nil,
			)
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

			pool.attachConsoleCapture(newTabCtx)
			pool.attachNetworkCapture(newTabCtx)

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
	return NewTool("browser_close_tab",
		"Close a browser tab by ID, or close the current active tab. When closing a non-active tab the pool is untouched; when closing the active tab the pool swaps to another remaining page (or relaunches if none remain).",
		schema,
		func(params map[string]any) ToolResult {
			tabID := getStringDefault(params, "tab_id", "")

			// Resolve which target to close. Default = currently active tab.
			var resolvedID cdptarget.ID
			if tabID != "" {
				resolvedID = cdptarget.ID(tabID)
			} else {
				pool.mu.Lock()
				if err := pool.ensureConnected(); err != nil {
					pool.mu.Unlock()
					return NewToolResult("", fmt.Errorf("browser_close_tab: %w", err))
				}
				resolvedID = pool.activeTargetID()
				pool.mu.Unlock()
				if resolvedID == "" {
					return NewToolResult("", fmt.Errorf("browser_close_tab: no active tab to close (pool not yet attached)"))
				}
			}

			if err := pool.closeTab(resolvedID); err != nil {
				return NewToolResult("", fmt.Errorf("browser_close_tab: %w", err))
			}
			return NewToolResult(fmt.Sprintf("Closed tab %s", resolvedID), nil)
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

// ── browser_network ───────────────────────────────────────────────────────────

func newBrowserNetworkTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        []string{"get", "clear"},
				"description": "get — return captured entries (default); clear — empty the buffer and return what was in it.",
			},
			"url_filter": map[string]any{
				"type":        "string",
				"description": "Case-insensitive substring filter on the request URL.",
			},
			"method": map[string]any{
				"type":        "string",
				"enum":        []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"},
				"description": "Return only requests with this HTTP method.",
			},
			"status_min": map[string]any{
				"type":        "integer",
				"minimum":     100,
				"maximum":     599,
				"description": "Return only responses with status >= this value.",
			},
			"status_max": map[string]any{
				"type":        "integer",
				"minimum":     100,
				"maximum":     599,
				"description": "Return only responses with status <= this value.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     200,
				"description": "Maximum number of entries to return, newest first (default 50).",
			},
			"include_pending": map[string]any{
				"type":        "boolean",
				"description": "Also return in-flight requests that have not yet received a response (default false).",
			},
		},
	}
	return NewTool("browser_network",
		"Return captured HTTP request/response pairs for the current tab. "+
			"The buffer is populated automatically as the page makes XHR, fetch, and navigation requests. "+
			"Filter by URL substring, HTTP method, or status code range. "+
			"Use action=clear to reset the buffer after inspecting.",
		schema,
		func(params map[string]any) ToolResult {
			action := getStringDefault(params, "action", "get")
			if action != "get" && action != "clear" {
				return NewToolResult("", fmt.Errorf("browser_network: action must be 'get' or 'clear'"))
			}
			limit := max(1, min(200, getInt(params, "limit", 50)))
			q := networkQuery{
				urlFilter:      getStringDefault(params, "url_filter", ""),
				method:         getStringDefault(params, "method", ""),
				statusMin:      getInt(params, "status_min", 0),
				statusMax:      getInt(params, "status_max", 0),
				limit:          limit,
				includePending: getBool(params, "include_pending", false),
				clear:          action == "clear",
			}
			entries := pool.networkBuf.read(q)
			if len(entries) == 0 {
				return NewToolResult("[]", nil)
			}
			out, err := json.MarshalIndent(entries, "", "  ")
			if err != nil {
				return NewToolResult("", fmt.Errorf("browser_network: marshal: %w", err))
			}
			return NewToolResult(string(out), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// ── browser_accessibility ─────────────────────────────────────────────────────

// simpleAXNode is the JSON-serialisable output node for browser_accessibility.
type simpleAXNode struct {
	Role        string            `json:"role,omitempty"`
	Name        string            `json:"name,omitempty"`
	Value       string            `json:"value,omitempty"`
	Description string            `json:"description,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
	Children    []*simpleAXNode   `json:"children,omitempty"`
}

// axValueStr extracts the string representation from a CDP AXValue.
// The underlying Value field is jsontext.Value (a []byte alias); we try to
// unmarshal it as a JSON string first, then fall back to the raw bytes.
func axValueStr(v *cdpaccessibility.Value) string {
	if v == nil || len(v.Value) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal([]byte(v.Value), &s); err == nil {
		return s
	}
	raw := strings.TrimSpace(string(v.Value))
	if raw == "null" {
		return ""
	}
	return raw
}

// axInterestingProps lists the AX property names worth surfacing in output.
var axInterestingProps = map[cdpaccessibility.PropertyName]bool{
	cdpaccessibility.PropertyNameDisabled:        true,
	cdpaccessibility.PropertyNameFocused:         true,
	cdpaccessibility.PropertyNameChecked:         true,
	cdpaccessibility.PropertyNameExpanded:        true,
	cdpaccessibility.PropertyNameSelected:        true,
	cdpaccessibility.PropertyNameRequired:        true,
	cdpaccessibility.PropertyNameInvalid:         true,
	cdpaccessibility.PropertyNameLevel:           true,
	cdpaccessibility.PropertyNameMultiselectable: true,
	cdpaccessibility.PropertyNameHasPopup:        true,
}

// isUninterestingAXRole returns true for structural/anonymous roles that add
// noise without semantic content.
func isUninterestingAXRole(role, name string) bool {
	if name != "" {
		return false
	}
	switch role {
	case "none", "presentation", "generic", "InlineTextBox", "":
		return true
	}
	return false
}

// buildAXTree converts the flat CDP node list into a nested simpleAXNode tree.
func buildAXTree(nodes []*cdpaccessibility.Node, rootSelector string, interestingOnly bool, maxDepth int) []*simpleAXNode {
	byID := make(map[cdpaccessibility.NodeID]*cdpaccessibility.Node, len(nodes))
	for _, n := range nodes {
		byID[n.NodeID] = n
	}

	var rootIDs []cdpaccessibility.NodeID
	if rootSelector != "" {
		// Partial tree: roots are nodes whose parent is absent from the set.
		for _, n := range nodes {
			if _, parentPresent := byID[n.ParentID]; !parentPresent {
				rootIDs = append(rootIDs, n.NodeID)
			}
		}
	} else {
		for _, n := range nodes {
			if n.ParentID == "" {
				rootIDs = append(rootIDs, n.NodeID)
			}
		}
	}

	var result []*simpleAXNode
	for _, id := range rootIDs {
		if n := buildAXNode(byID, id, interestingOnly, 0, maxDepth); n != nil {
			result = append(result, n)
		}
	}
	return result
}

func buildAXNode(byID map[cdpaccessibility.NodeID]*cdpaccessibility.Node, id cdpaccessibility.NodeID, interestingOnly bool, depth, maxDepth int) *simpleAXNode {
	if depth > maxDepth {
		return nil
	}
	n, ok := byID[id]
	if !ok || n.Ignored {
		return nil
	}

	role := axValueStr(n.Role)
	name := axValueStr(n.Name)

	// For uninteresting nodes, skip this node but still recurse into children
	// so that meaningful descendants are not lost. Flatten single-child wrappers.
	if interestingOnly && isUninterestingAXRole(role, name) {
		var children []*simpleAXNode
		for _, childID := range n.ChildIDs {
			if c := buildAXNode(byID, childID, interestingOnly, depth, maxDepth); c != nil {
				children = append(children, c)
			}
		}
		if len(children) == 1 {
			return children[0]
		}
		if len(children) > 1 {
			return &simpleAXNode{Children: children}
		}
		return nil
	}

	out := &simpleAXNode{
		Role:        role,
		Name:        name,
		Value:       axValueStr(n.Value),
		Description: axValueStr(n.Description),
	}
	for _, p := range n.Properties {
		if !axInterestingProps[p.Name] {
			continue
		}
		val := axValueStr(p.Value)
		if val == "" || val == "false" || val == "null" {
			continue
		}
		if out.Properties == nil {
			out.Properties = make(map[string]string)
		}
		out.Properties[string(p.Name)] = val
	}
	for _, childID := range n.ChildIDs {
		if c := buildAXNode(byID, childID, interestingOnly, depth+1, maxDepth); c != nil {
			out.Children = append(out.Children, c)
		}
	}
	return out
}

func newBrowserAccessibilityTool(pool *browserPool, logger *slog.Logger) Tool {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"selector": map[string]any{
				"type":        "string",
				"description": "CSS selector to scope the tree to one element and its subtree. Omit for the full page tree.",
			},
			"max_depth": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"maximum":     50,
				"description": "Maximum tree depth to return (1–50, default 15).",
			},
			"interesting_only": map[string]any{
				"type":        "boolean",
				"description": "Skip structural/anonymous nodes (role=none/generic/presentation/InlineTextBox with no name). Meaningful descendants are still included (default true).",
			},
		},
	}
	return NewTool("browser_accessibility",
		"Return the ARIA accessibility tree of the current page or a subtree rooted at 'selector'. "+
			"Useful for discovering form labels, button names, widget states, and ARIA roles without parsing raw HTML. "+
			"Results are a nested JSON tree of nodes with role, name, value, description, and key properties (disabled, checked, expanded, etc.).",
		schema,
		func(params map[string]any) ToolResult {
			selector := getStringDefault(params, "selector", "")
			maxDepth := max(1, min(50, getInt(params, "max_depth", 15)))
			interestingOnly := getBool(params, "interesting_only", true)

			var nodes []*cdpaccessibility.Node
			if err := pool.runDefault(chromedp.ActionFunc(func(ctx context.Context) error {
				if err := cdpaccessibility.Enable().Do(ctx); err != nil {
					return fmt.Errorf("enable accessibility: %w", err)
				}
				if selector != "" {
					var domNodes []*cdp.Node
					if err := chromedp.Nodes(selector, &domNodes, chromedp.ByQuery).Do(ctx); err != nil {
						return fmt.Errorf("selector %q: %w", selector, err)
					}
					if len(domNodes) == 0 {
						return fmt.Errorf("selector %q: element not found", selector)
					}
					var err error
					nodes, err = cdpaccessibility.GetPartialAXTree().
						WithBackendNodeID(domNodes[0].BackendNodeID).
						WithFetchRelatives(true).
						Do(ctx)
					return err
				}
				var err error
				nodes, err = cdpaccessibility.GetFullAXTree().
					WithDepth(int64(maxDepth)).
					Do(ctx)
				return err
			})); err != nil {
				return NewToolResult("", fmt.Errorf("browser_accessibility: %w", err))
			}

			tree := buildAXTree(nodes, selector, interestingOnly, maxDepth)
			if len(tree) == 0 {
				return NewToolResult("[]", nil)
			}
			out, err := json.MarshalIndent(tree, "", "  ")
			if err != nil {
				return NewToolResult("", fmt.Errorf("browser_accessibility: marshal: %w", err))
			}
			return NewToolResult(string(out), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}
