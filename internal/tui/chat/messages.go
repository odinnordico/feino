// Package chat implements the primary interactive chat TUI using Bubble Tea.
package chat

import (
	"github.com/odinnordico/feino/internal/agent"
	"github.com/odinnordico/feino/internal/app"
	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/tokens"
	"github.com/odinnordico/feino/internal/tui/wizard"
)

// SessionEventMsg carries a raw app.Event from sess.Subscribe to the program.
// The subscriber goroutine sends this via prog.Send; Update dispatches it.
type SessionEventMsg struct{ Event app.Event }

// PartReceivedMsg carries a single streaming text chunk.
type PartReceivedMsg struct{ Text string }

// ThoughtReceivedMsg carries a single streaming reasoning/thinking chunk.
type ThoughtReceivedMsg struct{ Text string }

// ToolCallMsg fires when the model invokes a tool.
type ToolCallMsg struct{ Call model.ToolCall }

// CompleteMsg fires when inference has fully finished for a turn.
type CompleteMsg struct{ Text string }

// StateChangedMsg fires on every ReAct state transition.
type StateChangedMsg struct{ State agent.ReActState }

// UsageUpdatedMsg carries fresh token accounting after a turn.
type UsageUpdatedMsg struct{ Meta tokens.UsageMetadata }

// ErrorMsg carries a non-fatal session error to display inline.
type ErrorMsg struct{ Err error }

// SetupRequestedMsg fires when the user types /setup.
type SetupRequestedMsg struct{}

// WizardCompleteMsg is sent after /setup successfully finishes.
type WizardCompleteMsg struct{ Result wizard.WizardResult }

// ThemeToggleMsg fires when Ctrl+T is pressed.
type ThemeToggleMsg struct{}

// PluginsReloadedMsg fires after a successful plugin reload.
// Count is the number of script plugins loaded (excluding native tools).
type PluginsReloadedMsg struct{ Count int }

// PermissionRequestMsg fires when the security gate blocks a tool call and the
// session is asking the user whether to allow it. The session's ReAct goroutine
// blocks until a bool is sent on Response. Send true to approve, false to deny.
type PermissionRequestMsg struct {
	ToolName string
	Required string // human-readable level name, e.g. "write"
	Allowed  string // human-readable level name, e.g. "read"
	Response chan<- bool
}

// YoloRequestedMsg fires when the user types /yolo to trigger the bypass-mode
// duration picker.
type YoloRequestedMsg struct{}

// YoloExpiredMsg fires when a timed bypass window has elapsed and safe mode
// should be restored.
type YoloExpiredMsg struct{}

// LangRequestedMsg fires when the user types /lang to open the language picker.
type LangRequestedMsg struct{}

// ThemeRequestedMsg fires when the user types /theme to open the theme picker.
type ThemeRequestedMsg struct{}

// SetupEmailRequestedMsg fires when the user types /email-setup.
type SetupEmailRequestedMsg struct{}

// EmailSetupCompleteMsg is sent after /email-setup successfully finishes.
type EmailSetupCompleteMsg struct{ Result wizard.EmailSetupResult }
