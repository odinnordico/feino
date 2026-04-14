package chat

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"

	"github.com/odinnordico/feino/internal/i18n"
	"github.com/odinnordico/feino/internal/memory"
)

// renderedMessage holds one conversation turn's display text.
type renderedMessage struct {
	role string
	raw  string // original unrendered source; non-empty only for roles that support re-rendering
	text string // styled/rendered output shown in the viewport
}

// queuedMessage is a user message waiting to be sent once the session is free.
// The display text is already shown in the viewport before enqueuing, so only
// the resolved expanded text (with @path refs expanded) is stored here.
type queuedMessage struct {
	expanded string // text with @path refs resolved (sent to the session)
}

const (
	maxQueueSize       = 10
	maxHistoryMessages = 100
)

// flushPendingChunk renders accumulated streaming text and appends it to the viewport.
func (m Model) flushPendingChunk() Model {
	m.inThought = false

	if m.pendingChunk == "" {
		return m
	}
	raw := m.pendingChunk
	label := m.th.AssistantBubble.Render("FEINO")
	m.messages = append(m.messages, renderedMessage{
		role: "assistant",
		raw:  raw,
		text: label + "\n" + m.renderMarkdown(raw),
	})
	m.pendingChunk = ""
	return m.truncateHistory().refreshViewport().scrollToBottom()
}

// appendUserMessage adds a user message to the view.
func (m Model) appendUserMessage(text string) Model {
	label := m.th.UserBubble.Render("You")
	wrapped := lipgloss.NewStyle().Width(m.vp.Width).Render(text)
	m.messages = append(m.messages, renderedMessage{
		role: "user",
		raw:  text,
		text: label + "\n" + wrapped + "\n",
	})
	return m.truncateHistory().refreshViewport().scrollToBottom()
}

func (m Model) appendErrorText(text string) Model {
	line := m.th.ErrorStyle.Render("⚠ "+text) + "\n"
	m.messages = append(m.messages, renderedMessage{role: "error", text: line})
	return m.truncateHistory().refreshViewport().scrollToBottom()
}

// appendInfoText adds a neutral informational line to the view.
func (m Model) appendInfoText(text string) Model {
	line := m.th.HelpStyle.Render("ⓘ "+text) + "\n"
	m.messages = append(m.messages, renderedMessage{role: "info", text: line})
	return m.truncateHistory().refreshViewport().scrollToBottom()
}

// appendHistory adds session history messages to the view.
func (m Model) appendHistory() Model {
	hist := m.sess.History()
	if len(hist) == 0 {
		return m.appendInfoText(i18n.T("history_empty"))
	}
	for _, msg := range hist {
		role := string(msg.GetRole())
		text := msg.GetTextContent()
		label := m.th.HelpStyle.Render("[" + role + "]")
		m.messages = append(m.messages, renderedMessage{role: role, text: label + " " + text + "\n"})
	}
	return m.truncateHistory().refreshViewport().scrollToBottom()
}

// appendProfile shows the user profile from config and stored agent memories.
func (m Model) appendProfile() Model {
	u := m.cfg.User

	name := u.Name
	if name == "" {
		name = i18n.T("profile_no_name")
	}
	tz := u.Timezone
	if tz == "" {
		tz = i18n.T("profile_no_timezone")
	}
	style := u.CommunicationStyle
	if style == "" {
		style = i18n.T("profile_no_style")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## %s\n\n", i18n.T("profile_title")))
	sb.WriteString(fmt.Sprintf("- **Name:** %s\n", name))
	sb.WriteString(fmt.Sprintf("- **Timezone:** %s\n", tz))
	sb.WriteString(fmt.Sprintf("- **Communication style:** %s\n\n", style))

	sb.WriteString(fmt.Sprintf("## %s\n\n", i18n.T("profile_memories_title")))
	if m.memStore == nil {
		sb.WriteString(i18n.T("profile_memories_empty") + "\n")
	} else {
		entries, err := m.memStore.All()
		if err != nil || len(entries) == 0 {
			sb.WriteString(i18n.T("profile_memories_empty") + "\n")
		} else {
			// Group entries by category in canonical order.
			grouped := make(map[memory.Category][]memory.Entry)
			for _, e := range entries {
				grouped[e.Category] = append(grouped[e.Category], e)
			}
			for _, cat := range memory.AllCategories() {
				catEntries := grouped[cat]
				if len(catEntries) == 0 {
					continue
				}
				// Category names are ASCII; byte-level capitalisation is safe.
				catName := strings.ToUpper(string(cat)[:1]) + string(cat)[1:]
				sb.WriteString(fmt.Sprintf("**%s**\n", catName))
				for _, e := range catEntries {
					sb.WriteString(fmt.Sprintf("- `%s` %s\n", e.ID[:8], e.Content))
				}
				sb.WriteString("\n")
			}
		}
	}
	sb.WriteString("\n_" + i18n.T("profile_memories_hint") + "_\n")

	rendered := m.renderMarkdown(sb.String())
	m.messages = append(m.messages, renderedMessage{role: "system", text: rendered})
	return m.truncateHistory().refreshViewport().scrollToBottom()
}

// appendConfig adds the active config YAML to the view.
func (m Model) appendConfig() Model {
	data, err := yaml.Marshal(m.cfg)
	if err != nil {
		return m.appendErrorText(i18n.Tf("config_marshal_failed", map[string]any{"Error": err.Error()}))
	}
	rendered := m.renderMarkdown("```yaml\n" + string(data) + "```")
	m.messages = append(m.messages, renderedMessage{role: "system", text: rendered})
	return m.truncateHistory().refreshViewport().scrollToBottom()
}

// rerenderMessages rebuilds display text using the current theme and glamour
// renderer. As a pragmatic optimisation we only re-wrap the last 50 messages
// to avoid extreme UI lag on window resize.
func (m Model) rerenderMessages() Model {
	startIndex := 0
	if len(m.messages) > 50 {
		startIndex = len(m.messages) - 50
	}

	for i := startIndex; i < len(m.messages); i++ {
		msg := m.messages[i]
		if msg.raw == "" {
			continue
		}
		switch msg.role {
		case "assistant":
			label := m.th.AssistantBubble.Render("FEINO")
			m.messages[i].text = label + "\n" + m.renderMarkdown(msg.raw)
		case "user":
			label := m.th.UserBubble.Render("You")
			wrapped := lipgloss.NewStyle().Width(m.vp.Width).Render(msg.raw)
			m.messages[i].text = label + "\n" + wrapped + "\n"
		}
	}
	return m.refreshViewport()
}

// refreshViewport rebuilds the viewport content from all stored messages.
func (m Model) refreshViewport() Model {
	var sb strings.Builder
	for _, msg := range m.messages {
		sb.WriteString(msg.text)
	}
	m.renderedContent = sb.String()
	m.vp.SetContent(m.renderedContent)
	return m
}

// scrollToBottom immediately jumps the viewport to the bottom.
func (m Model) scrollToBottom() Model {
	m.vp.GotoBottom()
	return m
}

func (m Model) truncateHistory() Model {
	if len(m.messages) > maxHistoryMessages {
		m.messages = m.messages[len(m.messages)-maxHistoryMessages:]
	}
	return m
}

// renderMarkdown runs the glamour renderer. Falls back to plain text on error.
func (m Model) renderMarkdown(text string) string {
	if m.renderer == nil {
		return text
	}
	out, err := m.renderer.Render(text)
	if err != nil {
		return text
	}
	return out
}

// routeStreamChunk splits incoming text into workspace content and thought
// content by parsing <thought>...</thought> tags. inThought tracks whether the
// previous call ended inside an open thought block (tag may span chunks).
func routeStreamChunk(text string, inThought bool) (workspace, thought string, stillInThought bool) {
	for len(text) > 0 {
		if inThought {
			if idx := strings.Index(text, "</thought>"); idx >= 0 {
				thought += text[:idx]
				text = text[idx+len("</thought>"):]
				inThought = false
			} else if idx := strings.Index(text, ")\n"); idx >= 0 && strings.HasPrefix(thought, "(thinking") {
				thought += text[:idx+1]
				text = text[idx+2:]
				inThought = false
			} else {
				thought += text
				text = ""
			}
		} else {
			if idx := strings.Index(text, "<thought>"); idx >= 0 {
				workspace += text[:idx]
				text = text[idx+len("<thought>"):]
				inThought = true
			} else if idx := strings.Index(text, "(thinking"); idx >= 0 {
				workspace += text[:idx]
				text = text[idx+len("(thinking"):]
				thought = "(thinking"
				inThought = true
			} else {
				workspace += text
				text = ""
			}
		}
	}
	return workspace, thought, inThought
}
