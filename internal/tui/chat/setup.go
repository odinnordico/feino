package chat

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/odinnordico/feino/internal/config"
	"github.com/odinnordico/feino/internal/i18n"
	"github.com/odinnordico/feino/internal/tui/theme"
	"github.com/odinnordico/feino/internal/tui/wizard"
)

func (m Model) enterEmailSetup() (tea.Model, tea.Cmd) {
	currentCfg := m.cfg
	store := m.store
	ctx := m.ctx
	return m, tea.Sequence(
		tea.ExitAltScreen,
		func() tea.Msg {
			res, err := wizard.RunEmailSetup(ctx, currentCfg.Services.Email)
			if err != nil {
				return ErrorMsg{Err: err}
			}
			// Save credentials immediately — the store is safe to call from any goroutine.
			if store != nil {
				if saveErr := wizard.SaveEmailSetup(&res, currentCfg, store); saveErr != nil {
					return ErrorMsg{Err: saveErr}
				}
			}
			return EmailSetupCompleteMsg{Result: res}
		},
		tea.EnterAltScreen,
	)
}

func (m Model) applyEmailSetupResult(res wizard.EmailSetupResult) (tea.Model, tea.Cmd) {
	// Merge the updated services config (non-sensitive settings) into m.cfg.
	m.cfg.Services.Email = config.EmailServiceConfig{
		Enabled:  new(true),
		Address:  res.Address,
		IMAPHost: res.IMAPHost,
		IMAPPort: res.IMAPPort,
		SMTPHost: res.SMTPHost,
		SMTPPort: res.SMTPPort,
	}

	if cfgPath, err := config.DefaultConfigPath(); err == nil {
		if saveErr := config.Save(cfgPath, m.cfg); saveErr != nil {
			return m.appendErrorText(i18n.Tf("email_setup_error", map[string]any{"Error": saveErr.Error()})), nil
		}
	}

	return m.appendInfoText(i18n.T("email_setup_saved")), nil
}

// reloadPluginsCmd returns a tea.Cmd that calls sess.ReloadPlugins off the
// main goroutine and delivers the result as a PluginsReloadedMsg or ErrorMsg.
func (m Model) reloadPluginsCmd() tea.Cmd {
	return func() tea.Msg {
		count, err := m.sess.ReloadPlugins()
		if err != nil {
			return ErrorMsg{Err: fmt.Errorf("reload plugins: %w", err)}
		}
		return PluginsReloadedMsg{Count: count}
	}
}

func (m Model) enterSetup() (tea.Model, tea.Cmd) {
	currentCfg := m.cfg
	ctx := m.ctx
	return m, tea.Sequence(
		tea.ExitAltScreen,
		func() tea.Msg {
			res, err := wizard.Run(ctx, currentCfg)
			if err != nil {
				return ErrorMsg{Err: err}
			}
			return WizardCompleteMsg{Result: res}
		},
		tea.EnterAltScreen,
	)
}

func (m Model) applyWizardResult(res wizard.Result) (tea.Model, tea.Cmd) {
	newCfg := res.ToConfig()
	if err := m.sess.UpdateConfig(newCfg); err != nil {
		return m.appendErrorText(i18n.Tf("config_apply_failed", map[string]any{"Error": err.Error()})), nil
	}
	merged := m.sess.Config()
	m.cfg = merged
	m.th = theme.FromConfig(merged.UI.Theme)
	m.input = m.input.SetTheme(m.th)
	m.input = m.input.SetWorkingDir(merged.Context.WorkingDir)

	if cfgPath, err := config.DefaultConfigPath(); err == nil {
		if saveErr := config.Save(cfgPath, merged); saveErr != nil {
			return m.appendErrorText(i18n.Tf("config_save_failed", map[string]any{"Error": saveErr.Error()})), nil
		}
	}

	m = m.appendInfoText(i18n.T("config_updated"))
	return m, m.reloadPluginsCmd()
}
