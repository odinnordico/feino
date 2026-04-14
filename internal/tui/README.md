# Package `internal/tui`

The `tui` package implements FEINO's terminal user interface using the [Bubble Tea](https://github.com/charmbracelet/bubbletea) framework. It provides a full-featured TUI with a split-pane chat view, streaming token display, thinking pane, status bar, and an interactive setup wizard.

---

## Entry point

```go
// Run starts the TUI. It blocks until the user quits.
// cfg must have at least one provider configured; if not, the wizard runs automatically.
err := tui.Run(ctx, cfg)
```

`Run` redirects `slog` to `~/.feino/feino.log` before starting Bubble Tea so that log output does not corrupt the terminal.

---

## Sub-packages

| Package | Responsibility |
|---------|---------------|
| `chat/` | Root Bubble Tea model; streaming display, input bar, status bar, theme cycling |
| `wizard/` | Sequential `huh.Form` setup wizard (provider creds, model, preferences, email) |
| `theme/` | Catppuccin colour palettes, lipgloss style composition, dark/light/auto detection |

---

## Layout

```
┌─ FEINO ──────── claude-opus-4-7 ─────────────────────────┐
├──────────────────────────────────────────────────────────┤
│                                    │                     │
│         WORKSPACE (~70%)           │   THINKING (~30%)   │
│         (viewport, scrollable)     │   (state label)     │
│                                    │                     │
├──────────────────────────────────────────────────────────┤
│  >> [spinner] [input field]                       100%   │
├──────────────────────────────────────────────────────────┤
│  state | Latency: 45ms | Turn: 1200p/800c | Tokens: 2000 │
└──────────────────────────────────────────────────────────┘
```

---

## chat/ sub-package

The `chat.Model` is the root Bubble Tea model. It:

- Drives `app.Session` via `sess.Send` and `sess.Subscribe`
- Renders streaming tokens in the workspace viewport via `glamour` markdown
- Shows ReAct state transitions in the thinking pane
- Handles keyboard shortcuts

### Key patterns

**`handleResize` and `cycleTheme` must return `(Model, tea.Cmd)`**, not just `tea.Cmd`. The `Update` method has a value receiver — any helper that only returns `tea.Cmd` silently discards mutations. If `m.vp.Height` is never set, `viewport.View()` returns `""`.

**`View()` must end with `return m.zm.Scan(combined)`** — the bubblezone manager performs a single scan at the root; never scan partial fragments.

**Streaming** — `EventPartReceived` calls `m.vp.SetContent(m.renderedContent + m.pendingChunk)` + `m.vp.GotoBottom()` on every chunk for real-time display. `flushPendingChunk` is called on `EventComplete` to glamour-render the full response and store it in `m.messages`.

**`@path` expansion** — `@filename` tokens in submitted text are expanded to `<file path="...">content</file>` before being sent to the session; the UI shows the original text.

**Glamour renderer** — created once in `handleResize` and on `cycleTheme`; never per-message to avoid repeated initialisation cost.

### Keyboard shortcuts

| Key | Action |
|-----|--------|
| `Ctrl+C` / `Esc` | Cancel in-flight turn (first press) or quit (second press) |
| `Ctrl+T` | Cycle theme (dark → light → auto) |
| `Ctrl+R` | Reset session (clear history) |
| `/setup` in input | Re-enter the setup wizard |
| `Enter` | Send message |
| `Shift+Enter` | Newline in input |
| Arrow keys / PgUp/PgDn | Scroll workspace viewport |

---

## wizard/ sub-package

The wizard runs automatically on first launch when no credentials are configured, and can be re-entered via `/setup`.

### Wizard steps

1. **Provider select** — choose one or more providers to configure
2. **Credentials** — enter API keys (stored in `credentials.Store`)
3. **Model select** — live model list fetch with 5s timeout; falls back to text input
4. **Working directory** — defaults to `$PWD`
5. **Theme** — dark / light / auto
6. **Confirmation** — review and confirm

```go
result, err := wizard.Run(ctx, existingConfig)
if errors.Is(err, wizard.ErrAborted) {
    // user cancelled
}
cfg := result.ToConfig()
```

### Re-entry from TUI

```go
// Exit alt screen → blocking wizard.Run in a tea.Cmd closure → re-enter alt screen
tea.ExitAltScreen,
func() tea.Msg { result, _ := wizard.Run(ctx, cfg); return wizardDoneMsg{result} },
tea.EnterAltScreen,
```

---

## theme/ sub-package

Themes are Catppuccin palettes (Mocha for dark, Latte for light) composed into lipgloss styles.

```go
theme := theme.FromConfig("dark")    // explicit
theme  = theme.FromConfig("auto")    // auto-detect via lipgloss.HasDarkBackground()

// Access styles:
style := theme.Primary           // lipgloss.Style
style  = theme.UserInputStyle
style  = theme.AssistantStyle
style  = theme.ThinkingStyle
```

---

## Best practices

- **Never call `sess.Send` from inside `Update`.** Use a `tea.Cmd` to run it in a goroutine. `Update` runs on the main event loop and must not block.
- **Emit `prog.Send(chat.SessionEventMsg{Event: e})` from the `sess.Subscribe` handler.** This is the only way to feed async session events back into the Bubble Tea loop.
- **Wrap all `sess.Subscribe` callbacks** to recover from panics — a panic in a subscriber crashes the background goroutine and leaves the TUI in a broken state.
- **Test `handleResize` in isolation** before adding new layout calculations. Wrong height math causes the viewport to disappear silently.

---

## Extending

### Adding a new keyboard shortcut

1. Add a `case` in the `Update` function's `tea.KeyMsg` handler in `chat/model.go`.
2. Update the help text in the status bar or a `/help` command.

### Adding a new theme

1. Add a `Palette` constant in `theme/theme.go` using Catppuccin or a custom colour set.
2. Add a `case` in `theme.FromConfig` for the new theme name.
3. Update the wizard's theme step to include the new option.

### Adding a new pane

1. Add a `viewport.Model` field to `chat.Model`.
2. Initialize it in `handleResize` with the correct height/width.
3. Call `pane.SetContent(...)` on relevant events.
4. Include `pane.View()` in the `View()` assembly string.
5. Remember: `handleResize` must return `(Model, tea.Cmd)`.
