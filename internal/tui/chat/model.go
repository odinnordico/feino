package chat

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"

	"github.com/odinnordico/feino/internal/agent"
	"github.com/odinnordico/feino/internal/app"
	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/credentials"
	"github.com/odinnordico/feino/internal/i18n"
	"github.com/odinnordico/feino/internal/memory"
	"github.com/odinnordico/feino/internal/tokens"
	"github.com/odinnordico/feino/internal/tui/theme"
)

// Model is the root Bubble Tea model for the feino chat TUI.
type Model struct {
	sess   *app.Session
	cfg    *config.Config
	th     theme.Theme
	prog   *tea.Program // set via SetProgram after construction
	zm     *zone.Manager
	ctx    context.Context
	cancel context.CancelFunc

	// Sub-models.
	vp    viewport.Model
	input inputModel
	spin  spinner.Model

	// Message rendering.
	messages        []renderedMessage
	renderedContent string // all rendered messages joined
	pendingChunk    string // in-flight streaming text
	turnStart       time.Time

	// Layout.
	width  int
	height int

	// State.
	busy       bool
	msgQueue   []queuedMessage // buffered messages waiting for the session to be free
	reactState agent.ReActState
	usage      tokens.UsageMetadata
	inThought  bool              // true while streaming is inside a <thought>...</thought> block
	permPrompt *permissionPrompt // non-nil while awaiting user approval for a tool
	yoloPick   *yoloPicker       // non-nil while the /yolo duration picker is visible
	langPick   *langPicker       // non-nil while the /lang language picker is visible
	themePick  *themePicker      // non-nil while the /theme picker is visible

	// Per-turn metrics shown in the status bar.
	lastLatencyMs        float64
	lastPromptTokens     int
	lastCompletionTokens int

	// Glamour markdown renderer — reused, recreated on resize/theme change.
	renderer *glamour.TermRenderer

	// store is the credential store used for /email-setup.
	// Set via SetStore after construction.
	store credentials.Store

	// memStore is the memory store used for /profile.
	// Set via SetMemoryStore after construction; nil when memory is unavailable.
	memStore memory.Store
}

// New constructs the chat Model. Call SetProgram before starting the tea.Program.
func New(sess *app.Session, cfg *config.Config, th theme.Theme, zm *zone.Manager) *Model {
	sp := spinner.New()
	sp.Style = th.SpinnerStyle
	sp.Spinner = spinner.Dot

	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel is stored in Model.cancel and called on model teardown

	workingDir := cfg.Context.WorkingDir
	if workingDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			workingDir = cwd
		}
	}

	// Use viewport.New so MouseWheelEnabled and MouseWheelDelta are set to
	// their correct defaults (true / 3). A zero-value viewport.Model leaves
	// MouseWheelEnabled=false and silently ignores all wheel events.
	vp := viewport.New(1, 1)

	return &Model{
		sess:       sess,
		cfg:        cfg,
		th:         th,
		zm:         zm,
		ctx:        ctx,
		cancel:     cancel,
		reactState: agent.StateInit,
		vp:         vp,
		input:      newInputModel(th, workingDir),
		spin:       sp,
	}
}

// SetStore wires the credential store used by /email-setup.
func (m *Model) SetStore(store credentials.Store) { m.store = store }

// SetMemoryStore wires in the memory store used for /profile.
func (m *Model) SetMemoryStore(ms memory.Store) { m.memStore = ms }

// SetProgram stores the program reference so the model can send messages from
// within Cmd closures (e.g. the /setup wizard re-entry path).
func (m *Model) SetProgram(prog *tea.Program) { m.prog = prog }

// Init returns the initial Cmd.
func (m Model) Init() tea.Cmd {
	return tea.Batch(tea.SetWindowTitle("FEINO"), m.spin.Tick, m.input.Init())
}

// Update handles all incoming messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m = m.handleResize()
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case SessionEventMsg:
		return m.handleSessionEvent(msg.Event)
	case ThoughtReceivedMsg:
		// Thought content is discarded — the status bar shows the ReAct state.
		return m, nil
	case PartReceivedMsg:
		workspace, _, newInThought := routeStreamChunk(msg.Text, m.inThought)
		m.inThought = newInThought
		if workspace != "" {
			m.pendingChunk += workspace
			m.vp.SetContent(m.renderedContent + m.pendingChunk)
			m.vp.GotoBottom()
		}
		return m, nil
	case ToolCallMsg:
		notification := fmt.Sprintf("\n> ⚙️ **Calling:** `%s`\n\n", msg.Call.Name)
		m.pendingChunk += notification
		m.vp.SetContent(m.renderedContent + m.pendingChunk)
		m.vp.GotoBottom()
		return m, nil
	case CompleteMsg:
		m.busy = false
		m = m.flushPendingChunk()
		m.lastLatencyMs = float64(time.Since(m.turnStart).Milliseconds())
		m.lastPromptTokens = m.usage.PromptTokens
		m.lastCompletionTokens = m.usage.CompletionTokens
		if len(m.msgQueue) > 0 {
			next := m.msgQueue[0]
			m.msgQueue = m.msgQueue[1:]
			mod, sendCmd := m.sendQueued(next)
			return mod, sendCmd
		}
		return m, nil
	case StateChangedMsg:
		m.reactState = msg.State
		return m, nil
	case UsageUpdatedMsg:
		m.usage = msg.Meta
		return m, nil
	case ErrorMsg:
		m.busy = false
		m = m.appendErrorText(msg.Err.Error())
		if n := len(m.msgQueue); n > 0 {
			m.msgQueue = nil
			m = m.appendInfoText(i18n.Tp("msg_queue_discarded", n, map[string]any{"Count": n}))
		}
		return m, nil
	case ThemeToggleMsg:
		m = m.cycleTheme()
		return m, nil
	case SetupRequestedMsg:
		return m.enterSetup()
	case WizardCompleteMsg:
		return m.applyWizardResult(msg.Result)
	case SetupEmailRequestedMsg:
		return m.enterEmailSetup()
	case EmailSetupCompleteMsg:
		return m.applyEmailSetupResult(msg.Result)
	case PluginsReloadedMsg:
		return m.appendInfoText(i18n.Tp("plugins_reloaded", msg.Count, map[string]any{"Count": msg.Count})), nil
	case YoloRequestedMsg:
		m.yoloPick = &yoloPicker{selectedIdx: 0}
		return m, nil
	case YoloExpiredMsg:
		m.sess.ClearBypassMode()
		m = m.appendInfoText(i18n.T("yolo_expired"))
		return m, nil
	case LangRequestedMsg:
		idx := 0
		for i, e := range langEntries {
			if e.code == m.cfg.UI.Language {
				idx = i
				break
			}
		}
		m.langPick = &langPicker{selectedIdx: idx}
		return m, nil
	case ThemeRequestedMsg:
		idx := 0
		for i, e := range themeEntries {
			if e.code == m.cfg.UI.Theme {
				idx = i
				break
			}
		}
		m.themePick = &themePicker{selectedIdx: idx}
		return m, nil
	case PermissionRequestMsg:
		m.permPrompt = &permissionPrompt{
			toolName: msg.ToolName,
			required: msg.Required,
			allowed:  msg.Allowed,
			response: msg.Response,
			choice:   false,
		}
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case tea.QuitMsg:
		m.cancel()
		return m, tea.Quit
	}

	newInput, cmd := m.input.Update(msg)
	m.input = newInput
	return m, cmd
}

// View renders the complete TUI. zone.Scan must wrap the final string.
func (m Model) View() string {
	if m.width == 0 {
		return "Loading…"
	}

	headerPill := renderHeader(m.currentModelName(), m.th)

	separator := lipgloss.NewStyle().
		Foreground(m.th.Muted).
		Render(strings.Repeat("─", m.width))

	innerVPWidth := max(m.width-2, 1)
	innerVPHeight := max(m.mainHeight()-2, 1)

	mainArea := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(m.th.Primary).
		Width(innerVPWidth).
		Height(innerVPHeight).
		Render(m.vp.View())

	var inputRow string
	switch {
	case m.themePick != nil:
		inputRow = m.renderThemePickerRow()
	case m.langPick != nil:
		inputRow = m.renderLangPickerRow()
	case m.yoloPick != nil:
		inputRow = m.renderYoloPickerRow()
	case m.permPrompt != nil:
		inputRow = m.renderPermissionRow()
	default:
		spinView := ""
		if m.busy {
			spinView = m.spin.View()
		}
		inputRow = renderInputRow(m.width, spinView, m.input.View(), m.tokenBudgetPct(), m.th)
	}

	statusBar := renderStatusBar(m.width, StatusBarData{
		State:          m.reactState,
		Usage:          m.usage,
		LatencyMs:      m.lastLatencyMs,
		PromptTurn:     m.lastPromptTokens,
		CompletionTurn: m.lastCompletionTokens,
		BypassActive:   m.sess.IsBypassActive(),
	}, m.th)

	combined := lipgloss.JoinVertical(lipgloss.Left,
		headerPill,
		separator,
		mainArea,
		inputRow,
		statusBar,
	)

	return m.zm.Scan(combined)
}

// ── layout helpers ────────────────────────────────────────────────────────────

// mainHeight is the height available for the workspace panel:
// total − header(1) − separator(1) − inputHeight − statusBar(1).
func (m Model) mainHeight() int {
	return max(m.height-2-inputHeight-1, 1)
}

// tokenBudgetPct returns the percentage of the token budget still available.
// Returns -1 when no budget is known, signalling the UI to hide the indicator.
func (m Model) tokenBudgetPct() int {
	budget := m.cfg.Context.MaxBudget
	if budget <= 0 {
		return -1
	}
	used := m.usage.TotalTokens
	pct := 100 - (used*100)/budget
	return max(min(pct, 100), 0)
}

// rebuildRenderer recreates the glamour markdown renderer using the current
// theme style and viewport width. Must be called after theme or layout changes.
func (m Model) rebuildRenderer() Model {
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(m.th.GlamourStyle()),
		glamour.WithWordWrap(m.vp.Width),
	)
	if err == nil {
		m.renderer = r
	} else {
		m.renderer = nil
	}
	return m
}

// handleResize returns the updated Model so Bubble Tea stores the new viewport
// dimensions. Returning only tea.Cmd silently discards mutations on value receivers.
func (m Model) handleResize() Model {
	mainH := m.mainHeight()
	m.vp.Width = max(m.width-4, 1)
	m.vp.Height = max(mainH-2, 1)

	pctW := 0
	if m.tokenBudgetPct() >= 0 {
		pctW = 8
	}
	m.input = m.input.SetWidth(max(m.width-3-pctW, 1))

	return m.rebuildRenderer().rerenderMessages()
}

// cycleTheme advances through the theme rotation: neo → dark → light → auto → neo.
func (m Model) cycleTheme() Model {
	switch m.cfg.UI.Theme {
	case "neo":
		m.cfg.UI.Theme = "dark"
	case "dark":
		m.cfg.UI.Theme = "light"
	case "light":
		m.cfg.UI.Theme = "auto"
	default: // "auto" and any unrecognised value reset to "neo"
		m.cfg.UI.Theme = "neo"
	}
	m.th = theme.FromConfig(m.cfg.UI.Theme)
	m.input = m.input.SetTheme(m.th)
	m.spin.Style = m.th.SpinnerStyle
	return m.rebuildRenderer().rerenderMessages()
}

// currentModelName returns the default model of the first configured provider.
func (m Model) currentModelName() string {
	p := m.cfg.Providers
	switch {
	case p.Anthropic.APIKey != "" && p.Anthropic.DefaultModel != "":
		return p.Anthropic.DefaultModel
	case p.OpenAI.APIKey != "" && p.OpenAI.DefaultModel != "":
		return p.OpenAI.DefaultModel
	case p.Gemini.APIKey != "" && p.Gemini.DefaultModel != "":
		return p.Gemini.DefaultModel
	case p.Ollama.DefaultModel != "":
		return p.Ollama.DefaultModel
	default:
		return "no model"
	}
}
