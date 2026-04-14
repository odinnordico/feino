package chat

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/odinnordico/feino/internal/agent"
	"github.com/odinnordico/feino/internal/app"
	"github.com/odinnordico/feino/internal/i18n"
	"github.com/odinnordico/feino/internal/model"
	"github.com/odinnordico/feino/internal/tokens"
)

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Overlay pickers consume all keys while active.
	if m.yoloPick != nil {
		idx, enter, esc := pickerNav(msg.String(), m.yoloPick.selectedIdx, len(yoloDurations))
		m.yoloPick.selectedIdx = idx
		switch {
		case enter:
			return m.activateYolo(idx)
		case esc:
			m.yoloPick = nil
			m = m.appendInfoText(i18n.T("yolo_cancelled"))
		}
		return m, nil
	}

	if m.langPick != nil {
		idx, enter, esc := pickerNav(msg.String(), m.langPick.selectedIdx, len(langEntries))
		m.langPick.selectedIdx = idx
		switch {
		case enter:
			return m.activateLang(idx)
		case esc:
			m.langPick = nil
			m = m.appendInfoText(i18n.T("lang_cancelled"))
		}
		return m, nil
	}

	if m.themePick != nil {
		idx, enter, esc := pickerNav(msg.String(), m.themePick.selectedIdx, len(themeEntries))
		m.themePick.selectedIdx = idx
		switch {
		case enter:
			return m.activateTheme(idx)
		case esc:
			m.themePick = nil
			m = m.appendInfoText(i18n.T("theme_cancelled"))
		}
		return m, nil
	}

	// While a permission prompt is active only navigation and confirmation keys
	// are meaningful; all others are blocked to protect the pending approval flow.
	if m.permPrompt != nil {
		switch msg.String() {
		case "left", "right", "tab":
			m.permPrompt.choice = !m.permPrompt.choice
		case "enter":
			return m.resolvePermission(m.permPrompt.choice)
		case "esc":
			return m.resolvePermission(false)
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c":
		if m.busy {
			m.sess.Cancel()
			return m, nil
		}
		m.cancel()
		return m, tea.Quit

	case "esc":
		if m.busy {
			m.sess.Cancel()
		}
		return m, nil

	case "ctrl+t":
		return m, func() tea.Msg { return ThemeToggleMsg{} }

	case "enter":
		if m.input.ShowCompletions() {
			isSlash := m.input.Kind() == "slash"
			m.input = m.input.AcceptCompletion()
			if isSlash {
				return m.submitInput()
			}
			return m, nil
		}
		return m.submitInput()

	case "ctrl+j":
		m.input = m.input.InsertNewline()
		return m, nil

	case "pgup", "pgdown":
		if !m.input.showComplete {
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		}

	case "up", "down":
		if m.input.Value() == "" && !m.input.showComplete {
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		}
	}

	newInput, cmd := m.input.Update(msg)
	m.input = newInput
	return m, cmd
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown,
		tea.MouseButtonWheelLeft, tea.MouseButtonWheelRight:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) submitInput() (tea.Model, tea.Cmd) {
	m.input = m.input.AcceptCompletion()
	val := strings.TrimSpace(m.input.Value())
	if val == "" {
		return m, nil
	}
	m.input = m.input.Clear()

	slog.Info("user message submitted", "text", val)

	switch val {
	case "/setup":
		return m, func() tea.Msg { return SetupRequestedMsg{} }
	case "/email-setup":
		return m, func() tea.Msg { return SetupEmailRequestedMsg{} }
	case "/yolo":
		return m, func() tea.Msg { return YoloRequestedMsg{} }
	case "/lang":
		return m, func() tea.Msg { return LangRequestedMsg{} }
	case "/theme":
		return m, func() tea.Msg { return ThemeRequestedMsg{} }
	case "/profile":
		return m.appendProfile(), nil
	case "/reset":
		m.sess.Reset()
		m.messages = nil
		m.renderedContent = ""
		m.msgQueue = nil
		m.vp.SetContent("")
		return m, nil
	case "/history":
		return m.appendHistory(), nil
	case "/clear":
		m.messages = nil
		m.renderedContent = ""
		m.vp.SetContent("")
		return m, nil
	case "/config":
		return m.appendConfig(), nil
	case "/reload-plugins":
		return m, m.reloadPluginsCmd()
	case "/quit", "/exit":
		m.cancel()
		return m, tea.Quit
	}

	expanded := m.expandAtRefs(val)

	if m.busy {
		if len(m.msgQueue) >= maxQueueSize {
			return m.appendErrorText(i18n.Tf("msg_queue_full", map[string]any{"Max": maxQueueSize})), nil
		}
		m = m.appendUserMessage(val)
		m.msgQueue = append(m.msgQueue, queuedMessage{expanded: expanded})
		return m.appendInfoText(i18n.Tf("msg_queued", map[string]any{"Current": len(m.msgQueue), "Max": maxQueueSize})), nil
	}

	m = m.appendUserMessage(val)
	m, sendCmd := m.sendQueued(queuedMessage{expanded: expanded})
	return m, sendCmd
}

// sendQueued marks the model busy and dispatches the message to the session.
// The user message must already be in the viewport before calling this.
// Returns the updated Model (with busy=true) so callers can pass it to Bubble Tea.
func (m Model) sendQueued(msg queuedMessage) (Model, tea.Cmd) {
	m.busy = true
	m.turnStart = time.Now()
	sendCtx := m.ctx

	return m, tea.Batch(
		m.spin.Tick,
		func() tea.Msg {
			if err := m.sess.Send(sendCtx, msg.expanded); err != nil {
				if errors.Is(err, app.ErrBusy) {
					return ErrorMsg{Err: fmt.Errorf("session busy")}
				}
				return ErrorMsg{Err: err}
			}
			return nil
		},
	)
}

// handleSessionEvent dispatches an app.Event to the correct typed Msg.
func (m Model) handleSessionEvent(e app.Event) (tea.Model, tea.Cmd) {
	switch e.Kind {
	case app.EventPartReceived:
		if part, ok := e.Payload.(model.MessagePart); ok {
			switch c := part.GetContent().(type) {
			case string:
				if _, ok := part.(*model.ThoughtPart); ok {
					return m, func() tea.Msg { return ThoughtReceivedMsg{Text: c} }
				}
				return m, func() tea.Msg { return PartReceivedMsg{Text: c} }
			case model.ToolCall:
				return m, func() tea.Msg { return ToolCallMsg{Call: c} }
			}
		}
	case app.EventStateChanged:
		if state, ok := e.Payload.(agent.ReActState); ok {
			return m, func() tea.Msg { return StateChangedMsg{State: state} }
		}
	case app.EventUsageUpdated:
		if meta, ok := e.Payload.(tokens.UsageMetadata); ok {
			return m, func() tea.Msg { return UsageUpdatedMsg{Meta: meta} }
		}
	case app.EventComplete:
		if msg, ok := e.Payload.(model.Message); ok {
			return m, func() tea.Msg { return CompleteMsg{Text: msg.GetTextContent()} }
		}
	case app.EventError:
		if err, ok := e.Payload.(error); ok {
			return m, func() tea.Msg { return ErrorMsg{Err: err} }
		}
	}
	return m, nil
}
