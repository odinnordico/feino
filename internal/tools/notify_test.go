package tools

import (
	"errors"
	"strings"
	"testing"

	"github.com/gen2brain/beeep"
)

// capturedNotification records a single notifyFunc / alertFunc call.
type capturedNotification struct {
	title, message, icon string
}

// withMockNotify installs mock notifyFunc and alertFunc for the duration of the
// test, then restores originals. Returns slices that accumulate calls.
func withMockNotify(t *testing.T) (notifs, alerts *[]capturedNotification) {
	t.Helper()
	origNotify := notifyFunc
	origAlert := alertFunc
	origPlatform := platformName

	ns := &[]capturedNotification{}
	as := &[]capturedNotification{}

	notifyFunc = func(title, message, icon string) error {
		*ns = append(*ns, capturedNotification{title, message, icon})
		return nil
	}
	alertFunc = func(title, message, icon string) error {
		*as = append(*as, capturedNotification{title, message, icon})
		return nil
	}
	// Pretend we always have a display so tests aren't skipped.
	platformName = func() string { return "linux" }
	t.Setenv("DISPLAY", ":0")

	t.Cleanup(func() {
		notifyFunc = origNotify
		alertFunc = origAlert
		platformName = origPlatform
	})
	return ns, as
}

// ── happy path ────────────────────────────────────────────────────────────────

func TestNotifyTool_SendsNotification(t *testing.T) {
	notifs, _ := withMockNotify(t)
	tool := newNotifyTool(nil)

	res := tool.Run(map[string]any{"title": "Done", "message": "Task finished"})
	if res.GetError() != nil {
		t.Fatalf("unexpected error: %v", res.GetError())
	}

	content, _ := res.GetContent().(string)
	if !strings.Contains(content, "notification sent") {
		t.Errorf("expected success message, got %q", content)
	}
	if len(*notifs) != 1 {
		t.Fatalf("want 1 notification sent, got %d", len(*notifs))
	}
	if (*notifs)[0].title != "Done" {
		t.Errorf("title: want %q, got %q", "Done", (*notifs)[0].title)
	}
	if (*notifs)[0].message != "Task finished" {
		t.Errorf("message: want %q, got %q", "Task finished", (*notifs)[0].message)
	}
}

func TestNotifyTool_Alert_UsesAlertFunc(t *testing.T) {
	_, alerts := withMockNotify(t)
	tool := newNotifyTool(nil)

	res := tool.Run(map[string]any{"title": "Warning", "message": "Check this", "alert": true})
	if res.GetError() != nil {
		t.Fatalf("unexpected error: %v", res.GetError())
	}

	content, _ := res.GetContent().(string)
	if !strings.Contains(content, "alert sent") {
		t.Errorf("expected 'alert sent', got %q", content)
	}
	if len(*alerts) != 1 {
		t.Fatalf("want 1 alert sent, got %d", len(*alerts))
	}
}

func TestNotifyTool_WithIcon(t *testing.T) {
	notifs, _ := withMockNotify(t)
	tool := newNotifyTool(nil)

	tool.Run(map[string]any{
		"title":   "Done",
		"message": "Finished",
		"icon":    "/path/to/icon.png",
	})

	if len(*notifs) != 1 {
		t.Fatalf("want 1 notification, got %d", len(*notifs))
	}
	if (*notifs)[0].icon != "/path/to/icon.png" {
		t.Errorf("icon: want %q, got %q", "/path/to/icon.png", (*notifs)[0].icon)
	}
}

// ── validation ────────────────────────────────────────────────────────────────

func TestNotifyTool_MissingTitle(t *testing.T) {
	withMockNotify(t)
	tool := newNotifyTool(nil)

	res := tool.Run(map[string]any{"message": "body"})
	if err := res.GetError(); err == nil || !strings.Contains(err.Error(), "title is required") {
		t.Errorf("expected title error, got %v", err)
	}
}

func TestNotifyTool_EmptyTitle(t *testing.T) {
	withMockNotify(t)
	tool := newNotifyTool(nil)

	res := tool.Run(map[string]any{"title": "", "message": "body"})
	if err := res.GetError(); err == nil || !strings.Contains(err.Error(), "title is required") {
		t.Errorf("expected title error, got %v", err)
	}
}

func TestNotifyTool_MissingMessage(t *testing.T) {
	withMockNotify(t)
	tool := newNotifyTool(nil)

	res := tool.Run(map[string]any{"title": "T"})
	if err := res.GetError(); err == nil || !strings.Contains(err.Error(), "message is required") {
		t.Errorf("expected message error, got %v", err)
	}
}

// ── headless / no display ─────────────────────────────────────────────────────

func TestNotifyTool_NoDisplay_Linux(t *testing.T) {
	origPlatform := platformName
	platformName = func() string { return "linux" }
	t.Cleanup(func() { platformName = origPlatform })

	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	tool := newNotifyTool(nil)
	res := tool.Run(map[string]any{"title": "T", "message": "M"})
	content, _ := res.GetContent().(string)
	if !strings.Contains(content, "no display") {
		t.Errorf("expected no-display message, got %q", content)
	}
}

func TestNotifyTool_WaylandDisplay(t *testing.T) {
	notifs, _ := withMockNotify(t)

	origPlatform := platformName
	platformName = func() string { return "linux" }
	t.Cleanup(func() { platformName = origPlatform })

	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")

	tool := newNotifyTool(nil)
	res := tool.Run(map[string]any{"title": "T", "message": "M"})
	if res.GetError() != nil {
		t.Fatalf("unexpected error: %v", res.GetError())
	}
	if len(*notifs) != 1 {
		t.Errorf("expected notification to be sent via Wayland display, got %d", len(*notifs))
	}
}

func TestNotifyTool_MacOS_NoDisplayCheck(t *testing.T) {
	notifs, _ := withMockNotify(t)

	origPlatform := platformName
	platformName = func() string { return "darwin" }
	t.Cleanup(func() { platformName = origPlatform })

	// Even with DISPLAY unset, macOS always sends.
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	tool := newNotifyTool(nil)
	tool.Run(map[string]any{"title": "T", "message": "M"})
	if len(*notifs) != 1 {
		t.Errorf("expected notification sent on macOS without display check, got %d", len(*notifs))
	}
}

// ── ErrUnsupported ────────────────────────────────────────────────────────────

func TestNotifyTool_UnsupportedPlatform(t *testing.T) {
	origNotify := notifyFunc
	origPlatform := platformName
	platformName = func() string { return "plan9" }
	notifyFunc = func(_, _, _ string) error { return beeep.ErrUnsupported }
	t.Cleanup(func() {
		notifyFunc = origNotify
		platformName = origPlatform
	})

	tool := newNotifyTool(nil)
	res := tool.Run(map[string]any{"title": "T", "message": "M"})
	content, _ := res.GetContent().(string)
	if !strings.Contains(content, "not supported") {
		t.Errorf("expected unsupported message, got %q", content)
	}
}

// ── backend error ─────────────────────────────────────────────────────────────

func TestNotifyTool_BackendError(t *testing.T) {
	origNotify := notifyFunc
	origPlatform := platformName
	platformName = func() string { return "linux" }
	notifyFunc = func(_, _, _ string) error { return errors.New("dbus: connection refused") }
	t.Setenv("DISPLAY", ":0")
	t.Cleanup(func() {
		notifyFunc = origNotify
		platformName = origPlatform
	})

	tool := newNotifyTool(nil)
	res := tool.Run(map[string]any{"title": "T", "message": "M"})
	content, _ := res.GetContent().(string)
	if !strings.Contains(content, "dbus: connection refused") {
		t.Errorf("expected backend error text, got %q", content)
	}
}

// ── permission level ──────────────────────────────────────────────────────────

func TestNotifyTool_PermissionLevel(t *testing.T) {
	tool := newNotifyTool(nil)
	c, ok := tool.(Classified)
	if !ok {
		t.Fatal("notify tool does not implement Classified")
	}
	if c.PermissionLevel() != PermLevelRead {
		t.Errorf("want PermLevelRead (%d), got %d", PermLevelRead, c.PermissionLevel())
	}
}

// ── registration ──────────────────────────────────────────────────────────────

func TestNewNativeTools_IncludesNotify(t *testing.T) {
	tools := NewNativeTools(nil)
	for _, tool := range tools {
		if tool.GetName() == "notify" {
			return
		}
	}
	t.Error("notify not found in NewNativeTools output")
}
