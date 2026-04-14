package tools

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"

	"github.com/gen2brain/beeep"
)

// notifyFunc is the function used to send a desktop notification.
// Replaced in tests to avoid real OS notifications.
var notifyFunc = func(title, message, icon string) error {
	return beeep.Notify(title, message, icon)
}

// alertFunc is the function used to send a modal alert dialog.
// Replaced in tests to avoid real OS dialogs.
var alertFunc = func(title, message, icon string) error {
	return beeep.Alert(title, message, icon)
}

func init() {
	beeep.AppName = "feino"
}

// NewNotifyTools returns the notify tool.
func NewNotifyTools(logger *slog.Logger) []Tool {
	return []Tool{newNotifyTool(logger)}
}

func newNotifyTool(logger *slog.Logger) Tool {
	return NewTool(
		"notify",
		"Send a desktop notification to the user. Use this to signal that a "+
			"long-running task has finished or that something requires attention, "+
			"without the user having to watch the TUI. "+
			"Returns an error message when no display server is available (headless/SSH).",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{
					"type":        "string",
					"description": "Notification title (short, one line).",
				},
				"message": map[string]any{
					"type":        "string",
					"description": "Notification body text.",
				},
				"icon": map[string]any{
					"type":        "string",
					"description": "Absolute path to a PNG/ICO icon file (optional).",
				},
				"alert": map[string]any{
					"type":        "boolean",
					"description": "When true, send a modal alert dialog instead of a passive notification.",
				},
			},
			"required": []string{"title", "message"},
		},
		func(params map[string]any) ToolResult {
			title, ok := getString(params, "title")
			if !ok || title == "" {
				return NewToolResult("", fmt.Errorf("notify: title is required"))
			}
			message, ok := getString(params, "message")
			if !ok || message == "" {
				return NewToolResult("", fmt.Errorf("notify: message is required"))
			}
			icon := getStringDefault(params, "icon", "")
			isAlert := getBool(params, "alert", false)

			if !hasDisplay() {
				return NewToolResult(
					"notify: no display server available (headless or SSH session) — notification not sent",
					nil,
				)
			}

			var err error
			if isAlert {
				err = alertFunc(title, message, icon)
			} else {
				err = notifyFunc(title, message, icon)
			}

			if err != nil {
				if errors.Is(err, beeep.ErrUnsupported) {
					return NewToolResult(
						fmt.Sprintf("notify: desktop notifications are not supported on this platform (%s)", platformName()),
						nil,
					)
				}
				return NewToolResult(fmt.Sprintf("notify: %v", err), nil)
			}

			kind := "notification"
			if isAlert {
				kind = "alert"
			}
			safeLogger(logger).Debug("desktop notification sent", "title", title, "kind", kind)
			return NewToolResult(fmt.Sprintf("%s sent: %q", kind, title), nil)
		},
		WithPermissionLevel(PermLevelRead),
		WithLogger(logger),
	)
}

// hasDisplay reports whether a display server appears to be available.
// On Linux this checks $DISPLAY (X11) and $WAYLAND_DISPLAY.
// On macOS and Windows a display is always assumed present.
// In containers and SSH sessions both variables are typically unset.
func hasDisplay() bool {
	switch platformName() {
	case "linux":
		return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
	default:
		// macOS, Windows — assume display present.
		return true
	}
}

// platformName returns the runtime OS name, extracted to a var so tests can
// stub it without build constraints.
var platformName = func() string {
	return runtime.GOOS
}
